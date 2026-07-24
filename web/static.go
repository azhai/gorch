package web

import (
	"io/fs"
	"net/http"
	"path"
	"strings"
	"sync"

	"github.com/azhai/gorch/webui"
	"github.com/labstack/echo/v4"
)

// webuiDist is the embedded filesystem for the web UI.
// It contains the built dist/ directory from webui/.
var webuiDist = webui.DistFS

var (
	strippedFS fs.FS
	fsOnce     sync.Once
)

// getFileSystem returns the stripped filesystem (dist/ only).
// It uses sync.Once to ensure the filesystem is only created once.
func getFileSystem() fs.FS {
	fsOnce.Do(func() {
		sub, err := fs.Sub(webuiDist, "dist")
		if err != nil {
			panic("failed to create sub filesystem: " + err.Error())
		}
		strippedFS = sub
	})
	return strippedFS
}

// staticAssetHandler serves files from the assets/ directory.
// Route: /assets/*
func staticAssetHandler(c echo.Context) error {
	filePath := c.Param("*")
	if filePath == "" {
		return c.NoContent(http.StatusNotFound)
	}

	cleaned := path.Clean(filePath)
	if strings.Contains(cleaned, "..") {
		return c.NoContent(http.StatusForbidden)
	}

	fsys := getFileSystem()
	data, err := fs.ReadFile(fsys, "assets/"+cleaned)
	if err != nil {
		return c.NoContent(http.StatusNotFound)
	}

	ext := path.Ext(cleaned)
	return c.Blob(http.StatusOK, mimeTypeForExt(ext), data)
}

// spaFallbackHandler serves the SPA and static files.
// Registered as GET "/*" on the server. Returns 404 for unmatched API
// paths, tries embedded static files, then falls back to index.html.
func (s *Server) spaFallbackHandler(c echo.Context) error {
	reqPath := c.Request().URL.Path
	cleaned := path.Clean(reqPath)
	if strings.Contains(cleaned, "..") {
		return c.NoContent(http.StatusForbidden)
	}

	// Refuse API paths that didn't match a group route
	if s.isAPIPath(cleaned) {
		return c.NoContent(http.StatusNotFound)
	}

	// Try to serve a static file (logo.svg, favicon.svg, etc.)
	if cleaned != "/" && cleaned != "" {
		fileName := strings.TrimPrefix(cleaned, "/")
		if s.urlPrefix != "" {
			fileName = strings.TrimPrefix(fileName, s.urlPrefix+"/")
		}
		fsys := getFileSystem()
		data, err := fs.ReadFile(fsys, fileName)
		if err == nil {
			ext := path.Ext(fileName)
			return c.Blob(http.StatusOK, mimeTypeForExt(ext), data)
		}
	}

	// Fallback to pre-rendered index.html (with __URL_PREFIX__ injected)
	return c.Blob(http.StatusOK, "text/html; charset=utf-8", s.indexHTML)
}

// isAPIPath reports whether a request path targets the API.
// Handles both root mount ("/api/...") and prefixed mount ("/gorch/api/...").
func (s *Server) isAPIPath(p string) bool {
	apiSeg := "/api"
	if s.urlPrefix != "" {
		apiSeg = "/" + s.urlPrefix + "/api"
	}
	return p == apiSeg || strings.HasPrefix(p, apiSeg+"/")
}

// mimeTypeForExt returns the MIME type for a file extension.
func mimeTypeForExt(ext string) string {
	switch ext {
	case ".html":
		return "text/html; charset=utf-8"
	case ".css":
		return "text/css"
	case ".js":
		return "application/javascript"
	case ".json":
		return "application/json"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".svg":
		return "image/svg+xml"
	case ".ico":
		return "image/x-icon"
	case ".woff":
		return "font/woff"
	case ".woff2":
		return "font/woff2"
	case ".ttf":
		return "font/ttf"
	default:
		return "application/octet-stream"
	}
}
