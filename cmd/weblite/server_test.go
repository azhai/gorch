package main

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/labstack/echo/v4"
)

// ── Options.validate ──────────────────────────────────────

func TestOptions_Validate_PortRange(t *testing.T) {
	tests := []struct {
		name    string
		port    int
		wantErr bool
	}{
		{"port 0", 0, true},
		{"port -1", -1, true},
		{"port 1", 1, false},
		{"port 8080", 8080, false},
		{"port 65535", 65535, false},
		{"port 65536", 65536, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := &Options{Port: tt.port, Dir: t.TempDir()}
			err := o.validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestOptions_Validate_Directory(t *testing.T) {
	tmpDir := t.TempDir()

	tests := []struct {
		name    string
		dir     string
		wantErr bool
	}{
		{"existing dir", tmpDir, false},
		{"non-existent dir", "/tmp/nonexistent_weblite_test_dir", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := &Options{Port: 8000, Dir: tt.dir}
			err := o.validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestOptions_Validate_FileInsteadOfDir(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "testfile.txt")
	os.WriteFile(filePath, []byte("hello"), 0644)

	o := &Options{Port: 8000, Dir: filePath}
	err := o.validate()
	if err == nil {
		t.Error("validate() should error for file path")
	}
}

// ── isPathWithinRoot ──────────────────────────────────────

func TestIsPathWithinRoot(t *testing.T) {
	tmpDir := t.TempDir()

	// Create test subdirs so EvalSymlinks can resolve them
	os.MkdirAll(filepath.Join(tmpDir, "sub"), 0755)
	os.MkdirAll(filepath.Join(tmpDir, "a", "b", "c"), 0755)

	tests := []struct {
		name   string
		target string
		want   bool
	}{
		{"same dir", tmpDir, true},
		{"subdir", filepath.Join(tmpDir, "sub"), true},
		{"deep subdir", filepath.Join(tmpDir, "a", "b", "c"), true},
		{"parent dir", filepath.Dir(tmpDir), false},
		{"dot-dot escape", tmpDir + "/../" + filepath.Base(filepath.Dir(tmpDir)), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isPathWithinRoot(tmpDir, tt.target)
			if got != tt.want {
				t.Errorf("isPathWithinRoot(%q, %q) = %v, want %v",
					tmpDir, tt.target, got, tt.want)
			}
		})
	}
}

// ── formatSize ────────────────────────────────────────────

func TestFormatSize(t *testing.T) {
	tests := []struct {
		b    int64
		want string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1023, "1023 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1048576, "1.0 MB"},
		{1572864, "1.5 MB"},
		{1073741824, "1.0 GB"},
		{1610612736, "1.5 GB"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := formatSize(tt.b)
			if got != tt.want {
				t.Errorf("formatSize(%d) = %q, want %q", tt.b, got, tt.want)
			}
		})
	}
}

// ── HTTP handler integration tests ───────────────────────

func setupTestServer(dir string) *echo.Echo {
	opts := &Options{
		Port: 0,
		Dir:  dir,
		Bind: "127.0.0.1",
	}
	srv := NewServer(opts)
	return srv.app
}

func doRequest(e *echo.Echo, method, target string) *http.Response {
	req := httptest.NewRequest(method, target, nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec.Result()
}

func readBody(resp *http.Response) string {
	body, _ := io.ReadAll(resp.Body)
	defer resp.Body.Close()
	return string(body)
}

func TestHandler_ServeFile(t *testing.T) {
	tmpDir := t.TempDir()
	content := []byte("<html><body>hello</body></html>")
	os.WriteFile(filepath.Join(tmpDir, "index.html"), content, 0644)

	app := setupTestServer(tmpDir)

	resp := doRequest(app, http.MethodGet, "/index.html")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	body := readBody(resp)
	if !bytes.Equal([]byte(body), content) {
		t.Errorf("body mismatch: got %q, want %q", body, content)
	}

	ct := resp.Header.Get("Content-Type")
	if ct != "text/html; charset=utf-8" {
		t.Errorf("Content-Type = %q, want text/html", ct)
	}
}

func TestHandler_ServeFile_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	app := setupTestServer(tmpDir)

	resp := doRequest(app, http.MethodGet, "/missing.txt")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestHandler_ServeDirectoryListing(t *testing.T) {
	tmpDir := t.TempDir()
	os.MkdirAll(filepath.Join(tmpDir, "subdir"), 0755)
	os.WriteFile(filepath.Join(tmpDir, "file.txt"), []byte("data"), 0644)

	app := setupTestServer(tmpDir)

	resp := doRequest(app, http.MethodGet, "/")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", resp.StatusCode, readBody(resp))
	}

	body := readBody(resp)
	if len(body) == 0 {
		t.Fatal("empty response body for directory listing")
	}

	// Should contain listed entries
	if !bytes.Contains([]byte(body), []byte("file.txt")) {
		t.Errorf("directory listing missing file.txt, body:\n%s", body)
	}
	if !bytes.Contains([]byte(body), []byte("subdir/")) {
		t.Errorf("directory listing missing subdir/, body:\n%s", body)
	}
}

func TestHandler_ServeDirectoryWithIndexHtml(t *testing.T) {
	tmpDir := t.TempDir()
	indexContent := []byte("<html>index page</html>")
	os.WriteFile(filepath.Join(tmpDir, "index.html"), indexContent, 0644)

	app := setupTestServer(tmpDir)

	resp := doRequest(app, http.MethodGet, "/")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	body := readBody(resp)
	if !bytes.Equal([]byte(body), indexContent) {
		t.Errorf("should serve index.html, got %q", body)
	}
}

func TestHandler_PathTraversalBlocked(t *testing.T) {
	tmpDir := t.TempDir()
	secretContent := []byte("secret data")

	parentDir := filepath.Dir(tmpDir)
	secretFile := filepath.Join(parentDir, "secret_file.txt")
	os.WriteFile(secretFile, secretContent, 0644)

	app := setupTestServer(tmpDir)

	// Try to escape via .. in URL
	resp := doRequest(app, http.MethodGet, "/../secret_file.txt")
	if resp.StatusCode != http.StatusForbidden && resp.StatusCode != http.StatusNotFound {
		t.Errorf("path traversal should be blocked, got status %d", resp.StatusCode)
	}
}

func TestHandler_RootPath(t *testing.T) {
	tmpDir := t.TempDir()
	app := setupTestServer(tmpDir)

	resp := doRequest(app, http.MethodGet, "/")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for root path, got %d", resp.StatusCode)
	}
}

func TestHandler_NestedPath(t *testing.T) {
	tmpDir := t.TempDir()
	nestedDir := filepath.Join(tmpDir, "a", "b")
	os.MkdirAll(nestedDir, 0755)
	nestedContent := []byte("nested file")
	os.WriteFile(filepath.Join(nestedDir, "deep.txt"), nestedContent, 0644)

	app := setupTestServer(tmpDir)

	resp := doRequest(app, http.MethodGet, "/a/b/deep.txt")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	body := readBody(resp)
	if !bytes.Equal([]byte(body), nestedContent) {
		t.Errorf("got %q, want %q", body, nestedContent)
	}
}

func TestFormatSize_EdgeCases(t *testing.T) {
	// Exactly at boundaries
	tests := []struct {
		b    int64
		want string
	}{
		{1024, "1.0 KB"},
		{1048576, "1.0 MB"},
		{1073741824, "1.0 GB"},
		{1099511627776, "1024.0 GB"}, // 1TB
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := formatSize(tt.b)
			if got != tt.want {
				t.Errorf("formatSize(%d) = %q, want %q", tt.b, got, tt.want)
			}
		})
	}
}
