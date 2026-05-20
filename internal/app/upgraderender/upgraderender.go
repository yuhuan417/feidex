// Package upgraderender holds pure rendering functions for Claude and Codex
// upgrade/restart cards. These were extracted from the app package to reduce
// the size of the "god package" and make the rendering logic independently
// testable.
package upgraderender

import (
	"strings"
	"time"

	"feidex/internal/app/apputil"
	"feidex/internal/app/backend"
	"feidex/internal/claudeinstall"
	"feidex/internal/codexinstall"
	"feidex/internal/feishu"
)

// DisplayLocation is the timezone used when formatting upgrade/restart
// timestamps. Defaults to time.Local; callers may override for tests.
var DisplayLocation = time.Local

// BackendUpgradeSnapshot mirrors the type used in the app package.
type BackendUpgradeSnapshot = backend.BackendUpgradeSnapshot

// BackendRestartSnapshot mirrors the type used in the app package.
type BackendRestartSnapshot = backend.BackendRestartSnapshot

// ClaudeUpgradeView mirrors the claudeUpgradeView type in the app package.
type ClaudeUpgradeView struct {
	Probe         claudeinstall.Probe
	LatestVersion string
	LatestError   string
	BusyReason    string
	Snapshot      BackendUpgradeSnapshot
	Restart       BackendRestartSnapshot
}

// CodexUpgradeView mirrors the codexUpgradeView type in the app package.
type CodexUpgradeView struct {
	Probe         codexinstall.Probe
	LatestVersion string
	LatestError   string
	BusyReason    string
	Snapshot      BackendUpgradeSnapshot
	Restart       BackendRestartSnapshot
}

// ---------------------------------------------------------------------------
// Claude rendering helpers
// ---------------------------------------------------------------------------

func RenderClaudeInstallSource(probe claudeinstall.Probe) string {
	if probe.Supported || strings.TrimSpace(probe.CurrentVersion) != "" {
		return "Claude CLI"
	}
	return "-"
}

func RenderClaudeUpgradeAvailability(view ClaudeUpgradeView, latestChecked bool) string {
	switch {
	case view.Snapshot.Running || view.Restart.Running:
		return "`维护中`"
	case !view.Probe.Supported:
		return "`不支持自动升级`"
	case strings.TrimSpace(view.BusyReason) != "":
		return "`暂不可升级`"
	case !latestChecked:
		return "`等待检查`"
	case strings.TrimSpace(view.LatestError) != "":
		return "`检查失败`"
	case strings.TrimSpace(view.LatestVersion) == "":
		return "`未知`"
	case !strings.EqualFold(strings.TrimSpace(view.LatestVersion), "latest") && strings.TrimSpace(view.LatestVersion) == strings.TrimSpace(view.Probe.CurrentVersion):
		return "`已是最新`"
	default:
		return "`可执行自升级`"
	}
}

func RenderClaudeUpgradeRuntimeLine(view ClaudeUpgradeView) string {
	if strings.TrimSpace(view.BusyReason) != "" {
		return "`busy` (" + strings.TrimSpace(view.BusyReason) + ")"
	}
	if view.Snapshot.Running || view.Restart.Running {
		return "`maintenance`"
	}
	return "`idle`"
}

func FormatClaudeUpgradeTime(ts time.Time) string {
	if ts.IsZero() {
		return ""
	}
	return ts.In(DisplayLocation).Format("2006-01-02 15:04:05")
}

func ClaudeUpgradePhaseText(phase string) string {
	switch strings.TrimSpace(phase) {
	case "preflight":
		return "preflight"
	case "installing":
		return "installing"
	case "smoke_testing":
		return "smoke_testing"
	case "rolling_back":
		return "rolling_back"
	case "completed":
		return "completed"
	case "failed":
		return "failed"
	default:
		return apputil.FirstNonEmpty(strings.TrimSpace(phase), "-")
	}
}

func ClaudeUpgradeResultText(result string) string {
	switch strings.TrimSpace(result) {
	case "success":
		return "success"
	case "rolled_back":
		return "rolled_back"
	case "rollback_failed":
		return "rollback_failed"
	default:
		return apputil.FirstNonEmpty(strings.TrimSpace(result), "-")
	}
}

func ClaudeRestartPhaseText(phase string) string {
	switch strings.TrimSpace(phase) {
	case "preflight":
		return "preflight"
	case "restarting":
		return "restarting"
	case "smoke_testing":
		return "smoke_testing"
	case "completed":
		return "completed"
	case "failed":
		return "failed"
	default:
		return apputil.FirstNonEmpty(strings.TrimSpace(phase), "-")
	}
}

func ClaudeRestartResultText(result string) string {
	switch strings.TrimSpace(result) {
	case "success":
		return "success"
	case "failed":
		return "failed"
	default:
		return apputil.FirstNonEmpty(strings.TrimSpace(result), "-")
	}
}

