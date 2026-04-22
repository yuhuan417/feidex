package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"feidex/internal/claudecli"
	"feidex/internal/claudeinstall"
	"feidex/internal/feishu"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

const (
	claudeUpgradePendingKind  = "claude_npm_upgrade"
	claudeUpgradeCommandUsage = "usage: /claude | /claude check | /claude upgrade | /claude restart"
)

type claudeUpgradePendingPayload struct {
	CurrentVersion string `json:"current_version"`
	TargetVersion  string `json:"target_version"`
	Command        string `json:"command"`
	CommandPath    string `json:"command_path"`
	NPMPath        string `json:"npm_path"`
}

type claudeUpgradeView struct {
	Probe         claudeinstall.Probe
	LatestVersion string
	LatestError   string
	BusyReason    string
	Snapshot      claudeUpgradeSnapshot
	Restart       claudeRestartSnapshot
}

func (a *App) commandClaude(msg *feishu.InboundMessage, args []string) error {
	if msg == nil {
		return nil
	}
	if len(args) > 1 {
		return fmt.Errorf(claudeUpgradeCommandUsage)
	}
	includeLatest := false
	prepareUpgrade := false
	if len(args) == 1 {
		switch strings.TrimSpace(args[0]) {
		case "check":
			includeLatest = true
		case "upgrade":
			includeLatest = true
			prepareUpgrade = true
		case "restart":
			return a.startClaudeRestartFromMessage(msg)
		default:
			return fmt.Errorf(claudeUpgradeCommandUsage)
		}
	}
	sessionKey := a.makeSessionKey(msg)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	view, err := a.loadClaudeUpgradeView(ctx, includeLatest)
	if err != nil {
		return err
	}
	if !prepareUpgrade {
		card := a.renderClaudeUpgradeStatusCard(sessionKey, view, includeLatest)
		_, err = a.feishu.ReplyCard(context.Background(), msg.MessageID, card, a.replyInThreadEnabled(msg.ChatType))
		return err
	}
	card, pendingID, err := a.prepareClaudeUpgradeCard(sessionKey, msg.UserID, view)
	if err != nil {
		return err
	}
	msgID, err := a.feishu.ReplyCard(context.Background(), msg.MessageID, card, a.replyInThreadEnabled(msg.ChatType))
	if err != nil {
		return err
	}
	if strings.TrimSpace(pendingID) != "" {
		_ = a.appState().updatePending(pendingID, func(req *state.PendingRequest) {
			req.FeishuMsgID = msgID
		})
	}
	return nil
}

func (a *App) loadClaudeUpgradeView(ctx context.Context, includeLatest bool) (claudeUpgradeView, error) {
	manager := newClaudeInstallManager(a.cfg.Claude.Command)
	probe, err := manager.Probe(ctx)
	if err != nil {
		return claudeUpgradeView{}, err
	}
	view := claudeUpgradeView{
		Probe:      probe,
		BusyReason: a.claudeUpgradeRuntimeBusyReason(),
		Snapshot:   a.claudeUpgradeState(),
		Restart:    a.claudeRestartState(),
	}
	if includeLatest && probe.Supported && !view.Snapshot.Running && !view.Restart.Running {
		latest, latestErr := manager.LatestVersion(ctx)
		if latestErr != nil {
			view.LatestError = latestErr.Error()
		} else {
			view.LatestVersion = strings.TrimSpace(latest)
		}
	}
	return view, nil
}

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
				"action":      "menu.group.system",
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
			"action":      "menu.group.system",
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

func (a *App) completeMenuClaudeUpgrade(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	return a.completeClaudeUpgradeAsyncAction(action, "/claude", "正在加载 Claude 状态", "正在读取本机 Claude 状态，请稍候。")
}

func (a *App) completeClaudeUpgradeRefresh(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	return a.completeClaudeUpgradeAsyncAction(action, "/claude", "正在刷新 Claude 状态", "正在刷新本机 Claude 状态，请稍候。")
}

