package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"

	"feidex/internal/buildinfo"
	"feidex/internal/config"
	"feidex/internal/daemon"
)

var (
	resolveDaemonConfig = daemon.Resolve
	enableLingerUser    = daemon.EnableLingerCurrentUser
	newDaemonManager    = daemon.NewManager
	runDaemonUpgrade    = daemon.RunUpgradeRunner
)

func runDaemon(args []string) int {
	if len(args) == 0 {
		printDaemonUsage()
		return 1
	}
	switch args[0] {
	case "install":
		return daemonInstall(args[1:])
	case "enable-linger":
		return daemonEnableLinger()
	case "uninstall":
		return daemonUninstall(args[1:])
	case "start":
		return daemonStart(args[1:])
	case "stop":
		return daemonStop(args[1:])
	case "restart":
		return daemonRestart(args[1:])
	case "status":
		return daemonStatus(args[1:])
	case "logs":
		return daemonLogs(args[1:])
	case "upgrade-runner":
		return daemonUpgradeRunner(args[1:])
	case "help", "--help", "-h":
		printDaemonUsage()
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unknown daemon command: %s\n\n", args[0])
		printDaemonUsage()
		return 1
	}
}

func daemonInstall(args []string) int {
	fs := flag.NewFlagSet("daemon install", flag.ContinueOnError)
	configPath := fs.String("config", "config.toml", "path to config file")
	force := fs.Bool("force", false, "reinstall even if service already exists")
	enableLinger := fs.Bool("enable-linger", false, "deprecated: linger is enabled by default during install")
	disableLinger := fs.Bool("disable-linger", false, "skip enabling loginctl linger for the current user before installing the service")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if *enableLinger && *disableLinger {
		fmt.Fprintln(os.Stderr, "cannot combine --enable-linger with --disable-linger")
		return 1
	}

	cfg, err := loadConfig(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load config: %v\n", err)
		return 1
	}
	daemonCfg := daemon.Config{ConfigPath: *configPath}
	if err := resolveDaemonConfig(&daemonCfg); err != nil {
		fmt.Fprintf(os.Stderr, "resolve daemon config: %v\n", err)
		return 1
	}
	if !*disableLinger {
		if err := enableLingerUser(); err != nil {
			fmt.Fprintf(os.Stderr, "enable linger failed: %v\n", err)
			fmt.Fprintln(os.Stderr, "hint: retry with --disable-linger if you do not want feidex to change the user linger setting")
			return 1
		}
	}
	mgr, err := newDaemonManager(cfg.Daemon.ServiceName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "daemon manager: %v\n", err)
		return 1
	}
	st, err := mgr.Status()
	if err != nil {
		fmt.Fprintf(os.Stderr, "daemon status: %v\n", err)
		return 1
	}
	if st != nil && st.Installed && !*force {
		fmt.Fprintln(os.Stderr, "service already installed; use --force to reinstall")
		return 1
	}
	if err := mgr.Install(daemonCfg); err != nil {
		fmt.Fprintf(os.Stderr, "daemon install failed: %v\n", err)
		return 1
	}
	fmt.Println("feidex daemon installed and started.")
	fmt.Println()
	fmt.Printf("  Platform:    %s\n", mgr.Platform())
	fmt.Printf("  Binary:      %s\n", daemonCfg.BinaryPath)
	fmt.Printf("  Config:      %s\n", daemonCfg.ConfigPath)
	fmt.Printf("  WorkDir:     %s\n", daemonCfg.WorkDir)
	if st != nil && st.UnitPath != "" {
		fmt.Printf("  Unit:        %s\n", st.UnitPath)
	} else {
		if fresh, statusErr := mgr.Status(); statusErr == nil && fresh != nil && fresh.UnitPath != "" {
			fmt.Printf("  Unit:        %s\n", fresh.UnitPath)
		}
	}
	fmt.Printf("  Log level:   %s\n", cfg.Log.Level)
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  feidex daemon status")
	fmt.Println("  feidex daemon restart")
	fmt.Println("  feidex daemon stop")
	fmt.Println("  feidex daemon uninstall")
	return 0
}

func daemonEnableLinger() int {
	if err := enableLingerUser(); err != nil {
		fmt.Fprintf(os.Stderr, "enable linger failed: %v\n", err)
		return 1
	}
	fmt.Println("linger enabled for current user.")
	return 0
}

func daemonUninstall(args []string) int {
	_, mgr, err := loadDaemonManager(args, "uninstall")
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return 1
	}
	if err := mgr.Uninstall(); err != nil {
		fmt.Fprintf(os.Stderr, "daemon uninstall failed: %v\n", err)
		return 1
	}
	fmt.Println("feidex daemon uninstalled.")
	return 0
}

func daemonStart(args []string) int {
	_, mgr, err := loadDaemonManager(args, "start")
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return 1
	}
	if err := requireInstalled(mgr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := mgr.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "daemon start failed: %v\n", err)
		return 1
	}
	fmt.Println("feidex daemon started.")
	return 0
}

func daemonStop(args []string) int {
	_, mgr, err := loadDaemonManager(args, "stop")
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return 1
	}
	if err := requireInstalled(mgr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := mgr.Stop(); err != nil {
		fmt.Fprintf(os.Stderr, "daemon stop failed: %v\n", err)
		return 1
	}
	fmt.Println("feidex daemon stopped.")
	return 0
}

func daemonRestart(args []string) int {
	_, mgr, err := loadDaemonManager(args, "restart")
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return 1
	}
	if err := requireInstalled(mgr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := mgr.Restart(); err != nil {
		fmt.Fprintf(os.Stderr, "daemon restart failed: %v\n", err)
		return 1
	}
	fmt.Println("feidex daemon restarted.")
	return 0
}

