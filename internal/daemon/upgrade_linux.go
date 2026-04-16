//go:build linux

package daemon

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"feidex/internal/release"
)

var upgradeHTTPClient = &http.Client{Timeout: 2 * time.Minute}

type UpgradeSpec struct {
	ServiceName    string
	Version        string
	BinaryPath     string
	DownloadURL    string
	SourcePath     string
	ExpectedSHA256 string
}

func StartBackgroundUpgrade(spec UpgradeSpec) (string, error) {
	if err := validateUpgradeSpec(spec); err != nil {
		return "", err
	}
	if _, err := exec.LookPath("systemd-run"); err != nil {
		return "", fmt.Errorf("systemd-run not found: %w", err)
	}
	unitName := fmt.Sprintf("feidex-upgrade-%d", time.Now().UnixNano())
	args := []string{
		"--user",
		"--unit", unitName,
		"--collect",
		"--property=Type=exec",
		"--property=Restart=no",
		"--",
		spec.BinaryPath,
		"daemon",
		"upgrade-runner",
		"--service-name", normalizeServiceName(spec.ServiceName),
		"--binary-path", spec.BinaryPath,
		"--version", spec.Version,
		"--expected-sha256", spec.ExpectedSHA256,
	}
	if strings.TrimSpace(spec.SourcePath) != "" {
		args = append(args, "--source-path", spec.SourcePath)
	} else {
		args = append(args, "--download-url", spec.DownloadURL)
	}
	cmd := exec.Command("systemd-run", args...)
	cmd.Env = append(os.Environ(), userSystemdEnv()...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("systemd-run failed: %s (%w)", strings.TrimSpace(string(output)), err)
	}
	return unitName, nil
}

func RunUpgradeRunner(ctx context.Context, spec UpgradeSpec) error {
	if err := validateUpgradeSpec(spec); err != nil {
		return err
	}
	manager, err := NewManager(spec.ServiceName)
	if err != nil {
		return err
	}
	return runUpgradeWithManager(ctx, manager, spec)
}

func runUpgradeWithManager(ctx context.Context, manager Manager, spec UpgradeSpec) error {
	dir := filepath.Dir(spec.BinaryPath)
	tmpDir, err := os.MkdirTemp(dir, ".feidex-upgrade-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	stagedPath := filepath.Join(tmpDir, filepath.Base(spec.BinaryPath))
	if err := stageUpgradeBinary(ctx, stagedPath, spec); err != nil {
		return err
	}
	if info, err := os.Stat(spec.BinaryPath); err == nil {
		_ = os.Chmod(stagedPath, info.Mode())
	} else {
		_ = os.Chmod(stagedPath, 0o755)
	}

	backupPath := filepath.Join(tmpDir, filepath.Base(spec.BinaryPath)+".bak")
	if err := manager.Stop(); err != nil {
		return fmt.Errorf("stop service before upgrade: %w", err)
	}
	if err := os.Rename(spec.BinaryPath, backupPath); err != nil {
		return fmt.Errorf("backup current binary: %w", err)
	}

	restore := func(reason error) error {
		_ = manager.Stop()
		if _, err := os.Stat(spec.BinaryPath); err == nil {
			_ = os.Remove(spec.BinaryPath)
		}
		if err := os.Rename(backupPath, spec.BinaryPath); err != nil {
			return fmt.Errorf("%v; rollback restore failed: %w", reason, err)
		}
		if err := manager.Start(); err != nil {
			return fmt.Errorf("%v; rollback restart failed: %w", reason, err)
		}
		return reason
	}

	if err := os.Rename(stagedPath, spec.BinaryPath); err != nil {
		return restore(fmt.Errorf("activate upgraded binary: %w", err))
	}
	if err := manager.Start(); err != nil {
		return restore(fmt.Errorf("start upgraded service: %w", err))
	}
	if err := waitForServiceHealthy(ctx, manager, 15*time.Second); err != nil {
		return restore(fmt.Errorf("health check failed after upgrade: %w", err))
	}
	_ = os.Remove(backupPath)
	return nil
}

func stageUpgradeBinary(ctx context.Context, targetPath string, spec UpgradeSpec) error {
	if strings.TrimSpace(spec.SourcePath) != "" {
		content, err := readUpgradeBinaryFromLocal(spec.SourcePath)
		if err != nil {
			return err
		}
		if err := release.VerifySHA256(content, spec.ExpectedSHA256); err != nil {
			return err
		}
		return os.WriteFile(targetPath, content, 0o755)
	}
	return downloadUpgradeBinary(ctx, targetPath, spec)
}

func downloadUpgradeBinary(ctx context.Context, targetPath string, spec UpgradeSpec) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, spec.DownloadURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "feidex-upgrade-runner")
	resp, err := upgradeHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("download binary failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	content, err := io.ReadAll(io.LimitReader(resp.Body, 128<<20))
	if err != nil {
		return err
	}
	if err := release.VerifySHA256(content, spec.ExpectedSHA256); err != nil {
		return err
	}
	return os.WriteFile(targetPath, content, 0o755)
}

func readUpgradeBinaryFromLocal(sourcePath string) ([]byte, error) {
	sourcePath = strings.TrimSpace(sourcePath)
	f, err := os.Open(sourcePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("local source %q is not a regular file", sourcePath)
	}
	content, err := io.ReadAll(io.LimitReader(f, 128<<20))
	if err != nil {
		return nil, err
	}
	if len(content) == 0 {
		return nil, fmt.Errorf("local source %q is empty", sourcePath)
	}
	return content, nil
}

func waitForServiceHealthy(ctx context.Context, manager Manager, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	const pollInterval = 250 * time.Millisecond
	for time.Now().Before(deadline) {
		status, err := manager.Status()
		if err == nil && status != nil && status.Running && status.PID > 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(pollInterval):
		}
	}
	return fmt.Errorf("service did not become active within %s", timeout)
}

func validateUpgradeSpec(spec UpgradeSpec) error {
	if strings.TrimSpace(spec.Version) == "" {
		return fmt.Errorf("missing upgrade version")
	}
	if strings.TrimSpace(spec.BinaryPath) == "" {
		return fmt.Errorf("missing binary path")
	}
	if !filepath.IsAbs(spec.BinaryPath) {
		return fmt.Errorf("binary path must be absolute")
	}
	hasDownloadURL := strings.TrimSpace(spec.DownloadURL) != ""
	hasSourcePath := strings.TrimSpace(spec.SourcePath) != ""
	switch {
	case hasDownloadURL && hasSourcePath:
		return fmt.Errorf("upgrade source must use either download url or source path")
	case !hasDownloadURL && !hasSourcePath:
		return fmt.Errorf("missing upgrade source")
	case hasDownloadURL:
		if !strings.HasPrefix(strings.TrimSpace(spec.DownloadURL), "https://") {
			return fmt.Errorf("download url must use https")
		}
	case hasSourcePath:
		if !filepath.IsAbs(strings.TrimSpace(spec.SourcePath)) {
			return fmt.Errorf("source path must be absolute")
		}
	}
	if strings.TrimSpace(spec.ExpectedSHA256) == "" {
		return fmt.Errorf("missing expected sha256")
	}
	return nil
}