func (a *App) completeClaudeUpgradeCheck(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	return a.completeClaudeUpgradeAsyncAction(action, "/claude check", "正在检查最新稳定版", "正在检查最新稳定版，请稍候。")
}

func (a *App) completeClaudeUpgradePrepare(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	return a.completeClaudeUpgradeAsyncAction(action, "/claude upgrade", "正在准备升级确认", "正在准备升级确认，请稍候。")
}

func (a *App) completeClaudeRestartRun(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	sessionKey := actionSessionKey(action)
	snapshot, err := a.beginClaudeRestartOperation()
	if err != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		view, viewErr := a.loadClaudeUpgradeView(ctx, false)
		if viewErr == nil {
			return &callback.CardActionTriggerResponse{
				Toast: &callback.Toast{Type: "warning", Content: err.Error()},
				Card:  rawCard(a.renderClaudeUpgradeStatusCard(sessionKey, view, false)),
			}, nil
		}
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "warning", Content: err.Error()},
			Card:  rawCard(a.renderClaudeUpgradeFailedCard(sessionKey, viewErr.Error())),
		}, nil
	}
	messageID := strings.TrimSpace(action.MessageID)
	go a.runClaudeRestartOperation(messageID, sessionKey)
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "info", Content: "正在重启 Claude runtime"},
		Card:  rawCard(a.renderClaudeRestartOperationCard(sessionKey, snapshot)),
	}, nil
}

func (a *App) completeClaudeUpgradeAsyncAction(action *feishu.CardAction, rawCommand, toastText, preparingText string) (*callback.CardActionTriggerResponse, error) {
	sessionKey := actionSessionKey(action)
	messageID := strings.TrimSpace(action.MessageID)
	if messageID == "" {
		return a.completeMenuCommand(action, sessionKey, rawCommand, "menu.group.system")
	}
	go func() {
		_, card, err := a.runCommandFromCardAction(action, sessionKey, rawCommand)
		if err != nil {
			card = a.renderClaudeUpgradeFailedCard(sessionKey, err.Error())
		} else if card == nil {
			card = a.renderClaudeUpgradeFailedCard(sessionKey, "命令没有返回卡片")
		}
		if patchErr := a.feishu.PatchCard(context.Background(), messageID, card); patchErr != nil {
			slog.Warn("claude upgrade panel patch failed",
				"session_key", sessionKey,
				"message_id", messageID,
				"error", patchErr,
			)
		}
	}()
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "info", Content: toastText},
		Card:  rawCard(a.renderClaudeUpgradePreparingCard(sessionKey, preparingText)),
	}, nil
}

