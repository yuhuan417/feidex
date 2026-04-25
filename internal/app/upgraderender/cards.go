package upgraderender

import (
	"strings"
	"time"

	"feidex/internal/app/apputil"
	appmenuutil "feidex/internal/app/menuutil"
	"feidex/internal/feishu"
)

// StatusCardRenderer abstracts the feishu card rendering dependency so that
// card-building functions do not need to import the app package.
type StatusCardRenderer interface {
	SimpleStatusCard(title, color, body string, buttons []feishu.Button) map[string]any
}

// PendingSaver abstracts the pending-request persistence dependency.
type PendingSaver interface {
	NextLocalID(prefix string) (string, error)
	SavePending(kind, sessionKey, ownerUserID, requestID, payloadJSON string, ttl time.Duration) error
}

// ---------------------------------------------------------------------------
// Codex card rendering
// ---------------------------------------------------------------------------

func RenderCodexUpgradeStatusCard(r StatusCardRenderer, sessionKey string, view CodexUpgradeView, latestChecked bool) map[string]any {
	snapshot := view.Snapshot
	restart := view.Restart
	lines := []string{
		"command: `" + apputil.FirstNonEmpty(view.Probe.Command, "codex") + "`",
		"解析路径: `" + apputil.FirstNonEmpty(view.Probe.CommandPath, "-") + "`",
		"安装来源: `" + RenderCodexInstallSource(view.Probe) + "`",
		"npm: `" + apputil.FirstNonEmpty(view.Probe.NPMPath, "-") + "`",
		"当前版本: `" + apputil.FirstNonEmpty(view.Probe.CurrentVersion, "-") + "`",
	}
	if strings.TrimSpace(view.Probe.Reason) != "" {
		lines = append(lines, "原因: "+strings.TrimSpace(view.Probe.Reason))
	}
	if latestChecked {
		switch {
		case strings.TrimSpace(view.LatestVersion) != "":
			lines = append(lines, "最新稳定版: `"+view.LatestVersion+"`")
		case strings.TrimSpace(view.LatestError) != "":
			lines = append(lines, "最新稳定版: 检查失败", "错误: "+view.LatestError)
		default:
			lines = append(lines, "最新稳定版: `-`")
		}
	} else {
		lines = append(lines, "最新稳定版: `未检查`")
	}
	lines = append(lines,
		"状态: "+RenderCodexUpgradeAvailability(view, latestChecked),
		"runtime: "+RenderCodexUpgradeRuntimeLine(view),
		"smoke test: `initialize + model/list`",
		"回滚策略: `npm reinstall previous version`",
	)
	if snapshot.Running {
		lines = append(lines,
			"",
			"当前升级状态: `"+CodexUpgradePhaseText(snapshot.Phase)+"`",
			"进度: "+apputil.FirstNonEmpty(snapshot.Message, "-"),
		)
		if !snapshot.StartedAt.IsZero() {
			lines = append(lines, "开始时间(本机时区): `"+FormatCodexUpgradeTime(snapshot.StartedAt)+"`")
		}
	} else if strings.TrimSpace(snapshot.Result) != "" {
		lines = append(lines,
			"",
			"上次结果: `"+CodexUpgradeResultText(snapshot.Result)+"`",
			"结果摘要: "+apputil.FirstNonEmpty(snapshot.Message, "-"),
		)
		if !snapshot.UpdatedAt.IsZero() {
			lines = append(lines, "完成时间(本机时区): `"+FormatCodexUpgradeTime(snapshot.UpdatedAt)+"`")
		}
	}
	if restart.Running {
		lines = append(lines,
			"",
			"当前重启状态: `"+CodexRestartPhaseText(restart.Phase)+"`",
			"重启进度: "+apputil.FirstNonEmpty(restart.Message, "-"),
		)
		if !restart.StartedAt.IsZero() {
			lines = append(lines, "重启开始时间(本机时区): `"+FormatCodexUpgradeTime(restart.StartedAt)+"`")
		}
	} else if strings.TrimSpace(restart.Result) != "" {
		lines = append(lines,
			"",
			"上次重启结果: `"+CodexRestartResultText(restart.Result)+"`",
			"重启摘要: "+apputil.FirstNonEmpty(restart.Message, "-"),
		)
		if !restart.UpdatedAt.IsZero() {
			lines = append(lines, "重启完成时间(本机时区): `"+FormatCodexUpgradeTime(restart.UpdatedAt)+"`")
		}
	}

	title := "Codex 管理"
	color := "blue"
	switch {
	case snapshot.Running || restart.Running:
		color = "orange"
	case strings.TrimSpace(snapshot.Result) == "success":
		color = "green"
	case strings.TrimSpace(restart.Result) == "success":
		color = "green"
	case strings.TrimSpace(snapshot.Result) != "":
		color = "orange"
	case strings.TrimSpace(restart.Result) != "":
		color = "orange"
	}
	body := appmenuutil.MenuCardBody("menu.codex_upgrade", strings.Join(lines, "\n"))
	return r.SimpleStatusCard(title, color, body, CodexUpgradeStatusButtons(sessionKey, snapshot.Running || restart.Running))
}

