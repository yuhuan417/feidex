//go:build darwin

package daemon

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

type launchdManager struct {
	serviceName string
}

func newPlatformManager(serviceName string) (Manager, error) {
	if os.Getuid() == 0 {
		return nil, fmt.Errorf("launchd install supports per-user LaunchAgents only; run as the target user instead of root")
	}
	if _, err := exec.LookPath("launchctl"); err != nil {
		return nil, fmt.Errorf("launchctl not found: launchd is required on macOS")
	}
	return &launchdManager{serviceName: normalizeServiceName(serviceName)}, nil
}

func (m *launchdManager) Platform() string {
	return "launchd (macOS)"
}

// LogFile returns the file launchd writes the agent's stdout/stderr to.
func (m *launchdManager) LogFile() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "Logs", "feidex", m.label()+".log")
}

func (m *launchdManager) Install(cfg Config) error {
	plistPath := m.plistPath()
	if err := os.MkdirAll(filepath.Dir(plistPath), 0o755); err != nil {
		return fmt.Errorf("create LaunchAgents dir: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(m.LogFile()), 0o755); err != nil {
		return fmt.Errorf("create log dir: %w", err)
	}
	if err := os.WriteFile(plistPath, []byte(m.buildPlist(cfg)), 0o644); err != nil {
		return fmt.Errorf("write plist: %w", err)
	}
	// Clear any stale registration so bootstrap does not fail with
	// "service already bootstrapped".
	_, _ = runLaunchctl("bootout", m.serviceTarget())
	if out, err := runLaunchctl("bootstrap", m.domainTarget(), plistPath); err != nil {
		return fmt.Errorf("launchctl bootstrap: %s (%w)", out, err)
	}
	if out, err := runLaunchctl("kickstart", "-k", m.serviceTarget()); err != nil {
		return fmt.Errorf("launchctl kickstart: %s (%w)", out, err)
	}
	return nil
}

func (m *launchdManager) Uninstall() error {
	_, _ = runLaunchctl("bootout", m.serviceTarget()) // best-effort unload
	if err := os.Remove(m.plistPath()); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove plist: %w", err)
	}
	return nil
}

func (m *launchdManager) Start() error {
	// bootstrap loads the agent (RunAtLoad starts it); tolerate "already
	// bootstrapped" and let kickstart guarantee it is running.
	_, _ = runLaunchctl("bootstrap", m.domainTarget(), m.plistPath())
	if out, err := runLaunchctl("kickstart", m.serviceTarget()); err != nil {
		return fmt.Errorf("start: %s (%w)", out, err)
	}
	return nil
}

func (m *launchdManager) Stop() error {
	// launchd has no "stop but keep loaded" for a KeepAlive agent; bootout
	// unloads it, which is the durable way to stop.
	if out, err := runLaunchctl("bootout", m.serviceTarget()); err != nil {
		return fmt.Errorf("stop: %s (%w)", out, err)
	}
	return nil
}

func (m *launchdManager) Restart() error {
	_, _ = runLaunchctl("bootstrap", m.domainTarget(), m.plistPath()) // ensure loaded
	if out, err := runLaunchctl("kickstart", "-k", m.serviceTarget()); err != nil {
		return fmt.Errorf("restart: %s (%w)", out, err)
	}
	return nil
}

func (m *launchdManager) Status() (*Status, error) {
	st := &Status{
		Platform: m.Platform(),
		UnitPath: m.plistPath(),
	}
	if _, err := os.Stat(st.UnitPath); err != nil {
		if os.IsNotExist(err) {
			return st, nil
		}
		return nil, err
	}
	st.Installed = true
	out, err := runLaunchctl("print", m.serviceTarget())
	if err != nil {
		// Plist present but not loaded: installed, not running.
		return st, nil
	}
	running, pid := parseLaunchctlPrint(out)
	st.Running = running
	st.PID = pid
	return st, nil
}

func (m *launchdManager) label() string {
	return normalizeServiceName(m.serviceName)
}

func (m *launchdManager) plistPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "LaunchAgents", m.label()+".plist")
}

func (m *launchdManager) domainTarget() string {
	return fmt.Sprintf("gui/%d", os.Getuid())
}

func (m *launchdManager) serviceTarget() string {
	return fmt.Sprintf("gui/%d/%s", os.Getuid(), m.label())
}

func (m *launchdManager) buildPlist(cfg Config) string {
	logPath := m.LogFile()
	var sb strings.Builder
	sb.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	sb.WriteString(`<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">` + "\n")
	sb.WriteString(`<plist version="1.0">` + "\n")
	sb.WriteString("<dict>\n")
	plistKeyString(&sb, "Label", m.label())
	sb.WriteString("\t<key>ProgramArguments</key>\n\t<array>\n")
	for _, arg := range []string{cfg.BinaryPath, "serve", "--config", cfg.ConfigPath} {
		fmt.Fprintf(&sb, "\t\t<string>%s</string>\n", xmlEscape(arg))
	}
	sb.WriteString("\t</array>\n")
	plistKeyString(&sb, "WorkingDirectory", cfg.WorkDir)
	sb.WriteString("\t<key>EnvironmentVariables</key>\n\t<dict>\n")
	if strings.TrimSpace(cfg.EnvPATH) != "" {
		plistKeyStringIndent(&sb, "PATH", cfg.EnvPATH, "\t\t")
	}
	if strings.TrimSpace(cfg.HomeDir) != "" {
		plistKeyStringIndent(&sb, "HOME", cfg.HomeDir, "\t\t")
	}
	sb.WriteString("\t</dict>\n")
	plistKeyBool(&sb, "RunAtLoad", true)
	plistKeyBool(&sb, "KeepAlive", true)
	plistKeyString(&sb, "StandardOutPath", logPath)
	plistKeyString(&sb, "StandardErrorPath", logPath)
	plistKeyString(&sb, "ProcessType", "Background")
	sb.WriteString("</dict>\n</plist>\n")
	return sb.String()
}

func plistKeyString(sb *strings.Builder, key, value string) {
	plistKeyStringIndent(sb, key, value, "\t")
}

func plistKeyStringIndent(sb *strings.Builder, key, value, indent string) {
	fmt.Fprintf(sb, "%s<key>%s</key>\n%s<string>%s</string>\n", indent, xmlEscape(key), indent, xmlEscape(value))
}

func plistKeyBool(sb *strings.Builder, key string, value bool) {
	tag := "<false/>"
	if value {
		tag = "<true/>"
	}
	fmt.Fprintf(sb, "\t<key>%s</key>\n\t%s\n", xmlEscape(key), tag)
}

func xmlEscape(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&apos;")
	return r.Replace(s)
}

// parseLaunchctlPrint extracts running state and PID from `launchctl print`
// output, whose body contains lines like "state = running" and "pid = 1234".
func parseLaunchctlPrint(out string) (running bool, pid int) {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if v, ok := strings.CutPrefix(line, "state = "); ok {
			running = strings.EqualFold(strings.TrimSpace(v), "running")
		}
		if v, ok := strings.CutPrefix(line, "pid = "); ok {
			if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
				pid = n
			}
		}
	}
	return running, pid
}

func runLaunchctl(args ...string) (string, error) {
	cmd := exec.Command("launchctl", args...)
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// EnableLingerCurrentUser is a no-op on macOS: a LaunchAgent starts at user
// login and is kept alive by launchd. There is no systemd-style "linger"; to
// run before login you would need a root LaunchDaemon, which is out of scope.
func EnableLingerCurrentUser() error {
	return nil
}