func (a *App) completeClaudeUpgradeAction(action *feishu.CardAction, actionName string) (*callback.CardActionTriggerResponse, error) {
	appState := a.appState()
	requestID := actionStringValue(action, "request_id")
	pending := appState.pending(requestID)
	if pending == nil || pending.Kind != claudeUpgradePendingKind || strings.TrimSpace(pending.Status) != "pending" {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "升级请求已过期"}}, nil
	}
	if pending.OwnerUserID != "" && pending.OwnerUserID != action.UserID {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "你没有权限处理这个升级请求"}}, nil
	}
	sessionKey := firstNonEmpty(actionSessionKey(action), pending.SessionKey)
	if actionName == "claude_upgrade.cancel" {
		_ = appState.updatePending(requestID, func(req *state.PendingRequest) { req.Status = "resolved" })
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		view, err := a.loadClaudeUpgradeView(ctx, false)
		if err != nil {
			return &callback.CardActionTriggerResponse{
				Toast: &callback.Toast{Type: "success", Content: "已取消升级"},
				Card:  rawCard(a.renderClaudeUpgradeFailedCard(sessionKey, err.Error())),
			}, nil
		}
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "success", Content: "已取消升级"},
			Card:  rawCard(a.renderClaudeUpgradeStatusCard(sessionKey, view, false)),
		}, nil
	}

	var payload claudeUpgradePendingPayload
	if err := json.Unmarshal([]byte(pending.PayloadJSON), &payload); err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "升级参数损坏"}}, nil
	}
	snapshot := claudeUpgradeSnapshot{
		Running:         true,
		Phase:           "preflight",
		Message:         "正在校验升级前置条件",
		CurrentVersion:  payload.CurrentVersion,
		PreviousVersion: payload.CurrentVersion,
		TargetVersion:   payload.TargetVersion,
		LatestVersion:   payload.TargetVersion,
	}
	if !a.beginClaudeUpgrade(snapshot) {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		view, err := a.loadClaudeUpgradeView(ctx, false)
		if err == nil {
			return &callback.CardActionTriggerResponse{
				Toast: &callback.Toast{Type: "warning", Content: "Claude 正在维护中"},
				Card:  rawCard(a.renderClaudeUpgradeStatusCard(sessionKey, view, false)),
			}, nil
		}
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "warning", Content: "Claude 正在维护中"},
			Card:  rawCard(a.renderClaudeUpgradeOperationCard(sessionKey, a.claudeUpgradeState())),
		}, nil
	}
	_ = appState.updatePending(requestID, func(req *state.PendingRequest) { req.Status = "resolved" })
	messageID := firstNonEmpty(strings.TrimSpace(action.MessageID), strings.TrimSpace(pending.FeishuMsgID))
	go a.runClaudeUpgradeOperation(messageID, sessionKey, payload)
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "info", Content: "Claude 升级已开始"},
		Card:  rawCard(a.renderClaudeUpgradeOperationCard(sessionKey, a.claudeUpgradeState())),
	}, nil
}

func (a *App) runClaudeUpgradeOperation(messageID, sessionKey string, payload claudeUpgradePendingPayload) {
	manager := newClaudeInstallManager(a.cfg.Claude.Command)
	patch := func(snapshot claudeUpgradeSnapshot) {
		if strings.TrimSpace(messageID) == "" {
			return
		}
		if err := a.feishu.PatchCard(context.Background(), messageID, a.renderClaudeUpgradeOperationCard(sessionKey, snapshot)); err != nil {
			slog.Warn("claude upgrade progress patch failed",
				"message_id", messageID,
				"phase", snapshot.Phase,
				"error", err,
			)
		}
	}
	update := func(phase, message string) claudeUpgradeSnapshot {
		snapshot := a.updateClaudeUpgrade(func(snapshot *claudeUpgradeSnapshot) {
			snapshot.Phase = phase
			snapshot.Message = message
		})
		patch(snapshot)
		return snapshot
	}
	finalize := func(result, message string) {
		patch(a.finishClaudeUpgrade(result, message))
	}
	rollback := func(previousVersion string, cause error) {
		update("rolling_back", "升级失败，正在回滚到 "+firstNonEmpty(previousVersion, "-"))
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		if strings.TrimSpace(previousVersion) == "" {
			finalize("rollback_failed", "升级失败，且缺少可回滚的旧版本。原因: "+cause.Error())
			return
		}
		if err := manager.InstallVersion(ctx, previousVersion); err != nil {
			finalize("rollback_failed", "升级失败，自动回滚也失败。原始错误: "+cause.Error()+"；回滚错误: "+err.Error())
			return
		}
		if err := runClaudeSmokeTest(a, ctx); err != nil {
			finalize("rollback_failed", "升级失败，回滚后的 smoke test 也失败。原始错误: "+cause.Error()+"；回滚验证错误: "+err.Error())
			return
		}
		finalize("rolled_back", "升级失败，已自动回滚到 `"+previousVersion+"`。原因: "+cause.Error())
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	probe, err := manager.Probe(ctx)
	cancel()
	if err != nil {
		finalize("failed", "升级前检查失败: "+err.Error())
		return
	}
	if !probe.Supported {
		finalize("failed", "当前环境不支持自动升级: "+firstNonEmpty(probe.Reason, "unknown"))
		return
	}
	previousVersion := firstNonEmpty(probe.CurrentVersion, payload.CurrentVersion)
	a.updateClaudeUpgrade(func(snapshot *claudeUpgradeSnapshot) {
		snapshot.CurrentVersion = previousVersion
		snapshot.PreviousVersion = previousVersion
		snapshot.TargetVersion = payload.TargetVersion
		snapshot.LatestVersion = payload.TargetVersion
	})
	if reason := a.claudeUpgradeRuntimeBusyReason(); strings.TrimSpace(reason) != "" {
		finalize("failed", "升级前检查失败: "+reason)
		return
	}
	if strings.TrimSpace(previousVersion) == strings.TrimSpace(payload.TargetVersion) {
		finalize("success", "当前已经是最新稳定版 `"+payload.TargetVersion+"`")
		return
	}

	update("installing", "正在安装 @anthropic-ai/claude-code@"+payload.TargetVersion)
	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Minute)
	err = manager.InstallVersion(ctx, payload.TargetVersion)
	cancel()
	if err != nil {
		rollback(previousVersion, err)
		return
	}

	update("smoke_testing", "正在验证新版本")
	ctx, cancel = context.WithTimeout(context.Background(), 45*time.Second)
	switched, err := a.refreshClaudeRuntimeAfterMaintenance(ctx)
	cancel()
	if err != nil {
		rollback(previousVersion, err)
		return
	}
	if switched {
		finalize("success", "升级成功，已切换到 `"+payload.TargetVersion+"`")
		return
	}
	finalize("success", "升级成功，已验证 `"+payload.TargetVersion+"` 可用；当前 frontend 未启用 Claude backend")
}

