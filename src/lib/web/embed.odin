package web

// Compile-time copy of web/ so an installed or relocated binary still serves the
// SPA. The disk directory wins when present, which keeps frontend edits live.
//
// #load_directory is NOT recursive, so every sub-directory under web/ is listed
// explicitly. Adding a directory there means adding one line here - otherwise
// its files silently fall back to index.html.

import "core:fmt"

@(private)
E_ROOT := #load_directory("../../../web")
@(private)
E_CSS := #load_directory("../../../web/css")
@(private)
E_JS := #load_directory("../../../web/js")
@(private)
E_VIEWS := #load_directory("../../../web/js/views")
@(private)
E_VENDOR := #load_directory("../../../web/vendor")

// embedded_assets keys assets by request path, e.g. "/js/views/config.js".
@(private)
embedded_assets :: proc() -> map[string][]byte {
	assets := make(map[string][]byte)
	for f in E_ROOT {
		if skip_name(f.name) {
			continue
		}
		assets[fmt.tprintf("/{}", f.name)] = f.data
	}
	for f in E_CSS {
		if skip_name(f.name) {
			continue
		}
		assets[fmt.tprintf("/css/{}", f.name)] = f.data
	}
	for f in E_JS {
		if skip_name(f.name) {
			continue
		}
		assets[fmt.tprintf("/js/{}", f.name)] = f.data
	}
	for f in E_VIEWS {
		if skip_name(f.name) {
			continue
		}
		assets[fmt.tprintf("/js/views/{}", f.name)] = f.data
	}
	for f in E_VENDOR {
		if skip_name(f.name) {
			continue
		}
		assets[fmt.tprintf("/vendor/{}", f.name)] = f.data
	}
	return assets
}

// skip_name drops empty names and dotfiles such as the .DS_Store that macOS
// leaves in every directory.
@(private)
skip_name :: proc(name: string) -> bool {
	return len(name) == 0 || name[0] == '.'
}
