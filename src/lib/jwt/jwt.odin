package jwt

import "core:fmt"
import "core:strings"
import "core:time"
import "core:mem"
import "core:crypto/hmac"
import "core:encoding/base64"

Claims :: struct {
	sub: string,
	iat: i64,
	exp: i64,
}

sb :: proc(s: string) -> []byte { return mem.byte_slice(raw_data(s), len(s)) }

b64url_encode :: proc(data: []byte) -> string {
	s, _ := base64.encode(data, base64.ENC_URL_TABLE, context.allocator, {.No_Padding})
	return s
}

b64url_decode :: proc(s: string) -> ([]byte, bool) {
	b, err := base64.decode(s, base64.DEC_URL_TABLE, nil, context.allocator, {.No_Padding})
	if err != nil {
		return nil, false
	}
	return b, true
}

json_escape :: proc(s: string) -> string {
	b := strings.builder_make()
	for i in 0..<len(s) {
		c := s[i]
		if c == '"' {
			strings.write_string(&b, "\\\"")
		} else if c == '\\' {
			strings.write_string(&b, "\\\\")
		} else {
			strings.write_byte(&b, c)
		}
	}
	return strings.to_string(b)
}

// sign produces the base64url(header).base64url(payload) signature bytes.
hmac_sign :: proc(signing_input, secret: []byte) -> [32]u8 {
	dst: [32]u8
	hmac.sum(.SHA256, dst[:], signing_input, secret)
	return dst
}

// generate returns a signed HS256 JWT for sub with a 24h expiry.
generate :: proc(sub, secret: string) -> string {
	now := time.to_unix_seconds(time.now())
	exp := now + 24 * 3600
	header := `{"alg":"HS256","typ":"JWT"}`
	// Build the payload manually: fmt's `{}` placeholders would collide with the
	// literal braces of the JSON object.
	pb := strings.builder_make()
	strings.write_string(&pb, `{"sub":"`)
	strings.write_string(&pb, json_escape(sub))
	strings.write_string(&pb, `","iat":`)
	strings.write_string(&pb, fmt.tprintf("{}", now))
	strings.write_string(&pb, `,"exp":`)
	strings.write_string(&pb, fmt.tprintf("{}", exp))
	strings.write_string(&pb, "}")
	payload := strings.to_string(pb)
	h := b64url_encode(sb(header))
	p := b64url_encode(sb(payload))
	strings.builder_destroy(&pb)
	si := strings.builder_make()
	strings.write_string(&si, h)
	strings.write_string(&si, ".")
	strings.write_string(&si, p)
	si_str := strings.to_string(si)
	sig := hmac_sign(sb(si_str), sb(secret))
	result := fmt.tprintf("{}.{}", si_str, b64url_encode(sig[:]))
	strings.builder_destroy(&si)
	return result
}

// parse_exp reads the "exp" integer from a decoded JSON payload.
extract_int :: proc(json, key: string) -> (i64, bool) {
	needle := fmt.tprintf("\"{}\":", key)
	idx := strings.index(json, needle)
	if idx < 0 {
		return 0, false
	}
	i := idx + len(needle)
	for i < len(json) && (json[i] == ' ' || json[i] == '\t') {
		i += 1
	}
	start := i
	for i < len(json) && json[i] >= '0' && json[i] <= '9' {
		i += 1
	}
	if start == i {
		return 0, false
	}
	v := i64(0)
	for j in start..<i {
		v = v * 10 + i64(json[j]-'0')
	}
	return v, true
}

extract_str :: proc(json, key: string) -> (string, bool) {
	needle := fmt.tprintf("\"{}\":\"", key)
	idx := strings.index(json, needle)
	if idx < 0 {
		return "", false
	}
	i := idx + len(needle)
	start := i
	for i < len(json) && json[i] != '"' {
		i += 1
	}
	return json[start:i], true
}

// validate checks the signature and expiry, returning the claims on success.
validate :: proc(token, secret: string) -> (Claims, bool) {
	parts, _ := strings.split(token, ".")
	defer delete(parts)
	if len(parts) != 3 {
		return Claims{}, false
	}
	signing_input := fmt.tprintf("{}.{}", parts[0], parts[1])
	sig_bytes, ok := b64url_decode(parts[2])
	if !ok {
		return Claims{}, false
	}
	want := hmac_sign(sb(signing_input), sb(secret))
	if len(sig_bytes) != len(want) {
		return Claims{}, false
	}
	mismatch := false
	for i in 0..<len(want) {
		if want[i] != sig_bytes[i] {
			mismatch = true
			break
		}
	}
	if mismatch {
		return Claims{}, false
	}

	payload_bytes, ok2 := b64url_decode(parts[1])
	if !ok2 {
		return Claims{}, false
	}
	payload := string(payload_bytes)
	exp, ok3 := extract_int(payload, "exp")
	if !ok3 {
		return Claims{}, false
	}
	sub, _ := extract_str(payload, "sub")
	now := time.to_unix_seconds(time.now())
	if exp <= now {
		return Claims{}, false
	}
	iat, _ := extract_int(payload, "iat")
	return Claims{sub = sub, iat = iat, exp = exp}, true
}
