package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"feidex/internal/codexinstall"
	"feidex/internal/codexrpc"
	"feidex/internal/feishu"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

const (
	codexUpgradePendingKind  = "codex_npm_upgrade"
	codexUpgradeCommandUsage = "usage: /codex | /codex check | /codex upgrade | /codex restart"
)

type codexUpgradePendingPayload struct {
	CurrentVersion string `json:"current_version"`
	TargetVersion  string `json:"target_version"`
	Command        string `json:"command"`
	CommandPath    string `json:"command_path"`
	NPMPath        string `json:"npm_path"`
}

type codexUpgradeView struct {
	Probe         codexinstall.Probe
	LatestVersion string
	LatestError   string
	BusyReason    string
	Snapshot      codexUpgradeSnapshot
	Restart       codexRestartSnapshot
}

func (a *App) commandCodex(msg *feishu.InboundMessage, args []string) error {
	if msg == nil {
		return nil
	}
	if len(args) > 1 {
		return fmt.Errorf(codexUpgradeCommandUsage)
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
			return a.startCodexRestartFromMessage(msg)
		default:
			return fmt.Errorf(codexUpgradeCommandUsage)
		}
	}
	sessionKey := a.makeSessionKey(msg)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	view, err := a.loadCodexUpgradeView(ctx, includeLatest)
	if err != nil {
		return err
	}
	if !prepareUpgrade {
		card := a.renderCodexUpgradeStatusCard(sessionKey, view, includeLatest)
		_, err = a.feishu.ReplyCard(context.Background(), msg.MessageID, card, a.replyInThreadEnabled(msg.ChatType))
		return err
	}
	card, pendingID, err := a.prepareCodexUpgradeCard(sessionKey, msg.UserID, view)
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

