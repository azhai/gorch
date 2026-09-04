package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTempConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "gorch.toml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

// ── Load ────────────────────────────────────────────────────

func TestLoad_Valid(t *testing.T) {
	path := writeTempConfig(t, `
[services]
[services.api]
EXEC_CMD = "sleep 1"
WORK_DIR = "/tmp"
RESTART_POLICY = "always"
BACK_OFF = 3
[services.worker]
EXEC_CMD = "sleep 1"
DEPENDS_ON = ["api"]
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Services) != 2 {
		t.Fatalf("loaded %d services, want 2", len(cfg.Services))
	}
	if cfg.Services["api"].EXEC_CMD != "sleep 1" {
		t.Errorf("EXEC_CMD = %q", cfg.Services["api"].EXEC_CMD)
	}
	if cfg.Services["api"].RESTART_POLICY != "always" {
		t.Errorf("RESTART_POLICY = %q", cfg.Services["api"].RESTART_POLICY)
	}

	// Dependencies must come first in the start order.
	order := cfg.TopologicalOrder()
	apiIdx, workerIdx := -1, -1
	for i, n := range order {
		if n == "api" {
			apiIdx = i
		}
		if n == "worker" {
			workerIdx = i
		}
	}
	if apiIdx < 0 || workerIdx < 0 {
		t.Fatalf("topological order %v missing services", order)
	}
	if apiIdx > workerIdx {
		t.Errorf("topological order %v: dependency 'api' must precede 'worker'", order)
	}
}

func TestLoad_MissingFile(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "nope.toml")); err == nil {
		t.Error("expected an error for a missing config file")
	}
}

func TestLoad_InvalidTOML(t *testing.T) {
	path := writeTempConfig(t, "this is not = = toml")
	if _, err := Load(path); err == nil {
		t.Error("expected an error for invalid TOML")
	}
}

func TestLoad_MissingExecCmd(t *testing.T) {
	path := writeTempConfig(t, "[services]\n[services.api]\nWORK_DIR = \"/tmp\"\n")
	if _, err := Load(path); err == nil {
		t.Error("expected an error when EXEC_CMD is missing")
	}
}

func TestLoad_InvalidRestartPolicy(t *testing.T) {
	path := writeTempConfig(t, "[services]\n[services.api]\nEXEC_CMD = \"x\"\nRESTART_POLICY = \"sometimes\"\n")
	if _, err := Load(path); err == nil {
		t.Error("expected an error for an invalid RESTART_POLICY")
	}
}

func TestLoad_NegativeBackOff(t *testing.T) {
	path := writeTempConfig(t, "[services]\n[services.api]\nEXEC_CMD = \"x\"\nBACK_OFF = -1\n")
	if _, err := Load(path); err == nil {
		t.Error("expected an error for a negative BACK_OFF")
	}
}

func TestLoad_UnknownDependency(t *testing.T) {
	path := writeTempConfig(t, "[services]\n[services.api]\nEXEC_CMD = \"x\"\nDEPENDS_ON = [\"ghost\"]\n")
	if _, err := Load(path); err == nil {
		t.Error("expected an error when depending on an unknown service")
	}
}

func TestLoad_CircularDependency(t *testing.T) {
	path := writeTempConfig(t, `
[services]
[services.a]
EXEC_CMD = "x"
DEPENDS_ON = ["b"]
[services.b]
EXEC_CMD = "x"
DEPENDS_ON = ["a"]
`)
	if _, err := Load(path); err == nil {
		t.Error("expected a circular dependency error")
	}
}

// TestLoad_DefaultsFromLogDir verifies LOG_DIR populates default log paths.
func TestLoad_DefaultsFromLogDir(t *testing.T) {
	dir := t.TempDir()
	path := writeTempConfig(t, "LOG_DIR = '"+dir+"'\n[services]\n[services.api]\nEXEC_CMD = \"x\"\n")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	svc := cfg.Services["api"]
	if svc.STDOUT != filepath.Join(dir, "api.out.log") {
		t.Errorf("STDOUT = %q, want the LOG_DIR default", svc.STDOUT)
	}
	if svc.STDERR != filepath.Join(dir, "api.err.log") {
		t.Errorf("STDERR = %q, want the LOG_DIR default", svc.STDERR)
	}
	// WORK_DIR defaults to the directory containing the config file.
	if svc.WORK_DIR != filepath.Dir(path) {
		t.Errorf("WORK_DIR = %q, want %q", svc.WORK_DIR, filepath.Dir(path))
	}
}

func TestLoad_WebDefaults(t *testing.T) {
	path := writeTempConfig(t, "[services]\n[services.api]\nEXEC_CMD = \"x\"\n")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Web.WEB_ADDR != "127.0.0.1:8080" {
		t.Errorf("WEB_ADDR = %q, want the default", cfg.Web.WEB_ADDR)
	}
	if cfg.PID_FILE != "/var/run/gorch.pid" {
		t.Errorf("PID_FILE = %q, want the default", cfg.PID_FILE)
	}
	if cfg.SERVICES_LOCK != "/var/run/gorch-services.lock" {
		t.Errorf("SERVICES_LOCK = %q, want the default", cfg.SERVICES_LOCK)
	}
}

func TestLoad_TotpDefaults(t *testing.T) {
	t.Setenv("GORCH_TOTP_SECRET", "from-env")
	path := writeTempConfig(t, "[services]\n[services.api]\nEXEC_CMD = \"x\"\n[web]\nTOTP_ENABLE = true\n")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Web.TOTP_DB != "/var/lib/gorch/totp.db" {
		t.Errorf("TOTP_DB = %q, want the default", cfg.Web.TOTP_DB)
	}
	if cfg.Web.TOTP_SECRET != "from-env" {
		t.Errorf("TOTP_SECRET = %q, want the value from the environment", cfg.Web.TOTP_SECRET)
	}
}

func TestLoad_EnvExpansion(t *testing.T) {
	t.Setenv("GORCH_TEST_HOME", "/opt/app")
	path := writeTempConfig(t, "[services]\n[services.api]\nEXEC_CMD = \"x\"\nWORK_DIR = \"${GORCH_TEST_HOME}/bin\"\n")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.Services["api"].WORK_DIR; got != "/opt/app/bin" {
		t.Errorf("WORK_DIR = %q, want /opt/app/bin", got)
	}
}

func TestLoad_EnvExpansion_Missing(t *testing.T) {
	path := writeTempConfig(t, "[services]\n[services.api]\nEXEC_CMD = \"x\"\nWORK_DIR = \"${DEFINITELY_UNSET_VAR_XYZ}\"\n")
	if _, err := Load(path); err == nil {
		t.Error("expected an error for an unset environment variable")
	}
}

// ── TopologicalOrder / RecalcTopoOrder ──────────────────────

func TestConfig_RecalcTopoOrder(t *testing.T) {
	cfg := &Config{Services: map[string]ServiceConfig{
		"db":     {EXEC_CMD: "x"},
		"api":    {EXEC_CMD: "x", DEPENDS_ON: []string{"db"}},
		"worker": {EXEC_CMD: "x", DEPENDS_ON: []string{"api"}},
	}}

	cfg.RecalcTopoOrder()
	order := cfg.TopologicalOrder()

	pos := map[string]int{}
	for i, n := range order {
		pos[n] = i
	}
	if len(order) != 3 {
		t.Fatalf("order = %v, want 3 entries", order)
	}
	if pos["db"] > pos["api"] || pos["api"] > pos["worker"] {
		t.Errorf("order %v violates dependency order", order)
	}
}

// A cycle must leave the previous order untouched rather than clearing it.
func TestConfig_RecalcTopoOrder_CycleKeepsPrevious(t *testing.T) {
	cfg := &Config{Services: map[string]ServiceConfig{
		"a": {EXEC_CMD: "x", DEPENDS_ON: []string{"b"}},
		"b": {EXEC_CMD: "x", DEPENDS_ON: []string{"a"}},
	}}
	cfg.topoOrder = []string{"a", "b"}

	cfg.RecalcTopoOrder()

	if len(cfg.TopologicalOrder()) != 2 {
		t.Errorf("topo order was clobbered by a failed recalculation: %v", cfg.TopologicalOrder())
	}
}

// ── cleanServiceConfig ──────────────────────────────────────

func TestCleanServiceConfig_OmitsDefaults(t *testing.T) {
	dir := "/etc/gorch"
	svc := ServiceConfig{
		EXEC_CMD:       "run",
		WORK_DIR:       dir,                  // equals configDir → omitted
		RESTART_POLICY: string(RestartNever), // default → omitted
	}
	clean := cleanServiceConfig(svc, dir)

	if _, ok := clean["WORK_DIR"]; ok {
		t.Error("WORK_DIR equal to the config dir should be omitted")
	}
	if _, ok := clean["RESTART_POLICY"]; ok {
		t.Error("RESTART_POLICY 'never' (the default) should be omitted")
	}
	if clean["EXEC_CMD"] != "run" {
		t.Errorf("EXEC_CMD = %v, want 'run'", clean["EXEC_CMD"])
	}
}

func TestCleanServiceConfig_OmitsDotWorkDir(t *testing.T) {
	clean := cleanServiceConfig(ServiceConfig{EXEC_CMD: "run", WORK_DIR: "."}, "/etc")
	if _, ok := clean["WORK_DIR"]; ok {
		t.Error("WORK_DIR '.' should be omitted")
	}
}

func TestCleanServiceConfig_KeepsExplicitValues(t *testing.T) {
	svc := ServiceConfig{
		EXEC_CMD:       "run",
		WORK_DIR:       "/opt/app",
		RESTART_CMD:    "reload",
		RESTART_POLICY: "always",
		BACK_OFF:       5,
		CHECK_PORT:     8080,
		PRE_ACTION:     "pre",
		STDOUT:         "/var/log/o.log",
		STDERR:         "/var/log/e.log",
		DEPENDS_ON:     []string{"db"},
		CRON:           "0 0 * * * *",
		CRON_TIMEOUT:   300,
		PID_FILE:       "/run/x.pid",
		ENV_VARS:       map[string]string{"A": "1"},
	}
	clean := cleanServiceConfig(svc, "/etc")

	for _, key := range []string{
		"EXEC_CMD", "WORK_DIR", "RESTART_CMD", "RESTART_POLICY", "BACK_OFF",
		"CHECK_PORT", "PRE_ACTION", "STDOUT", "STDERR", "DEPENDS_ON",
		"CRON", "CRON_TIMEOUT", "PID_FILE", "ENV_VARS",
	} {
		if _, ok := clean[key]; !ok {
			t.Errorf("%s should be present in the saved config", key)
		}
	}
}

func TestCleanServiceConfig_OmitsZeroCronTimeout(t *testing.T) {
	clean := cleanServiceConfig(ServiceConfig{EXEC_CMD: "run", CRON: "0 0 * * * *"}, "/etc")
	if _, ok := clean["CRON_TIMEOUT"]; ok {
		t.Error("CRON_TIMEOUT of 0 should be omitted")
	}
}

func TestCleanServiceConfig_EmptyService(t *testing.T) {
	if clean := cleanServiceConfig(ServiceConfig{}, "/etc"); len(clean) != 0 {
		t.Errorf("empty service produced %v, want an empty map", clean)
	}
}

// ── Save ────────────────────────────────────────────────────

func TestSave_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.toml")

	cfg := &Config{
		Services: map[string]ServiceConfig{
			"api": {EXEC_CMD: "serve", WORK_DIR: "/opt/app", RESTART_POLICY: "always", BACK_OFF: 2, CRON: "0 0 * * * *"},
		},
		Web: WebConfig{WEB_ENABLE: true, WEB_ADDR: "127.0.0.1:9090", WEB_AUTH: true, WEB_USER: "admin", WEB_PASS: "pw"},
	}
	if err := cfg.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load after Save: %v", err)
	}
	if got := loaded.Services["api"].EXEC_CMD; got != "serve" {
		t.Errorf("EXEC_CMD = %q, want serve", got)
	}
	if got := loaded.Services["api"].WORK_DIR; got != "/opt/app" {
		t.Errorf("WORK_DIR = %q, want /opt/app", got)
	}
	if got := loaded.Services["api"].CRON; got != "0 0 * * * *" {
		t.Errorf("CRON = %q, want the cron expression preserved", got)
	}
	if !loaded.Web.WEB_ENABLE || loaded.Web.WEB_USER != "admin" {
		t.Errorf("web settings lost: %+v", loaded.Web)
	}
}

// Save must create the parent directory when it does not exist.
func TestSave_CreatesDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "deeper", "gorch.toml")

	cfg := &Config{Services: map[string]ServiceConfig{"api": {EXEC_CMD: "x"}}}
	if err := cfg.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("config file was not written: %v", err)
	}
}

func TestSave_EmptyConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.toml")
	if err := (&Config{}).Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if strings.Contains(string(data), "services") {
		t.Error("an empty config should not contain a services table")
	}
}

// ── IsValidRestartPolicy ────────────────────────────────────

func TestIsValidRestartPolicy(t *testing.T) {
	valid := []string{"always", "on-failure", "never"}
	for _, p := range valid {
		if !IsValidRestartPolicy(p) {
			t.Errorf("IsValidRestartPolicy(%q) = false, want true", p)
		}
	}
	for _, p := range []string{"", "Always", "sometimes", "on_failure"} {
		if IsValidRestartPolicy(p) {
			t.Errorf("IsValidRestartPolicy(%q) = true, want false", p)
		}
	}
}

// ── expandEnv ───────────────────────────────────────────────

func TestExpandEnv_NoPlaceholder(t *testing.T) {
	got, err := expandEnv("/plain/path")
	if err != nil {
		t.Fatalf("expandEnv: %v", err)
	}
	if got != "/plain/path" {
		t.Errorf("got %q, want the input unchanged", got)
	}
}

func TestExpandEnv_Substitutes(t *testing.T) {
	t.Setenv("GORCH_X", "value")
	got, err := expandEnv("pre-${GORCH_X}-post")
	if err != nil {
		t.Fatalf("expandEnv: %v", err)
	}
	if got != "pre-value-post" {
		t.Errorf("got %q, want pre-value-post", got)
	}
}

func TestExpandEnv_Multiple(t *testing.T) {
	t.Setenv("GORCH_A", "1")
	t.Setenv("GORCH_B", "2")
	got, err := expandEnv("${GORCH_A}/${GORCH_B}/${GORCH_A}")
	if err != nil {
		t.Fatalf("expandEnv: %v", err)
	}
	if got != "1/2/1" {
		t.Errorf("got %q, want 1/2/1", got)
	}
}

func TestExpandEnv_MissingVar(t *testing.T) {
	_, err := expandEnv("${GORCH_UNSET_VAR_ABC}")
	if err == nil {
		t.Fatal("expected an error for an unset variable")
	}
	if !strings.Contains(err.Error(), "GORCH_UNSET_VAR_ABC") {
		t.Errorf("error %q should name the missing variable", err)
	}
}

// ── New ─────────────────────────────────────────────────────

func TestNew(t *testing.T) {
	cfg := New()
	if cfg.Services == nil {
		t.Error("Services map should be initialized")
	}
	if cfg.Web.WEB_ADDR != "127.0.0.1:8080" {
		t.Errorf("WEB_ADDR = %q, want the default", cfg.Web.WEB_ADDR)
	}
}
