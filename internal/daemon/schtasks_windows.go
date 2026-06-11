//go:build windows

package daemon

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"unicode/utf16"
)

// schtasksManager runs Feidex as a per-user Windows Scheduled Task.
//
// This is the no-admin equivalent of the Linux user systemd unit and the macOS
// per-user LaunchAgent: a task scoped to the current user needs no elevation, an
// at-logon trigger starts it automatically, and Task Scheduler restarts it on
// failure. Like the macOS LaunchAgent, it runs only while the user is logged on;
// surviving logout would require stored credentials or a Windows service, both of
// which pull in privileges or a password we deliberately avoid.
//
// The task action launches a generated VBScript through wscript.exe. wscript is a
// windowless (GUI-subsystem) host, so the daemon starts with no flashing console
// window; the script runs `feidex serve` via cmd with stdout/stderr redirected to
// a log file and waits on it, so the task stays in the Running state for as long
// as the daemon lives and propagates its exit code for restart-on-failure.
type schtasksManager struct {
	serviceName string
}

func newPlatformManager(serviceName string) (Manager, error) {
	if _, err := exec.LookPath("schtasks"); err != nil {
		return nil, fmt.Errorf("schtasks.exe not found: Windows Task Scheduler is required")
	}
	return &schtasksManager{serviceName: normalizeServiceName(serviceName)}, nil
}

func (m *schtasksManager) Platform() string {
	return "Task Scheduler (Windows)"
}

func (m *schtasksManager) taskName() string {
	return normalizeServiceName(m.serviceName)
}

// stateDir holds the generated launcher script, task XML, and log file. It mirrors
// where macOS keeps its LaunchAgent log: a per-user, non-roaming location.
func (m *schtasksManager) stateDir() string {
	base := strings.TrimSpace(os.Getenv("LOCALAPPDATA"))
	if base == "" {
		if home, err := os.UserHomeDir(); err == nil {
			base = filepath.Join(home, "AppData", "Local")
		}
	}
	return filepath.Join(base, "feidex")
}

// LogFile returns the file the daemon's stdout/stderr is redirected to. The CLI
// tails this file, the same way it does for the macOS LaunchAgent log.
func (m *schtasksManager) LogFile() string {
	return filepath.Join(m.stateDir(), m.taskName()+".log")
}

func (m *schtasksManager) launcherPath() string {
	return filepath.Join(m.stateDir(), m.taskName()+"-launch.vbs")
}

func (m *schtasksManager) taskXMLPath() string {
	return filepath.Join(m.stateDir(), m.taskName()+"-task.xml")
}

func (m *schtasksManager) Install(cfg Config) error {
	if err := os.MkdirAll(m.stateDir(), 0o755); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}
	if err := os.WriteFile(m.launcherPath(), []byte(m.buildLauncherVBS(cfg)), 0o644); err != nil {
		return fmt.Errorf("write launcher: %w", err)
	}
	xml, err := m.buildTaskXML()
	if err != nil {
		return fmt.Errorf("build task xml: %w", err)
	}
	// schtasks /Create /XML requires a UTF-16 (LE, BOM) document.
	if err := os.WriteFile(m.taskXMLPath(), encodeUTF16LE(xml), 0o644); err != nil {
		return fmt.Errorf("write task xml: %w", err)
	}
	// /F replaces any existing task so reinstall is idempotent.
	if out, err := runSchtasks("/Create", "/TN", m.taskName(), "/XML", m.taskXMLPath(), "/F"); err != nil {
		return fmt.Errorf("schtasks create: %s (%w)", out, err)
	}
	if out, err := runSchtasks("/Run", "/TN", m.taskName()); err != nil {
		return fmt.Errorf("schtasks run: %s (%w)", out, err)
	}
	return nil
}

func (m *schtasksManager) Uninstall() error {
	_, _ = runSchtasks("/End", "/TN", m.taskName())          // best-effort stop
	_, _ = runSchtasks("/Delete", "/TN", m.taskName(), "/F") // best-effort delete
	if err := os.Remove(m.launcherPath()); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove launcher: %w", err)
	}
	if err := os.Remove(m.taskXMLPath()); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove task xml: %w", err)
	}
	return nil
}