func (a *App) loadCodexUpgradeView(ctx context.Context, includeLatest bool) (codexUpgradeView, error) {
	manager := newCodexInstallManager(a.cfg.Codex.Command)
	probe, err := manager.Probe(ctx)
	if err != nil {
		return codexUpgradeView{}, err
	}
	view := codexUpgradeView{
		Probe:      probe,
		BusyReason: a.codexUpgradeRuntimeBusyReason(),
		Snapshot:   a.codexUpgradeState(),
		Restart:    a.codexRestartState(),
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

func (a *App) renderCodexUpgradeStatusCard(sessionKey string, view codexUpgradeView, latestChecked bool) map[string]any {
	snapshot := view.Snapshot
	restart := view.Restart
	lines := []string{
		"command: `" + firstNonEmpty(view.Probe.Command, "codex") + "`",
		"解析路径: `" + firstNonEmpty(view.Probe.CommandPath, "-") + "`",
		"安装来源: `" + renderCodexInstallSource(view.Probe) + "`",
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
		"状态: "+renderCodexUpgradeAvailability(view, latestChecked),
		"runtime: "+renderCodexUpgradeRuntimeLine(view),
		"smoke test: `initialize + model/list`",
		"回滚策略: `npm reinstall previous version`",
	)
	if snapshot.Running {
		lines = append(lines,
			"",
			"当前升级状态: `"+codexUpgradePhaseText(snapshot.Phase)+"`",
			"进度: "+firstNonEmpty(snapshot.Message, "-"),
		)
		if !snapshot.StartedAt.IsZero() {
			lines = append(lines, "开始时间(本机时区): `"+formatCodexUpgradeTime(snapshot.StartedAt)+"`")
		}
	} else if strings.TrimSpace(snapshot.Result) != "" {
		lines = append(lines,
			"",
			"上次结果: `"+codexUpgradeResultText(snapshot.Result)+"`",
			"结果摘要: "+firstNonEmpty(snapshot.Message, "-"),
		)
		if !snapshot.UpdatedAt.IsZero() {
			lines = append(lines, "完成时间(本机时区): `"+formatCodexUpgradeTime(snapshot.UpdatedAt)+"`")
		}
	}
	if restart.Running {
		lines = append(lines,
			"",
			"当前重启状态: `"+codexRestartPhaseText(restart.Phase)+"`",
			"重启进度: "+firstNonEmpty(restart.Message, "-"),
		)
		if !restart.StartedAt.IsZero() {
			lines = append(lines, "重启开始时间(本机时区): `"+formatCodexUpgradeTime(restart.StartedAt)+"`")
		}
	} else if strings.TrimSpace(restart.Result) != "" {
		lines = append(lines,
			"",
			"上次重启结果: `"+codexRestartResultText(restart.Result)+"`",
			"重启摘要: "+firstNonEmpty(restart.Message, "-"),
		)
		if !restart.UpdatedAt.IsZero() {
			lines = append(lines, "重启完成时间(本机时区): `"+formatCodexUpgradeTime(restart.UpdatedAt)+"`")
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
	return a.feishu.SimpleStatusCard(title, color, menuCardBody("menu.codex_upgrade", strings.Join(lines, "\n")), codexUpgradeStatusButtons(sessionKey, snapshot.Running || restart.Running))
}

func (a *App) prepareCodexUpgradeCard(sessionKey, ownerUserID string, view codexUpgradeView) (map[string]any, string, error) {
	switch {
	case view.Snapshot.Running:
		return a.renderCodexUpgradeStatusCard(sessionKey, view, true), "", nil
	case !view.Probe.Supported:
		return a.renderCodexUpgradeStatusCard(sessionKey, view, true), "", nil
	case strings.TrimSpace(view.BusyReason) != "":
		return a.renderCodexUpgradeStatusCard(sessionKey, view, true), "", nil
	case strings.TrimSpace(view.LatestError) != "":
		return a.renderCodexUpgradeStatusCard(sessionKey, view, true), "", nil
	case strings.TrimSpace(view.LatestVersion) == "":
		return a.renderCodexUpgradeStatusCard(sessionKey, view, true), "", nil
	case strings.TrimSpace(view.Probe.CurrentVersion) == strings.TrimSpace(view.LatestVersion):
		return a.renderCodexUpgradeStatusCard(sessionKey, view, true), "", nil
	}

	requestID, err := a.appState().nextLocalID("codex-upgrade")
	if err != nil {
		return nil, "", err
	}
	payload := codexUpgradePendingPayload{
		CurrentVersion: view.Probe.CurrentVersion,
		TargetVersion:  view.LatestVersion,
		Command:        view.Probe.Command,
		CommandPath:    view.Probe.CommandPath,
		NPMPath:        view.Probe.NPMPath,
	}
	if err := a.appState().savePending(&state.PendingRequest{
		ID:          requestID,
		Kind:        codexUpgradePendingKind,
		SessionKey:  sessionKey,
		OwnerUserID: ownerUserID,
		PayloadJSON: mustJSON(payload),
		Status:      "pending",
		CreatedAt:   time.Now().Unix(),
		ExpiresAt:   time.Now().Add(15 * time.Minute).Unix(),
	}); err != nil {
		return nil, "", err
	}
	return a.renderCodexUpgradeConfirmCard(sessionKey, requestID, payload), requestID, nil
}

func (a *App) renderCodexUpgradeConfirmCard(sessionKey, requestID string, payload codexUpgradePendingPayload) map[string]any {
	lines := []string{
		"当前版本: `" + firstNonEmpty(payload.CurrentVersion, "-") + "`",
		"目标版本: `" + firstNonEmpty(payload.TargetVersion, "-") + "`",
		"升级方式: `npm i -g @openai/codex@" + strings.TrimSpace(payload.TargetVersion) + "`",
		"验证方式: `initialize + model/list`",
		"失败处理: 自动回滚到 `" + firstNonEmpty(payload.CurrentVersion, "-") + "`",
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
				"action":      "menu.group.system",
				"session_key": sessionKey,
			},
		},
	}
	return a.feishu.SimpleStatusCard("Codex 升级确认", "orange", menuCardBody("menu.codex_upgrade", strings.Join(lines, "\n")), buttons)
}

func (a *App) renderCodexUpgradePreparingCard(sessionKey, body string) map[string]any {
	if strings.TrimSpace(body) == "" {
		body = "正在准备 Codex 升级信息，请稍候。\n\n这张卡片会自动刷新。"
	}
	return a.feishu.SimpleStatusCard("Codex 管理", "blue", menuCardBody("menu.codex_upgrade", body), nil)
}

func (a *App) renderCodexUpgradeFailedCard(sessionKey, errText string) map[string]any {
	body := "加载 Codex 升级面板失败。"
	if strings.TrimSpace(errText) != "" {
		body += "\n\n错误: " + strings.TrimSpace(errText)
	}
	return a.feishu.SimpleStatusCard("Codex 管理", "orange", menuCardBody("menu.codex_upgrade", body), codexUpgradeStatusButtons(sessionKey, false))
}

func (a *App) renderCodexUpgradeOperationCard(sessionKey string, snapshot codexUpgradeSnapshot) map[string]any {
	lines := []string{
		"当前版本: `" + firstNonEmpty(snapshot.CurrentVersion, "-") + "`",
		"目标版本: `" + firstNonEmpty(snapshot.TargetVersion, "-") + "`",
		"阶段: `" + codexUpgradePhaseText(snapshot.Phase) + "`",
		"进度: " + firstNonEmpty(snapshot.Message, "-"),
	}
	if !snapshot.StartedAt.IsZero() {
		lines = append(lines, "开始时间(本机时区): `"+formatCodexUpgradeTime(snapshot.StartedAt)+"`")
	}
	if !snapshot.UpdatedAt.IsZero() {
		lines = append(lines, "最近更新(本机时区): `"+formatCodexUpgradeTime(snapshot.UpdatedAt)+"`")
	}
	title := "Codex 升级中"
	color := "orange"
	buttons := codexUpgradeStatusButtons(sessionKey, snapshot.Running)
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
		lines = append(lines, "结果: `"+codexUpgradeResultText(snapshot.Result)+"`")
	}
	return a.feishu.SimpleStatusCard(title, color, menuCardBody("menu.codex_upgrade", strings.Join(lines, "\n")), buttons)
}

func codexUpgradeStatusButtons(sessionKey string, running bool) []feishu.Button {
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
				Text: "升级到最新稳定版",
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
			"action":      "menu.group.system",
			"session_key": sessionKey,
		},
	})
	return buttons
}

