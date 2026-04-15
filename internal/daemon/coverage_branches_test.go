//go:build linux

package daemon

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSystemdLifecycleErrorBranches(t *testing.T) {
	home := t.TempDir()
	binDir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "systemctl-error.log")
	writeExecutable(t, binDir, "systemctl", `
printf '%s\n' "$*" >> "$SYSTEMCTL_LOG"
case " $* " in
  *" --user start feidex.service "*|*" --user stop feidex.service "*|*" --user restart feidex.service "*)
    echo failed >&2
    exit 1
  ;;
esac
`)
	t.Setenv("HOME", home)
	t.Setenv("PATH", binDir)
	t.Setenv("SYSTEMCTL_LOG", logPath)

	mgr := &systemdManager{}
	if err := mgr.Start(); err == nil || !strings.Contains(err.Error(), "start:") {
		t.Fatalf("Start() error = %v", err)
	}
	if err := mgr.Stop(); err == nil || !strings.Contains(err.Error(), "stop:") {
		t.Fatalf("Stop() error = %v", err)
	}
	if err := mgr.Restart(); err == nil || !strings.Contains(err.Error(), "restart:") {
		t.Fatalf("Restart() error = %v", err)
	}
}

func TestEnableLingerFallbackAndInteractive(t *testing.T) {
	if err := runInteractive("sh", "-c", "exit 0"); err != nil {
		t.Fatalf("runInteractive(success) error = %v", err)
	}

	binDir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "linger.log")
	writeExecutable(t, binDir, "loginctl", `
printf 'loginctl %s\n' "$*" >> "$LINGER_LOG"
if [ "${1:-}" = "show-user" ]; then
  echo boom >&2
  exit 1
fi
echo denied >&2
exit 1
`)
	writeExecutable(t, binDir, "sudo", `
printf 'sudo %s\n' "$*" >> "$LINGER_LOG"
exit 0
`)
	t.Setenv("PATH", binDir)
	t.Setenv("LINGER_LOG", logPath)
	t.Setenv("USER", "tester")

	if err := EnableLingerCurrentUser(); err != nil {
		t.Fatalf("EnableLingerCurrentUser(fallback sudo) error = %v", err)
	}
	content, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("ReadFile(linger.log) error = %v", err)
	}
	text := string(content)
	if !strings.Contains(text, "loginctl show-user tester") || !strings.Contains(text, "sudo loginctl enable-linger tester") {
		t.Fatalf("linger log = %q", text)
	}
}

func TestUpgradeBranchHelpers(t *testing.T) {
	dir := t.TempDir()
	empty := filepath.Join(dir, "empty")
	if err := os.WriteFile(empty, nil, 0o644); err != nil {
		t.Fatalf("WriteFile(empty) error = %v", err)
	}
	if _, err := readUpgradeBinaryFromLocal(empty); err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("readUpgradeBinaryFromLocal(empty) error = %v", err)
	}
	if _, err := readUpgradeBinaryFromLocal(dir); err == nil || !strings.Contains(err.Error(), "regular file") {
		t.Fatalf("readUpgradeBinaryFromLocal(dir) error = %v", err)
	}
	if err := validateUpgradeSpec(UpgradeSpec{BinaryPath: "/tmp/x", DownloadURL: "https://x", ExpectedSHA256: "abc"}); err == nil || !strings.Contains(err.Error(), "missing upgrade version") {
		t.Fatalf("validateUpgradeSpec(missing version) error = %v", err)
	}
	if err := validateUpgradeSpec(UpgradeSpec{Version: "v1", DownloadURL: "https://x", ExpectedSHA256: "abc"}); err == nil || !strings.Contains(err.Error(), "missing binary path") {
		t.Fatalf("validateUpgradeSpec(missing binary) error = %v", err)
	}
	if err := validateUpgradeSpec(UpgradeSpec{Version: "v1", BinaryPath: "/tmp/x", ExpectedSHA256: "abc"}); err == nil || !strings.Contains(err.Error(), "missing upgrade source") {
		t.Fatalf("validateUpgradeSpec(missing source) error = %v", err)
	}
	if err := validateUpgradeSpec(UpgradeSpec{Version: "v1", BinaryPath: "/tmp/x", DownloadURL: "https://x"}); err == nil || !strings.Contains(err.Error(), "missing expected sha256") {
		t.Fatalf("validateUpgradeSpec(missing sha) error = %v", err)
	}

	if err := waitForServiceHealthy(context.Background(), &fakeUpgradeManager{}, 10*time.Millisecond); err == nil || !strings.Contains(err.Error(), "did not become active") {
		t.Fatalf("waitForServiceHealthy(timeout) error = %v", err)
	}
}

func TestRunUpgradeWithManagerStopFailure(t *testing.T) {
	dir := t.TempDir()
	binaryPath := filepath.Join(dir, "feidex")
	if err := os.WriteFile(binaryPath, []byte("old-binary"), 0o755); err != nil {
		t.Fatalf("WriteFile(old) error = %v", err)
	}

	manager := &fakeUpgradeManager{running: true, pid: 99, stopErr: context.Canceled}
	err := runUpgradeWithManager(context.Background(), manager, UpgradeSpec{
		Version:        "v0.2.0",
		BinaryPath:     binaryPath,
		SourcePath:     binaryPath,
		ExpectedSHA256: mustSHA256([]byte("old-binary")),
	})
	if err == nil || !strings.Contains(err.Error(), "stop service before upgrade") {
		t.Fatalf("runUpgradeWithManager(stop failure) error = %v", err)
	}
}
