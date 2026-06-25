package config

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

var envPattern = regexp.MustCompile(`\$\{([^}]+)\}`)

func Load(path string) (*Config, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("invalid config path: %w", err)
	}

	data, err := os.ReadFile(absPath)
	if err != nil {
		return nil, fmt.Errorf("config file not found: %s", absPath)
	}

	var cfg Config
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse TOML: %w", err)
	}

	if cfg.Services == nil {
		cfg.Services = make(map[string]ServiceConfig)
	}

	if err := validateAndFillDefaults(&cfg, filepath.Dir(absPath)); err != nil {
		return nil, err
	}

	if err := expandEnvInConfig(&cfg); err != nil {
		return nil, err
	}

	if err := validateDependencies(&cfg); err != nil {
		return nil, err
	}

	topoOrder, err := topologicalSort(cfg.Services)
	if err != nil {
		return nil, err
	}
	cfg.topoOrder = topoOrder

	return &cfg, nil
}

func (c *Config) TopologicalOrder() []string {
	return c.topoOrder
}

// RecalcTopoOrder recalculates the topological order after service changes.
func (c *Config) RecalcTopoOrder() {
	if order, err := topologicalSort(c.Services); err == nil {
		c.topoOrder = order
	}
}

// New creates a new empty config with default values.
func New() *Config {
	return &Config{
		Services: make(map[string]ServiceConfig),
		Web:      WebConfig{WEB_ADDR: "127.0.0.1:8080"},
	}
}

// cleanServiceConfig returns a copy with empty/zero fields removed so they won't appear in the saved TOML.
// configDir is used to filter out the default WORK_DIR value filled by validateAndFillDefaults.
func cleanServiceConfig(svc ServiceConfig, configDir string) map[string]any {
	clean := make(map[string]any)
	if svc.EXEC_CMD != "" {
		clean["EXEC_CMD"] = svc.EXEC_CMD
	}
	if svc.RESTART_CMD != "" {
		clean["RESTART_CMD"] = svc.RESTART_CMD
	}
	if svc.WORK_DIR != "" && svc.WORK_DIR != "." && svc.WORK_DIR != configDir {
		clean["WORK_DIR"] = svc.WORK_DIR
	}
	if svc.RESTART_POLICY != "" && svc.RESTART_POLICY != string(RestartNever) {
		clean["RESTART_POLICY"] = svc.RESTART_POLICY
	}
	if svc.BACK_OFF > 0 {
		clean["BACK_OFF"] = svc.BACK_OFF
	}
	if svc.CHECK_PORT > 0 {
		clean["CHECK_PORT"] = svc.CHECK_PORT
	}
	if svc.PRE_ACTION != "" {
		clean["PRE_ACTION"] = svc.PRE_ACTION
	}
	if svc.STDOUT != "" {
		clean["STDOUT"] = svc.STDOUT
	}
	if svc.STDERR != "" {
		clean["STDERR"] = svc.STDERR
	}
	if len(svc.DEPENDS_ON) > 0 {
		clean["DEPENDS_ON"] = svc.DEPENDS_ON
	}
	if svc.CRON != "" {
		clean["CRON"] = svc.CRON
	}
	if svc.PID_FILE != "" {
		clean["PID_FILE"] = svc.PID_FILE
	}
	if len(svc.ENV_VARS) > 0 {
		clean["ENV_VARS"] = svc.ENV_VARS
	}
	return clean
}

