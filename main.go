package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"

	"github.com/azhai/gorch/internal/common"
	"github.com/azhai/gorch/internal/config"
	"github.com/azhai/gorch/internal/ipc"
	"github.com/azhai/gorch/internal/supervisor"
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

var defaultSocketPath = "/tmp/gorch.sock"

func main() {
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

func startCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start services",
		RunE:  runStart,
	}

	cmd.Flags().BoolVarP(&daemonize, "daemonize", "d", false, "run as daemon")
	cmd.Flags().StringVarP(&serviceName, "service", "s", "", "specific service to start")

	return cmd
}

func runStart(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	if daemonize {
		pid, err := supervisor.Daemonize()
		if err != nil {
			return fmt.Errorf("failed to daemonize: %w", err)
		}
		fmt.Printf("supervisor started as daemon (pid: %d)\n", pid)
		return nil
	}

	sup := supervisor.NewSupervisor(cfg,
		supervisor.WithPidPath("/tmp/gorch.pid"),
		supervisor.WithSocketPath(defaultSocketPath),
		supervisor.WithConfigPath(configPath),
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	common.SetupSignalHandler(func(sig os.Signal) {
		if sig == syscall.SIGHUP {
			sup.HandleReload()
			return
		}
		cancel()
	})

	return sup.Start(ctx)
}

func stopCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "stop",
		Short: "Stop services",
		RunE:  runStop,
	}

	cmd.Flags().StringVarP(&serviceName, "service", "s", "", "specific service to stop")

	return cmd
}

func runStop(cmd *cobra.Command, args []string) error {
	return ipcAction("shutdown", serviceName)
}

func restartCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "restart",
		Short: "Restart services",
		RunE:  runRestart,
	}

	cmd.Flags().StringVarP(&serviceName, "service", "s", "", "specific service to restart")

	return cmd
}

func runRestart(cmd *cobra.Command, args []string) error {
	if serviceName == "" {
		return fmt.Errorf("service name required for restart")
	}
	return ipcAction("restart", serviceName)
}

func ipcAction(action, service string) error {
	resp, err := ipc.SendCommand(defaultSocketPath, ipc.ControlCommand{
		Action:  action,
		Service: &service,
	})
	if err != nil {
		return err
	}
	if resp.Status != "ok" {
		return fmt.Errorf("%s", resp.Message)
	}
	fmt.Println(resp.Message)
	return nil
}

func statusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show service status",
		RunE:  runStatus,
	}

	cmd.Flags().StringVarP(&serviceName, "service", "s", "", "specific service")
	cmd.Flags().BoolVarP(&jsonOutput, "json", "j", false, "JSON output")
	cmd.Flags().BoolVarP(&liveOutput, "live", "l", false, "live refresh")

	return cmd
}

func runStatus(cmd *cobra.Command, args []string) error {
	resp, err := ipc.SendCommand(defaultSocketPath, ipc.ControlCommand{
		Action:  "status",
		Service: &serviceName,
	})
	if err != nil {
		return err
	}

	if resp.Status != "ok" {
		return fmt.Errorf("%s", resp.Message)
	}

	if jsonOutput {
		fmt.Println(string(resp.Data))
		return nil
	}

	fmt.Printf("%-20s %-12s %-8s %-12s %-8s\n", "NAME", "STATUS", "PID", "UPTIME", "RESTARTS")
	fmt.Printf("%-20s %-12s %-8s %-12s %-8s\n", "----", "------", "---", "------", "--------")

	type statusEntry struct {
		Name         string `json:"name"`
		Status       string `json:"status"`
		Pid          int    `json:"pid"`
		Uptime       int64  `json:"uptime"`
		RestartCount int    `json:"restartCount"`
	}

	printEntry := func(st statusEntry) {
		pid := "-"
		if st.Pid > 0 {
			pid = fmt.Sprintf("%d", st.Pid)
		}
		uptime := "-"
		if st.Uptime > 0 {
			uptime = fmt.Sprintf("%ds", st.Uptime)
		}
		fmt.Printf("%-20s %-12s %-8s %-12s %-8d\n", st.Name, st.Status, pid, uptime, st.RestartCount)
	}

	if serviceName != "" {
		// single service: response is a single object, not a map
		var st statusEntry
		if err := json.Unmarshal(resp.Data, &st); err != nil {
			return fmt.Errorf("failed to parse status: %w", err)
		}
		printEntry(st)
	} else {
		var statuses map[string]statusEntry
		if err := json.Unmarshal(resp.Data, &statuses); err != nil {
			return fmt.Errorf("failed to parse status: %w", err)
		}
		for _, st := range statuses {
			printEntry(st)
		}
	}

	return nil
}

func logsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "logs",
		Short: "Show service logs",
		RunE:  runLogs,
	}

	cmd.Flags().StringVarP(&serviceName, "service", "s", "", "service name")
	cmd.Flags().IntVarP(&lines, "lines", "n", 100, "number of lines")
	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "follow log output")
	cmd.Flags().BoolVarP(&purge, "purge", "P", false, "purge log files")

	return cmd
}

func runLogs(cmd *cobra.Command, args []string) error {
	if purge {
		_, err := ipc.SendCommand(defaultSocketPath, ipc.ControlCommand{
			Action: "logs",
		})
		if err != nil {
			return err
		}
		fmt.Println("logs purged")
		return nil
	}

	if serviceName == "" {
		return fmt.Errorf("service name required")
	}

	resp, err := ipc.SendCommand(defaultSocketPath, ipc.ControlCommand{
		Action:  "logs",
		Service: &serviceName,
		Lines:   lines,
		Follow:  follow,
	})
	if err != nil {
		return err
	}

	if resp.Status != "ok" {
		return fmt.Errorf("%s", resp.Message)
	}

	fmt.Println(string(resp.Data))
	return nil
}

// ── Install / Uninstall commands ──────────────────────────

var installUser bool

func installCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install as system service (systemd/launchd)",
		Long:  "Install gorch as a system service. On Linux this creates a systemd unit file and enables it. On macOS this creates a launchd plist.",
		RunE:  runInstall,
	}

	cmd.Flags().BoolVarP(&installUser, "user", "u", false, "install as user service (not system-wide)")

	return cmd
}

func runInstall(cmd *cobra.Command, args []string) error {
	binPath, err := resolveBinPath()
	if err != nil {
		return fmt.Errorf("failed to detect binary path: %w", err)
	}

	cfgPath, err := filepath.Abs(configPath)
	if err != nil {
		return fmt.Errorf("failed to resolve config path: %w", err)
	}

	switch runtime.GOOS {
	case "linux":
		return installSystemd(binPath, cfgPath, installUser)
	case "darwin":
		return installLaunchd(binPath, cfgPath)
	default:
		return fmt.Errorf("unsupported OS: %s (only linux/darwin supported)", runtime.GOOS)
	}
}

func uninstallCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Remove system service (systemd/launchd)",
		Long:  "Remove the gorch system service installed by 'gorch install'.",
		RunE:  runUninstall,
	}

	cmd.Flags().BoolVarP(&installUser, "user", "u", false, "uninstall user service")

	return cmd
}

func runUninstall(cmd *cobra.Command, args []string) error {
	switch runtime.GOOS {
	case "linux":
		return uninstallSystemd(installUser)
	case "darwin":
		return uninstallLaunchd()
	default:
		return fmt.Errorf("unsupported OS: %s", runtime.GOOS)
	}
}

// ── Binary path detection ─────────────────────────────────

func resolveBinPath() (string, error) {
	self, err := os.Executable()
	if err != nil {
		return "", err
	}
	abs, err := filepath.EvalSymlinks(self)
	if err != nil {
		return self, nil
	}
	return abs, nil
}

// ── Linux systemd ─────────────────────────────────────────

const systemdServiceName = "gorch.service"

func systemdUnitPath(user bool) string {
	if user {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, ".config/systemd/user", systemdServiceName)
	}
	return "/etc/systemd/system/" + systemdServiceName
}

