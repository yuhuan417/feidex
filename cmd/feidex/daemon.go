package main

import (
	"flag"
	"fmt"
	"os"

	"feidex/internal/daemon"
)

var (
	resolveDaemonConfig = daemon.Resolve
	enableLingerUser    = daemon.EnableLingerCurrentUser
	newDaemonManager    = daemon.NewManager
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
		return daemonUninstall()
	case "start":
		return daemonStart()
	case "stop":
		return daemonStop()
	case "restart":
		return daemonRestart()
	case "status":
		return daemonStatus()
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
	enableLinger := fs.Bool("enable-linger", false, "enable loginctl linger for the current user before installing the service")
	if err := fs.Parse(args); err != nil {
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
	if *enableLinger {
		if err := enableLingerUser(); err != nil {
			fmt.Fprintf(os.Stderr, "enable linger failed: %v\n", err)
			return 1
		}
	}
	mgr, err := newDaemonManager()
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
		if !*enableLinger {
			fmt.Fprintln(os.Stderr, "hint: if this is an SSH/non-login session, retry with --enable-linger")
		}
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

func daemonUninstall() int {
	mgr, err := newDaemonManager()
	if err != nil {
		fmt.Fprintf(os.Stderr, "daemon manager: %v\n", err)
		return 1
	}
	if err := mgr.Uninstall(); err != nil {
		fmt.Fprintf(os.Stderr, "daemon uninstall failed: %v\n", err)
		return 1
	}
	fmt.Println("feidex daemon uninstalled.")
	return 0
}

func daemonStart() int {
	mgr, err := newDaemonManager()
	if err != nil {
		fmt.Fprintf(os.Stderr, "daemon manager: %v\n", err)
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

func daemonStop() int {
	mgr, err := newDaemonManager()
	if err != nil {
		fmt.Fprintf(os.Stderr, "daemon manager: %v\n", err)
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

func daemonRestart() int {
	mgr, err := newDaemonManager()
	if err != nil {
		fmt.Fprintf(os.Stderr, "daemon manager: %v\n", err)
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

func daemonStatus() int {
	mgr, err := newDaemonManager()
	if err != nil {
		fmt.Fprintf(os.Stderr, "daemon manager: %v\n", err)
		return 1
	}
	st, err := mgr.Status()
	if err != nil {
		fmt.Fprintf(os.Stderr, "daemon status failed: %v\n", err)
		return 1
	}
	fmt.Println("feidex daemon status")
	fmt.Println()
	if !st.Installed {
		fmt.Println("  Status:      Not installed")
		fmt.Printf("  Platform:    %s\n", st.Platform)
		fmt.Printf("  Unit:        %s\n", st.UnitPath)
		fmt.Println()
		fmt.Println("  Run: feidex daemon install --config config.toml")
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

func requireInstalled(mgr daemon.Manager) error {
	st, err := mgr.Status()
	if err != nil {
		return err
	}
	if st == nil || !st.Installed {
		return fmt.Errorf("service is not installed; run: feidex daemon install --config config.toml")
	}
	return nil
}

func printDaemonUsage() {
	fmt.Println(`Usage:
  feidex daemon install [--config config.toml] [--force] [--enable-linger]
  feidex daemon enable-linger
  feidex daemon uninstall
  feidex daemon start
  feidex daemon stop
  feidex daemon restart
  feidex daemon status`)
}
