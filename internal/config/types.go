package config

type Config struct {
	Services  map[string]ServiceConfig `toml:"services"`
	Web       WebConfig                `toml:"web"`
	LOG_DIR   string                   `toml:"LOG_DIR"`
	topoOrder []string
}

type ServiceConfig struct {
	WORK_DIR       string            `toml:"WORK_DIR"`
	EXEC_CMD       string            `toml:"EXEC_CMD"`
	RESTART_POLICY string            `toml:"RESTART_POLICY"`
	BACK_OFF       int               `toml:"BACK_OFF"`
	STDOUT         string            `toml:"STDOUT"`
	STDERR         string            `toml:"STDERR"`
	DEPENDS_ON     []string          `toml:"DEPENDS_ON"`
	CRON           string            `toml:"CRON"`
	ENV_VARS       map[string]string `toml:"ENV_VARS"`
}

type WebConfig struct {
	WEB_ENABLE bool   `toml:"WEB_ENABLE"`
	WEB_ADDR   string `toml:"WEB_ADDR"`
	WEB_AUTH   bool   `toml:"WEB_AUTH"`
	WEB_USER   string `toml:"WEB_USER"`
	WEB_PASS   string `toml:"WEB_PASS"`
}

type RestartPolicy string

const (
	RestartAlways    RestartPolicy = "always"
	RestartOnFailure RestartPolicy = "on-failure"
	RestartNever     RestartPolicy = "never"
)

type StatusCode string

const (
	StatusRunning  StatusCode = "running"
	StatusStopped  StatusCode = "stopped"
	StatusFailed   StatusCode = "failed"
	StatusCrashed  StatusCode = "crashed"
	StatusStarting StatusCode = "starting"
)
