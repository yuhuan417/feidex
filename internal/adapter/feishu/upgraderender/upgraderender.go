// Package upgraderender holds the pure rendering functions for backend
// upgrade and restart cards. Both backends share one implementation,
// parameterized by a Spec.
package upgraderender

import (
	"feidex/internal/textutil"
	"strings"
	"time"

	"feidex/internal/adapter/feishu/backend"
	"feidex/internal/feishu"
	"feidex/internal/install"
)

// DisplayLocation is the timezone used when formatting upgrade/restart
// timestamps. Defaults to time.Local; callers may override for tests.
var DisplayLocation = time.Local

// BackendUpgradeSnapshot mirrors the type used in the app package.
type BackendUpgradeSnapshot = backend.BackendUpgradeSnapshot

// BackendRestartSnapshot mirrors the type used in the app package.
type BackendRestartSnapshot = backend.BackendRestartSnapshot

func RenderInstallSource(spec Spec, probe install.Probe) string {
	if probe.Supported || strings.TrimSpace(probe.CurrentVersion) != "" {
		return spec.InstallSource
	}
	return "-"
}

func RenderUpgradeAvailability(view UpgradeView, latestChecked bool) string {
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

func RenderUpgradeRuntimeLine(view UpgradeView) string {
	if strings.TrimSpace(view.BusyReason) != "" {
		return "`busy` (" + strings.TrimSpace(view.BusyReason) + ")"
	}
	if view.Snapshot.Running || view.Restart.Running {
		return "`maintenance`"
	}
	return "`idle`"
}

func FormatUpgradeTime(ts time.Time) string {
	if ts.IsZero() {
		return ""
	}
	return ts.In(DisplayLocation).Format("2006-01-02 15:04:05")
}

func UpgradePhaseText(phase string) string {
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
		return textutil.FirstNonEmpty(strings.TrimSpace(phase), "-")
	}
}

func UpgradeResultText(result string) string {
	switch strings.TrimSpace(result) {
	case "success":
		return "success"
	case "rolled_back":
		return "rolled_back"
	case "rollback_failed":
		return "rollback_failed"
	default:
		return textutil.FirstNonEmpty(strings.TrimSpace(result), "-")
	}
}

func RestartPhaseText(phase string) string {
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
		return textutil.FirstNonEmpty(strings.TrimSpace(phase), "-")
	}
}

func RestartResultText(result string) string {
	switch strings.TrimSpace(result) {
	case "success":
		return "success"
	case "failed":
		return "failed"
	default:
		return textutil.FirstNonEmpty(strings.TrimSpace(result), "-")
	}
}

// UpgradeStatusButtons builds the forward controls of the upgrade panel. The
// back control is injected by menuutil.PageCard from the declared node.
func UpgradeStatusButtons(spec Spec, sessionKey string, running bool) []feishu.Button {
	buttons := []feishu.Button{
		{
			Text: "刷新状态",
			Type: "default",
			Value: map[string]any{
				"action":      spec.ActionPrefix + ".refresh",
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
					"action":      spec.ActionPrefix + ".check",
					"session_key": sessionKey,
				},
			},
			feishu.Button{
				Text: "运行自升级",
				Type: "primary",
				Value: map[string]any{
					"action":      spec.ActionPrefix + ".prepare",
					"session_key": sessionKey,
				},
			},
			feishu.Button{
				Text: "原地重启 Runtime",
				Type: "default",
				Value: map[string]any{
					"action":      spec.RestartAction,
					"session_key": sessionKey,
				},
			},
		)
	}
	return buttons
}
