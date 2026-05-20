package codexinstall

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

const packageName = "@openai/codex"
const selfUpdateTargetLatest = "latest"

type Probe struct {
	Command         string
	CommandPath     string
	RealCommandPath string
	PackagePath     string
	CurrentVersion  string
	UpdateCommand   string
	Supported       bool
	Reason          string
}

type Manager struct {
	command string
}

var commandRunner = func(ctx context.Context, name string, args ...string) (string, string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", strings.TrimSpace(string(output)), err
	}
	return strings.TrimSpace(string(output)), "", nil
}

func New(command string) *Manager {
	command = strings.TrimSpace(command)
	if command == "" {
		command = "codex"
	}
	return &Manager{command: command}
}

func (m *Manager) Probe(ctx context.Context) (Probe, error) {
	command := "codex"
	if m != nil && strings.TrimSpace(m.command) != "" {
		command = strings.TrimSpace(m.command)
	}
	probe := Probe{Command: command}
	commandPath, err := exec.LookPath(command)
	if err != nil {
		probe.Reason = "未找到 codex 命令"
		return probe, nil
	}
	probe.CommandPath = commandPath
	probe.RealCommandPath = commandPath
	if realPath, realErr := filepath.EvalSymlinks(commandPath); realErr == nil {
		probe.RealCommandPath = filepath.Clean(realPath)
	}
	commandPackagePath, commandVersion, commandPackageFound := packageFromCommandPath(probe.RealCommandPath, packageName)
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
		probe.Reason = "无法读取 codex 当前版本"
		return probe, nil
	}
	probe.CurrentVersion = firstNonEmpty(probe.CurrentVersion, version)

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
		return "", fmt.Errorf("检查 Codex 自升级命令失败: %w", err)
	}
	return selfUpdateTargetLatest, nil
}

func (m *Manager) InstallVersion(ctx context.Context, version string) error {
	version = strings.TrimSpace(version)
	if version != "" && version != selfUpdateTargetLatest {
		return fmt.Errorf("Codex 自升级不支持指定版本 %q", version)
	}
	command := "codex"
	if m != nil && strings.TrimSpace(m.command) != "" {
		command = strings.TrimSpace(m.command)
	}
	updateCommand, err := m.selfUpdateCommand(ctx)
	if err != nil {
		return err
	}
	_, stderr, err := commandRunner(ctx, command, updateCommand)
	if err != nil {
		return fmt.Errorf("运行 `%s %s` 失败: %s", command, updateCommand, firstNonEmpty(stderr, err.Error()))
	}
	return nil
}

func (m *Manager) currentVersion(ctx context.Context) (string, error) {
	command := "codex"
	if m != nil && strings.TrimSpace(m.command) != "" {
		command = strings.TrimSpace(m.command)
	}
	stdout, stderr, err := commandRunner(ctx, command, "--version")
	if err != nil {
		return "", fmt.Errorf("读取当前版本失败: %s", firstNonEmpty(stderr, err.Error()))
	}
	version := parseCodexVersion(stdout)
	if version == "" {
		return "", fmt.Errorf("解析当前版本失败: %q", strings.TrimSpace(stdout))
	}
	return version, nil
}

func (m *Manager) selfUpdateCommand(ctx context.Context) (string, error) {
	command := "codex"
	if m != nil && strings.TrimSpace(m.command) != "" {
		command = strings.TrimSpace(m.command)
	}
	_, stderr, err := commandRunner(ctx, command, "help", "update")
	if err != nil {
		return "", fmt.Errorf("当前 Codex CLI 不支持 `update` 自升级命令: %s", firstNonEmpty(stderr, err.Error()))
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

func parseCodexVersion(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	versionPattern := regexp.MustCompile(`\bv?\d+\.\d+\.\d+(?:[-+][0-9A-Za-z.-]+)?\b`)
	version := versionPattern.FindString(raw)
	return strings.TrimPrefix(strings.TrimSpace(version), "v")
}

func parseJSONMaybeString(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("empty output")
	}
	var out string
	if err := json.Unmarshal([]byte(raw), &out); err == nil {
		return out, nil
	}
	return strings.Trim(raw, "\""), nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
