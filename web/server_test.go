package web

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/azhai/gobus/log"
	"github.com/azhai/gorch/config"
	"github.com/azhai/gorch/cron"
	"github.com/azhai/gorch/status"
)

// fakeSupervisor implements SupervisorProvider for the HTTP API tests.
type fakeSupervisor struct {
	cfg      *config.Config
	statuses map[string]status.ServiceStatus
	logMgr   *log.Manager
	sched    *cron.Scheduler
	hub      *Hub

	started   []string
	stopped   []string
	restarted []string
	updated   map[string]config.ServiceConfig
	created   map[string]config.ServiceConfig
	deleted   []string
	saved     int

	errs map[string]error
}

func (f *fakeSupervisor) GetStatus(name string) (status.ServiceStatus, bool) {
	st, ok := f.statuses[name]
	return st, ok
}

func (f *fakeSupervisor) GetAllStatus() map[string]status.ServiceStatus { return f.statuses }
func (f *fakeSupervisor) GetConfig() *config.Config                     { return f.cfg }
func (f *fakeSupervisor) GetLogManager() *log.Manager                   { return f.logMgr }
func (f *fakeSupervisor) GetCronScheduler() *cron.Scheduler             { return f.sched }
func (f *fakeSupervisor) GetHub() *Hub                                  { return f.hub }

func (f *fakeSupervisor) StartService(ctx context.Context, name string) error {
	f.started = append(f.started, name)
	return f.errs["start"]
}

func (f *fakeSupervisor) StopService(ctx context.Context, name string) error {
	f.stopped = append(f.stopped, name)
	return f.errs["stop"]
}

func (f *fakeSupervisor) RestartService(ctx context.Context, name string) error {
	f.restarted = append(f.restarted, name)
	return f.errs["restart"]
}

func (f *fakeSupervisor) UpdateServiceConfig(name string, svc config.ServiceConfig) error {
	if f.errs["update"] != nil {
		return f.errs["update"]
	}
	if f.updated == nil {
		f.updated = make(map[string]config.ServiceConfig)
	}
	f.updated[name] = svc
	return nil
}

func (f *fakeSupervisor) CreateService(name string, svc config.ServiceConfig) error {
	if f.errs["create"] != nil {
		return f.errs["create"]
	}
	if f.created == nil {
		f.created = make(map[string]config.ServiceConfig)
	}
	f.created[name] = svc
	return nil
}

func (f *fakeSupervisor) DeleteService(name string) error {
	if f.errs["delete"] != nil {
		return f.errs["delete"]
	}
	f.deleted = append(f.deleted, name)
	return nil
}

func (f *fakeSupervisor) SaveConfig() error {
	if f.errs["save"] != nil {
		return f.errs["save"]
	}
	f.saved++
	return nil
}

// newTestServer builds a Server backed by a fake supervisor with one service.
func newTestServer(t *testing.T) (*Server, *fakeSupervisor) {
	t.Helper()
	dir := t.TempDir()

	sup := &fakeSupervisor{
		cfg: &config.Config{
			Services: map[string]config.ServiceConfig{
				"api": {
					EXEC_CMD: "serve",
					WORK_DIR: dir,
					STDOUT:   filepath.Join(dir, "api.out.log"),
					STDERR:   filepath.Join(dir, "api.err.log"),
				},
			},
			Web: config.WebConfig{
				WEB_ENABLE: true,
				WEB_USER:   "admin",
				WEB_PASS:   "secret",
			},
		},
		statuses: map[string]status.ServiceStatus{
			"api": {Name: "api", Status: config.StatusRunning, Pid: 1234},
		},
		logMgr: log.NewManager(dir),
		sched:  cron.NewScheduler(),
		hub:    NewHub(),
		errs:   map[string]error{},
	}

	return NewServer("127.0.0.1:0", sup), sup
}