func daemonStatus(args []string) int {
	_, mgr, err := loadDaemonManager(args, "status")
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return 1
	}
	st, err := mgr.Status()
	if err != nil {
		fmt.Fprintf(os.Stderr, "daemon status failed: %v\n", err)
		return 1
	}
	fmt.Println("feidex daemon status")
	fmt.Println()
	fmt.Printf("  Version:     %s\n", buildinfo.CurrentVersion())
	if !st.Installed {
		fmt.Println("  Status:      Not installed")
		fmt.Printf("  Platform:    %s\n", st.Platform)
		fmt.Printf("  Unit:        %s\n", st.UnitPath)
		fmt.Println()
		fmt.Println("  Run: feidex daemon install")
		return 0
	}
	statusText := "Stopped"
	if st.Running {
		statusText = "Running"
	}
	fmt.Printf("  Status:      %s\n", statusText)
	fmt.Printf("  Platform:    %s\n", st.Platform)
	fmt.Printf("  Unit:        %s\n", st.UnitPath)
	if st.PID > 0 {
		fmt.Printf("  PID:         %d\n", st.PID)
	}
	return 0
}

func daemonLogs(args []string) int {
	fs := flag.NewFlagSet("daemon logs", flag.ContinueOnError)
	configPath := fs.String("config", "config.toml", "path to config file")
	lines := fs.Int("n", 50, "number of log lines to show")
	follow := fs.Bool("f", false, "follow log output")
	if err := fs.Parse(args); err != nil {
		return 1
	}

	cfg, err := loadConfig(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load config: %v\n", err)
		return 1
	}
	mgr, err := newDaemonManager(cfg.Daemon.ServiceName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "daemon manager: %v\n", err)
		return 1
	}

	// Platforms that log to a file (macOS launchd) are tailed directly;
	// Linux logs live in systemd journald and are read with journalctl.
	if logFile := mgr.LogFile(); logFile != "" {
		return tailDaemonLogFile(logFile, *lines, *follow)
	}

	unitName := daemon.NormalizeServiceName(cfg.Daemon.ServiceName) + ".service"
	if _, err := exec.LookPath("journalctl"); err != nil {
		fmt.Fprintf(os.Stderr, "journalctl not found; daemon logs are managed by systemd journald on Linux\n")
		fmt.Fprintf(os.Stderr, "You can view logs manually with: journalctl --user -u %s\n", unitName)
		return 1
	}

	journalArgs := []string{"--user", "-u", unitName, "-n", fmt.Sprintf("%d", *lines)}
	if *follow {
		journalArgs = append(journalArgs, "-f")
	}

	cmd := exec.Command("journalctl", journalArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "journalctl failed: %v\n", err)
		return 1
	}
	return 0
}

func tailDaemonLogFile(path string, lines int, follow bool) int {
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "log file not found yet: %s\n", path)
			fmt.Fprintln(os.Stderr, "(it appears once the daemon has started and produced output)")
			return 1
		}
		fmt.Fprintf(os.Stderr, "stat log file: %v\n", err)
		return 1
	}
	tailArgs := []string{"-n", fmt.Sprintf("%d", lines)}
	if follow {
		tailArgs = append(tailArgs, "-f")
	}
	tailArgs = append(tailArgs, path)
	cmd := exec.Command("tail", tailArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "tail failed: %v\n", err)
		return 1
	}
	return 0
}

func daemonUpgradeRunner(args []string) int {
	fs := flag.NewFlagSet("daemon upgrade-runner", flag.ContinueOnError)
	binaryPath := fs.String("binary-path", "", "installed daemon binary path")
	serviceName := fs.String("service-name", daemon.DefaultServiceName, "daemon service name")
	version := fs.String("version", "", "target version")
	downloadURL := fs.String("download-url", "", "binary download URL")
	sourcePath := fs.String("source-path", "", "local staged binary path")
	expectedSHA256 := fs.String("expected-sha256", "", "expected SHA256 of the downloaded binary")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if err := runDaemonUpgrade(context.Background(), daemon.UpgradeSpec{
		ServiceName:    *serviceName,
		Version:        *version,
		BinaryPath:     *binaryPath,
		DownloadURL:    *downloadURL,
		SourcePath:     *sourcePath,
		ExpectedSHA256: *expectedSHA256,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "daemon upgrade runner failed: %v\n", err)
		return 1
	}
	return 0
}

func requireInstalled(mgr daemon.Manager) error {
	st, err := mgr.Status()
	if err != nil {
		return err
	}
	if st == nil || !st.Installed {
		return fmt.Errorf("service is not installed; run: feidex daemon install")
	}
	return nil
}

func printDaemonUsage() {
	fmt.Println(`Usage:
  feidex daemon install [--config config.toml] [--force] [--disable-linger]
  feidex daemon enable-linger
  feidex daemon uninstall [--config config.toml]
  feidex daemon start [--config config.toml]
  feidex daemon stop [--config config.toml]
  feidex daemon restart [--config config.toml]
  feidex daemon status [--config config.toml]
  feidex daemon logs [-n LINES] [-f] [--config config.toml]`)
}

func loadDaemonManager(args []string, command string) (*config.Config, daemon.Manager, error) {
	fs := flag.NewFlagSet("daemon "+command, flag.ContinueOnError)
	configPath := fs.String("config", "config.toml", "path to config file")
	if err := fs.Parse(args); err != nil {
		return nil, nil, err
	}
	cfg, err := loadConfig(*configPath)
	if err != nil {
		return nil, nil, fmt.Errorf("load config: %w", err)
	}
	mgr, err := newDaemonManager(cfg.Daemon.ServiceName)
	if err != nil {
		return nil, nil, fmt.Errorf("daemon manager: %w", err)
	}
	return cfg, mgr, nil
}
