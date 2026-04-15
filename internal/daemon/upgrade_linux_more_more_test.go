//go:build linux

package daemon

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStartBackgroundUpgradeAndDownloadErrors(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if _, err := StartBackgroundUpgrade(UpgradeSpec{
		Version:        "v0.2.0",
		BinaryPath:     "/tmp/feidex",
		DownloadURL:    "https://download.test/bin",
		ExpectedSHA256: strings.Repeat("a", 64),
	}); err == nil || !strings.Contains(err.Error(), "systemd-run not found") {
		t.Fatalf("StartBackgroundUpgrade(no systemd-run) error = %v", err)
	}

	origClient := upgradeHTTPClient
	defer func() { upgradeHTTPClient = origClient }()

	upgradeHTTPClient = &http.Client{Transport: daemonStubTransport{
		responses: map[string][]byte{
			"https://download.test/bad": []byte("content"),
		},
	}}
	if err := stageUpgradeBinary(context.Background(), filepath.Join(t.TempDir(), "bin"), UpgradeSpec{
		Version:        "v0.2.0",
		BinaryPath:     "/tmp/feidex",
		DownloadURL:    "https://download.test/missing",
		ExpectedSHA256: mustSHA256([]byte("content")),
	}); err == nil || !strings.Contains(err.Error(), "status=404") {
		t.Fatalf("stageUpgradeBinary(404) error = %v", err)
	}

	if err := stageUpgradeBinary(context.Background(), filepath.Join(t.TempDir(), "bin"), UpgradeSpec{
		Version:        "v0.2.0",
		BinaryPath:     "/tmp/feidex",
		DownloadURL:    "https://download.test/bad",
		ExpectedSHA256: "deadbeef",
	}); err == nil || !strings.Contains(err.Error(), "sha256 mismatch") {
		t.Fatalf("stageUpgradeBinary(checksum) error = %v", err)
	}

	sourcePath := filepath.Join(t.TempDir(), "local-bin")
	if err := os.WriteFile(sourcePath, []byte("local-content"), 0o755); err != nil {
		t.Fatalf("WriteFile(local-bin) error = %v", err)
	}
	if err := stageUpgradeBinary(context.Background(), filepath.Join(t.TempDir(), "copied-bin"), UpgradeSpec{
		Version:        "v0.2.0",
		BinaryPath:     "/tmp/feidex",
		SourcePath:     sourcePath,
		ExpectedSHA256: mustSHA256([]byte("local-content")),
	}); err != nil {
		t.Fatalf("stageUpgradeBinary(local) error = %v", err)
	}
}