func RenderCodexUpgradeConfirmCard(r StatusCardRenderer, sessionKey, requestID string, currentVersion, targetVersion string) map[string]any {
	lines := []string{
		"当前版本: `" + apputil.FirstNonEmpty(currentVersion, "-") + "`",
		"目标版本: `" + apputil.FirstNonEmpty(targetVersion, "-") + "`",
		"升级方式: `npm i -g @openai/codex@" + strings.TrimSpace(targetVersion) + "`",
		"验证方式: `initialize + model/list`",
		"失败处理: 自动回滚到 `" + apputil.FirstNonEmpty(currentVersion, "-") + "`",
		"开始条件: 当前不得有活动任务或待处理审批/表单",
	}
	buttons := []feishu.Button{
		{
			Text: "确认升级",
			Type: "primary",
			Value: map[string]any{
				"action":      "codex_upgrade.confirm",
				"request_id":  requestID,
				"session_key": sessionKey,
			},
		},
		{
			Text: "取消",
			Type: "default",
			Value: map[string]any{
				"action":      "codex_upgrade.cancel",
				"request_id":  requestID,
				"session_key": sessionKey,
			},
		},
		{
			Text: "返回上一级",
			Type: "default",
			Value: map[string]any{
				"action":      "menu.group.backend",
				"session_key": sessionKey,
			},
		},
	}
	body := appmenuutil.MenuCardBody("menu.codex_upgrade", strings.Join(lines, "\n"))
	return r.SimpleStatusCard("Codex 升级确认", "orange", body, buttons)
}

func RenderCodexUpgradePreparingCard(r StatusCardRenderer, body string) map[string]any {
	if strings.TrimSpace(body) == "" {
		body = "正在准备 Codex 升级信息，请稍候。\n\n这张卡片会自动刷新。"
	}
	return r.SimpleStatusCard("Codex 管理", "blue", appmenuutil.MenuCardBody("menu.codex_upgrade", body), nil)
}

func RenderCodexUpgradeFailedCard(r StatusCardRenderer, sessionKey, errText string) map[string]any {
	body := "加载 Codex 升级面板失败。"
	if strings.TrimSpace(errText) != "" {
		body += "\n\n错误: " + strings.TrimSpace(errText)
	}
	return r.SimpleStatusCard("Codex 管理", "orange", appmenuutil.MenuCardBody("menu.codex_upgrade", body), CodexUpgradeStatusButtons(sessionKey, false))
}

