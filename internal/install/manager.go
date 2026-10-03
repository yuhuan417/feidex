// Package install implements the shared self-upgrade probe and install flow
// used by every CLI backend. A backend supplies a Spec describing what makes
// it different (command name, npm package, version parsing, User-Agent); the
// Manager holds everything else, which is identical across backends.
package install

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"feidex/internal/domain/backend"
	"feidex/internal/npmregistry"
)

// Probe describes the self-upgrade capability of one installed CLI.
type Probe = backend.InstallProbe

// Manager probes and upgrades a single CLI backend.
type Manager struct {
	spec    Spec
	command string
}

// Package-level seams, overridden by tests.
var (
	commandRunner = func(ctx context.Context, name string, args ...string) (string, string, error) {
		cmd := exec.CommandContext(ctx, name, args...)
		output, err := cmd.CombinedOutput()
		if err != nil {
			return "", strings.TrimSpace(string(output)), err
		}
		return strings.TrimSpace(string(output)), "", nil
	}

	latestVersionLookup = func(ctx context.Context, packageName, userAgent string) (string, error) {
		return npmregistry.LatestVersion(ctx, nil, packageName, userAgent)
	}
)

// New creates a Manager for the given backend spec. An empty command falls
// back to the spec's default command name.
func New(spec Spec, command string) *Manager {
	command = strings.TrimSpace(command)
	if command == "" {
		command = spec.CommandName
	}
	return &Manager{spec: spec, command: command}
}

func (m *Manager) commandOrDefault() string {
	if m != nil && strings.TrimSpace(m.command) != "" {
		return strings.TrimSpace(m.command)
	}
	return m.spec.CommandName
}

func (m *Manager) Probe(ctx context.Context) (Probe, error) {
	command := m.commandOrDefault()
	probe := Probe{Command: command}
	commandPath, err := exec.LookPath(command)
	if err != nil {
		probe.Reason = "未找到 " + m.spec.CommandName + " 命令"
		return probe, nil
	}
	probe.CommandPath = commandPath
	probe.RealCommandPath = commandPath
	if realPath, realErr := filepath.EvalSymlinks(commandPath); realErr == nil {
		probe.RealCommandPath = filepath.Clean(realPath)
	}
	commandPackagePath, commandVersion, commandPackageFound := packageFromCommandPath(probe.RealCommandPath, m.spec.PackageName)
	if commandPackageFound {
		probe.PackagePath = commandPackagePath
		probe.CurrentVersion = commandVersion
	}

	version, err := m.currentVersion(ctx)
	if err != nil {
		probe.Reason = err.Error()
		return probe, nil
	}
	if version == "" {
		probe.Reason = "无法读取 " + m.spec.CommandName + " 当前版本"
		return probe, nil
	}
	probe.CurrentVersion = FirstNonEmpty(probe.CurrentVersion, version)

	updateCommand, err := m.selfUpdateCommand(ctx)
	if err != nil {
		probe.Reason = err.Error()
		return probe, nil
	}
	probe.UpdateCommand = updateCommand
	probe.Supported = true
	return probe, nil
}

func (m *Manager) LatestVersion(ctx context.Context) (string, error) {
	if _, err := m.selfUpdateCommand(ctx); err != nil {
		return "", fmt.Errorf("检查 %s 自升级命令失败: %w", m.spec.Name, err)
	}
	userAgent, err := m.spec.UserAgent(m, ctx)
	if err != nil {
		return "", fmt.Errorf("读取 %s 标准 User-Agent 失败: %w", m.spec.Name, err)
	}
	version, err := latestVersionLookup(ctx, m.spec.PackageName, userAgent)
	if err != nil {
		return "", fmt.Errorf("查询 %s 最新版本失败: %w", m.spec.Name, err)
	}
	return strings.TrimSpace(version), nil
}

func (m *Manager) InstallVersion(ctx context.Context, version string) error {
	version = strings.TrimSpace(version)
	if version != "" && version != selfUpdateTargetLatest {
		return fmt.Errorf("%s 自升级不支持指定版本 %q", m.spec.Name, version)
	}
	command := m.commandOrDefault()
	updateCommand, err := m.selfUpdateCommand(ctx)
	if err != nil {
		return err
	}
	_, stderr, err := commandRunner(ctx, command, updateCommand)
	if err != nil {
		return fmt.Errorf("运行 `%s %s` 失败: %s", command, updateCommand, FirstNonEmpty(stderr, err.Error()))
	}
	return nil
}

func (m *Manager) currentVersion(ctx context.Context) (string, error) {
	command := m.commandOrDefault()
	stdout, stderr, err := commandRunner(ctx, command, "--version")
	if err != nil {
		return "", fmt.Errorf("读取当前版本失败: %s", FirstNonEmpty(stderr, err.Error()))
	}
	version := m.spec.ParseVersion(stdout)
	if version == "" {
		return "", fmt.Errorf("解析当前版本失败: %q", strings.TrimSpace(stdout))
	}
	return version, nil
}

func (m *Manager) selfUpdateCommand(ctx context.Context) (string, error) {
	command := m.commandOrDefault()
	_, stderr, err := commandRunner(ctx, command, "update", "--help")
	if err != nil {
		return "", fmt.Errorf("当前 %s CLI 不支持 `update` 自升级命令: %s", m.spec.Name, FirstNonEmpty(stderr, err.Error()))
	}
	return "update", nil
}

func packageFromCommandPath(commandPath, expectedPackageName string) (string, string, bool) {
	dir := filepath.Clean(strings.TrimSpace(commandPath))
	if dir == "" {
		return "", "", false
	}
	if info, err := os.Stat(dir); err == nil && !info.IsDir() {
		dir = filepath.Dir(dir)
	}
	for {
		packageJSON := filepath.Join(dir, "package.json")
		name, version, err := readPackageManifest(packageJSON)
		if err == nil && strings.TrimSpace(name) == strings.TrimSpace(expectedPackageName) {
			return packageJSON, strings.TrimSpace(version), true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", "", false
		}
		dir = parent
	}
}

func readPackageManifest(path string) (string, string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", "", err
	}
	var payload struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return "", "", err
	}
	return strings.TrimSpace(payload.Name), strings.TrimSpace(payload.Version), nil
}

// FirstNonEmpty returns the first value that is non-empty after trimming.
func FirstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
