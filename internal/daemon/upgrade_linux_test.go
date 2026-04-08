//go:build linux

package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type fakeUpgradeManager struct {
	startErr error
	stopErr  error
	running  bool
	pid      int
}

func (m *fakeUpgradeManager) Install(Config) error { return nil }
func (m *fakeUpgradeManager) Uninstall() error     { return nil }
func (m *fakeUpgradeManager) Platform() string     { return "test" }
func (m *fakeUpgradeManager) Restart() error       { return nil }
func (m *fakeUpgradeManager) Start() error {
	if m.startErr != nil {
		return m.startErr
	}
	m.running = true
	m.pid = 123
	return nil
}
func (m *fakeUpgradeManager) Stop() error {
	if m.stopErr != nil {
		return m.stopErr
	}
	m.running = false
	m.pid = 0
	return nil
}
func (m *fakeUpgradeManager) Status() (*Status, error) {
	return &Status{Installed: true, Running: m.running, PID: m.pid}, nil
}

func TestValidateUpgradeSpec(t *testing.T) {
	if err := validateUpgradeSpec(UpgradeSpec{}); err == nil {
		t.Fatal("expected empty upgrade spec to fail")
	}
	if err := validateUpgradeSpec(UpgradeSpec{
		Version:        "v0.2.0",
		BinaryPath:     "/tmp/feidex",
		DownloadURL:    "https://example.test/feidex",
		ExpectedSHA256: "abc",
	}); err != nil {
		t.Fatalf("validateUpgradeSpec() error = %v", err)
	}
}

func TestRunUpgradeWithManagerRollsBackOnStartFailure(t *testing.T) {
	dir := t.TempDir()
	binaryPath := filepath.Join(dir, "feidex")
	oldContent := []byte("old-binary")
	if err := os.WriteFile(binaryPath, oldContent, 0o755); err != nil {
		t.Fatalf("WriteFile(old) error = %v", err)
	}
	newContent := []byte("new-binary")
	origClient := upgradeHTTPClient
	upgradeHTTPClient = &http.Client{Transport: daemonStubTransport{
		responses: map[string][]byte{
			"https://download.test/bin": newContent,
		},
	}}
	defer func() { upgradeHTTPClient = origClient }()
	manager := &fakeUpgradeManager{running: true, pid: 99, startErr: errors.New("boom")}
	err := runUpgradeWithManager(context.Background(), manager, UpgradeSpec{
		Version:        "v0.2.0",
		BinaryPath:     binaryPath,
		DownloadURL:    "https://download.test/bin",
		ExpectedSHA256: mustSHA256(newContent),
	})
	if err == nil {
		t.Fatal("expected upgrade to fail")
	}
	got, readErr := os.ReadFile(binaryPath)
	if readErr != nil {
		t.Fatalf("ReadFile(binary) error = %v", readErr)
	}
	if string(got) != string(oldContent) {
		t.Fatalf("binary content = %q, want rollback to old content", string(got))
	}
}

func mustSHA256(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

func TestWaitForServiceHealthy(t *testing.T) {
	manager := &fakeUpgradeManager{}
	go func() {
		time.Sleep(10 * time.Millisecond)
		manager.running = true
		manager.pid = 1
	}()
	if err := waitForServiceHealthy(context.Background(), manager, time.Second); err != nil {
		t.Fatalf("waitForServiceHealthy() error = %v", err)
	}
}

type daemonStubTransport struct {
	responses map[string][]byte
}

func (t daemonStubTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	body, ok := t.responses[req.URL.String()]
	if !ok {
		return &http.Response{
			StatusCode: http.StatusNotFound,
			Body:       io.NopCloser(strings.NewReader("not found")),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(string(body))),
		Header:     make(http.Header),
		Request:    req,
	}, nil
}