func RenderCodexUpgradeOperationCard(r StatusCardRenderer, sessionKey string, snapshot BackendUpgradeSnapshot) map[string]any {
	lines := []string{
		"当前版本: `" + apputil.FirstNonEmpty(snapshot.CurrentVersion, "-") + "`",
		"目标版本: `" + apputil.FirstNonEmpty(snapshot.TargetVersion, "-") + "`",
		"阶段: `" + CodexUpgradePhaseText(snapshot.Phase) + "`",
		"进度: " + apputil.FirstNonEmpty(snapshot.Message, "-"),
	}
	if !snapshot.StartedAt.IsZero() {
		lines = append(lines, "开始时间(本机时区): `"+FormatCodexUpgradeTime(snapshot.StartedAt)+"`")
	}
	if !snapshot.UpdatedAt.IsZero() {
		lines = append(lines, "最近更新(本机时区): `"+FormatCodexUpgradeTime(snapshot.UpdatedAt)+"`")
	}
	title := "Codex 升级中"
	color := "orange"
	buttons := CodexUpgradeStatusButtons(sessionKey, snapshot.Running)
	if !snapshot.Running {
		switch snapshot.Result {
		case "success":
			title = "Codex 升级成功"
			color = "green"
		case "rolled_back":
			title = "Codex 已回滚"
			color = "orange"
		case "rollback_failed":
			title = "Codex 回滚失败"
			color = "red"
		default:
			title = "Codex 升级失败"
			color = "orange"
		}
		lines = append(lines, "结果: `"+CodexUpgradeResultText(snapshot.Result)+"`")
	}
	body := appmenuutil.MenuCardBody("menu.codex_upgrade", strings.Join(lines, "\n"))
	return r.SimpleStatusCard(title, color, body, buttons)
}

func RenderCodexRestartOperationCard(r StatusCardRenderer, sessionKey string, snapshot BackendRestartSnapshot) map[string]any {
	lines := []string{
		"当前版本: `" + apputil.FirstNonEmpty(snapshot.CurrentVersion, "-") + "`",
		"阶段: `" + CodexRestartPhaseText(snapshot.Phase) + "`",
		"进度: " + apputil.FirstNonEmpty(snapshot.Message, "-"),
	}
	if !snapshot.StartedAt.IsZero() {
		lines = append(lines, "开始时间(本机时区): `"+FormatCodexUpgradeTime(snapshot.StartedAt)+"`")
	}
	if !snapshot.UpdatedAt.IsZero() {
		lines = append(lines, "最近更新(本机时区): `"+FormatCodexUpgradeTime(snapshot.UpdatedAt)+"`")
	}
	title := "Codex Runtime 重启中"
	color := "orange"
	if !snapshot.Running {
		switch snapshot.Result {
		case "success":
			title = "Codex Runtime 已重启"
			color = "green"
		default:
			title = "Codex Runtime 重启失败"
			color = "orange"
		}
		lines = append(lines, "结果: `"+CodexRestartResultText(snapshot.Result)+"`")
	}
	body := appmenuutil.MenuCardBody("menu.codex_upgrade", strings.Join(lines, "\n"))
	return r.SimpleStatusCard(title, color, body, CodexUpgradeStatusButtons(sessionKey, snapshot.Running))
}

// ---------------------------------------------------------------------------
// Claude card rendering
// ---------------------------------------------------------------------------

