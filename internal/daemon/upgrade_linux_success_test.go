//go:build linux

package daemon

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestRunUpgradeRunnerSuccess(t *testing.T) {
	binDir := t.TempDir()
	home := t.TempDir()
	writeExecutable(t, binDir, "systemctl", `
case " $* " in
  *" --user show feidex.service --no-page --property ActiveState,MainPID "*)
  printf 'ActiveState=active\nMainPID=123\n'
  ;;
esac
`)
	t.Setenv("PATH", binDir)
	t.Setenv("HOME", home)

	unitDir := filepath.Join(home, ".config", "systemd", "user")
	if err := os.MkdirAll(unitDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(unitDir) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(unitDir, systemdServiceName), []byte("[Unit]\nDescription=feidex\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(unit) error = %v", err)
	}

	dir := t.TempDir()
	binaryPath := filepath.Join(dir, "feidex")
	if err := os.WriteFile(binaryPath, []byte("old-binary"), 0o755); err != nil {
		t.Fatalf("WriteFile(old binary) error = %v", err)
	}

	newContent := []byte("new-binary")
	origClient := upgradeHTTPClient
	upgradeHTTPClient = &http.Client{Transport: daemonStubTransport{
		responses: map[string][]byte{
			"https://download.test/bin": newContent,
		},
	}}
	defer func() { upgradeHTTPClient = origClient }()

	if err := RunUpgradeRunner(context.Background(), UpgradeSpec{
		Version:        "v0.2.0",
		BinaryPath:     binaryPath,
		DownloadURL:    "https://download.test/bin",
		ExpectedSHA256: mustSHA256(newContent),
	}); err != nil {
		t.Fatalf("RunUpgradeRunner() error = %v", err)
	}
	got, err := os.ReadFile(binaryPath)
	if err != nil {
		t.Fatalf("ReadFile(binary) error = %v", err)
	}
	if string(got) != string(newContent) {
		t.Fatalf("binary content = %q, want upgraded binary", string(got))
	}
}
