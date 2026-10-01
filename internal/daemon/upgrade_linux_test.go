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
	"sync"
	"testing"
	"time"
)

type fakeUpgradeManager struct {
	mu       sync.Mutex
	startErr error
	stopErr  error
	running  bool
	pid      int
}

func (m *fakeUpgradeManager) Install(Config) error { return nil }
func (m *fakeUpgradeManager) Uninstall() error     { return nil }
func (m *fakeUpgradeManager) Platform() string     { return "test" }
func (m *fakeUpgradeManager) LogFile() string      { return "" }
func (m *fakeUpgradeManager) Restart() error       { return nil }
func (m *fakeUpgradeManager) Start() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.startErr != nil {
		return m.startErr
	}
	m.running = true
	m.pid = 123
	return nil
}
func (m *fakeUpgradeManager) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.stopErr != nil {
		return m.stopErr
	}
	m.running = false
	m.pid = 0
	return nil
}
func (m *fakeUpgradeManager) Status() (*Status, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
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
		manager.mu.Lock()
		manager.running = true
		manager.pid = 1
		manager.mu.Unlock()
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

// --- merged from upgrade_linux_more_test.go ---

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

// --- merged from upgrade_linux_more_more_test.go ---

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