func doRequest(srv *Server, method, path, body string) *httptest.ResponseRecorder {
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, r)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	srv.app.ServeHTTP(rec, req)
	return rec
}

// decode parses the standard API envelope.
func decode(t *testing.T, rec *httptest.ResponseRecorder) APIResponse {
	t.Helper()
	var resp APIResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("response is not valid JSON (status %d): %s", rec.Code, rec.Body.String())
	}
	return resp
}

// ── construction & helpers ──────────────────────────────────

func TestNewServer_Basic(t *testing.T) {
	srv, sup := newTestServer(t)
	if srv == nil {
		t.Fatal("NewServer() returned nil")
	}
	if srv.supervisor != sup {
		t.Error("supervisor was not stored")
	}
	if srv.app == nil {
		t.Error("echo instance was not created")
	}
}

func TestNormalizePrefix(t *testing.T) {
	cases := map[string]string{
		"/gorch/":  "gorch",
		"gorch":    "gorch",
		"/":        "",
		"":         "",
		"  /a/b/ ": "a/b",
	}
	for in, want := range cases {
		if got := normalizePrefix(in); got != want {
			t.Errorf("normalizePrefix(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestServer_UrlPrefixRoutes(t *testing.T) {
	dir := t.TempDir()
	sup := &fakeSupervisor{
		cfg: &config.Config{
			Services: map[string]config.ServiceConfig{},
			Web:      config.WebConfig{URL_PREFIX: "/gorch"},
		},
		statuses: map[string]status.ServiceStatus{},
		logMgr:   log.NewManager(dir),
		sched:    cron.NewScheduler(),
		hub:      NewHub(),
		errs:     map[string]error{},
	}
	srv := NewServer("127.0.0.1:0", sup)
	if srv.urlPrefix != "gorch" {
		t.Errorf("urlPrefix = %q, want 'gorch'", srv.urlPrefix)
	}

	// API must be mounted under the prefix.
	rec := doRequest(srv, http.MethodGet, "/gorch/api/services", "")
	if rec.Code != http.StatusOK {
		t.Errorf("GET /gorch/api/services = %d, want 200", rec.Code)
	}
	// The prefix must be injected into index.html for the SPA.
	if !strings.Contains(string(srv.indexHTML), "__URL_PREFIX__") {
		t.Error("index.html should carry the injected URL prefix")
	}
}

// ── services read endpoints ─────────────────────────────────

func TestHandleGetServices(t *testing.T) {
	srv, _ := newTestServer(t)

	rec := doRequest(srv, http.MethodGet, "/api/services", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	resp := decode(t, rec)
	if !resp.Success {
		t.Errorf("success = false, message=%q", resp.Message)
	}
	var data map[string]status.ServiceStatus
	if err := json.Unmarshal(resp.Data, &data); err != nil {
		t.Fatalf("unmarshal data: %v", err)
	}
	if data["api"].Pid != 1234 {
		t.Errorf("api Pid = %d, want 1234", data["api"].Pid)
	}
}

func TestHandleGetService_Found(t *testing.T) {
	srv, _ := newTestServer(t)

	rec := doRequest(srv, http.MethodGet, "/api/services/api", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !decode(t, rec).Success {
		t.Error("expected success")
	}
}

func TestHandleGetService_NotFound(t *testing.T) {
	srv, _ := newTestServer(t)

	rec := doRequest(srv, http.MethodGet, "/api/services/ghost", "")
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

// ── lifecycle endpoints ─────────────────────────────────────

func TestHandleStartService(t *testing.T) {
	srv, sup := newTestServer(t)

	rec := doRequest(srv, http.MethodPost, "/api/services/api/start", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !decode(t, rec).Success {
		t.Error("expected success")
	}
	if len(sup.started) != 1 || sup.started[0] != "api" {
		t.Errorf("StartService calls = %v, want [api]", sup.started)
	}
}

func TestHandleStartService_Error(t *testing.T) {
	srv, sup := newTestServer(t)
	sup.errs["start"] = errors.New("boom")

	rec := doRequest(srv, http.MethodPost, "/api/services/api/start", "")
	resp := decode(t, rec)
	if resp.Success {
		t.Error("expected success=false when StartService fails")
	}
	if !strings.Contains(resp.Message, "boom") {
		t.Errorf("message = %q, want it to contain 'boom'", resp.Message)
	}
}

func TestHandleStopService(t *testing.T) {
	srv, sup := newTestServer(t)

	rec := doRequest(srv, http.MethodPost, "/api/services/api/stop", "")
	if !decode(t, rec).Success {
		t.Error("expected success")
	}
	if len(sup.stopped) != 1 || sup.stopped[0] != "api" {
		t.Errorf("StopService calls = %v, want [api]", sup.stopped)
	}
}

func TestHandleRestartService(t *testing.T) {
	srv, sup := newTestServer(t)

	rec := doRequest(srv, http.MethodPost, "/api/services/api/restart", "")
	if !decode(t, rec).Success {
		t.Error("expected success")
	}
	if len(sup.restarted) != 1 || sup.restarted[0] != "api" {
		t.Errorf("RestartService calls = %v, want [api]", sup.restarted)
	}
}

func TestHandleRestartService_Error(t *testing.T) {
	srv, sup := newTestServer(t)
	sup.errs["restart"] = errors.New("nope")

	resp := decode(t, doRequest(srv, http.MethodPost, "/api/services/api/restart", ""))
	if resp.Success || !strings.Contains(resp.Message, "nope") {
		t.Errorf("resp = %+v, want failure mentioning 'nope'", resp)
	}
}

// ── logs ────────────────────────────────────────────────────

func TestHandleGetLogs(t *testing.T) {
	srv, sup := newTestServer(t)

	logPath := sup.cfg.Services["api"].STDOUT
	if err := os.WriteFile(logPath, []byte("line1\nline2\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	rec := doRequest(srv, http.MethodGet, "/api/services/api/logs?lines=10", "")
	resp := decode(t, rec)
	if !resp.Success {
		t.Fatalf("resp = %+v", resp)
	}
	var data struct {
		Lines   []string `json:"lines"`
		LogPath string   `json:"logPath"`
	}
	if err := json.Unmarshal(resp.Data, &data); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if data.LogPath != logPath {
		t.Errorf("logPath = %q, want %q", data.LogPath, logPath)
	}
	if len(data.Lines) == 0 {
		t.Error("expected log lines to be returned")
	}
}

func TestHandleGetLogs_NotFound(t *testing.T) {
	srv, _ := newTestServer(t)

	rec := doRequest(srv, http.MethodGet, "/api/services/ghost/logs", "")
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestHandleClearLogs(t *testing.T) {
	srv, sup := newTestServer(t)

	logPath := sup.cfg.Services["api"].STDOUT
	if err := os.WriteFile(logPath, []byte("stuff\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	rec := doRequest(srv, http.MethodPost, "/api/services/api/logs/clear", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if data, err := os.ReadFile(logPath); err != nil || len(data) != 0 {
		t.Errorf("log file not cleared: data=%q err=%v", data, err)
	}
}

// ── config endpoints ────────────────────────────────────────

func TestHandleGetConfig(t *testing.T) {
	srv, _ := newTestServer(t)

	rec := doRequest(srv, http.MethodGet, "/api/services/api/config", "")
	resp := decode(t, rec)
	if !resp.Success {
		t.Fatalf("resp = %+v", resp)
	}
	var svc config.ServiceConfig
	if err := json.Unmarshal(resp.Data, &svc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if svc.EXEC_CMD != "serve" {
		t.Errorf("EXEC_CMD = %q, want 'serve'", svc.EXEC_CMD)
	}
}

func TestHandleGetConfig_NotFound(t *testing.T) {
	srv, _ := newTestServer(t)

	rec := doRequest(srv, http.MethodGet, "/api/services/ghost/config", "")
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestHandleUpdateConfig_Success(t *testing.T) {
	srv, sup := newTestServer(t)

	body := `{"EXEC_CMD":"serve-v2","WORK_DIR":"/tmp","RESTART_POLICY":"always"}`
	rec := doRequest(srv, http.MethodPut, "/api/services/api/config", body)
	if !decode(t, rec).Success {
		t.Fatalf("resp = %+v", rec.Body.String())
	}
	if got := sup.updated["api"].EXEC_CMD; got != "serve-v2" {
		t.Errorf("updated EXEC_CMD = %q, want 'serve-v2'", got)
	}
}

func TestHandleUpdateConfig_MissingExecCmd(t *testing.T) {
	srv, _ := newTestServer(t)

	rec := doRequest(srv, http.MethodPut, "/api/services/api/config", `{"WORK_DIR":"/tmp"}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 when EXEC_CMD is missing", rec.Code)
	}
}

func TestHandleUpdateConfig_InvalidRestartPolicy(t *testing.T) {
	srv, _ := newTestServer(t)

	body := `{"EXEC_CMD":"x","RESTART_POLICY":"sometimes"}`
	rec := doRequest(srv, http.MethodPut, "/api/services/api/config", body)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for an invalid RESTART_POLICY", rec.Code)
	}
}

func TestHandleUpdateConfig_NegativeBackOff(t *testing.T) {
	srv, _ := newTestServer(t)

	rec := doRequest(srv, http.MethodPut, "/api/services/api/config", `{"EXEC_CMD":"x","BACK_OFF":-1}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for a negative BACK_OFF", rec.Code)
	}
}

func TestHandleUpdateConfig_UnknownDependency(t *testing.T) {
	srv, _ := newTestServer(t)

	body := `{"EXEC_CMD":"x","DEPENDS_ON":["ghost"]}`
	rec := doRequest(srv, http.MethodPut, "/api/services/api/config", body)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for an unknown dependency", rec.Code)
	}
}

func TestHandleUpdateConfig_NotFound(t *testing.T) {
	srv, _ := newTestServer(t)

	rec := doRequest(srv, http.MethodPut, "/api/services/ghost/config", `{"EXEC_CMD":"x"}`)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestHandleUpdateConfig_InvalidBody(t *testing.T) {
	srv, _ := newTestServer(t)

	rec := doRequest(srv, http.MethodPut, "/api/services/api/config", `not json`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestHandleCreateService(t *testing.T) {
	srv, sup := newTestServer(t)

	body := `{"name":"worker","svc":{"EXEC_CMD":"work"}}`
	rec := doRequest(srv, http.MethodPost, "/api/services", body)
	if !decode(t, rec).Success {
		t.Fatalf("resp = %+v", rec.Body.String())
	}
	if _, ok := sup.created["worker"]; !ok {
		t.Error("CreateService was not called with 'worker'")
	}
	if sup.saved == 0 {
		t.Error("config should be persisted after creating a service")
	}
}

func TestHandleCreateService_MissingName(t *testing.T) {
	srv, _ := newTestServer(t)

	rec := doRequest(srv, http.MethodPost, "/api/services", `{"svc":{"EXEC_CMD":"work"}}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 when the name is missing", rec.Code)
	}
}

func TestHandleCreateService_MissingExecCmd(t *testing.T) {
	srv, _ := newTestServer(t)

	rec := doRequest(srv, http.MethodPost, "/api/services", `{"name":"worker","svc":{}}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 when EXEC_CMD is missing", rec.Code)
	}
}

func TestHandleDeleteService(t *testing.T) {
	srv, sup := newTestServer(t)

	rec := doRequest(srv, http.MethodDelete, "/api/services/api", "")
	if !decode(t, rec).Success {
		t.Fatalf("resp = %+v", rec.Body.String())
	}
	if len(sup.deleted) != 1 || sup.deleted[0] != "api" {
		t.Errorf("deleted = %v, want [api]", sup.deleted)
	}
}

func TestHandleDeleteService_Error(t *testing.T) {
	srv, sup := newTestServer(t)
	sup.errs["delete"] = errors.New("cannot delete")

	resp := decode(t, doRequest(srv, http.MethodDelete, "/api/services/api", ""))
	if resp.Success {
		t.Error("expected failure when DeleteService errors")
	}
}

func TestHandleSaveConfigToFile(t *testing.T) {
	srv, sup := newTestServer(t)

	rec := doRequest(srv, http.MethodPost, "/api/save-config", "")
	if !decode(t, rec).Success {
		t.Fatalf("resp = %+v", rec.Body.String())
	}
	if sup.saved != 1 {
		t.Errorf("SaveConfig called %d times, want 1", sup.saved)
	}
}

func TestHandleSaveConfigToFile_Error(t *testing.T) {
	srv, sup := newTestServer(t)
	sup.errs["save"] = errors.New("disk full")

	resp := decode(t, doRequest(srv, http.MethodPost, "/api/save-config", ""))
	if resp.Success {
		t.Error("expected failure when SaveConfig errors")
	}
}

// ── cron endpoints ──────────────────────────────────────────

func TestHandleGetCronHistory(t *testing.T) {
	srv, sup := newTestServer(t)
	sup.sched.RecordExecution("api", cron.CronExecutionRecord{Service: "api", Status: "started"})

	rec := doRequest(srv, http.MethodGet, "/api/cron/api/history", "")
	resp := decode(t, rec)
	if !resp.Success {
		t.Fatalf("resp = %+v", rec.Body.String())
	}
	var history []cron.CronExecutionRecord
	if err := json.Unmarshal(resp.Data, &history); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(history) != 1 {
		t.Fatalf("history length = %d, want 1", len(history))
	}
	if history[0].Status != "started" {
		t.Errorf("status = %q, want 'started'", history[0].Status)
	}
}

func TestHandleValidateCron_Valid(t *testing.T) {
	srv, _ := newTestServer(t)

	rec := doRequest(srv, http.MethodPost, "/api/cron/validate", `{"expression":"0 0 * * * *"}`)
	resp := decode(t, rec)
	if !resp.Success {
		t.Fatalf("resp = %+v", rec.Body.String())
	}
	var data struct {
		Valid bool `json:"valid"`
	}
	if err := json.Unmarshal(resp.Data, &data); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !data.Valid {
		t.Error("a valid cron expression should validate")
	}
}

func TestHandleValidateCron_Invalid(t *testing.T) {
	srv, _ := newTestServer(t)

	rec := doRequest(srv, http.MethodPost, "/api/cron/validate", `{"expression":"not a cron"}`)
	resp := decode(t, rec)
	var data struct {
		Valid bool `json:"valid"`
	}
	if err := json.Unmarshal(resp.Data, &data); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if data.Valid {
		t.Error("an invalid cron expression should not validate")
	}
}

func TestHandleValidateCron_Empty(t *testing.T) {
	srv, _ := newTestServer(t)

	rec := doRequest(srv, http.MethodPost, "/api/cron/validate", `{"expression":""}`)
	resp := decode(t, rec)
	var data struct {
		Valid   bool   `json:"valid"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(resp.Data, &data); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if data.Valid || data.Message == "" {
		t.Errorf("empty expression: valid=%v message=%q, want valid=false with a message", data.Valid, data.Message)
	}
}

func TestHandleValidateCron_InvalidBody(t *testing.T) {
	srv, _ := newTestServer(t)

	rec := doRequest(srv, http.MethodPost, "/api/cron/validate", `garbage`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

// ── auth ────────────────────────────────────────────────────

func TestHandleLogin_Success(t *testing.T) {
	srv, _ := newTestServer(t)

	rec := doRequest(srv, http.MethodPost, "/api/auth/login", `{"username":"admin","password":"secret"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !decode(t, rec).Success {
		t.Error("expected successful login")
	}
}

func TestHandleLogin_BadCredentials(t *testing.T) {
	srv, _ := newTestServer(t)

	rec := doRequest(srv, http.MethodPost, "/api/auth/login", `{"username":"admin","password":"wrong"}`)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func TestHandleLogin_InvalidBody(t *testing.T) {
	srv, _ := newTestServer(t)

	rec := doRequest(srv, http.MethodPost, "/api/auth/login", `nope`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

// When auth is enabled, API routes (other than /auth) must reject anonymous calls.
func TestAuthMiddleware_RequiresToken(t *testing.T) {
	dir := t.TempDir()
	sup := &fakeSupervisor{
		cfg: &config.Config{
			Services: map[string]config.ServiceConfig{},
			Web:      config.WebConfig{WEB_AUTH: true, WEB_USER: "admin", WEB_PASS: "secret"},
		},
		statuses: map[string]status.ServiceStatus{},
		logMgr:   log.NewManager(dir),
		sched:    cron.NewScheduler(),
		hub:      NewHub(),
		errs:     map[string]error{},
	}
	srv := NewServer("127.0.0.1:0", sup)

	if rec := doRequest(srv, http.MethodGet, "/api/services", ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("anonymous GET /api/services = %d, want 401", rec.Code)
	}
	// A bogus token must also be rejected.
	req := httptest.NewRequest(http.MethodGet, "/api/services", nil)
	req.Header.Set("Authorization", "Bearer bogus")
	rec := httptest.NewRecorder()
	srv.app.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("bad token = %d, want 401", rec.Code)
	}
	// The login endpoint stays reachable without a token.
	if rec := doRequest(srv, http.MethodPost, "/api/auth/login", `{"username":"admin","password":"secret"}`); rec.Code != http.StatusOK {
		t.Errorf("login = %d, want 200 (auth routes are exempt)", rec.Code)
	}
}

// ── TOTP not configured ─────────────────────────────────────

// TestTOTP_NotConfigured verifies the /totp/* routes report 503 when TOTP is
// disabled: NewTOTPFromOptions returns nil when Enable is false, so the server
// registers the "TOTP not configured" fallback for the whole /totp namespace.
func TestTOTP_NotConfigured(t *testing.T) {
	srv, _ := newTestServer(t) // TOTP_ENABLE defaults to false

	if srv.TOTP != nil {
		t.Fatal("expected TOTP to be nil when TOTP_ENABLE is false")
	}
	for _, path := range []string{
		"/api/totp/status",
		"/api/totp/setup",
		"/api/totp/verify-setup",
		"/api/totp/verify",
		"/api/totp/verify-backup",
		"/api/totp/disable",
		"/api/totp/regenerate-backup",
	} {
		rec := doRequest(srv, http.MethodPost, path, `{"code":"000000"}`)
		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("POST %s = %d, want 503 when TOTP is disabled", path, rec.Code)
		}
	}
}

// ── SPA fallback / static ───────────────────────────────────

func TestSPAFallback(t *testing.T) {
	srv, _ := newTestServer(t)

	rec := doRequest(srv, http.MethodGet, "/", "")
	if rec.Code != http.StatusOK {
		t.Errorf("GET / = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Errorf("Content-Type = %q, want html", ct)
	}
}

func TestAPINotFound(t *testing.T) {
	srv, _ := newTestServer(t)

	rec := doRequest(srv, http.MethodGet, "/api/definitely-not-a-route", "")
	if rec.Code != http.StatusNotFound {
		t.Errorf("unknown API route = %d, want 404", rec.Code)
	}
}

func TestServerStartStop(t *testing.T) {
	srv, _ := newTestServer(t)
	// Stop on a server that was never started must not panic.
	srv.Stop()
}
