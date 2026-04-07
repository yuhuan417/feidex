package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"feidex/internal/app"
	"feidex/internal/config"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) == 0 {
		return runServe(nil)
	}
	switch args[0] {
	case "serve", "run":
		return runServe(args[1:])
	case "daemon":
		return runDaemon(args[1:])
	case "feishu":
		return runFeishu(args[1:])
	case "version", "--version", "-v":
		fmt.Println("feidex 0.1.0")
		return 0
	case "help", "--help", "-h":
		printUsage()
		return 0
	default:
		printUsage()
		return 1
	}
}

func runServe(args []string) int {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	configPath := fs.String("config", "config.toml", "path to config file")
	if err := fs.Parse(args); err != nil {
		return 1
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load config: %v\n", err)
		return 1
	}

	if cfg.DataDir == "" {
		cfg.DataDir = filepath.Join(filepath.Dir(*configPath), ".feidex-data")
	}
	logLevel, err := config.ParseLogLevel(cfg.Log.Level)
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid log level: %v\n", err)
		return 1
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: logLevel}))
	slog.SetDefault(logger)
	slog.Info("service starting", "config_path", *configPath, "data_dir", cfg.DataDir, "log_level", cfg.Log.Level)

	svc, err := app.New(cfg, *configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "init service: %v\n", err)
		return 1
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := svc.Start(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "start service: %v\n", err)
		return 1
	}
	slog.Info("service started")
	<-ctx.Done()
	slog.Info("service stopping")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), config.DefaultShutdownTimeout)
	defer shutdownCancel()
	if err := svc.Stop(shutdownCtx); err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintf(os.Stderr, "stop service: %v\n", err)
		return 1
	}
	slog.Info("service stopped")
	return 0
}

func printUsage() {
	fmt.Println(`Usage:
  feidex serve [--config config.toml]
  feidex daemon <install|uninstall|start|stop|restart|status>
  feidex feishu setup [options]
  feidex feishu new [options]
  feidex feishu bind [options]
  feidex version`)
}
