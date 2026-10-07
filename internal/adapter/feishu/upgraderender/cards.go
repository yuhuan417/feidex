package upgraderender

import (
	"feidex/internal/textutil"
	"strings"

	appmenuutil "feidex/internal/adapter/feishu/menuutil"
	"feidex/internal/feishu"
)

func updateCommandText(spec Spec, command, updateCommand string) string {
	command = textutil.FirstNonEmpty(strings.TrimSpace(command), spec.DefaultCommand)
	updateCommand = textutil.FirstNonEmpty(strings.TrimSpace(updateCommand), "update")
	return command + " " + updateCommand
}

func RenderUpgradeStatusCard(spec Spec, sessionKey string, view UpgradeView, latestChecked bool) map[string]any {
	snapshot := view.Snapshot
	restart := view.Restart
	lines := []string{
		"command: `" + textutil.FirstNonEmpty(view.Probe.Command, spec.DefaultCommand) + "`",
		"解析路径: `" + textutil.FirstNonEmpty(view.Probe.CommandPath, "-") + "`",
		"安装来源: `" + RenderInstallSource(spec, view.Probe) + "`",
		"自升级命令: `" + updateCommandText(spec, view.Probe.Command, view.Probe.UpdateCommand) + "`",
		"当前版本: `" + textutil.FirstNonEmpty(view.Probe.CurrentVersion, "-") + "`",
	}
	if strings.TrimSpace(view.Probe.Reason) != "" {
		lines = append(lines, "原因: "+strings.TrimSpace(view.Probe.Reason))
	}
	if latestChecked {
		switch {
		case strings.TrimSpace(view.LatestVersion) != "":
			lines = append(lines, "目标版本: `"+view.LatestVersion+"`")
		case strings.TrimSpace(view.LatestError) != "":
			lines = append(lines, "目标版本: 检查失败", "错误: "+view.LatestError)
		default:
			lines = append(lines, "目标版本: `-`")
		}
	} else {
		lines = append(lines, "目标版本: `未检查`")
	}
	lines = append(lines,
		"状态: "+RenderUpgradeAvailability(view, latestChecked),
		"runtime: "+RenderUpgradeRuntimeLine(view),
		"smoke test: `"+spec.SmokeTestLabel+"`",
		"回滚策略: `不自动回滚`",
	)
	if snapshot.Running {
		lines = append(lines,
			"",
			"当前升级状态: `"+UpgradePhaseText(snapshot.Phase)+"`",
			"进度: "+textutil.FirstNonEmpty(snapshot.Message, "-"),
		)
		if !snapshot.StartedAt.IsZero() {
			lines = append(lines, "开始时间(本机时区): `"+FormatUpgradeTime(snapshot.StartedAt)+"`")
		}
	} else if strings.TrimSpace(snapshot.Result) != "" {
		lines = append(lines,
			"",
			"上次结果: `"+UpgradeResultText(snapshot.Result)+"`",
			"结果摘要: "+textutil.FirstNonEmpty(snapshot.Message, "-"),
		)
		if !snapshot.UpdatedAt.IsZero() {
			lines = append(lines, "完成时间(本机时区): `"+FormatUpgradeTime(snapshot.UpdatedAt)+"`")
		}
	}
	if restart.Running {
		lines = append(lines,
			"",
			"当前重启状态: `"+RestartPhaseText(restart.Phase)+"`",
			"重启进度: "+textutil.FirstNonEmpty(restart.Message, "-"),
		)
		if !restart.StartedAt.IsZero() {
			lines = append(lines, "重启开始时间(本机时区): `"+FormatUpgradeTime(restart.StartedAt)+"`")
		}
	} else if strings.TrimSpace(restart.Result) != "" {
		lines = append(lines,
			"",
			"上次重启结果: `"+RestartResultText(restart.Result)+"`",
			"重启摘要: "+textutil.FirstNonEmpty(restart.Message, "-"),
		)
		if !restart.UpdatedAt.IsZero() {
			lines = append(lines, "重启完成时间(本机时区): `"+FormatUpgradeTime(restart.UpdatedAt)+"`")
		}
	}

	title := spec.Name + " 管理"
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
	return appmenuutil.PageCard{
		Node: spec.MenuAction, SessionKey: sessionKey, Title: title, Color: color,
		Body:    strings.Join(lines, "\n"),
		Buttons: UpgradeStatusButtons(spec, sessionKey, snapshot.Running || restart.Running),
	}.Render()
}

func RenderUpgradeConfirmCard(spec Spec, sessionKey, requestID string, currentVersion, targetVersion, updateCommand string) map[string]any {
	lines := []string{
		"当前版本: `" + textutil.FirstNonEmpty(currentVersion, "-") + "`",
		"目标版本: `" + textutil.FirstNonEmpty(targetVersion, "-") + "`",
		"升级方式: `" + updateCommandText(spec, spec.DefaultCommand, updateCommand) + "`",
		"验证方式: `" + spec.SmokeTestLabel + "`",
		"失败处理: 不自动回滚；验证失败会保留旧 runtime",
		"开始条件: 当前不得有活动任务或待处理审批/表单",
	}
	buttons := []feishu.Button{
		{
			Text: "确认升级",
			Type: "primary",
			Value: map[string]any{
				"action":      spec.ActionPrefix + ".confirm",
				"request_id":  requestID,
				"session_key": sessionKey,
			},
		},
		{
			Text: "取消",
			Type: "default",
			Value: map[string]any{
				"action":      spec.ActionPrefix + ".cancel",
				"request_id":  requestID,
				"session_key": sessionKey,
			},
		},
	}
	return appmenuutil.PageCard{
		Node: spec.MenuAction, SessionKey: sessionKey, Title: spec.Name + " 升级确认", Color: "orange",
		Body: strings.Join(lines, "\n"), Buttons: buttons,
	}.Render()
}

