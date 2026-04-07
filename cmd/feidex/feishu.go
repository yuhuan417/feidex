package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"feidex/internal/config"
)

var setupFeishu = config.SetupFeishu

func runFeishu(args []string) int {
	if len(args) == 0 {
		printFeishuUsage()
		return 0
	}
	switch args[0] {
	case "setup":
		return runFeishuSetup(config.FeishuSetupAuto, args[1:])
	case "new":
		return runFeishuSetup(config.FeishuSetupNew, args[1:])
	case "bind":
		return runFeishuSetup(config.FeishuSetupBind, args[1:])
	case "help", "--help", "-h":
		printFeishuUsage()
		return 0
	default:
		printFeishuUsage()
		return 1
	}
}

func runFeishuSetup(mode config.FeishuSetupMode, args []string) int {
	fs := flag.NewFlagSet("feishu", flag.ContinueOnError)
	configPath := fs.String("config", "config.toml", "path to config file")
	workspace := fs.String("workspace", "", "workspace id to create when config does not exist")
	appPair := fs.String("app", "", "existing app_id:app_secret")
	appID := fs.String("app-id", "", "existing app id")
	appSecret := fs.String("app-secret", "", "existing app secret")
	timeout := fs.Int("timeout", 600, "registration timeout in seconds")
	qrImage := fs.String("qr-image", "", "optional PNG path for saved QR code")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if err := setupFeishu(mode, config.FeishuSetupOptions{
		ConfigPath: *configPath,
		Workspace:  *workspace,
		AppPair:    *appPair,
		AppID:      *appID,
		AppSecret:  *appSecret,
		Timeout:    time.Duration(*timeout) * time.Second,
		QRImage:    *qrImage,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "feishu setup failed: %v\n", err)
		return 1
	}
	return 0
}

func printFeishuUsage() {
	fmt.Println(`Usage:
  feidex feishu setup [--config config.toml]
  feidex feishu new [--config config.toml]
  feidex feishu bind --app app_id:app_secret`)
}