func RenderClaudeUpgradeStatusCard(r StatusCardRenderer, sessionKey string, view ClaudeUpgradeView, latestChecked bool) map[string]any {
	snapshot := view.Snapshot
	restart := view.Restart
	lines := []string{
		"command: `" + apputil.FirstNonEmpty(view.Probe.Command, "claude") + "`",
		"解析路径: `" + apputil.FirstNonEmpty(view.Probe.CommandPath, "-") + "`",
		"安装来源: `" + RenderClaudeInstallSource(view.Probe) + "`",
		"npm: `" + apputil.FirstNonEmpty(view.Probe.NPMPath, "-") + "`",
		"当前版本: `" + apputil.FirstNonEmpty(view.Probe.CurrentVersion, "-") + "`",
	}
	if strings.TrimSpace(view.Probe.Reason) != "" {
		lines = append(lines, "原因: "+strings.TrimSpace(view.Probe.Reason))
	}
	if latestChecked {
		switch {
		case strings.TrimSpace(view.LatestVersion) != "":
			lines = append(lines, "最新稳定版: `"+view.LatestVersion+"`")
		case strings.TrimSpace(view.LatestError) != "":
			lines = append(lines, "最新稳定版: 检查失败", "错误: "+view.LatestError)
		default:
			lines = append(lines, "最新稳定版: `-`")
		}
	} else {
		lines = append(lines, "最新稳定版: `未检查`")
	}
	lines = append(lines,
		"状态: "+RenderClaudeUpgradeAvailability(view, latestChecked),
		"runtime: "+RenderClaudeUpgradeRuntimeLine(view),
		"smoke test: `start + init`",
		"回滚策略: `npm reinstall previous version`",
	)
	if snapshot.Running {
		lines = append(lines,
			"",
			"当前升级状态: `"+ClaudeUpgradePhaseText(snapshot.Phase)+"`",
			"进度: "+apputil.FirstNonEmpty(snapshot.Message, "-"),
		)
		if !snapshot.StartedAt.IsZero() {
			lines = append(lines, "开始时间(本机时区): `"+FormatClaudeUpgradeTime(snapshot.StartedAt)+"`")
		}
	} else if strings.TrimSpace(snapshot.Result) != "" {
		lines = append(lines,
			"",
			"上次结果: `"+ClaudeUpgradeResultText(snapshot.Result)+"`",
			"结果摘要: "+apputil.FirstNonEmpty(snapshot.Message, "-"),
		)
		if !snapshot.UpdatedAt.IsZero() {
			lines = append(lines, "完成时间(本机时区): `"+FormatClaudeUpgradeTime(snapshot.UpdatedAt)+"`")
		}
	}
	if restart.Running {
		lines = append(lines,
			"",
			"当前重启状态: `"+ClaudeRestartPhaseText(restart.Phase)+"`",
			"重启进度: "+apputil.FirstNonEmpty(restart.Message, "-"),
		)
		if !restart.StartedAt.IsZero() {
			lines = append(lines, "重启开始时间(本机时区): `"+FormatClaudeUpgradeTime(restart.StartedAt)+"`")
		}
	} else if strings.TrimSpace(restart.Result) != "" {
		lines = append(lines,
			"",
			"上次重启结果: `"+ClaudeRestartResultText(restart.Result)+"`",
			"重启摘要: "+apputil.FirstNonEmpty(restart.Message, "-"),
		)
		if !restart.UpdatedAt.IsZero() {
			lines = append(lines, "重启完成时间(本机时区): `"+FormatClaudeUpgradeTime(restart.UpdatedAt)+"`")
		}
	}

	title := "Claude 管理"
	color := "blue"
	switch {
	case snapshot.Running || restart.Running:
		color = "orange"
	case strings.TrimSpace(snapshot.Result) == "success":
		color = "green"
	case strings.TrimSpace(restart.Result) == "success":
		color = "green"
	case strings.TrimSpace(snapshot.Result) != "":
		color = "orange"
	case strings.TrimSpace(restart.Result) != "":
		color = "orange"
	}
	body := appmenuutil.MenuCardBody("menu.claude_upgrade", strings.Join(lines, "\n"))
	return r.SimpleStatusCard(title, color, body, ClaudeUpgradeStatusButtons(sessionKey, snapshot.Running || restart.Running))
}

func RenderClaudeUpgradeConfirmCard(r StatusCardRenderer, sessionKey, requestID string, currentVersion, targetVersion string) map[string]any {
	lines := []string{
		"当前版本: `" + apputil.FirstNonEmpty(currentVersion, "-") + "`",
		"目标版本: `" + apputil.FirstNonEmpty(targetVersion, "-") + "`",
		"升级方式: `npm i -g @anthropic-ai/claude-code@" + strings.TrimSpace(targetVersion) + "`",
		"失败处理: 自动回滚到 `" + apputil.FirstNonEmpty(currentVersion, "-") + "`",
		"开始条件: 当前不得有活动任务或待处理审批/表单",
	}
	buttons := []feishu.Button{
		{
			Text: "确认升级",
			Type: "primary",
			Value: map[string]any{
				"action":      "claude_upgrade.confirm",
				"request_id":  requestID,
				"session_key": sessionKey,
			},
		},
		{
			Text: "取消",
			Type: "default",
			Value: map[string]any{
				"action":      "claude_upgrade.cancel",
				"request_id":  requestID,
				"session_key": sessionKey,
			},
		},
		{
			Text: "返回上一级",
			Type: "default",
			Value: map[string]any{
				"action":      "menu.group.backend",
				"session_key": sessionKey,
			},
		},
	}
	body := appmenuutil.MenuCardBody("menu.claude_upgrade", strings.Join(lines, "\n"))
	return r.SimpleStatusCard("Claude 升级确认", "orange", body, buttons)
}

