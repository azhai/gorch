package web

// Compile-time copy of web/, compressed.
//
// The SPA is about 190 KB of HTML, JS and CSS. Dropped into the binary as-is
// that is a sixth of the executable; raw-deflated it is 50 KB, so `make assets`
// compresses web/ into assets.bin and writes the table that indexes it. The
// blob is expanded once, when the server starts without a web/ directory, and
// the result lives for the rest of the process - it is the asset, not a copy
// of one, so it is never freed.
//
// A web/ directory next to the binary or the config still wins over this copy,
// which keeps frontend edits live without rebuilding.

import "core:compress/deflate"
import "core:fmt"

// embedded_assets keys assets by request path, e.g. "/js/views/config.js".
embedded_assets :: proc() -> map[string][]byte {
	all: [dynamic]u8
	reserve(&all, ASSET_SIZE)
	err := deflate.inflate_raw(&all, ASSET_BLOB[:], ASSET_SIZE)
	if err != nil || len(all) != ASSET_SIZE {
		// A damaged blob is a build error, not a runtime one: say so once and
		// let the caller serve nothing rather than panicking on a bad slice.
		fmt.eprintfln(
			"embedded frontend is damaged: {} ({} of {} bytes)",
			err,
			len(all),
			ASSET_SIZE,
		)
		delete(all)
		return {}
	}
	// The backing store is not freed: the map holds slices into it, and those
	// slices are the assets for as long as the process runs.
	assets := make(map[string][]byte, len(ASSETS))
	for entry in ASSETS {
		assets[entry.path] = all[entry.offset:][:entry.size]
	}
	return assets
}