func renderCodexInstallSource(probe codexinstall.Probe) string {
	if probe.Supported || strings.TrimSpace(probe.CurrentVersion) != "" {
		return "npm global"
	}
	return "-"
}

func renderCodexUpgradeAvailability(view codexUpgradeView, latestChecked bool) string {
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

func renderCodexUpgradeRuntimeLine(view codexUpgradeView) string {
	if strings.TrimSpace(view.BusyReason) != "" {
		return "`busy` (" + strings.TrimSpace(view.BusyReason) + ")"
	}
	if view.Snapshot.Running || view.Restart.Running {
		return "`maintenance`"
	}
	return "`idle`"
}

func formatCodexUpgradeTime(ts time.Time) string {
	if ts.IsZero() {
		return ""
	}
	return ts.In(upgradeDisplayLocation).Format("2006-01-02 15:04:05")
}

func codexUpgradePhaseText(phase string) string {
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

func codexUpgradeResultText(result string) string {
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

func codexRestartPhaseText(phase string) string {
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

func codexRestartResultText(result string) string {
	switch strings.TrimSpace(result) {
	case "success":
		return "success"
	case "failed":
		return "failed"
	default:
		return firstNonEmpty(strings.TrimSpace(result), "-")
	}
}

func (a *App) completeMenuCodexUpgrade(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	return a.completeCodexUpgradeAsyncAction(action, "/codex", "正在加载 Codex 状态", "正在读取本机 Codex 状态，请稍候。")
}

func (a *App) completeCodexUpgradeRefresh(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	return a.completeCodexUpgradeAsyncAction(action, "/codex", "正在刷新 Codex 状态", "正在刷新本机 Codex 状态，请稍候。")
}

func (a *App) completeCodexUpgradeCheck(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	return a.completeCodexUpgradeAsyncAction(action, "/codex check", "正在检查最新稳定版", "正在检查最新稳定版，请稍候。")
}

func (a *App) completeCodexUpgradePrepare(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	return a.completeCodexUpgradeAsyncAction(action, "/codex upgrade", "正在准备升级确认", "正在准备升级确认，请稍候。")
}

func (a *App) completeCodexRestartRun(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	sessionKey := actionSessionKey(action)
	snapshot, err := a.beginCodexRestartOperation()
	if err != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		view, viewErr := a.loadCodexUpgradeView(ctx, false)
		if viewErr == nil {
			return &callback.CardActionTriggerResponse{
				Toast: &callback.Toast{Type: "warning", Content: err.Error()},
				Card:  rawCard(a.renderCodexUpgradeStatusCard(sessionKey, view, false)),
			}, nil
		}
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "warning", Content: err.Error()},
			Card:  rawCard(a.renderCodexUpgradeFailedCard(sessionKey, viewErr.Error())),
		}, nil
	}
	messageID := strings.TrimSpace(action.MessageID)
	go a.runCodexRestartOperation(messageID, sessionKey)
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "info", Content: "正在重启 Codex runtime"},
		Card:  rawCard(a.renderCodexRestartOperationCard(sessionKey, snapshot)),
	}, nil
}

