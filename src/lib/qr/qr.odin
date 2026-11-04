package qr

// A byte-mode QR encoder, which is the whole of what the TOTP enrolment screen
// needs: it draws one short URI for an authenticator app to scan.
//
// Only error-correction level M is tabled, and only versions 1..10. The
// otpauth URI is around 120 bytes and version 8-M holds 152; a longer one means
// the operator's user/issuer strings are unreasonably long, and reporting that
// beats carrying tables for 30 more versions nobody can use here.
//
// Output is a 1-bit greyscale PNG built here: the standard library decodes PNG
// but does not encode it, and a QR image needs no palette, no alpha and no
// compression, so a stored (uncompressed) zlib stream is enough. What the
// browser gets is a data URI, so the client needs no library.

import "base:runtime"
import "core:encoding/base64"
import "core:hash"
import "core:mem"

Error :: enum {
	None,
	Empty,
	Too_Long,
}

// Matrix is the symbol: `dark` is row-major with true for a dark module. The
// quiet zone is not part of it - png() adds that.
Matrix :: struct {
	version: int,
	size:    int,
	mask:    int,
	dark:    []bool,
}

MAX_VERSION :: 10
QUIET_ZONE :: 4

// Total codewords, error-correction codewords and block count per version, at
// level M.
@(rodata)
TOTAL_CODEWORDS := [MAX_VERSION]int{26, 44, 70, 100, 134, 172, 196, 242, 292, 346}

@(rodata)
EC_CODEWORDS := [MAX_VERSION]int{10, 16, 26, 36, 48, 64, 72, 88, 110, 130}

@(rodata)
EC_BLOCKS := [MAX_VERSION]int{1, 1, 1, 2, 2, 4, 4, 4, 5, 5}

// Version 10-M carries 216 data codewords, which bounds every buffer below.
MAX_DATA_CODEWORDS :: 216
MAX_TOTAL_CODEWORDS :: 346
MAX_BLOCKS :: 5
MAX_EC_PER_BLOCK :: 26
MAX_ALIGNMENT :: 3

// encode picks the smallest version that holds `text` and the best mask.
encode :: proc(text: string, allocator := context.allocator) -> (Matrix, Error) {
	return encode_masked(text, -1, allocator)
}

// encode_masked is encode with the mask forced. The tests use it to compare
// against a reference implementation: pinning the mask separates "the codewords
// and their placement are right" from "the penalty scoring picked the same
// mask", and both are worth knowing.
encode_masked :: proc(text: string, mask: int, allocator := context.allocator) -> (Matrix, Error) {
	data := transmute([]byte)text
	if len(data) == 0 {
		return {}, .Empty
	}

	version := 0
	for v in 1 ..= MAX_VERSION {
		if 4 + count_bits(v) + len(data) * 8 <= capacity_bits(v) {
			version = v
			break
		}
	}
	if version == 0 {
		return {}, .Too_Long
	}

	bits: Bits
	bits_put(&bits, 0b0100, 4) // byte mode
	bits_put(&bits, len(data), count_bits(version))
	for b in data {
		bits_put(&bits, int(b), 8)
	}

	capacity := capacity_bits(version)
	if bits.bit_len + 4 <= capacity {
		bits_put(&bits, 0, 4) // terminator
	}
	for bits.bit_len % 8 != 0 {
		bits_put(&bits, 0, 1)
	}
	pad := 0
	for bits.bit_len < capacity {
		bits_put(&bits, pad % 2 == 0 ? 0xEC : 0x11, 8)
		pad += 1
	}

	size := version * 4 + 17
	m := Matrix {
		version = version,
		size    = size,
		dark    = make([]bool, size * size, allocator),
	}
	reserved := make([]bool, len(m.dark), allocator)
	defer delete(reserved, allocator)

	setup_function_patterns(&m, reserved)
	codewords := make_codewords(bits.buf[:capacity / 8], version)
	place_data(&m, reserved, codewords[:])

	chosen := mask
	if chosen < 0 {
		chosen = best_mask(m.dark, reserved, size, allocator)
	}
	apply_mask(m.dark, reserved, size, chosen)
	m.mask = chosen
	write_format(m.dark, nil, size, chosen)

	return m, .None
}

destroy :: proc(m: ^Matrix, allocator := context.allocator) {
	if m.dark != nil {
		delete(m.dark, allocator)
		m.dark = nil
	}
}

// ---------------------------------------------------------------------------
// Capacity
// ---------------------------------------------------------------------------

