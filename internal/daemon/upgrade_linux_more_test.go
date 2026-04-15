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

func TestStartBackgroundUpgradeUsesSystemdRun(t *testing.T) {
	binDir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "systemd-run.log")
	writeExecutable(t, binDir, "systemd-run", `
printf '%s\n' "$*" >> "$SYSTEMDRUN_LOG"
`)
	t.Setenv("PATH", binDir)
	t.Setenv("SYSTEMDRUN_LOG", logPath)

	unitName, err := StartBackgroundUpgrade(UpgradeSpec{
		Version:        "v0.2.0",
		BinaryPath:     "/tmp/feidex",
		DownloadURL:    "https://download.test/feidex",
		ExpectedSHA256: strings.Repeat("a", 64),
	})
	if err != nil {
		t.Fatalf("StartBackgroundUpgrade() error = %v", err)
	}
	if !strings.HasPrefix(unitName, "feidex-upgrade-") {
		t.Fatalf("unitName = %q, want generated prefix", unitName)
	}

	content, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("ReadFile(log) error = %v", err)
	}
	text := string(content)
	for _, want := range []string{
		"--user",
		"--collect",
		"--property=Type=exec",
		"daemon upgrade-runner",
		"--version v0.2.0",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("systemd-run log missing %q:\n%s", want, text)
		}
	}
}

func TestUpgradeHelpersValidateAndWaitErrors(t *testing.T) {
	if err := validateUpgradeSpec(UpgradeSpec{Version: "v1", BinaryPath: "relative", DownloadURL: "https://x", ExpectedSHA256: "abc"}); err == nil || !strings.Contains(err.Error(), "absolute") {
		t.Fatalf("validateUpgradeSpec(relative) error = %v", err)
	}
	if err := validateUpgradeSpec(UpgradeSpec{Version: "v1", BinaryPath: "/tmp/x", DownloadURL: "http://x", ExpectedSHA256: "abc"}); err == nil || !strings.Contains(err.Error(), "https") {
		t.Fatalf("validateUpgradeSpec(http) error = %v", err)
	}
	if err := validateUpgradeSpec(UpgradeSpec{Version: "v1", BinaryPath: "/tmp/x", SourcePath: "relative", ExpectedSHA256: "abc"}); err == nil || !strings.Contains(err.Error(), "source path") {
		t.Fatalf("validateUpgradeSpec(relative source) error = %v", err)
	}
	if err := validateUpgradeSpec(UpgradeSpec{Version: "v1", BinaryPath: "/tmp/x", DownloadURL: "https://x", SourcePath: "/tmp/y", ExpectedSHA256: "abc"}); err == nil || !strings.Contains(err.Error(), "either") {
		t.Fatalf("validateUpgradeSpec(dual source) error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := waitForServiceHealthy(ctx, &fakeUpgradeManager{}, 500*time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "context canceled") {
		t.Fatalf("waitForServiceHealthy(cancelled) error = %v", err)
	}
}