func (a *App) claudeSmokeTest(ctx context.Context) error {
	if a == nil || a.cfg == nil {
		return fmt.Errorf("claude app not initialized")
	}
	workdir := "."
	for _, ws := range a.cfg.Workspaces {
		if cwd := strings.TrimSpace(ws.Cwd); cwd != "" {
			workdir = cwd
			break
		}
	}
	opts := []claudecli.SessionOption{
		claudecli.WithCLIPath(firstNonEmpty(strings.TrimSpace(a.cfg.Claude.Command), "claude")),
		claudecli.WithWorkDir(workdir),
		claudecli.WithPermissionMode(claudePermissionModeValue(a.cfg.Claude.PermissionMode)),
		claudecli.WithEventBufferSize(16),
	}
	if model := strings.TrimSpace(a.cfg.Claude.Model); model != "" {
		opts = append(opts, claudecli.WithModel(model))
	}
	if effort := strings.TrimSpace(a.cfg.Claude.Effort); effort != "" {
		opts = append(opts, claudecli.WithEffort(effort))
	}
	if a.cfg.Claude.DisablePlugins {
		opts = append(opts, claudecli.WithDisablePlugins())
	}
	if strings.TrimSpace(a.cfg.Claude.SystemPrompt) != "" {
		opts = append(opts, claudecli.WithSystemPrompt(strings.TrimSpace(a.cfg.Claude.SystemPrompt)))
	}
	if a.cfg.Claude.PermissionPromptToolStdio {
		opts = append(opts, claudecli.WithPermissionPromptToolStdio())
	}
	session := claudecli.NewSession(opts...)
	if err := session.Start(ctx); err != nil {
		return err
	}
	defer session.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case event, ok := <-session.Events():
			if !ok {
				if err := session.ExitError(); err != nil {
					return err
				}
				return fmt.Errorf("claude session exited before ready")
			}
			switch value := event.(type) {
			case claudecli.ReadyEvent:
				return nil
			case claudecli.ErrorEvent:
				if value.Error != nil {
					return value.Error
				}
				return fmt.Errorf("claude session startup failed")
			}
		}
	}
}

