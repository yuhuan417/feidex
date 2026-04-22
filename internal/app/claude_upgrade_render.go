package app

import (
	"strings"
	"time"

	"feidex/internal/claudeinstall"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

func (a *App) renderClaudeUpgradeStatusCard(sessionKey string, view claudeUpgradeView, latestChecked bool) map[string]any {
	snapshot := view.Snapshot
	restart := view.Restart
	lines := []string{
		"command: `" + firstNonEmpty(view.Probe.Command, "claude") + "`",
		"解析路径: `" + firstNonEmpty(view.Probe.CommandPath, "-") + "`",
		"安装来源: `" + renderClaudeInstallSource(view.Probe) + "`",
		"npm: `" + firstNonEmpty(view.Probe.NPMPath, "-") + "`",
		"当前版本: `" + firstNonEmpty(view.Probe.CurrentVersion, "-") + "`",
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
		"状态: "+renderClaudeUpgradeAvailability(view, latestChecked),
		"runtime: "+renderClaudeUpgradeRuntimeLine(view),
		"smoke test: `start + init`",
		"回滚策略: `npm reinstall previous version`",
	)
	if snapshot.Running {
		lines = append(lines,
			"",
			"当前升级状态: `"+claudeUpgradePhaseText(snapshot.Phase)+"`",
			"进度: "+firstNonEmpty(snapshot.Message, "-"),
		)
		if !snapshot.StartedAt.IsZero() {
			lines = append(lines, "开始时间(本机时区): `"+formatClaudeUpgradeTime(snapshot.StartedAt)+"`")
		}
	} else if strings.TrimSpace(snapshot.Result) != "" {
		lines = append(lines,
			"",
			"上次结果: `"+claudeUpgradeResultText(snapshot.Result)+"`",
			"结果摘要: "+firstNonEmpty(snapshot.Message, "-"),
		)
		if !snapshot.UpdatedAt.IsZero() {
			lines = append(lines, "完成时间(本机时区): `"+formatClaudeUpgradeTime(snapshot.UpdatedAt)+"`")
		}
	}
	if restart.Running {
		lines = append(lines,
			"",
			"当前重启状态: `"+claudeRestartPhaseText(restart.Phase)+"`",
			"重启进度: "+firstNonEmpty(restart.Message, "-"),
		)
		if !restart.StartedAt.IsZero() {
			lines = append(lines, "重启开始时间(本机时区): `"+formatClaudeUpgradeTime(restart.StartedAt)+"`")
		}
	} else if strings.TrimSpace(restart.Result) != "" {
		lines = append(lines,
			"",
			"上次重启结果: `"+claudeRestartResultText(restart.Result)+"`",
			"重启摘要: "+firstNonEmpty(restart.Message, "-"),
		)
		if !restart.UpdatedAt.IsZero() {
			lines = append(lines, "重启完成时间(本机时区): `"+formatClaudeUpgradeTime(restart.UpdatedAt)+"`")
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
	return a.feishu.SimpleStatusCard(title, color, menuCardBody("menu.claude_upgrade", strings.Join(lines, "\n")), claudeUpgradeStatusButtons(sessionKey, snapshot.Running || restart.Running))
}

func (a *App) prepareClaudeUpgradeCard(sessionKey, ownerUserID string, view claudeUpgradeView) (map[string]any, string, error) {
	switch {
	case view.Snapshot.Running:
		return a.renderClaudeUpgradeStatusCard(sessionKey, view, true), "", nil
	case !view.Probe.Supported:
		return a.renderClaudeUpgradeStatusCard(sessionKey, view, true), "", nil
	case strings.TrimSpace(view.BusyReason) != "":
		return a.renderClaudeUpgradeStatusCard(sessionKey, view, true), "", nil
	case strings.TrimSpace(view.LatestError) != "":
		return a.renderClaudeUpgradeStatusCard(sessionKey, view, true), "", nil
	case strings.TrimSpace(view.LatestVersion) == "":
		return a.renderClaudeUpgradeStatusCard(sessionKey, view, true), "", nil
	case strings.TrimSpace(view.Probe.CurrentVersion) == strings.TrimSpace(view.LatestVersion):
		return a.renderClaudeUpgradeStatusCard(sessionKey, view, true), "", nil
	}

	requestID, err := a.appState().nextLocalID("claude-upgrade")
	if err != nil {
		return nil, "", err
	}
	payload := claudeUpgradePendingPayload{
		CurrentVersion: view.Probe.CurrentVersion,
		TargetVersion:  view.LatestVersion,
		Command:        view.Probe.Command,
		CommandPath:    view.Probe.CommandPath,
		NPMPath:        view.Probe.NPMPath,
	}
	if err := a.appState().savePending(&state.PendingRequest{
		ID:          requestID,
		Kind:        claudeUpgradePendingKind,
		SessionKey:  sessionKey,
		OwnerUserID: ownerUserID,
		PayloadJSON: mustJSON(payload),
		Status:      "pending",
		CreatedAt:   time.Now().Unix(),
		ExpiresAt:   time.Now().Add(15 * time.Minute).Unix(),
	}); err != nil {
		return nil, "", err
	}
	return a.renderClaudeUpgradeConfirmCard(sessionKey, requestID, payload), requestID, nil
}

func (a *App) renderClaudeUpgradeConfirmCard(sessionKey, requestID string, payload claudeUpgradePendingPayload) map[string]any {
	lines := []string{
		"当前版本: `" + firstNonEmpty(payload.CurrentVersion, "-") + "`",
		"目标版本: `" + firstNonEmpty(payload.TargetVersion, "-") + "`",
		"升级方式: `npm i -g @anthropic-ai/claude-code@" + strings.TrimSpace(payload.TargetVersion) + "`",
		"失败处理: 自动回滚到 `" + firstNonEmpty(payload.CurrentVersion, "-") + "`",
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
	return a.feishu.SimpleStatusCard("Claude 升级确认", "orange", menuCardBody("menu.claude_upgrade", strings.Join(lines, "\n")), buttons)
}

func (a *App) renderClaudeUpgradePreparingCard(sessionKey, body string) map[string]any {
	if strings.TrimSpace(body) == "" {
		body = "正在准备 Claude 升级信息，请稍候。\n\n这张卡片会自动刷新。"
	}
	return a.feishu.SimpleStatusCard("Claude 管理", "blue", menuCardBody("menu.claude_upgrade", body), nil)
}

func (a *App) renderClaudeUpgradeFailedCard(sessionKey, errText string) map[string]any {
	body := "加载 Claude 升级面板失败。"
	if strings.TrimSpace(errText) != "" {
		body += "\n\n错误: " + strings.TrimSpace(errText)
	}
	return a.feishu.SimpleStatusCard("Claude 管理", "orange", menuCardBody("menu.claude_upgrade", body), claudeUpgradeStatusButtons(sessionKey, false))
}

func (a *App) renderClaudeUpgradeOperationCard(sessionKey string, snapshot claudeUpgradeSnapshot) map[string]any {
	lines := []string{
		"当前版本: `" + firstNonEmpty(snapshot.CurrentVersion, "-") + "`",
		"目标版本: `" + firstNonEmpty(snapshot.TargetVersion, "-") + "`",
		"阶段: `" + claudeUpgradePhaseText(snapshot.Phase) + "`",
		"进度: " + firstNonEmpty(snapshot.Message, "-"),
	}
	if !snapshot.StartedAt.IsZero() {
		lines = append(lines, "开始时间(本机时区): `"+formatClaudeUpgradeTime(snapshot.StartedAt)+"`")
	}
	if !snapshot.UpdatedAt.IsZero() {
		lines = append(lines, "最近更新(本机时区): `"+formatClaudeUpgradeTime(snapshot.UpdatedAt)+"`")
	}
	title := "Claude 升级中"
	color := "orange"
	buttons := claudeUpgradeStatusButtons(sessionKey, snapshot.Running)
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
		lines = append(lines, "结果: `"+claudeUpgradeResultText(snapshot.Result)+"`")
	}
	return a.feishu.SimpleStatusCard(title, color, menuCardBody("menu.claude_upgrade", strings.Join(lines, "\n")), buttons)
}

func (a *App) renderClaudeRestartOperationCard(sessionKey string, snapshot claudeRestartSnapshot) map[string]any {
	lines := []string{
		"当前版本: `" + firstNonEmpty(snapshot.CurrentVersion, "-") + "`",
		"阶段: `" + claudeRestartPhaseText(snapshot.Phase) + "`",
		"进度: " + firstNonEmpty(snapshot.Message, "-"),
	}
	if !snapshot.StartedAt.IsZero() {
		lines = append(lines, "开始时间(本机时区): `"+formatClaudeUpgradeTime(snapshot.StartedAt)+"`")
	}
	if !snapshot.UpdatedAt.IsZero() {
		lines = append(lines, "最近更新(本机时区): `"+formatClaudeUpgradeTime(snapshot.UpdatedAt)+"`")
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
		lines = append(lines, "结果: `"+claudeRestartResultText(snapshot.Result)+"`")
	}
	return a.feishu.SimpleStatusCard(title, color, menuCardBody("menu.claude_upgrade", strings.Join(lines, "\n")), claudeUpgradeStatusButtons(sessionKey, snapshot.Running))
}

func claudeUpgradeStatusButtons(sessionKey string, running bool) []feishu.Button {
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
				Text: "升级到最新稳定版",
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

func renderClaudeInstallSource(probe claudeinstall.Probe) string {
	if probe.Supported || strings.TrimSpace(probe.CurrentVersion) != "" {
		return "npm global"
	}
	return "-"
}

func renderClaudeUpgradeAvailability(view claudeUpgradeView, latestChecked bool) string {
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
	case strings.TrimSpace(view.LatestVersion) == strings.TrimSpace(view.Probe.CurrentVersion):
		return "`已是最新`"
	default:
		return "`可升级`"
	}
}

func renderClaudeUpgradeRuntimeLine(view claudeUpgradeView) string {
	if strings.TrimSpace(view.BusyReason) != "" {
		return "`busy` (" + strings.TrimSpace(view.BusyReason) + ")"
	}
	if view.Snapshot.Running || view.Restart.Running {
		return "`maintenance`"
	}
	return "`idle`"
}

func formatClaudeUpgradeTime(ts time.Time) string {
	if ts.IsZero() {
		return ""
	}
	return ts.In(upgradeDisplayLocation).Format("2006-01-02 15:04:05")
}

func claudeUpgradePhaseText(phase string) string {
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
		return firstNonEmpty(strings.TrimSpace(phase), "-")
	}
}

func claudeUpgradeResultText(result string) string {
	switch strings.TrimSpace(result) {
	case "success":
		return "success"
	case "rolled_back":
		return "rolled_back"
	case "rollback_failed":
		return "rollback_failed"
	default:
		return firstNonEmpty(strings.TrimSpace(result), "-")
	}
}

func claudeRestartPhaseText(phase string) string {
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
		return firstNonEmpty(strings.TrimSpace(phase), "-")
	}
}

func claudeRestartResultText(result string) string {
	switch strings.TrimSpace(result) {
	case "success":
		return "success"
	case "failed":
		return "failed"
	default:
		return firstNonEmpty(strings.TrimSpace(result), "-")
	}
}
