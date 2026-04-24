package codexinstall

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const packageName = "@openai/codex"

type Probe struct {
	Command         string
	CommandPath     string
	RealCommandPath string
	NPMPath         string
	NPMRoot         string
	PackagePath     string
	CurrentVersion  string
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
	if command != "codex" {
		probe.Reason = "当前只支持默认 `codex` 命令"
		return probe, nil
	}

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

	npmPath, err := exec.LookPath("npm")
	if err != nil {
		probe.Reason = "未找到 npm 命令"
		return probe, nil
	}
	probe.NPMPath = npmPath

	npmRoot, err := m.npmRoot(ctx)
	if err != nil {
		probe.Reason = err.Error()
		return probe, nil
	}
	probe.NPMRoot = npmRoot
	npmPackageDir := filepath.Join(npmRoot, filepath.FromSlash(packageName))
	npmPackagePath := filepath.Join(npmPackageDir, "package.json")
	if commandPackageFound && !pathWithinRoot(npmPackageDir, probe.RealCommandPath) {
		probe.Reason = "当前 codex 所属安装目录与 npm global prefix 不一致"
		return probe, nil
	}

	version, err := m.currentVersion(ctx)
	if err != nil {
		probe.Reason = err.Error()
		return probe, nil
	}
	if version == "" {
		probe.Reason = "npm global 未安装 @openai/codex"
		return probe, nil
	}
	probe.CurrentVersion = firstNonEmpty(probe.CurrentVersion, version)
	if strings.TrimSpace(probe.PackagePath) == "" {
		probe.PackagePath = npmPackagePath
	}
	if _, statErr := os.Stat(npmPackagePath); statErr != nil {
		probe.Reason = "npm global 的 @openai/codex 安装目录不可用"
		return probe, nil
	}
	if !pathWithinRoot(npmPackageDir, probe.RealCommandPath) {
		probe.Reason = "当前 codex 不是 npm global 的 @openai/codex"
		return probe, nil
	}
	probe.PackagePath = npmPackagePath
	probe.Supported = true
	return probe, nil
}

func (m *Manager) LatestVersion(ctx context.Context) (string, error) {
	stdout, stderr, err := commandRunner(ctx, "npm", "view", packageName, "version", "--json")
	if err != nil {
		return "", fmt.Errorf("查询最新版本失败: %s", firstNonEmpty(stderr, err.Error()))
	}
	version, parseErr := parseJSONMaybeString(stdout)
	if parseErr != nil {
		return "", fmt.Errorf("解析最新版本失败: %w", parseErr)
	}
	return strings.TrimSpace(version), nil
}

func (m *Manager) InstallVersion(ctx context.Context, version string) error {
	version = strings.TrimSpace(version)
	if version == "" {
		return fmt.Errorf("missing version")
	}
	spec := packageName + "@" + version
	_, stderr, err := commandRunner(ctx, "npm", "i", "-g", spec)
	if err != nil {
		return fmt.Errorf("安装 %s 失败: %s", spec, firstNonEmpty(stderr, err.Error()))
	}
	return nil
}

func (m *Manager) currentVersion(ctx context.Context) (string, error) {
	stdout, stderr, err := commandRunner(ctx, "npm", "ls", "-g", packageName, "--json", "--depth=0")
	if err != nil && strings.TrimSpace(stdout) == "" {
		return "", fmt.Errorf("读取当前版本失败: %s", firstNonEmpty(stderr, err.Error()))
	}
	var payload struct {
		Dependencies map[string]struct {
			Version string `json:"version"`
		} `json:"dependencies"`
	}
	if unmarshalErr := json.Unmarshal([]byte(stdout), &payload); unmarshalErr != nil {
		if strings.TrimSpace(stdout) == "" {
			return "", fmt.Errorf("读取当前版本失败: %s", firstNonEmpty(stderr, err.Error()))
		}
		return "", fmt.Errorf("解析当前版本失败: %w", unmarshalErr)
	}
	if payload.Dependencies == nil {
		return "", nil
	}
	return strings.TrimSpace(payload.Dependencies[packageName].Version), nil
}

func (m *Manager) npmRoot(ctx context.Context) (string, error) {
	stdout, stderr, err := commandRunner(ctx, "npm", "root", "-g")
	if err != nil {
		return "", fmt.Errorf("查询 npm global root 失败: %s", firstNonEmpty(stderr, err.Error()))
	}
	root := strings.TrimSpace(stdout)
	if root == "" {
		return "", fmt.Errorf("查询 npm global root 失败: empty output")
	}
	return filepath.Clean(root), nil
}

func pathWithinRoot(root, candidate string) bool {
	root = filepath.Clean(strings.TrimSpace(root))
	candidate = filepath.Clean(strings.TrimSpace(candidate))
	if root == "" || candidate == "" {
		return false
	}
	if root == string(filepath.Separator) {
		return filepath.IsAbs(candidate)
	}
	if root == candidate {
		return true
	}
	return strings.HasPrefix(candidate, root+string(filepath.Separator))
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
