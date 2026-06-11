//go:build darwin

package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeExecutable(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	script := "#!/bin/sh\nset -eu\n" + body + "\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile(%s) error = %v", name, err)
	}
	return path
}

func TestBuildPlistContent(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	mgr := &launchdManager{serviceName: DefaultServiceName}
	plist := mgr.buildPlist(Config{
		BinaryPath: "/opt/feidex/bin/feidex",
		ConfigPath: "/Users/tester/config.toml",
		WorkDir:    "/Users/tester",
		EnvPATH:    "/usr/local/bin:/usr/bin",
		HomeDir:    "/Users/tester",
	})
	for _, want := range []string{
		"<key>Label</key>\n\t<string>feidex</string>",
		"<key>ProgramArguments</key>",
		"<string>/opt/feidex/bin/feidex</string>",
		"<string>serve</string>",
		"<string>--config</string>",
		"<string>/Users/tester/config.toml</string>",
		"<key>WorkingDirectory</key>\n\t<string>/Users/tester</string>",
		"<key>PATH</key>\n\t\t<string>/usr/local/bin:/usr/bin</string>",
		"<key>HOME</key>\n\t\t<string>/Users/tester</string>",
		"<key>RunAtLoad</key>\n\t<true/>",
		"<key>KeepAlive</key>\n\t<true/>",
		"<key>ProcessType</key>\n\t<string>Background</string>",
		"Library/Logs/feidex/feidex.log",
	} {
		if !strings.Contains(plist, want) {
			t.Fatalf("plist missing %q:\n%s", want, plist)
		}
	}
}

func TestBuildPlistEscapesXML(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	mgr := &launchdManager{serviceName: DefaultServiceName}
	plist := mgr.buildPlist(Config{
		BinaryPath: "/opt/a&b/feidex",
		ConfigPath: "/Users/x <y>/config.toml",
		WorkDir:    "/Users/x <y>",
	})
	for _, want := range []string{
		"<string>/opt/a&amp;b/feidex</string>",
		"<string>/Users/x &lt;y&gt;/config.toml</string>",
	} {
		if !strings.Contains(plist, want) {
			t.Fatalf("plist missing escaped value %q:\n%s", want, plist)
		}
	}
}

func TestParseLaunchctlPrint(t *testing.T) {
	out := "gui/501/feidex = {\n\tstate = running\n\tpid = 456\n}\n"
	running, pid := parseLaunchctlPrint(out)
	if !running || pid != 456 {
		t.Fatalf("parseLaunchctlPrint() = running=%v pid=%d, want true/456", running, pid)
	}
	running, pid = parseLaunchctlPrint("gui/501/feidex = {\n\tstate = not running\n}\n")
	if running || pid != 0 {
		t.Fatalf("parseLaunchctlPrint(stopped) = running=%v pid=%d, want false/0", running, pid)
	}
}

func TestLaunchdLifecycleWithFakeLaunchctl(t *testing.T) {
	home := t.TempDir()
	binDir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "launchctl.log")
	writeExecutable(t, binDir, "launchctl", `
printf '%s\n' "$*" >> "$LAUNCHCTL_LOG"
case " $* " in
  *" print "*)
  printf 'gui/0/feidex = {\n\tstate = running\n\tpid = 456\n}\n'
  ;;
esac
`)
	t.Setenv("HOME", home)
	t.Setenv("PATH", binDir)
	t.Setenv("LAUNCHCTL_LOG", logPath)

	manager, err := newPlatformManager(DefaultServiceName)
	if err != nil {
		t.Fatalf("newPlatformManager() error = %v", err)
	}

	cfg := Config{BinaryPath: "/opt/feidex/bin/feidex", ConfigPath: "/tmp/config.toml", WorkDir: "/tmp"}
	if err := manager.Install(cfg); err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	// plist file written under fake HOME
	plistPath := filepath.Join(home, "Library", "LaunchAgents", "feidex.plist")
	if _, err := os.Stat(plistPath); err != nil {
		t.Fatalf("plist not written: %v", err)
	}

	status, err := manager.Status()
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if status == nil || !status.Installed || !status.Running || status.PID != 456 {
		t.Fatalf("Status() = %+v, want installed running pid=456", status)
	}
	if manager.LogFile() == "" {
		t.Fatal("LogFile() should be non-empty on macOS")
	}
	for _, step := range []func() error{manager.Start, manager.Stop, manager.Restart} {
		if err := step(); err != nil {
			t.Fatalf("lifecycle step error = %v", err)
		}
	}
	if err := manager.Uninstall(); err != nil {
		t.Fatalf("Uninstall() error = %v", err)
	}
	if st, err := manager.Status(); err != nil || st == nil || st.Installed {
		t.Fatalf("Status(after uninstall) = %+v, %v, want not installed", st, err)
	}

	logContent, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("ReadFile(log) error = %v", err)
	}
	text := string(logContent)
	uid := os.Getuid()
	for _, want := range []string{
		fmt.Sprintf("bootstrap gui/%d", uid),
		fmt.Sprintf("kickstart -k gui/%d/feidex", uid),
		fmt.Sprintf("kickstart gui/%d/feidex", uid),
		fmt.Sprintf("bootout gui/%d/feidex", uid),
		fmt.Sprintf("print gui/%d/feidex", uid),
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("launchctl log missing %q:\n%s", want, text)
		}
	}
}

func TestEnableLingerNoopOnDarwin(t *testing.T) {
	if err := EnableLingerCurrentUser(); err != nil {
		t.Fatalf("EnableLingerCurrentUser() on darwin = %v, want nil", err)
	}
}