func (a *App) completeCodexUpgradeAsyncAction(action *feishu.CardAction, rawCommand, toastText, preparingText string) (*callback.CardActionTriggerResponse, error) {
	sessionKey := actionSessionKey(action)
	messageID := strings.TrimSpace(action.MessageID)
	if messageID == "" {
		return a.completeMenuCommand(action, sessionKey, rawCommand, "menu.group.system")
	}
	go func() {
		_, card, err := a.runCommandFromCardAction(action, sessionKey, rawCommand)
		if err != nil {
			card = a.renderCodexUpgradeFailedCard(sessionKey, err.Error())
		} else if card == nil {
			card = a.renderCodexUpgradeFailedCard(sessionKey, "命令没有返回卡片")
		}
		if patchErr := a.feishu.PatchCard(context.Background(), messageID, card); patchErr != nil {
			slog.Warn("codex upgrade panel patch failed",
				"session_key", sessionKey,
				"message_id", messageID,
				"error", patchErr,
			)
		}
	}()
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "info", Content: toastText},
		Card:  rawCard(a.renderCodexUpgradePreparingCard(sessionKey, preparingText)),
	}, nil
}

func (a *App) completeCodexUpgradeAction(action *feishu.CardAction, actionName string) (*callback.CardActionTriggerResponse, error) {
	appState := a.appState()
	requestID := actionStringValue(action, "request_id")
	pending := appState.pending(requestID)
	if pending == nil || pending.Kind != codexUpgradePendingKind || strings.TrimSpace(pending.Status) != "pending" {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "升级请求已过期"}}, nil
	}
	if pending.OwnerUserID != "" && pending.OwnerUserID != action.UserID {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "你没有权限处理这个升级请求"}}, nil
	}
	sessionKey := firstNonEmpty(actionSessionKey(action), pending.SessionKey)
	if actionName == "codex_upgrade.cancel" {
		_ = appState.updatePending(requestID, func(req *state.PendingRequest) { req.Status = "resolved" })
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		view, err := a.loadCodexUpgradeView(ctx, false)
		if err != nil {
			return &callback.CardActionTriggerResponse{
				Toast: &callback.Toast{Type: "success", Content: "已取消升级"},
				Card:  rawCard(a.renderCodexUpgradeFailedCard(sessionKey, err.Error())),
			}, nil
		}
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "success", Content: "已取消升级"},
			Card:  rawCard(a.renderCodexUpgradeStatusCard(sessionKey, view, false)),
		}, nil
	}

	var payload codexUpgradePendingPayload
	if err := json.Unmarshal([]byte(pending.PayloadJSON), &payload); err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "升级参数损坏"}}, nil
	}
	snapshot := codexUpgradeSnapshot{
		Running:         true,
		Phase:           "preflight",
		Message:         "正在校验升级前置条件",
		CurrentVersion:  payload.CurrentVersion,
		PreviousVersion: payload.CurrentVersion,
		TargetVersion:   payload.TargetVersion,
		LatestVersion:   payload.TargetVersion,
	}
	if !a.beginCodexUpgrade(snapshot) {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		view, err := a.loadCodexUpgradeView(ctx, false)
		if err == nil {
			return &callback.CardActionTriggerResponse{
				Toast: &callback.Toast{Type: "warning", Content: "Codex 正在维护中"},
				Card:  rawCard(a.renderCodexUpgradeStatusCard(sessionKey, view, false)),
			}, nil
		}
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "warning", Content: "Codex 正在维护中"},
			Card:  rawCard(a.renderCodexUpgradeOperationCard(sessionKey, a.codexUpgradeState())),
		}, nil
	}
	_ = appState.updatePending(requestID, func(req *state.PendingRequest) { req.Status = "resolved" })
	messageID := firstNonEmpty(strings.TrimSpace(action.MessageID), strings.TrimSpace(pending.FeishuMsgID))
	go a.runCodexUpgradeOperation(messageID, sessionKey, payload)
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "info", Content: "Codex 升级已开始"},
		Card:  rawCard(a.renderCodexUpgradeOperationCard(sessionKey, a.codexUpgradeState())),
	}, nil
}

