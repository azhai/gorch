package web

// Compile-time copy of web/, compressed.
//
// The SPA is about 236 KB of HTML, JS and CSS. Dropped into the binary as-is
// that is a sixth of the executable; gzipped it is 50 KB, so `make assets`
// compresses web/ into assets.bin and writes the table that indexes it. The
// blob is expanded once, when the server starts without a web/ directory, and
// the result lives for the rest of the process - it is the asset, not a copy
// of one, so it is never freed.
//
// A web/ directory next to the binary or the config still wins over this copy,
// which keeps frontend edits live without rebuilding.

import "core:bytes"
import "core:compress/zlib"
import "core:fmt"

// embedded_assets keys assets by request path, e.g. "/js/views/config.js".
embedded_assets :: proc() -> map[string][]byte {
	buf: bytes.Buffer
	bytes.buffer_init_allocator(&buf, 0, ASSET_SIZE, context.allocator)
	err := zlib.inflate_from_byte_array(
		ASSET_BLOB,
		&buf,
		raw = true,
		size_unpacked = ASSET_SIZE,
	)
	if err != nil {
		// A damaged blob is a build error, not a runtime one: say so once and
		// let the caller serve nothing rather than crashing on a nil map.
		fmt.eprintfln("embedded frontend is damaged: {}", err)
		bytes.buffer_destroy(&buf)
		return {}
	}
	// The buffer is not destroyed: the map holds slices into it, and those
	// slices are the assets for as long as the process runs.
	all := bytes.buffer_to_bytes(&buf)
	assets := make(map[string][]byte, len(ASSETS))
	for entry in ASSETS {
		assets[entry.path] = all[entry.offset:][:entry.size]
	}
	return assets
}
