//go:build linux

package daemon

import (
	"context"
	"net/http"
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
	if err := downloadUpgradeBinary(context.Background(), filepath.Join(t.TempDir(), "bin"), UpgradeSpec{
		Version:        "v0.2.0",
		BinaryPath:     "/tmp/feidex",
		DownloadURL:    "https://download.test/missing",
		ExpectedSHA256: mustSHA256([]byte("content")),
	}); err == nil || !strings.Contains(err.Error(), "status=404") {
		t.Fatalf("downloadUpgradeBinary(404) error = %v", err)
	}

	if err := downloadUpgradeBinary(context.Background(), filepath.Join(t.TempDir(), "bin"), UpgradeSpec{
		Version:        "v0.2.0",
		BinaryPath:     "/tmp/feidex",
		DownloadURL:    "https://download.test/bad",
		ExpectedSHA256: "deadbeef",
	}); err == nil || !strings.Contains(err.Error(), "sha256 mismatch") {
		t.Fatalf("downloadUpgradeBinary(checksum) error = %v", err)
	}
}
