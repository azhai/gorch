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

// spaFallbackHandler serves the SPA.
// It tries to serve the requested file, then falls back to index.html.
// Route: /*
func spaFallbackHandler(c echo.Context) error {
	// Use c.Request().URL.Path to get the actual request path, not the route pattern
	reqPath := c.Request().URL.Path
	cleaned := path.Clean(reqPath)
	if strings.Contains(cleaned, "..") {
		return c.NoContent(http.StatusForbidden)
	}

	// Try to serve the requested file directly (for favicon.svg, logo.svg, etc.)
	if cleaned != "/" && cleaned != "" {
		fileName := strings.TrimPrefix(cleaned, "/")
		fsys := getFileSystem()
		data, err := fs.ReadFile(fsys, fileName)
		if err == nil {
			ext := path.Ext(fileName)
			return c.Blob(http.StatusOK, mimeTypeForExt(ext), data)
		}
	}

	// Fallback to index.html for SPA routing
	fsys := getFileSystem()
	data, err := fs.ReadFile(fsys, "index.html")
	if err != nil {
		return c.NoContent(http.StatusInternalServerError)
	}

	c.Response().Header().Set("Content-Type", "text/html; charset=utf-8")
	return c.Blob(http.StatusOK, "text/html; charset=utf-8", data)
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