func ClaudeUpgradeStatusButtons(sessionKey string, running bool) []feishu.Button {
	buttons := []feishu.Button{
		{
			Text: "刷新状态",
			Type: "default",
			Value: map[string]any{
				"action":      "claude_upgrade.refresh",
				"session_key": sessionKey,
			},
		},
	}
	if !running {
		buttons = append(buttons,
			feishu.Button{
				Text: "检查更新",
				Type: "default",
				Value: map[string]any{
					"action":      "claude_upgrade.check",
					"session_key": sessionKey,
				},
			},
			feishu.Button{
				Text: "运行自升级",
				Type: "primary",
				Value: map[string]any{
					"action":      "claude_upgrade.prepare",
					"session_key": sessionKey,
				},
			},
			feishu.Button{
				Text: "原地重启 Runtime",
				Type: "default",
				Value: map[string]any{
					"action":      "claude_restart.run",
					"session_key": sessionKey,
				},
			},
		)
	}
	buttons = append(buttons, feishu.Button{
		Text: "返回上一级",
		Type: "default",
		Value: map[string]any{
			"action":      "menu.group.backend",
			"session_key": sessionKey,
		},
	})
	return buttons
}

// ---------------------------------------------------------------------------
// Codex rendering helpers
// ---------------------------------------------------------------------------

func RenderCodexInstallSource(probe codexinstall.Probe) string {
	if probe.Supported || strings.TrimSpace(probe.CurrentVersion) != "" {
		return "Codex CLI"
	}
	return "-"
}

func RenderCodexUpgradeAvailability(view CodexUpgradeView, latestChecked bool) string {
	switch {
	case view.Snapshot.Running || view.Restart.Running:
		return "`维护中`"
	case !view.Probe.Supported:
		return "`不支持自动升级`"
	case strings.TrimSpace(view.BusyReason) != "":
		return "`暂不可升级`"
	case !latestChecked:
		return "`等待检查`"
	case strings.TrimSpace(view.LatestError) != "":
		return "`检查失败`"
	case strings.TrimSpace(view.LatestVersion) == "":
		return "`未知`"
	case !strings.EqualFold(strings.TrimSpace(view.LatestVersion), "latest") && strings.TrimSpace(view.LatestVersion) == strings.TrimSpace(view.Probe.CurrentVersion):
		return "`已是最新`"
	default:
		return "`可执行自升级`"
	}
}

func RenderCodexUpgradeRuntimeLine(view CodexUpgradeView) string {
	if strings.TrimSpace(view.BusyReason) != "" {
		return "`busy` (" + strings.TrimSpace(view.BusyReason) + ")"
	}
	if view.Snapshot.Running || view.Restart.Running {
		return "`maintenance`"
	}
	return "`idle`"
}

func FormatCodexUpgradeTime(ts time.Time) string {
	if ts.IsZero() {
		return ""
	}
	return ts.In(DisplayLocation).Format("2006-01-02 15:04:05")
}

func CodexUpgradePhaseText(phase string) string {
	switch strings.TrimSpace(phase) {
	case "preflight":
		return "preflight"
	case "installing":
		return "installing"
	case "smoke_testing":
		return "smoke_testing"
	case "rolling_back":
		return "rolling_back"
	case "completed":
		return "completed"
	case "failed":
		return "failed"
	default:
		return apputil.FirstNonEmpty(strings.TrimSpace(phase), "-")
	}
}

func CodexUpgradeResultText(result string) string {
	switch strings.TrimSpace(result) {
	case "success":
		return "success"
	case "rolled_back":
		return "rolled_back"
	case "rollback_failed":
		return "rollback_failed"
	default:
		return apputil.FirstNonEmpty(strings.TrimSpace(result), "-")
	}
}

func CodexRestartPhaseText(phase string) string {
	switch strings.TrimSpace(phase) {
	case "preflight":
		return "preflight"
	case "restarting":
		return "restarting"
	case "smoke_testing":
		return "smoke_testing"
	case "completed":
		return "completed"
	case "failed":
		return "failed"
	default:
		return apputil.FirstNonEmpty(strings.TrimSpace(phase), "-")
	}
}

func CodexRestartResultText(result string) string {
	switch strings.TrimSpace(result) {
	case "success":
		return "success"
	case "failed":
		return "failed"
	default:
		return apputil.FirstNonEmpty(strings.TrimSpace(result), "-")
	}
}

func CodexUpgradeStatusButtons(sessionKey string, running bool) []feishu.Button {
	buttons := []feishu.Button{
		{
			Text: "刷新状态",
			Type: "default",
			Value: map[string]any{
				"action":      "codex_upgrade.refresh",
				"session_key": sessionKey,
			},
		},
	}
	if !running {
		buttons = append(buttons,
			feishu.Button{
				Text: "检查更新",
				Type: "default",
				Value: map[string]any{
					"action":      "codex_upgrade.check",
					"session_key": sessionKey,
				},
			},
			feishu.Button{
				Text: "运行自升级",
				Type: "primary",
				Value: map[string]any{
					"action":      "codex_upgrade.prepare",
					"session_key": sessionKey,
				},
			},
			feishu.Button{
				Text: "原地重启 Runtime",
				Type: "default",
				Value: map[string]any{
					"action":      "codex_restart.run",
					"session_key": sessionKey,
				},
			},
		)
	}
	buttons = append(buttons, feishu.Button{
		Text: "返回上一级",
		Type: "default",
		Value: map[string]any{
			"action":      "menu.group.backend",
			"session_key": sessionKey,
		},
	})
	return buttons
}