func (a *App) runCodexUpgradeOperation(messageID, sessionKey string, payload codexUpgradePendingPayload) {
	manager := newCodexInstallManager(a.cfg.Codex.Command)
	patch := func(snapshot codexUpgradeSnapshot) {
		if strings.TrimSpace(messageID) == "" {
			return
		}
		if err := a.feishu.PatchCard(context.Background(), messageID, a.renderCodexUpgradeOperationCard(sessionKey, snapshot)); err != nil {
			slog.Warn("codex upgrade progress patch failed",
				"message_id", messageID,
				"phase", snapshot.Phase,
				"error", err,
			)
		}
	}
	update := func(phase, message string) codexUpgradeSnapshot {
		snapshot := a.updateCodexUpgrade(func(snapshot *codexUpgradeSnapshot) {
			snapshot.Phase = phase
			snapshot.Message = message
		})
		patch(snapshot)
		return snapshot
	}
	finalize := func(result, message string) {
		patch(a.finishCodexUpgrade(result, message))
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
		if err := a.codexSmokeTest(ctx); err != nil {
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
	a.updateCodexUpgrade(func(snapshot *codexUpgradeSnapshot) {
		snapshot.CurrentVersion = previousVersion
		snapshot.PreviousVersion = previousVersion
		snapshot.TargetVersion = payload.TargetVersion
		snapshot.LatestVersion = payload.TargetVersion
	})
	if reason := a.codexUpgradeRuntimeBusyReason(); strings.TrimSpace(reason) != "" {
		finalize("failed", "升级前检查失败: "+reason)
		return
	}
	if strings.TrimSpace(previousVersion) == strings.TrimSpace(payload.TargetVersion) {
		finalize("success", "当前已经是最新稳定版 `"+payload.TargetVersion+"`")
		return
	}

	update("installing", "正在安装 @openai/codex@"+payload.TargetVersion)
	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Minute)
	err = manager.InstallVersion(ctx, payload.TargetVersion)
	cancel()
	if err != nil {
		rollback(previousVersion, err)
		return
	}

	update("smoke_testing", "正在验证新版本")
	ctx, cancel = context.WithTimeout(context.Background(), 45*time.Second)
	err = a.codexSmokeTest(ctx)
	cancel()
	if err != nil {
		rollback(previousVersion, err)
		return
	}
	if a.codex != nil {
		if err := a.codex.Close(); err != nil {
			rollback(previousVersion, fmt.Errorf("切换 runtime 失败: %w", err))
			return
		}
	}
	finalize("success", "升级成功，已切换到 `"+payload.TargetVersion+"`")
}

func (a *App) codexSmokeTest(ctx context.Context) error {
	client := newCodexClient(a.cfg.Codex)
	if client == nil {
		return fmt.Errorf("codex client not initialized")
	}
	if err := client.Start(ctx, a.cfg.Codex.ExperimentalAPI); err != nil {
		return err
	}
	defer client.Close()
	var result codexrpc.ModelListResult
	if err := client.Call(ctx, "model/list", map[string]any{"limit": 1, "includeHidden": false}, &result); err != nil {
		return err
	}
	if len(result.Data) == 0 {
		return fmt.Errorf("model/list returned no visible models")
	}
	return nil
}

func (a *App) startCodexRestartFromMessage(msg *feishu.InboundMessage) error {
	if msg == nil {
		return nil
	}
	sessionKey := a.makeSessionKey(msg)
	snapshot, err := a.beginCodexRestartOperation()
	if err != nil {
		return err
	}
	msgID, err := a.feishu.ReplyCard(context.Background(), msg.MessageID, a.renderCodexRestartOperationCard(sessionKey, snapshot), a.replyInThreadEnabled(msg.ChatType))
	if err != nil {
		a.finishCodexRestart("failed", "启动重启卡片失败: "+err.Error())
		return err
	}
	go a.runCodexRestartOperation(msgID, sessionKey)
	return nil
}

func (a *App) beginCodexRestartOperation() (codexRestartSnapshot, error) {
	if err := a.ensureCodexUpgradeReady(); err != nil {
		return codexRestartSnapshot{}, err
	}
	snapshot := codexRestartSnapshot{
		Running:        true,
		Phase:          "preflight",
		Message:        "正在校验重启前置条件",
		CurrentVersion: firstNonEmpty(a.codexUpgradeState().CurrentVersion, a.codexRestartState().CurrentVersion),
	}
	if !a.beginCodexRestart(snapshot) {
		return codexRestartSnapshot{}, errString("Codex 正在维护中，请稍后再试")
	}
	return a.codexRestartState(), nil
}

func (a *App) renderCodexRestartOperationCard(sessionKey string, snapshot codexRestartSnapshot) map[string]any {
	lines := []string{
		"当前版本: `" + firstNonEmpty(snapshot.CurrentVersion, "-") + "`",
		"阶段: `" + codexRestartPhaseText(snapshot.Phase) + "`",
		"进度: " + firstNonEmpty(snapshot.Message, "-"),
	}
	if !snapshot.StartedAt.IsZero() {
		lines = append(lines, "开始时间(本机时区): `"+formatCodexUpgradeTime(snapshot.StartedAt)+"`")
	}
	if !snapshot.UpdatedAt.IsZero() {
		lines = append(lines, "最近更新(本机时区): `"+formatCodexUpgradeTime(snapshot.UpdatedAt)+"`")
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
		lines = append(lines, "结果: `"+codexRestartResultText(snapshot.Result)+"`")
	}
	return a.feishu.SimpleStatusCard(title, color, menuCardBody("menu.codex_upgrade", strings.Join(lines, "\n")), codexUpgradeStatusButtons(sessionKey, snapshot.Running))
}

func (a *App) runCodexRestartOperation(messageID, sessionKey string) {
	patch := func(snapshot codexRestartSnapshot) {
		if strings.TrimSpace(messageID) == "" {
			return
		}
		if err := a.feishu.PatchCard(context.Background(), messageID, a.renderCodexRestartOperationCard(sessionKey, snapshot)); err != nil {
			slog.Warn("codex restart progress patch failed",
				"message_id", messageID,
				"phase", snapshot.Phase,
				"error", err,
			)
		}
	}
	update := func(phase, message string) codexRestartSnapshot {
		snapshot := a.updateCodexRestart(func(snapshot *codexRestartSnapshot) {
			snapshot.Phase = phase
			snapshot.Message = message
		})
		patch(snapshot)
		return snapshot
	}
	finalize := func(result, message string) {
		patch(a.finishCodexRestart(result, message))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	manager := newCodexInstallManager(a.cfg.Codex.Command)
	probe, err := manager.Probe(ctx)
	cancel()
	if err != nil {
		finalize("failed", "重启前检查失败: "+err.Error())
		return
	}
	a.updateCodexRestart(func(snapshot *codexRestartSnapshot) {
		snapshot.CurrentVersion = firstNonEmpty(probe.CurrentVersion, snapshot.CurrentVersion)
	})
	if reason := a.codexUpgradeRuntimeBusyReason(); strings.TrimSpace(reason) != "" {
		finalize("failed", "重启前检查失败: "+reason)
		return
	}

	update("restarting", "正在关闭当前 Codex runtime")
	if a.codex != nil {
		if err := a.codex.Close(); err != nil {
			finalize("failed", "关闭当前 Codex runtime 失败: "+err.Error())
			return
		}
	}
	update("smoke_testing", "正在验证重启后的 runtime")
	ctx, cancel = context.WithTimeout(context.Background(), 45*time.Second)
	err = a.codexSmokeTest(ctx)
	cancel()
	if err != nil {
		finalize("failed", "Codex runtime 已关闭，但重启后的 smoke test 失败: "+err.Error())
		return
	}
	finalize("success", "Codex runtime 已原地重启，后续任务会使用新进程")
}