func RenderClaudeUpgradePreparingCard(r StatusCardRenderer, body string) map[string]any {
	if strings.TrimSpace(body) == "" {
		body = "正在准备 Claude 升级信息，请稍候。\n\n这张卡片会自动刷新。"
	}
	return r.SimpleStatusCard("Claude 管理", "blue", appmenuutil.MenuCardBody("menu.claude_upgrade", body), nil)
}

func RenderClaudeUpgradeFailedCard(r StatusCardRenderer, sessionKey, errText string) map[string]any {
	body := "加载 Claude 升级面板失败。"
	if strings.TrimSpace(errText) != "" {
		body += "\n\n错误: " + strings.TrimSpace(errText)
	}
	return r.SimpleStatusCard("Claude 管理", "orange", appmenuutil.MenuCardBody("menu.claude_upgrade", body), ClaudeUpgradeStatusButtons(sessionKey, false))
}

func RenderClaudeUpgradeOperationCard(r StatusCardRenderer, sessionKey string, snapshot BackendUpgradeSnapshot) map[string]any {
	lines := []string{
		"当前版本: `" + apputil.FirstNonEmpty(snapshot.CurrentVersion, "-") + "`",
		"目标版本: `" + apputil.FirstNonEmpty(snapshot.TargetVersion, "-") + "`",
		"阶段: `" + ClaudeUpgradePhaseText(snapshot.Phase) + "`",
		"进度: " + apputil.FirstNonEmpty(snapshot.Message, "-"),
	}
	if !snapshot.StartedAt.IsZero() {
		lines = append(lines, "开始时间(本机时区): `"+FormatClaudeUpgradeTime(snapshot.StartedAt)+"`")
	}
	if !snapshot.UpdatedAt.IsZero() {
		lines = append(lines, "最近更新(本机时区): `"+FormatClaudeUpgradeTime(snapshot.UpdatedAt)+"`")
	}
	title := "Claude 升级中"
	color := "orange"
	buttons := ClaudeUpgradeStatusButtons(sessionKey, snapshot.Running)
	if !snapshot.Running {
		switch snapshot.Result {
		case "success":
			title = "Claude 升级成功"
			color = "green"
		case "rolled_back":
			title = "Claude 已回滚"
			color = "orange"
		case "rollback_failed":
			title = "Claude 回滚失败"
			color = "red"
		default:
			title = "Claude 升级失败"
			color = "orange"
		}
		lines = append(lines, "结果: `"+ClaudeUpgradeResultText(snapshot.Result)+"`")
	}
	body := appmenuutil.MenuCardBody("menu.claude_upgrade", strings.Join(lines, "\n"))
	return r.SimpleStatusCard(title, color, body, buttons)
}

func RenderClaudeRestartOperationCard(r StatusCardRenderer, sessionKey string, snapshot BackendRestartSnapshot) map[string]any {
	lines := []string{
		"当前版本: `" + apputil.FirstNonEmpty(snapshot.CurrentVersion, "-") + "`",
		"阶段: `" + ClaudeRestartPhaseText(snapshot.Phase) + "`",
		"进度: " + apputil.FirstNonEmpty(snapshot.Message, "-"),
	}
	if !snapshot.StartedAt.IsZero() {
		lines = append(lines, "开始时间(本机时区): `"+FormatClaudeUpgradeTime(snapshot.StartedAt)+"`")
	}
	if !snapshot.UpdatedAt.IsZero() {
		lines = append(lines, "最近更新(本机时区): `"+FormatClaudeUpgradeTime(snapshot.UpdatedAt)+"`")
	}
	title := "Claude Runtime 重启中"
	color := "orange"
	if !snapshot.Running {
		switch snapshot.Result {
		case "success":
			title = "Claude Runtime 已重启"
			color = "green"
		default:
			title = "Claude Runtime 重启失败"
			color = "orange"
		}
		lines = append(lines, "结果: `"+ClaudeRestartResultText(snapshot.Result)+"`")
	}
	body := appmenuutil.MenuCardBody("menu.claude_upgrade", strings.Join(lines, "\n"))
	return r.SimpleStatusCard(title, color, body, ClaudeUpgradeStatusButtons(sessionKey, snapshot.Running))
}