func installSystemd(binPath, cfgPath string, user bool) error {
	unitPath := systemdUnitPath(user)

	unit := fmt.Sprintf(`[Unit]
Description=gorch process supervisor
After=network.target

[Service]
Type=simple
ExecStart=%s start -c %s
Restart=on-failure
PIDFile=/tmp/gorch.pid

[Install]
WantedBy=%s
`, binPath, cfgPath, wantedByTarget(user))

	if err := os.MkdirAll(filepath.Dir(unitPath), 0755); err != nil {
		return fmt.Errorf("failed to create systemd dir: %w", err)
	}
	if err := os.WriteFile(unitPath, []byte(unit), 0644); err != nil {
		return fmt.Errorf("failed to write unit file: %w", err)
	}

	fmt.Println("service file written:", unitPath)

	// Reload daemon and enable+start
	systemctlArgs := []string{"daemon-reload"}
	if user {
		systemctlArgs = append(systemctlArgs, "--user")
	}
	if err := runCommand("systemctl", systemctlArgs...); err != nil {
		fmt.Fprintf(os.Stderr, "warning: systemctl daemon-reload: %v\n", err)
	}

	enableArgs := []string{"enable", "--now", systemdServiceName}
	if user {
		enableArgs = append([]string{"--user"}, enableArgs...)
	}
	if err := runCommand("systemctl", enableArgs...); err != nil {
		return fmt.Errorf("failed to enable service: %w", err)
	}

	fmt.Printf("gorch installed and started as %s service\n", serviceScope(user))
	return nil
}

func uninstallSystemd(user bool) error {
	unitPath := systemdUnitPath(user)

	stopArgs := []string{"stop", systemdServiceName}
	if user {
		stopArgs = append([]string{"--user"}, stopArgs...)
	}
	runCommand("systemctl", stopArgs...) // ignore errors if not running

	disableArgs := []string{"disable", systemdServiceName}
	if user {
		disableArgs = append([]string{"--user"}, disableArgs...)
	}
	runCommand("systemctl", disableArgs...) // ignore errors

	if err := os.Remove(unitPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove unit file: %w", err)
	}

	reloadArgs := []string{"daemon-reload"}
	if user {
		reloadArgs = append(reloadArgs, "--user")
	}
	runCommand("systemctl", reloadArgs...)

	fmt.Printf("gorch uninstalled (%s)\n", serviceScope(user))
	return nil
}

func wantedByTarget(user bool) string {
	if user {
		return "default.target"
	}
	return "multi-user.target"
}

func serviceScope(user bool) string {
	if user {
		return "user"
	}
	return "system"
}

// ── macOS launchd ──────────────────────────────────────────

const launchdLabel = "com.github.azhai.gorch"

func launchdPlistPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library/LaunchAgents", launchdLabel+".plist")
}

func installLaunchd(binPath, cfgPath string) error {
	plistPath := launchdPlistPath()

	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>%s</string>
    <key>ProgramArguments</key>
    <array>
        <string>%s</string>
        <string>start</string>
        <string>-c</string>
        <string>%s</string>
    </array>
    <key>KeepAlive</key>
    <true/>
    <key>RunAtLoad</key>
    <true/>
    <key>StandardOutPath</key>
    <string>/var/log/gorch/stdout.log</string>
    <key>StandardErrorPath</key>
    <string>/var/log/gorch/stderr.log</string>
</dict>
</plist>
`, launchdLabel, binPath, cfgPath)

	if err := os.MkdirAll(filepath.Dir(plistPath), 0755); err != nil {
		return fmt.Errorf("failed to create LaunchAgents dir: %w", err)
	}
	if err := os.WriteFile(plistPath, []byte(plist), 0644); err != nil {
		return fmt.Errorf("failed to write plist: %w", err)
	}

	fmt.Println("plist written:", plistPath)

	if err := runCommand("launchctl", "load", plistPath); err != nil {
		return fmt.Errorf("failed to load launchd service: %w", err)
	}

	fmt.Println("gorch installed and started as launchd service")
	return nil
}

func uninstallLaunchd() error {
	plistPath := launchdPlistPath()

	runCommand("launchctl", "unload", plistPath) // ignore errors

	if err := os.Remove(plistPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove plist: %w", err)
	}

	fmt.Println("gorch uninstalled (launchd)")
	return nil
}

// ── Helpers ────────────────────────────────────────────────

func runCommand(name string, args ...string) error {
	c := exec.Command(name, args...)
	c.Stdout = os.Stderr
	c.Stderr = os.Stderr
	return c.Run()
}
