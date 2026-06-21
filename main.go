package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
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
	action := "shutdown"
	service := ""
	if serviceName != "" {
		action = "stop"
		service = serviceName
	}

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

	resp, err := ipc.SendCommand(defaultSocketPath, ipc.ControlCommand{
		Action:  "restart",
		Service: &serviceName,
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

	var statuses map[string]struct {
		Name         string `json:"name"`
		Status       string `json:"status"`
		Pid          int    `json:"pid"`
		Uptime       int64  `json:"uptime"`
		RestartCount int    `json:"restartCount"`
	}

	if err := json.Unmarshal(resp.Data, &statuses); err != nil {
		return fmt.Errorf("failed to parse status: %w", err)
	}

	for _, st := range statuses {
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
