//go:build linux

package daemon

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

type systemdManager struct {
	serviceName string
}

func newPlatformManager(serviceName string) (Manager, error) {
	if os.Getuid() == 0 {
		return nil, fmt.Errorf("systemd install currently supports user services only; run as the target user instead of root")
	}
	if _, err := exec.LookPath("systemctl"); err != nil {
		return nil, fmt.Errorf("systemctl not found: systemd is required on Linux")
	}
	return &systemdManager{serviceName: normalizeServiceName(serviceName)}, nil
}

func (m *systemdManager) Platform() string {
	return "systemd (user)"
}

func (m *systemdManager) Install(cfg Config) error {
	unitPath := m.unitPath()
	if err := os.MkdirAll(filepath.Dir(unitPath), 0o755); err != nil {
		return fmt.Errorf("create systemd dir: %w", err)
	}
	unit := m.buildUnit(cfg)
	if err := os.WriteFile(unitPath, []byte(unit), 0o644); err != nil {
		return fmt.Errorf("write unit file: %w", err)
	}
	for _, args := range [][]string{
		{"--user", "daemon-reload"},
		{"--user", "enable", m.serviceUnitName()},
		{"--user", "restart", m.serviceUnitName()},
	} {
		if out, err := runSystemctl(args...); err != nil {
			return fmt.Errorf("systemctl %s: %s (%w)", strings.Join(args, " "), out, err)
		}
	}
	return nil
}

func (m *systemdManager) Uninstall() error {
	if _, err := runSystemctl("--user", "disable", "--now", m.serviceUnitName()); err != nil {
		// best-effort stop/disable
	}
	unitPath := m.unitPath()
	if err := os.Remove(unitPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove unit file: %w", err)
	}
	if out, err := runSystemctl("--user", "daemon-reload"); err != nil {
		return fmt.Errorf("systemctl --user daemon-reload: %s (%w)", out, err)
	}
	return nil
}

func (m *systemdManager) Start() error {
	out, err := runSystemctl("--user", "start", m.serviceUnitName())
	if err != nil {
		return fmt.Errorf("start: %s (%w)", out, err)
	}
	return nil
}

func (m *systemdManager) Stop() error {
	out, err := runSystemctl("--user", "stop", m.serviceUnitName())
	if err != nil {
		return fmt.Errorf("stop: %s (%w)", out, err)
	}
	return nil
}

func (m *systemdManager) Restart() error {
	out, err := runSystemctl("--user", "restart", m.serviceUnitName())
	if err != nil {
		return fmt.Errorf("restart: %s (%w)", out, err)
	}
	return nil
}

func (m *systemdManager) Status() (*Status, error) {
	st := &Status{
		Platform: m.Platform(),
		UnitPath: m.unitPath(),
	}
	if _, err := os.Stat(st.UnitPath); err != nil {
		if os.IsNotExist(err) {
			return st, nil
		}
		return nil, err
	}
	st.Installed = true
	out, err := runSystemctl("--user", "show", m.serviceUnitName(), "--no-page", "--property", "ActiveState,MainPID")
	if err != nil {
		return st, nil
	}
	props := parseKeyValue(out)
	if strings.EqualFold(props["ActiveState"], "active") {
		st.Running = true
	}
	if pid, err := strconv.Atoi(strings.TrimSpace(props["MainPID"])); err == nil && pid > 0 {
		st.PID = pid
	}
	return st, nil
}

func (m *systemdManager) unitPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "systemd", "user", m.serviceUnitName())
}

