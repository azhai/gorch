package main

import (
	"log/slog"
	"os"

	"github.com/spf13/cobra"
)

var (
	configPath  string
	daemonize   bool
	serviceName string
	jsonOutput  bool
	liveOutput  bool
	lines       int
	follow      bool
	purge       bool
)

var defaultSocketPath = "/var/run/gorch.sock"

// setupLogger configures slog level based on GORCH_MODE env var.
// dev mode → debug level; prod/empty → info level.
func setupLogger() {
	var level slog.Level
	if os.Getenv("GORCH_MODE") == "dev" {
		level = slog.LevelDebug
	} else {
		level = slog.LevelInfo
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})))
}

func main() {
	setupLogger()

	rootCmd := &cobra.Command{
		Use:   "gorch",
		Short: "gorch - process supervisor",
	}

	rootCmd.PersistentFlags().StringVarP(&configPath, "config", "c", "gorch.toml", "config file path")

	rootCmd.AddCommand(startCmd())
	rootCmd.AddCommand(stopCmd())
	rootCmd.AddCommand(restartCmd())
	rootCmd.AddCommand(statusCmd())
	rootCmd.AddCommand(logsCmd())
	rootCmd.AddCommand(installCmd())
	rootCmd.AddCommand(uninstallCmd())

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
