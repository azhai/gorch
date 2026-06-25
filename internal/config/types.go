package config

type Config struct {
	Services      map[string]ServiceConfig `toml:"services"`
	Web           WebConfig                `toml:"web"`
	LOG_DIR       string                   `toml:"LOG_DIR"`
	PID_FILE      string                   `toml:"PID_FILE"`
	SERVICES_LOCK string                   `toml:"SERVICES_LOCK"`
	topoOrder     []string
}

type ServiceConfig struct {
	WORK_DIR       string            `toml:"WORK_DIR"`
	EXEC_CMD       string            `toml:"EXEC_CMD"`
	RESTART_CMD    string            `toml:"RESTART_CMD"`
	RESTART_POLICY string            `toml:"RESTART_POLICY"`
	BACK_OFF       int               `toml:"BACK_OFF"`
	CHECK_PORT     int               `toml:"CHECK_PORT"`
	PRE_ACTION     string            `toml:"PRE_ACTION"`
	STDOUT         string            `toml:"STDOUT"`
	STDERR         string            `toml:"STDERR"`
	DEPENDS_ON     []string          `toml:"DEPENDS_ON"`
	CRON           string            `toml:"CRON"`
	ENV_VARS       map[string]string `toml:"ENV_VARS"`
	PID_FILE       string            `toml:"PID_FILE"`
}

type WebConfig struct {
	WEB_ENABLE  bool   `toml:"WEB_ENABLE"`
	WEB_ADDR    string `toml:"WEB_ADDR"`
	WEB_AUTH    bool   `toml:"WEB_AUTH"`
	WEB_USER    string `toml:"WEB_USER"`
	WEB_PASS    string `toml:"WEB_PASS"`
	TOTP_ENABLE bool   `toml:"TOTP_ENABLE"`
	TOTP_SECRET string `toml:"TOTP_SECRET"`
	TOTP_DB     string `toml:"TOTP_DB"`
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