func (m *systemdManager) buildUnit(cfg Config) string {
	serviceName := normalizeServiceName(m.serviceName)
	var sb strings.Builder
	sb.WriteString("[Unit]\n")
	fmt.Fprintf(&sb, "Description=%s - Feishu Codex Bridge\n", serviceName)
	sb.WriteString("After=network-online.target\n")
	sb.WriteString("Wants=network-online.target\n\n")
	sb.WriteString("[Service]\n")
	sb.WriteString("Type=simple\n")
	fmt.Fprintf(&sb, "ExecStart=%s serve --config %s\n", quoteSystemd(cfg.BinaryPath), quoteSystemd(cfg.ConfigPath))
	fmt.Fprintf(&sb, "WorkingDirectory=%s\n", quoteSystemd(cfg.WorkDir))
	sb.WriteString("Restart=on-failure\n")
	sb.WriteString("RestartSec=10\n")
	if strings.TrimSpace(cfg.EnvPATH) != "" {
		fmt.Fprintf(&sb, "Environment=%s\n", quoteSystemd("PATH="+cfg.EnvPATH))
	}
	if strings.TrimSpace(cfg.HomeDir) != "" {
		fmt.Fprintf(&sb, "Environment=%s\n", quoteSystemd("HOME="+cfg.HomeDir))
	}
	sb.WriteString("\n[Install]\n")
	sb.WriteString("WantedBy=default.target\n")
	return sb.String()
}

func (m *systemdManager) serviceUnitName() string {
	return normalizeServiceName(m.serviceName) + ".service"
}

func runSystemctl(args ...string) (string, error) {
	cmd := exec.Command("systemctl", args...)
	cmd.Env = append(os.Environ(), userSystemdEnv()...)
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func userSystemdEnv() []string {
	uid := os.Getuid()
	runtimeDir := strings.TrimSpace(os.Getenv("XDG_RUNTIME_DIR"))
	if runtimeDir == "" {
		runtimeDir = fmt.Sprintf("/run/user/%d", uid)
	}
	values := []string{}
	if strings.TrimSpace(os.Getenv("XDG_RUNTIME_DIR")) == "" {
		values = append(values, "XDG_RUNTIME_DIR="+runtimeDir)
	}
	if strings.TrimSpace(os.Getenv("DBUS_SESSION_BUS_ADDRESS")) == "" {
		values = append(values, "DBUS_SESSION_BUS_ADDRESS=unix:path="+filepath.Join(runtimeDir, "bus"))
	}
	return values
}

func EnableLingerCurrentUser() error {
	user := strings.TrimSpace(os.Getenv("USER"))
	if user == "" {
		return fmt.Errorf("cannot determine current user for linger setup")
	}
	enabled, err := lingerEnabled(user)
	if err == nil && enabled {
		return nil
	}
	if _, err := exec.LookPath("loginctl"); err != nil {
		return fmt.Errorf("loginctl not found: cannot enable linger automatically")
	}
	if out, err := runLoginctl("enable-linger", user); err == nil {
		return nil
	} else if _, sudoErr := exec.LookPath("sudo"); sudoErr == nil {
		if err := runInteractive("sudo", "loginctl", "enable-linger", user); err == nil {
			return nil
		}
		return fmt.Errorf("enable linger failed: %s (%w)", out, err)
	} else {
		return fmt.Errorf("enable linger failed: %s (%w)", out, err)
	}
}

func lingerEnabled(user string) (bool, error) {
	out, err := runLoginctl("show-user", user, "--property=Linger", "--value")
	if err != nil {
		return false, err
	}
	return strings.EqualFold(strings.TrimSpace(out), "yes"), nil
}

func runLoginctl(args ...string) (string, error) {
	cmd := exec.Command("loginctl", args...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	text := strings.TrimSpace(strings.TrimSpace(stdout.String()) + "\n" + strings.TrimSpace(stderr.String()))
	text = strings.TrimSpace(text)
	return text, err
}

func runInteractive(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func parseKeyValue(text string) map[string]string {
	values := map[string]string{}
	for _, line := range strings.Split(text, "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		values[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	return values
}

func quoteSystemd(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return `""`
	}
	if strings.IndexFunc(value, func(r rune) bool {
		return r == ' ' || r == '\t' || r == '"' || r == '\\'
	}) >= 0 {
		return strconv.Quote(value)
	}
	return value
}
