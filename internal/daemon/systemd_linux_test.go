//go:build linux

package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildUnitUsesServeWithConfig(t *testing.T) {
	mgr := &systemdManager{serviceName: DefaultServiceName}
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
	mgr := &systemdManager{serviceName: DefaultServiceName}
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

// --- merged from systemd_linux_more_test.go ---

func writeExecutable(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	script := "#!/bin/sh\nset -eu\n" + body + "\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile(%s) error = %v", name, err)
	}
	return path
}

func TestSystemdManagerLifecycleWithFakeSystemctl(t *testing.T) {
	home := t.TempDir()
	binDir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "systemctl.log")
	writeExecutable(t, binDir, "systemctl", `
printf '%s\n' "$*" >> "$SYSTEMCTL_LOG"
case " $* " in
  *" --user show feidex.service --no-page --property ActiveState,MainPID "*)
  printf 'ActiveState=active\nMainPID=123\n'
  ;;
esac
`)
	t.Setenv("HOME", home)
	t.Setenv("PATH", binDir)
	t.Setenv("SYSTEMCTL_LOG", logPath)

	manager, err := newPlatformManager(DefaultServiceName)
	if err != nil {
		t.Fatalf("newPlatformManager() error = %v", err)
	}

	cfg := Config{
		BinaryPath: "/opt/feidex/bin/feidex",
		ConfigPath: "/tmp/config.toml",
		WorkDir:    "/tmp",
	}
	if err := manager.Install(cfg); err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	status, err := manager.Status()
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if status == nil || !status.Installed || !status.Running || status.PID != 123 {
		t.Fatalf("Status() = %+v, want installed running pid=123", status)
	}
	if err := manager.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := manager.Stop(); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if err := manager.Restart(); err != nil {
		t.Fatalf("Restart() error = %v", err)
	}
	if err := manager.Uninstall(); err != nil {
		t.Fatalf("Uninstall() error = %v", err)
	}
	if status, err := manager.Status(); err != nil || status == nil || status.Installed {
		t.Fatalf("Status(after uninstall) = %+v, %v, want not installed", status, err)
	}

	logContent, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("ReadFile(log) error = %v", err)
	}
	text := string(logContent)
	for _, want := range []string{
		"--user daemon-reload",
		"--user enable feidex.service",
		"--user restart feidex.service",
		"--user start feidex.service",
		"--user stop feidex.service",
		"--user show feidex.service --no-page --property ActiveState,MainPID",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("systemctl log missing %q:\n%s", want, text)
		}
	}
}

func TestSystemdHelpersAndEnableLinger(t *testing.T) {
	if got := parseKeyValue("A=1\nB = 2\ninvalid"); got["A"] != "1" || got["B"] != "2" {
		t.Fatalf("parseKeyValue() = %+v", got)
	}
	if got := quoteSystemd(` a b\c `); !strings.HasPrefix(got, `"a b`) {
		t.Fatalf("quoteSystemd() = %q, want quoted string", got)
	}
	if got := quoteSystemd(""); got != `""` {
		t.Fatalf("quoteSystemd(empty) = %q, want empty quotes", got)
	}

	t.Setenv("XDG_RUNTIME_DIR", "/tmp/runtime")
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "unix:path=/tmp/runtime/bus")
	if got := userSystemdEnv(); len(got) != 0 {
		t.Fatalf("userSystemdEnv() = %+v, want preserved env with no additions", got)
	}

	noSystemctl := t.TempDir()
	t.Setenv("PATH", noSystemctl)
	if _, err := newPlatformManager(DefaultServiceName); err == nil || !strings.Contains(err.Error(), "systemctl not found") {
		t.Fatalf("newPlatformManager(no systemctl) error = %v", err)
	}

	binDir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "loginctl.log")
	writeExecutable(t, binDir, "loginctl", `
printf '%s\n' "$*" >> "$LOGINCTL_LOG"
if [ "${1:-}" = "show-user" ]; then
  if [ "${LOGINCTL_SHOW_RESULT:-yes}" = "error" ]; then
    echo boom >&2
    exit 1
  fi
  printf '%s\n' "${LOGINCTL_SHOW_RESULT:-yes}"
  exit 0
fi
exit 0
`)
	t.Setenv("PATH", binDir)
	t.Setenv("LOGINCTL_LOG", logPath)

	t.Setenv("USER", "")
	if err := EnableLingerCurrentUser(); err == nil {
		t.Fatal("expected missing USER to fail")
	}

	t.Setenv("USER", "tester")
	t.Setenv("LOGINCTL_SHOW_RESULT", "yes")
	if err := EnableLingerCurrentUser(); err != nil {
		t.Fatalf("EnableLingerCurrentUser(already enabled) error = %v", err)
	}

	t.Setenv("LOGINCTL_SHOW_RESULT", "no")
	if err := EnableLingerCurrentUser(); err != nil {
		t.Fatalf("EnableLingerCurrentUser(enable) error = %v", err)
	}
	content, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("ReadFile(loginctl.log) error = %v", err)
	}
	if !strings.Contains(string(content), "enable-linger tester") {
		t.Fatalf("loginctl log = %q, want enable-linger call", string(content))
	}
}