// Save writes the config to a TOML file at the given path.
// The directory will be created if it does not exist.
// Empty fields are omitted from the output to keep the file clean.
func (c *Config) Save(path string) error {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("invalid config path: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(absPath), 0755); err != nil {
		return fmt.Errorf("failed to create config dir: %w", err)
	}

	// Build a clean map — omit empty service fields
	configDir := filepath.Dir(absPath)
	services := make(map[string]any, len(c.Services))
	for name, svc := range c.Services {
		// Log service names for debugging name corruption issues
		if strings.ContainsAny(name, "/\\.") {
			slog.Warn("service name contains special characters", "service", name)
		}
		clean := cleanServiceConfig(svc, configDir)
		if len(clean) > 0 {
			services[name] = clean
		}
	}

	out := make(map[string]any)
	if len(services) > 0 {
		out["services"] = services
	}
	web := make(map[string]any)
	if c.Web.WEB_ENABLE {
		web["WEB_ENABLE"] = true
	}
	if c.Web.WEB_ADDR != "" && c.Web.WEB_ADDR != "127.0.0.1:8080" {
		web["WEB_ADDR"] = c.Web.WEB_ADDR
	}
	if c.Web.WEB_AUTH {
		web["WEB_AUTH"] = true
	}
	if c.Web.WEB_USER != "" {
		web["WEB_USER"] = c.Web.WEB_USER
	}
	if c.Web.WEB_PASS != "" {
		web["WEB_PASS"] = c.Web.WEB_PASS
	}
	if len(web) > 0 {
		out["web"] = web
	}
	if c.LOG_DIR != "" {
		out["LOG_DIR"] = c.LOG_DIR
	}

	data, err := toml.Marshal(out)
	if err != nil {
		return fmt.Errorf("failed to serialize config: %w", err)
	}

	if err := os.WriteFile(absPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

func validateAndFillDefaults(cfg *Config, configDir string) error {
	for name, svc := range cfg.Services {
		if svc.EXEC_CMD == "" {
			return fmt.Errorf("service '%s' missing required field: EXEC_CMD", name)
		}

		if svc.WORK_DIR == "" {
			svc.WORK_DIR = configDir
		}

		if svc.RESTART_POLICY == "" {
			svc.RESTART_POLICY = string(RestartNever)
		} else if !IsValidRestartPolicy(svc.RESTART_POLICY) {
			return fmt.Errorf("service '%s' invalid RESTART_POLICY: %s", name, svc.RESTART_POLICY)
		}

		if svc.BACK_OFF < 0 {
			return fmt.Errorf("service '%s' BACK_OFF must be non-negative", name)
		}

		// Fill default log paths from LOG_DIR
		if cfg.LOG_DIR != "" {
			if svc.STDOUT == "" {
				svc.STDOUT = filepath.Join(cfg.LOG_DIR, name+".out.log")
			}
			if svc.STDERR == "" {
				svc.STDERR = filepath.Join(cfg.LOG_DIR, name+".err.log")
			}
		}

		cfg.Services[name] = svc
	}

	if cfg.Web.WEB_ADDR == "" {
		cfg.Web.WEB_ADDR = "127.0.0.1:8080"
	}

	if cfg.Web.TOTP_ENABLE {
		if cfg.Web.TOTP_DB == "" {
			cfg.Web.TOTP_DB = "/var/lib/gorch/totp.db"
		}
		if cfg.Web.TOTP_SECRET == "" {
			cfg.Web.TOTP_SECRET = os.Getenv("GORCH_TOTP_SECRET")
		}
	}

	if cfg.PID_FILE == "" {
		cfg.PID_FILE = "/var/run/gorch.pid"
	}
	if cfg.SERVICES_LOCK == "" {
		cfg.SERVICES_LOCK = "/var/run/gorch-services.lock"
	}

	return nil
}

func IsValidRestartPolicy(p string) bool {
	switch RestartPolicy(p) {
	case RestartAlways, RestartOnFailure, RestartNever:
		return true
	}
	return false
}

func expandEnvInConfig(cfg *Config) error {
	for name, svc := range cfg.Services {
		var err error
		fields := []*string{&svc.EXEC_CMD, &svc.WORK_DIR, &svc.STDOUT, &svc.STDERR, &svc.PID_FILE}
		names := []string{"EXEC_CMD", "WORK_DIR", "STDOUT", "STDERR", "PID_FILE"}
		for i, p := range fields {
			if *p, err = expandEnv(*p); err != nil {
				return fmt.Errorf("service '%s' %s: %w", name, names[i], err)
			}
		}

		for k, v := range svc.ENV_VARS {
			if svc.ENV_VARS[k], err = expandEnv(v); err != nil {
				return fmt.Errorf("service '%s' ENV_VARS[%s]: %w", name, k, err)
			}
		}

		cfg.Services[name] = svc
	}
	return nil
}

// expandEnv replaces ${VAR} patterns with environment variable values.
// Uses single regex pass + strings.NewReplacer for minimal allocations.
func expandEnv(s string) (string, error) {
	if !strings.Contains(s, "${") {
		return s, nil
	}

	matches := envPattern.FindAllStringSubmatch(s, -1)
	if matches == nil {
		return s, nil
	}

	pairs := make([]string, 0, len(matches)*2)
	var missing string
	for _, m := range matches {
		val := os.Getenv(m[1])
		if val == "" {
			if missing == "" {
				missing = m[1]
			}
			continue
		}
		pairs = append(pairs, m[0], val)
	}

	if missing != "" {
		return "", fmt.Errorf("environment variable not found: %s", missing)
	}

	return strings.NewReplacer(pairs...).Replace(s), nil
}

func validateDependencies(cfg *Config) error {
	for name, svc := range cfg.Services {
		for _, dep := range svc.DEPENDS_ON {
			if _, exists := cfg.Services[dep]; !exists {
				return fmt.Errorf("service '%s' depends on unknown service '%s'", name, dep)
			}
		}
	}
	return nil
}

func topologicalSort(services map[string]ServiceConfig) ([]string, error) {
	const (
		visiting int8 = 1
		visited  int8 = 2
	)
	state := make(map[string]int8, len(services))
	order := make([]string, 0, len(services))

	var visit func(name string) error
	visit = func(name string) error {
		switch state[name] {
		case visited:
			return nil
		case visiting:
			return fmt.Errorf("circular dependency detected involving service '%s'", name)
		}

		state[name] = visiting
		for _, dep := range services[name].DEPENDS_ON {
			if err := visit(dep); err != nil {
				return err
			}
		}
		state[name] = visited
		order = append(order, name)
		return nil
	}

	for name := range services {
		if state[name] == 0 {
			if err := visit(name); err != nil {
				return nil, err
			}
		}
	}

	return order, nil
}
