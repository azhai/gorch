package config

import (
	"fmt"
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

// New creates a new empty config with default values.
func New() *Config {
	return &Config{
		Services: make(map[string]ServiceConfig),
		Web:      WebConfig{WEB_ADDR: "127.0.0.1:8080"},
	}
}

// Save writes the config to a TOML file at the given path.
// The directory will be created if it does not exist.
func (c *Config) Save(path string) error {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("invalid config path: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(absPath), 0755); err != nil {
		return fmt.Errorf("failed to create config dir: %w", err)
	}

	data, err := toml.Marshal(c)
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
		} else if !isValidRestartPolicy(svc.RESTART_POLICY) {
			return fmt.Errorf("service '%s' invalid RESTART_POLICY: %s", name, svc.RESTART_POLICY)
		}

		if svc.BACK_OFF < 0 {
			return fmt.Errorf("service '%s' BACK_OFF must be non-negative", name)
		}

		cfg.Services[name] = svc
	}

	if cfg.Web.WEB_ADDR == "" {
		cfg.Web.WEB_ADDR = "127.0.0.1:8080"
	}

	return nil
}

func isValidRestartPolicy(p string) bool {
	switch RestartPolicy(p) {
	case RestartAlways, RestartOnFailure, RestartNever:
		return true
	}
	return false
}

func expandEnvInConfig(cfg *Config) error {
	for name, svc := range cfg.Services {
		var err error

		svc.EXEC_CMD, err = expandEnvField(svc.EXEC_CMD, name, "EXEC_CMD")
		if err != nil {
			return err
		}
		svc.WORK_DIR, err = expandEnvField(svc.WORK_DIR, name, "WORK_DIR")
		if err != nil {
			return err
		}
		svc.STDOUT, err = expandEnvField(svc.STDOUT, name, "STDOUT")
		if err != nil {
			return err
		}
		svc.STDERR, err = expandEnvField(svc.STDERR, name, "STDERR")
		if err != nil {
			return err
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

func expandEnvField(val, name, field string) (string, error) {
	expanded, err := expandEnv(val)
	if err != nil {
		return "", fmt.Errorf("service '%s' %s: %w", name, field, err)
	}
	return expanded, nil
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
