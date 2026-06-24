package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"syscall"
	"time"

	"github.com/azhai/gorch/internal/common"
	"github.com/azhai/gorch/internal/config"
	"github.com/azhai/gorch/internal/ipc"
	"github.com/azhai/gorch/internal/supervisor"
	"github.com/spf13/cobra"
)

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
		supervisor.WithPidPath(cfg.PID_FILE),
		supervisor.WithServicesLock(cfg.SERVICES_LOCK),
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
	var service *string
	if serviceName != "" {
		service = &serviceName
	}

	type statusEntry struct {
		Name     string `json:"name"`
		Status   string `json:"status"`
		Pid      int    `json:"pid"`
		Uptime   int64  `json:"uptime"`
		MemoryMB int64  `json:"memoryMB"`
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
		mem := "-"
		if st.MemoryMB > 0 {
			mem = fmt.Sprintf("%dMB", st.MemoryMB)
		}
		fmt.Printf("%-20s %-12s %-8s %-12s %-10s\n", st.Name, st.Status, pid, uptime, mem)
	}

	fetchAndPrint := func() error {
		resp, err := ipc.SendCommand(defaultSocketPath, ipc.ControlCommand{
			Action:  "status",
			Service: service,
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

		fmt.Printf("%-20s %-12s %-8s %-12s %-10s\n", "NAME", "STATUS", "PID", "UPTIME", "MEMORY")
		fmt.Printf("%-20s %-12s %-8s %-12s %-10s\n", "----", "------", "---", "------", "------")

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
			names := make([]string, 0, len(statuses))
			for n := range statuses {
				names = append(names, n)
			}
			sort.Strings(names)
			for _, n := range names {
				printEntry(statuses[n])
			}
		}
		return nil
	}

	if !liveOutput {
		return fetchAndPrint()
	}

	// live refresh: clear screen and re-fetch every second until interrupted
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	common.SetupSignalHandler(func(sig os.Signal) {
		if sig == syscall.SIGHUP {
			return
		}
		cancel()
	})

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	clearScreen := func() {
		fmt.Print("\033[2J\033[H")
	}

	for {
		clearScreen()
		if err := fetchAndPrint(); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
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