capacity_bits :: proc(version: int) -> int {
	return (TOTAL_CODEWORDS[version - 1] - EC_CODEWORDS[version - 1]) * 8
}

// The character count is 8 bits up to version 9 and 16 from version 10 on.
count_bits :: proc(version: int) -> int {
	return version <= 9 ? 8 : 16
}

// ---------------------------------------------------------------------------
// Bit stream
// ---------------------------------------------------------------------------

Bits :: struct {
	buf:     [MAX_DATA_CODEWORDS]u8,
	bit_len: int,
}

bits_put :: proc(b: ^Bits, value, count: int) {
	for i := count - 1; i >= 0; i -= 1 {
		if (value >> uint(i)) & 1 == 1 {
			b.buf[b.bit_len / 8] |= u8(1) << uint(7 - b.bit_len % 8)
		}
		b.bit_len += 1
	}
}

// ---------------------------------------------------------------------------
// Reed-Solomon over GF(256), primitive polynomial 0x11D
// ---------------------------------------------------------------------------

GF :: struct {
	exp: [512]u8,
	log: [256]u8,
}

gf_init :: proc() -> GF {
	g: GF
	x := 1
	for i in 0 ..< 255 {
		g.exp[i] = u8(x)
		g.log[x] = u8(i)
		x <<= 1
		if x & 0x100 != 0 {
			x ~= 0x11D
		}
	}
	for i in 255 ..< 512 {
		g.exp[i] = g.exp[i - 255]
	}
	return g
}

gf_mul :: #force_inline proc "contextless" (g: ^GF, a, b: u8) -> u8 {
	if a == 0 || b == 0 {
		return 0
	}
	return g.exp[int(g.log[a]) + int(g.log[b])]
}

// generator_poly builds prod (x - a^i) for i in 0..<degree, highest power
// first. The leading coefficient is 1 and is not stored.
generator_poly :: proc(g: ^GF, degree: int) -> [MAX_EC_PER_BLOCK]u8 {
	out: [MAX_EC_PER_BLOCK]u8
	out[degree - 1] = 1
	root: u8 = 1
	for _ in 0 ..< degree {
		for j in 0 ..< degree {
			out[j] = gf_mul(g, out[j], root)
			if j + 1 < degree {
				out[j] ~= out[j + 1]
			}
		}
		root = gf_mul(g, root, 2)
	}
	return out
}

rs_remainder :: proc(g: ^GF, data: []u8, gen: [MAX_EC_PER_BLOCK]u8, degree: int) -> [MAX_EC_PER_BLOCK]u8 {
	rem: [MAX_EC_PER_BLOCK]u8
	for b in data {
		factor := b ~ rem[0]
		for i in 0 ..< degree - 1 {
			rem[i] = rem[i + 1]
		}
		rem[degree - 1] = 0
		for i in 0 ..< degree {
			rem[i] ~= gf_mul(g, gen[i], factor)
		}
	}
	return rem
}