// RenderUpgradePreparingCard renders the in-flight and canceled states of an
// upgrade request. These are status displays rather than menu pages: the card
// is patched in place (the cancel state is replaced by the status card as soon
// as the cancel round-trip lands), so it claims no breadcrumb and offers no
// back control.
func RenderUpgradePreparingCard(spec Spec, body string) map[string]any {
	if strings.TrimSpace(body) == "" {
		body = "正在准备 " + spec.Name + " 升级信息，请稍候。\n\n这张卡片会自动刷新。"
	}
	return feishu.SimpleStatusCard(spec.Name+" 管理", "blue", body, nil)
}

func RenderUpgradeFailedCard(spec Spec, sessionKey, errText string) map[string]any {
	body := "加载 " + spec.Name + " 升级面板失败。"
	if strings.TrimSpace(errText) != "" {
		body += "\n\n错误: " + strings.TrimSpace(errText)
	}
	return appmenuutil.PageCard{
		Node: spec.MenuAction, SessionKey: sessionKey, Title: spec.Name + " 管理", Color: "orange",
		Body: body, Buttons: UpgradeStatusButtons(spec, sessionKey, false),
	}.Render()
}

// RenderUpgradeOperationCard renders the in-flight and finished upgrade states.
// Like the preparing card these are status displays, not menu pages: the card
// is patched in place by the maintenance runner, so it claims no breadcrumb and
// offers no back control. Its own panel controls stay.
func RenderUpgradeOperationCard(spec Spec, sessionKey string, snapshot BackendUpgradeSnapshot) map[string]any {
	lines := []string{
		"当前版本: `" + textutil.FirstNonEmpty(snapshot.CurrentVersion, "-") + "`",
		"目标版本: `" + textutil.FirstNonEmpty(snapshot.TargetVersion, "-") + "`",
		"阶段: `" + UpgradePhaseText(snapshot.Phase) + "`",
		"进度: " + textutil.FirstNonEmpty(snapshot.Message, "-"),
	}
	if !snapshot.StartedAt.IsZero() {
		lines = append(lines, "开始时间(本机时区): `"+FormatUpgradeTime(snapshot.StartedAt)+"`")
	}
	if !snapshot.UpdatedAt.IsZero() {
		lines = append(lines, "最近更新(本机时区): `"+FormatUpgradeTime(snapshot.UpdatedAt)+"`")
	}
	title := spec.Name + " 升级中"
	color := "orange"
	buttons := UpgradeStatusButtons(spec, sessionKey, snapshot.Running)
	if !snapshot.Running {
		switch snapshot.Result {
		case "success":
			title = spec.Name + " 升级成功"
			color = "green"
		case "rolled_back":
			title = spec.Name + " 已回滚"
			color = "orange"
		case "rollback_failed":
			title = spec.Name + " 回滚失败"
			color = "red"
		default:
			title = spec.Name + " 升级失败"
			color = "orange"
		}
		lines = append(lines, "结果: `"+UpgradeResultText(snapshot.Result)+"`")
	}
	return feishu.SimpleStatusCard(title, color, strings.Join(lines, "\n"), buttons)
}

// RenderRestartOperationCard renders the in-flight and finished restart states
// as status displays, matching RenderUpgradeOperationCard.
func RenderRestartOperationCard(spec Spec, sessionKey string, snapshot BackendRestartSnapshot) map[string]any {
	lines := []string{
		"当前版本: `" + textutil.FirstNonEmpty(snapshot.CurrentVersion, "-") + "`",
		"阶段: `" + RestartPhaseText(snapshot.Phase) + "`",
		"进度: " + textutil.FirstNonEmpty(snapshot.Message, "-"),
	}
	if !snapshot.StartedAt.IsZero() {
		lines = append(lines, "开始时间(本机时区): `"+FormatUpgradeTime(snapshot.StartedAt)+"`")
	}
	if !snapshot.UpdatedAt.IsZero() {
		lines = append(lines, "最近更新(本机时区): `"+FormatUpgradeTime(snapshot.UpdatedAt)+"`")
	}
	title := spec.Name + " Runtime 重启中"
	color := "orange"
	if !snapshot.Running {
		switch snapshot.Result {
		case "success":
			title = spec.Name + " Runtime 已重启"
			color = "green"
		default:
			title = spec.Name + " Runtime 重启失败"
			color = "orange"
		}
		lines = append(lines, "结果: `"+RestartResultText(snapshot.Result)+"`")
	}
	return feishu.SimpleStatusCard(title, color, strings.Join(lines, "\n"), UpgradeStatusButtons(spec, sessionKey, snapshot.Running))
}
