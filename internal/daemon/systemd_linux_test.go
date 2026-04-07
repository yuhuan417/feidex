//go:build linux

package daemon

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestBuildUnitUsesServeWithConfig(t *testing.T) {
	mgr := &systemdManager{}
	unit := mgr.buildUnit(Config{
		BinaryPath: "/opt/feidex/bin/feidex",
		ConfigPath: "/home/tester/config.toml",
		WorkDir:    "/home/tester",
		EnvPATH:    "/usr/local/bin:/usr/bin",
		HomeDir:    "/home/tester",
	})
	for _, want := range []string{
		"Description=feidex - Feishu Codex Bridge",
		"ExecStart=/opt/feidex/bin/feidex serve --config /home/tester/config.toml",
		"WorkingDirectory=/home/tester",
		"Environment=PATH=/usr/local/bin:/usr/bin",
		"Environment=HOME=/home/tester",
		"WantedBy=default.target",
	} {
		if !strings.Contains(unit, want) {
			t.Fatalf("unit missing %q:\n%s", want, unit)
		}
	}
}

func TestBuildUnitQuotesPathsWithSpaces(t *testing.T) {
	mgr := &systemdManager{}
	unit := mgr.buildUnit(Config{
		BinaryPath: "/opt/feidex bin/feidex",
		ConfigPath: "/home/test user/config.toml",
		WorkDir:    "/home/test user",
		EnvPATH:    "/usr/local/bin:/mnt/c/Program Files/Go/bin",
		HomeDir:    "/home/test user",
	})
	for _, want := range []string{
		`ExecStart="/opt/feidex bin/feidex" serve --config "/home/test user/config.toml"`,
		`WorkingDirectory="/home/test user"`,
		`Environment="PATH=/usr/local/bin:/mnt/c/Program Files/Go/bin"`,
		`Environment="HOME=/home/test user"`,
	} {
		if !strings.Contains(unit, want) {
			t.Fatalf("unit missing quoted value %q:\n%s", want, unit)
		}
	}
}

func TestUserSystemdEnvFillsMissingRuntimeAndBus(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", "")
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "")
	env := userSystemdEnv()
	wantRuntime := fmt.Sprintf("XDG_RUNTIME_DIR=/run/user/%d", os.Getuid())
	wantBus := fmt.Sprintf("DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/%d/bus", os.Getuid())
	if len(env) != 2 || env[0] != wantRuntime || env[1] != wantBus {
		t.Fatalf("unexpected systemd env: %#v", env)
	}
}
