package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewConfig(t *testing.T) {
	cfg := New()

	if cfg == nil {
		t.Fatal("New() returned nil")
	}
	if cfg.Services == nil {
		t.Error("New().Services should not be nil")
	}
	if len(cfg.Services) != 0 {
		t.Errorf("New().Services should be empty, got %d items", len(cfg.Services))
	}
	if cfg.Web.WEB_ADDR != "127.0.0.1:8080" {
		t.Errorf("default WEB_ADDR = %q, want 127.0.0.1:8080", cfg.Web.WEB_ADDR)
	}
}

func TestSave_NewFile(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "sub", "dir", "config.toml")

	cfg := New()
	if err := cfg.Save(path); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("saved file not found: %v", err)
	}

	content := string(data)
	if len(content) == 0 {
		t.Error("saved file is empty")
	}
	// Should contain default web addr
	if !strings.Contains(content, "127.0.0.1:8080") {
		t.Errorf("saved file missing default WEB_ADDR, content:\n%s", content)
	}
}

func TestSave_WithServices(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "config.toml")

	cfg := New()
	cfg.Services["web"] = ServiceConfig{
		EXEC_CMD:       "./bin/web",
		RESTART_POLICY: string(RestartAlways),
		BACK_OFF:       5,
		WORK_DIR:       "/opt/app",
		ENV_VARS: map[string]string{
			"PORT": "8080",
		},
	}

	if err := cfg.Save(path); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	// Reload and verify
	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error after Save = %v", err)
	}

	svc, ok := loaded.Services["web"]
	if !ok {
		t.Fatal("reloaded config missing service 'web'")
	}
	if svc.EXEC_CMD != "./bin/web" {
		t.Errorf("EXEC_CMD = %q, want ./bin/web", svc.EXEC_CMD)
	}
	if svc.RESTART_POLICY != "always" {
		t.Errorf("RESTART_POLICY = %q, want always", svc.RESTART_POLICY)
	}
	if svc.BACK_OFF != 5 {
		t.Errorf("BACK_OFF = %d, want 5", svc.BACK_OFF)
	}
	if svc.WORK_DIR != "/opt/app" {
		t.Errorf("WORK_DIR = %q, want /opt/app", svc.WORK_DIR)
	}
	if svc.ENV_VARS["PORT"] != "8080" {
		t.Errorf("ENV_VARS[PORT] = %q, want 8080", svc.ENV_VARS["PORT"])
	}
}

func TestLoad_Save_RoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "config.toml")

	// Write a valid TOML
	original := `[services.web]
EXEC_CMD = "./server"
WORK_DIR = "/app"
RESTART_POLICY = "always"
BACK_OFF = 3

[services.db]
EXEC_CMD = "./db-server"
WORK_DIR = "/data"
DEPENDS_ON = ["web"]

[web]
WEB_ADDR = "0.0.0.0:9090"
WEB_ENABLE = true`
	os.WriteFile(path, []byte(original), 0644)

	// Load
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	// Modify
	cfg.Services["cache"] = ServiceConfig{
		EXEC_CMD: "./redis",
		WORK_DIR: "/var/cache",
	}
	cfg.Web.WEB_ADDR = "0.0.0.0:8080"

	// Save back
	savePath := filepath.Join(tmpDir, "modified.toml")
	if err := cfg.Save(savePath); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	// Reload saved
	reloaded, err := Load(savePath)
	if err != nil {
		t.Fatalf("Load(modified) error = %v", err)
	}

	// Verify original services preserved
	if _, ok := reloaded.Services["web"]; !ok {
		t.Error("missing service 'web' in reloaded")
	}
	if _, ok := reloaded.Services["db"]; !ok {
		t.Error("missing service 'db' in reloaded")
	}

	// Verify new service added
	cache, ok := reloaded.Services["cache"]
	if !ok {
		t.Fatal("missing service 'cache' in reloaded")
	}
	if cache.EXEC_CMD != "./redis" {
		t.Errorf("cache EXEC_CMD = %q, want ./redis", cache.EXEC_CMD)
	}

	// Verify web config modified
	if reloaded.Web.WEB_ADDR != "0.0.0.0:8080" {
		t.Errorf("WEB_ADDR = %q, want 0.0.0.0:8080", reloaded.Web.WEB_ADDR)
	}
}

func TestSave_Overwrite(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "config.toml")

	// Initial save
	cfg1 := New()
	cfg1.Services["svc1"] = ServiceConfig{EXEC_CMD: "cmd1"}
	if err := cfg1.Save(path); err != nil {
		t.Fatalf("first Save() error = %v", err)
	}

	// Overwrite with different data
	cfg2 := New()
	cfg2.Services["svc2"] = ServiceConfig{EXEC_CMD: "cmd2"}
	if err := cfg2.Save(path); err != nil {
		t.Fatalf("second Save() error = %v", err)
	}

	// Reload should only have svc2
	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if _, ok := loaded.Services["svc1"]; ok {
		t.Error("old service 'svc1' should not exist after overwrite")
	}
	if _, ok := loaded.Services["svc2"]; !ok {
		t.Error("new service 'svc2' should exist after overwrite")
	}
}

func TestSave_Permissions(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "config.toml")

	cfg := New()
	if err := cfg.Save(path); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	perm := info.Mode().Perm()
	if perm != 0644 {
		t.Errorf("file permissions = %04o, want 0644", perm)
	}
}
