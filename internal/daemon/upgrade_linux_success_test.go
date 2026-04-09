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
	writeExecutable(t, binDir, "systemctl", `
if [ "${2:-}" = "show" ]; then
  printf 'ActiveState=active\nMainPID=123\n'
fi
`)
	t.Setenv("PATH", binDir)

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
