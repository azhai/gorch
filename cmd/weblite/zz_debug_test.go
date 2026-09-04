package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestZZDebugParam(t *testing.T) {
	dir := t.TempDir()
	idx := filepath.Join(dir, "index.html")
	os.WriteFile(idx, []byte("hello"), 0644)
	opts := &Options{Port: 0, Dir: dir, Bind: "127.0.0.1"}
	srv := NewServer(opts)

	// replicate serveFile's path resolution
	relPath := "index.html"
	localPath := filepath.Join(srv.rootDir, filepath.Clean(relPath))
	fi, err := os.Stat(localPath)
	t.Logf("localPath=%q err=%v isDir=%v", localPath, err, fi != nil && fi.IsDir())

	req := httptest.NewRequest(http.MethodGet, "/index.html", nil)
	rec := httptest.NewRecorder()
	srv.app.ServeHTTP(rec, req)
	t.Logf("status=%d loc=%q", rec.Code, rec.Header().Get("Location"))
	_ = fmt.Sprint
}
