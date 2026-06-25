package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/spf13/cobra"
)

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
PIDFile=/var/run/gorch.pid

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