func (a *App) refreshClaudeRuntimeAfterMaintenance(ctx context.Context) (bool, error) {
	if a == nil {
		return false, fmt.Errorf("claude app not initialized")
	}
	if err := runClaudeSmokeTest(a, ctx); err != nil {
		return false, err
	}
	if a.configuredBackend() != backendClaude {
		return false, nil
	}
	if a.claude == nil {
		a.claude = newClaudeCore(a, a.cfg.Claude)
		return true, nil
	}
	if err := a.claude.Close(); err != nil {
		return false, fmt.Errorf("切换 runtime 失败: %w", err)
	}
	return true, nil
}

func (a *App) startClaudeRestartFromMessage(msg *feishu.InboundMessage) error {
	if msg == nil {
		return nil
	}
	sessionKey := a.makeSessionKey(msg)
	snapshot, err := a.beginClaudeRestartOperation()
	if err != nil {
		return err
	}
	msgID, err := a.feishu.ReplyCard(context.Background(), msg.MessageID, a.renderClaudeRestartOperationCard(sessionKey, snapshot), a.replyInThreadEnabled(msg.ChatType))
	if err != nil {
		a.finishClaudeRestart("failed", "启动重启卡片失败: "+err.Error())
		return err
	}
	go a.runClaudeRestartOperation(msgID, sessionKey)
	return nil
}

func (a *App) beginClaudeRestartOperation() (claudeRestartSnapshot, error) {
	if err := a.ensureClaudeUpgradeReady(); err != nil {
		return claudeRestartSnapshot{}, err
	}
	snapshot := claudeRestartSnapshot{
		Running:        true,
		Phase:          "preflight",
		Message:        "正在校验重启前置条件",
		CurrentVersion: firstNonEmpty(a.claudeUpgradeState().CurrentVersion, a.claudeRestartState().CurrentVersion),
	}
	if !a.beginClaudeRestart(snapshot) {
		return claudeRestartSnapshot{}, errString("Claude 正在维护中，请稍后再试")
	}
	return a.claudeRestartState(), nil
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

func (a *App) runClaudeRestartOperation(messageID, sessionKey string) {
	patch := func(snapshot claudeRestartSnapshot) {
		if strings.TrimSpace(messageID) == "" {
			return
		}
		if err := a.feishu.PatchCard(context.Background(), messageID, a.renderClaudeRestartOperationCard(sessionKey, snapshot)); err != nil {
			slog.Warn("claude restart progress patch failed",
				"message_id", messageID,
				"phase", snapshot.Phase,
				"error", err,
			)
		}
	}
	update := func(phase, message string) claudeRestartSnapshot {
		snapshot := a.updateClaudeRestart(func(snapshot *claudeRestartSnapshot) {
			snapshot.Phase = phase
			snapshot.Message = message
		})
		patch(snapshot)
		return snapshot
	}
	finalize := func(result, message string) {
		patch(a.finishClaudeRestart(result, message))
	}

	update("restarting", "正在校验 Claude runtime 状态")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	manager := newClaudeInstallManager(a.cfg.Claude.Command)
	probe, err := manager.Probe(ctx)
	cancel()
	if err != nil {
		finalize("failed", "重启前检查失败: "+err.Error())
		return
	}
	a.updateClaudeRestart(func(snapshot *claudeRestartSnapshot) {
		snapshot.CurrentVersion = firstNonEmpty(probe.CurrentVersion, snapshot.CurrentVersion)
	})
	if reason := a.claudeUpgradeRuntimeBusyReason(); strings.TrimSpace(reason) != "" {
		finalize("failed", "重启前检查失败: "+reason)
		return
	}

	update("restarting", "正在准备新的 Claude runtime")
	update("smoke_testing", "正在验证重启后的 runtime")
	ctx, cancel = context.WithTimeout(context.Background(), 45*time.Second)
	switched, err := a.refreshClaudeRuntimeAfterMaintenance(ctx)
	cancel()
	if err != nil {
		finalize("failed", "Claude runtime 重启失败: "+err.Error())
		return
	}
	if switched {
		finalize("success", "Claude runtime 已原地重启，后续任务会使用新进程")
		return
	}
	finalize("success", "Claude CLI 校验通过；当前 frontend 未启用 Claude backend")
}