// make_codewords splits the data into blocks, appends each block's error
// correction, and interleaves the result the way the standard lays out a symbol.
make_codewords :: proc(data: []u8, version: int) -> [MAX_TOTAL_CODEWORDS]u8 {
	total := TOTAL_CODEWORDS[version - 1]
	ec_total := EC_CODEWORDS[version - 1]
	blocks := EC_BLOCKS[version - 1]
	data_total := total - ec_total

	// The remainder of the division is how many blocks carry one extra byte.
	blocks_group2 := total % blocks
	data_short := data_total / blocks
	ec_count := total / blocks - data_short

	gf := gf_init()
	gen := generator_poly(&gf, ec_count)

	data_start: [MAX_BLOCKS]int
	data_len: [MAX_BLOCKS]int
	ec: [MAX_BLOCKS][MAX_EC_PER_BLOCK]u8

	offset := 0
	for b in 0 ..< blocks {
		n := b < blocks - blocks_group2 ? data_short : data_short + 1
		data_start[b] = offset
		data_len[b] = n
		ec[b] = rs_remainder(&gf, data[offset:offset + n], gen, ec_count)
		offset += n
	}

	out: [MAX_TOTAL_CODEWORDS]u8
	n := 0
	for i in 0 ..< data_short + 1 {
		for b in 0 ..< blocks {
			if i < data_len[b] {
				out[n] = data[data_start[b] + i]
				n += 1
			}
		}
	}
	for i in 0 ..< ec_count {
		for b in 0 ..< blocks {
			out[n] = ec[b][i]
			n += 1
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Symbol layout
// ---------------------------------------------------------------------------

setup_function_patterns :: proc(m: ^Matrix, reserved: []bool) {
	size := m.size

	// Finder patterns plus their separators: three 8x8 corners.
	corners := [3][2]int{{0, 0}, {0, size - 7}, {size - 7, 0}}
	for corner in corners {
		for r := -1; r <= 7; r += 1 {
			row := corner[0] + r
			if row < 0 || row >= size {
				continue
			}
			for c := -1; c <= 7; c += 1 {
				col := corner[1] + c
				if col < 0 || col >= size {
					continue
				}
				dark :=
					(r >= 0 && r <= 6 && (c == 0 || c == 6)) ||
					(c >= 0 && c <= 6 && (r == 0 || r == 6)) ||
					(r >= 2 && r <= 4 && c >= 2 && c <= 4)
				set_module(m.dark, reserved, size, row, col, dark)
			}
		}
	}

	// Timing patterns.
	for r in 8 ..< size - 8 {
		set_module(m.dark, reserved, size, r, 6, r % 2 == 0)
		set_module(m.dark, reserved, size, 6, r, r % 2 == 0)
	}

	// Alignment patterns, everywhere except the three finder corners.
	coords, count := alignment_coords(m.version)
	for i in 0 ..< count {
		for j in 0 ..< count {
			if (i == 0 && j == 0) || (i == 0 && j == count - 1) || (i == count - 1 && j == 0) {
				continue
			}
			row := coords[i]
			col := coords[j]
			for r := -2; r <= 2; r += 1 {
				for c := -2; c <= 2; c += 1 {
					dark := r == -2 || r == 2 || c == -2 || c == 2 || (r == 0 && c == 0)
					set_module(m.dark, reserved, size, row + r, col + c, dark)
				}
			}
		}
	}

	// Format information is written twice and reserved, so neither the data
	// placement nor a mask touches it. The values are placeholders until the
	// mask is known.
	write_format(m.dark, reserved, size, 0)

	if m.version >= 7 {
		bits := version_bits(m.version)
		for i in 0 ..< 18 {
			row := i / 3
			col := i % 3 + size - 11
			mod := (bits >> uint(i)) & 1 == 1
			set_module(m.dark, reserved, size, row, col, mod)
			set_module(m.dark, reserved, size, col, row, mod)
		}
	}
}

set_module :: #force_inline proc "contextless" (dark, reserved: []bool, size, row, col: int, value: bool) {
	index := row * size + col
	dark[index] = value
	if reserved != nil {
		reserved[index] = true
	}
}

alignment_coords :: proc(version: int) -> (coords: [MAX_ALIGNMENT]int, count: int) {
	if version == 1 {
		return coords, 0
	}
	count = version / 7 + 2
	size := version * 4 + 17
	step := ((size - 13) + (2 * count - 2) - 1) / (2 * count - 2) * 2

	coords[count - 1] = size - 7
	for i := count - 2; i >= 1; i -= 1 {
		coords[i] = coords[i + 1] - step
	}
	coords[0] = 6
	return coords, count
}

place_data :: proc(m: ^Matrix, reserved: []bool, codewords: []u8) {
	size := m.size
	inc := -1
	row := size - 1
	bit_index := 7
	byte_index := 0

	col := size - 1
	for col > 0 {
		if col == 6 {
			col -= 1
		}
		for {
			for c in 0 ..< 2 {
				index := row * size + col - c
				if !reserved[index] {
					dark := false
					if byte_index < len(codewords) {
						dark = (codewords[byte_index] >> uint(bit_index)) & 1 == 1
					}
					m.dark[index] = dark
					bit_index -= 1
					if bit_index == -1 {
						byte_index += 1
						bit_index = 7
					}
				}
			}
			row += inc
			if row < 0 || row >= size {
				row -= inc
				inc = -inc
				break
			}
		}
		col -= 2
	}
}

// ---------------------------------------------------------------------------
// Masks
// ---------------------------------------------------------------------------

apply_mask :: proc(dark, reserved: []bool, size, pattern: int) {
	for col in 0 ..< size {
		for row in 0 ..< size {
			index := row * size + col
			if reserved[index] {
				continue
			}
			if mask_at(pattern, row, col) {
				dark[index] = !dark[index]
			}
		}
	}
}

mask_at :: proc "contextless" (pattern, i, j: int) -> bool {
	switch pattern {
	case 0:
		return (i + j) % 2 == 0
	case 1:
		return i % 2 == 0
	case 2:
		return j % 3 == 0
	case 3:
		return (i + j) % 3 == 0
	case 4:
		return (i / 2 + j / 3) % 2 == 0
	case 5:
		return (i * j) % 2 + (i * j) % 3 == 0
	case 6:
		return ((i * j) % 2 + (i * j) % 3) % 2 == 0
	case 7:
		return ((i * j) % 3 + (i + j) % 2) % 2 == 0
	}
	return false
}

best_mask :: proc(dark, reserved: []bool, size: int, allocator := context.allocator) -> int {
	scratch := make([]bool, len(dark), allocator)
	defer delete(scratch, allocator)
	mem.copy(raw_data(scratch), raw_data(dark), len(dark))

	best := 0
	lowest := max(int)
	for p in 0 ..< 8 {
		apply_mask(scratch, reserved, size, p)
		write_format(scratch, nil, size, p)
		score :=
			penalty_n1(scratch, size) +
			penalty_n2(scratch, size) +
			penalty_n3(scratch, size) +
			penalty_n4(scratch)
		apply_mask(scratch, reserved, size, p) // undo
		if score < lowest {
			lowest = score
			best = p
		}
	}
	return best
}

penalty_n1 :: proc(dark: []bool, size: int) -> int {
	points := 0
	for axis in 0 ..< 2 {
		for line in 0 ..< size {
			run := 1
			for i in 1 ..< size {
				prev := axis == 0 ? dark[line * size + i - 1] : dark[(i - 1) * size + line]
				next := axis == 0 ? dark[line * size + i] : dark[i * size + line]
				if next == prev {
					run += 1
				} else {
					if run >= 5 {
						points += 3 + run - 5
					}
					run = 1
				}
			}
			if run >= 5 {
				points += 3 + run - 5
			}
		}
	}
	return points
}

penalty_n2 :: proc(dark: []bool, size: int) -> int {
	points := 0
	for row in 0 ..< size - 1 {
		for col in 0 ..< size - 1 {
			bits := 0
			if dark[row * size + col] { bits += 1 }
			if dark[row * size + col + 1] { bits += 1 }
			if dark[(row + 1) * size + col] { bits += 1 }
			if dark[(row + 1) * size + col + 1] { bits += 1 }
			if bits == 0 || bits == 4 {
				points += 1
			}
		}
	}
	return points * 3
}

// The finder pattern's 1:1:3:1:1, seen as two 11-bit windows.
penalty_n3 :: proc(dark: []bool, size: int) -> int {
	points := 0
	for axis in 0 ..< 2 {
		for line in 0 ..< size {
			window := 0
			for i in 0 ..< size {
				bit := axis == 0 ? dark[line * size + i] : dark[i * size + line]
				window = ((window << 1) & 0x7FF) | (bit ? 1 : 0)
				if i >= 10 && (window == 0x5D0 || window == 0x05D) {
					points += 1
				}
			}
		}
	}
	return points * 40
}

penalty_n4 :: proc(dark: []bool) -> int {
	count := 0
	for d in dark {
		if d {
			count += 1
		}
	}
	// Deviation from 50% dark, in 5% steps.
	percent := (count * 100 + len(dark) * 5 - 1) / (len(dark) * 5)
	k := percent - 10
	if k < 0 {
		k = -k
	}
	return k * 10
}

// ---------------------------------------------------------------------------
// Format and version information
// ---------------------------------------------------------------------------

write_format :: proc(dark, reserved: []bool, size, mask: int) {
	bits := format_bits(mask)
	for i in 0 ..< 15 {
		mod := (bits >> uint(i)) & 1 == 1

		switch {
		case i < 6:
			set_module(dark, reserved, size, i, 8, mod)
		case i < 8:
			set_module(dark, reserved, size, i + 1, 8, mod)
		case:
			set_module(dark, reserved, size, size - 15 + i, 8, mod)
		}

		switch {
		case i < 8:
			set_module(dark, reserved, size, 8, size - i - 1, mod)
		case i < 9:
			set_module(dark, reserved, size, 8, 7, mod)
		case:
			set_module(dark, reserved, size, 8, 14 - i, mod)
		}
	}
	set_module(dark, reserved, size, size - 8, 8, true) // always dark
}

// format_bits is the (15,5) BCH code of the error level (M is 0b00) and mask,
// then XORed with 0x5412 so no combination encodes as all zeroes.
format_bits :: proc(mask: int) -> int {
	data := mask
	rem := data << 10
	for bch_digit(rem) >= bch_digit(0x537) {
		rem ~= 0x537 << uint(bch_digit(rem) - bch_digit(0x537))
	}
	return ((data << 10) | rem) ~ 0x5412
}

version_bits :: proc(version: int) -> int {
	rem := version << 12
	for bch_digit(rem) >= bch_digit(0x1F25) {
		rem ~= 0x1F25 << uint(bch_digit(rem) - bch_digit(0x1F25))
	}
	return (version << 12) | rem
}

bch_digit :: #force_inline proc "contextless" (value: int) -> int {
	digit := 0
	v := value
	for v != 0 {
		digit += 1
		v >>= 1
	}
	return digit
}

// ---------------------------------------------------------------------------
// PNG
// ---------------------------------------------------------------------------

// png renders the symbol with a four-module quiet zone, `scale` pixels per
// module, as a 1-bit greyscale PNG.
png :: proc(m: Matrix, scale := 6, allocator := context.allocator) -> []u8 {
	modules := m.size + QUIET_ZONE * 2
	width := modules * scale
	height := width
	row_bytes := (width + 7) / 8

	// One filter byte (0 = none) in front of every packed row.
	raw := make([]u8, (row_bytes + 1) * height, allocator)
	defer delete(raw, allocator)

	for y in 0 ..< height {
		row := y * (row_bytes + 1)
		raw[row] = 0
		for x in 0 ..< width {
			mx := x / scale - QUIET_ZONE
			my := y / scale - QUIET_ZONE
			dark := false
			if mx >= 0 && my >= 0 && mx < m.size && my < m.size {
				dark = m.dark[my * m.size + mx]
			}
			if !dark {
				raw[row + 1 + x / 8] |= u8(0x80) >> uint(x % 8)
			}
		}
	}

	z := zlib_stored(raw, allocator)
	defer delete(z, allocator)

	out := make([dynamic]u8, 0, 64 + len(z), allocator)
	append(&out, 0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A)

	ihdr := [13]u8 {
		u8(width >> 24), u8(width >> 16), u8(width >> 8), u8(width),
		u8(height >> 24), u8(height >> 16), u8(height >> 8), u8(height),
		1, // bit depth
		0, // greyscale
		0, // deflate
		0, // adaptive filtering
		0, // no interlace
	}
	png_chunk(&out, "IHDR", ihdr[:])
	png_chunk(&out, "IDAT", z)
	png_chunk(&out, "IEND", nil)
	return out[:]
}

// data_uri wraps png() the way the SPA consumes it.
data_uri :: proc(m: Matrix, scale := 6, allocator := context.allocator) -> string {
	image := png(m, scale, allocator)
	defer delete(image, allocator)
	encoded, _ := base64.encode(image, allocator = allocator)
	defer delete(encoded, allocator)

	prefix := "data:image/png;base64,"
	out := make([]u8, len(prefix) + len(encoded), allocator)
	mem.copy(raw_data(out), raw_data(prefix), len(prefix))
	mem.copy(raw_data(out[len(prefix):]), raw_data(encoded), len(encoded))
	return string(out)
}

// zlib_stored writes a zlib stream made of stored (uncompressed) deflate
// blocks. Compression would buy nothing here - a QR symbol is close to random -
// and a stored stream needs no Huffman coder.
zlib_stored :: proc(raw: []u8, allocator: runtime.Allocator) -> []u8 {
	out := make([dynamic]u8, 0, len(raw) + 16, allocator)

	append(&out, 0x78, 0x01) // deflate, 32K window, no preset dictionary
	offset := 0
	for {
		n := min(65535, len(raw) - offset)
		final := offset + n == len(raw)
		inv := ~n
		append(&out, final ? 1 : 0) // BFINAL | BTYPE=00
		append(&out, u8(n), u8(n >> 8), u8(inv), u8(inv >> 8))
		append(&out, ..raw[offset:offset + n])
		offset += n
		if final {
			break
		}
	}
	sum := hash.adler32(raw)
	append(&out, u8(sum >> 24), u8(sum >> 16), u8(sum >> 8), u8(sum))

	return out[:]
}

png_chunk :: proc(out: ^[dynamic]u8, kind: string, data: []u8) {
	put_u32be(out, u32(len(data)))
	start := len(out)
	for c in kind {
		append(out, u8(c))
	}
	append(out, ..data)
	put_u32be(out, hash.crc32(out^[start:]))
}

put_u32be :: proc(out: ^[dynamic]u8, value: u32) {
	append(out, u8(value >> 24), u8(value >> 16), u8(value >> 8), u8(value))
}