func (m *schtasksManager) Start() error {
	if out, err := runSchtasks("/Run", "/TN", m.taskName()); err != nil {
		return fmt.Errorf("start: %s (%w)", out, err)
	}
	return nil
}

func (m *schtasksManager) Stop() error {
	if out, err := runSchtasks("/End", "/TN", m.taskName()); err != nil {
		return fmt.Errorf("stop: %s (%w)", out, err)
	}
	return nil
}

func (m *schtasksManager) Restart() error {
	_, _ = runSchtasks("/End", "/TN", m.taskName())
	if out, err := runSchtasks("/Run", "/TN", m.taskName()); err != nil {
		return fmt.Errorf("restart: %s (%w)", out, err)
	}
	return nil
}

func (m *schtasksManager) Status() (*Status, error) {
	st := &Status{
		Platform: m.Platform(),
		UnitPath: `Task Scheduler\` + m.taskName(),
	}
	// The scheduled task is the source of truth for "installed"; a zero exit
	// from /Query means the task exists.
	if _, err := runSchtasks("/Query", "/TN", m.taskName()); err != nil {
		return st, nil
	}
	st.Installed = true
	st.Running = m.taskRunning()
	return st, nil
}

// taskRunning reports whether the scheduled task is currently executing. It reads
// the task State via PowerShell's ScheduledTasks module because that value is a
// culture-invariant enum ("Running"/"Ready"/"Disabled"), unlike the localized
// text schtasks /Query prints. If PowerShell is unavailable it conservatively
// reports false.
func (m *schtasksManager) taskRunning() bool {
	script := fmt.Sprintf(
		"$ErrorActionPreference='SilentlyContinue';(Get-ScheduledTask -TaskName %s -TaskPath '\\').State",
		powershellSingleQuote(m.taskName()),
	)
	out, err := runPowerShell(script)
	if err != nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(out), "Running")
}

// buildLauncherVBS writes a tiny windowless launcher. wscript runs it with no
// console; it starts `feidex serve` (output appended to the log file) hidden and
// waits, exiting with the daemon's exit code so Task Scheduler can restart it on
// failure.
func (m *schtasksManager) buildLauncherVBS(cfg Config) string {
	inner := fmt.Sprintf(`"%s" serve --config "%s" >> "%s" 2>&1`, cfg.BinaryPath, cfg.ConfigPath, m.LogFile())
	cmdline := `cmd /c "` + inner + `"`
	var sb strings.Builder
	sb.WriteString("' Auto-generated by `feidex daemon install`. Do not edit.\r\n")
	sb.WriteString("Set sh = CreateObject(\"WScript.Shell\")\r\n")
	sb.WriteString("WScript.Quit sh.Run(" + vbsString(cmdline) + ", 0, True)\r\n")
	return sb.String()
}

// buildTaskXML renders a Task Scheduler 1.2 document for a per-user, at-logon
// task with restart-on-failure and no execution time limit.
func (m *schtasksManager) buildTaskXML() (string, error) {
	u, err := user.Current()
	if err != nil {
		return "", fmt.Errorf("current user: %w", err)
	}
	userID := u.Username
	var sb strings.Builder
	sb.WriteString(`<?xml version="1.0" encoding="UTF-16"?>` + "\r\n")
	sb.WriteString(`<Task version="1.2" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">` + "\r\n")
	sb.WriteString("  <RegistrationInfo>\r\n")
	fmt.Fprintf(&sb, "    <Description>%s - Feishu Codex Bridge</Description>\r\n", xmlEscape(m.taskName()))
	sb.WriteString("  </RegistrationInfo>\r\n")
	sb.WriteString("  <Triggers>\r\n")
	sb.WriteString("    <LogonTrigger>\r\n")
	sb.WriteString("      <Enabled>true</Enabled>\r\n")
	fmt.Fprintf(&sb, "      <UserId>%s</UserId>\r\n", xmlEscape(userID))
	sb.WriteString("    </LogonTrigger>\r\n")
	sb.WriteString("  </Triggers>\r\n")
	sb.WriteString("  <Principals>\r\n")
	sb.WriteString(`    <Principal id="Author">` + "\r\n")
	fmt.Fprintf(&sb, "      <UserId>%s</UserId>\r\n", xmlEscape(userID))
	sb.WriteString("      <LogonType>InteractiveToken</LogonType>\r\n")
	sb.WriteString("      <RunLevel>LeastPrivilege</RunLevel>\r\n")
	sb.WriteString("    </Principal>\r\n")
	sb.WriteString("  </Principals>\r\n")
	sb.WriteString("  <Settings>\r\n")
	sb.WriteString("    <MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>\r\n")
	sb.WriteString("    <DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries>\r\n")
	sb.WriteString("    <StopIfGoingOnBatteries>false</StopIfGoingOnBatteries>\r\n")
	sb.WriteString("    <AllowHardTerminate>true</AllowHardTerminate>\r\n")
	sb.WriteString("    <StartWhenAvailable>true</StartWhenAvailable>\r\n")
	sb.WriteString("    <RunOnlyIfNetworkAvailable>false</RunOnlyIfNetworkAvailable>\r\n")
	sb.WriteString("    <IdleSettings>\r\n")
	sb.WriteString("      <StopOnIdleEnd>false</StopOnIdleEnd>\r\n")
	sb.WriteString("      <RestartOnIdle>false</RestartOnIdle>\r\n")
	sb.WriteString("    </IdleSettings>\r\n")
	sb.WriteString("    <AllowStartOnDemand>true</AllowStartOnDemand>\r\n")
	sb.WriteString("    <Enabled>true</Enabled>\r\n")
	sb.WriteString("    <Hidden>false</Hidden>\r\n")
	sb.WriteString("    <RunOnlyIfIdle>false</RunOnlyIfIdle>\r\n")
	sb.WriteString("    <WakeToRun>false</WakeToRun>\r\n")
	sb.WriteString("    <ExecutionTimeLimit>PT0S</ExecutionTimeLimit>\r\n")
	sb.WriteString("    <Priority>7</Priority>\r\n")
	sb.WriteString("    <RestartOnFailure>\r\n")
	sb.WriteString("      <Interval>PT1M</Interval>\r\n")
	sb.WriteString("      <Count>9999</Count>\r\n")
	sb.WriteString("    </RestartOnFailure>\r\n")
	sb.WriteString("  </Settings>\r\n")
	sb.WriteString(`  <Actions Context="Author">` + "\r\n")
	sb.WriteString("    <Exec>\r\n")
	sb.WriteString("      <Command>wscript.exe</Command>\r\n")
	fmt.Fprintf(&sb, "      <Arguments>%s</Arguments>\r\n", xmlEscape(`"`+m.launcherPath()+`"`))
	sb.WriteString("    </Exec>\r\n")
	sb.WriteString("  </Actions>\r\n")
	sb.WriteString("</Task>\r\n")
	return sb.String(), nil
}

func runSchtasks(args ...string) (string, error) {
	cmd := exec.Command("schtasks", args...)
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func runPowerShell(script string) (string, error) {
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", script)
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// vbsString renders a Go string as a VBScript double-quoted literal, doubling any
// embedded quotes per VBScript escaping rules.
func vbsString(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

// powershellSingleQuote renders a value as a PowerShell single-quoted literal,
// doubling embedded single quotes.
func powershellSingleQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// xmlEscape mirrors the helper used by the macOS plist builder.
func xmlEscape(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&apos;")
	return r.Replace(s)
}

// encodeUTF16LE encodes text as UTF-16 little-endian with a BOM, the format
// schtasks /Create /XML expects.
func encodeUTF16LE(s string) []byte {
	units := utf16.Encode([]rune(s))
	buf := make([]byte, 0, 2+len(units)*2)
	buf = append(buf, 0xFF, 0xFE) // little-endian BOM
	for _, u := range units {
		buf = append(buf, byte(u), byte(u>>8))
	}
	return buf
}

// EnableLingerCurrentUser is a no-op on Windows. The scheduled task starts at
// logon and is managed by Task Scheduler; there is no systemd-style linger, and a
// no-admin task intentionally runs only within the user's logon session.
func EnableLingerCurrentUser() error {
	return nil
}
