package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"feidex/internal/codexrpc"
	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

func (a *App) dispatchCardAction(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	if action == nil {
		return &callback.CardActionTriggerResponse{}, nil
	}
	name, _ := action.ActionValue["action"].(string)
	if strings.TrimSpace(name) == "" {
		if alt := strings.TrimSpace(action.Name); alt != "" {
			if strings.HasPrefix(alt, "turn.item.toggle:") {
				name = "turn.item.toggle"
			} else {
				name = alt
			}
		}
	}
	switch name {
	case "menu.new":
		sessionKey, _ := action.ActionValue["session_key"].(string)
		return a.completeMenuNew(action, sessionKey)
	case "menu.quiet":
		return a.completeMenuQuiet(action)
	case "menu.model":
		sessionKey, _ := action.ActionValue["session_key"].(string)
		return a.completeMenuModel(action, sessionKey)
	case "menu.status":
		sessionKey, _ := action.ActionValue["session_key"].(string)
		return a.completeMenuStatus(action, sessionKey)
	case "quiet.set":
		enabled, _ := action.ActionValue["enabled"].(bool)
		return a.completeQuietSet(action, enabled)
	case "model.config.set_model":
		modelID, _ := action.ActionValue["model_id"].(string)
		return a.completeGlobalModelSet(action, modelID)
	case "model.config.set_effort":
		reasoningEffort, _ := action.ActionValue["reasoning_effort"].(string)
		return a.completeGlobalReasoningEffortSet(action, reasoningEffort)
	case "menu.threads":
		sessionKey, _ := action.ActionValue["session_key"].(string)
		return a.completeMenuThreads(action, sessionKey)
	case "menu.interrupt":
		sessionKey, _ := action.ActionValue["session_key"].(string)
		turnID, _ := action.ActionValue["turn_id"].(string)
		return a.completeMenuInterrupt(action, sessionKey, turnID)
	case "menu.workspace":
		sessionKey, _ := action.ActionValue["session_key"].(string)
		return a.completeMenuWorkspace(action, sessionKey)
	case "workspace.use":
		sessionKey, _ := action.ActionValue["session_key"].(string)
		workspaceID, _ := action.ActionValue["workspace_id"].(string)
		return a.completeWorkspaceUse(action, sessionKey, workspaceID)
	case "workspace.new":
		sessionKey, _ := action.ActionValue["session_key"].(string)
		return a.completeWorkspaceNew(action, sessionKey)
	case "workspace.sandbox.menu":
		sessionKey, _ := action.ActionValue["session_key"].(string)
		return a.completeWorkspaceSandboxMenu(action, sessionKey)
	case "workspace.policy.menu":
		sessionKey, _ := action.ActionValue["session_key"].(string)
		return a.completeWorkspacePolicyMenu(action, sessionKey)
	case "workspace.sandbox.set":
		sessionKey, _ := action.ActionValue["session_key"].(string)
		workspaceID, _ := action.ActionValue["workspace_id"].(string)
		sandboxMode, _ := action.ActionValue["sandbox_mode"].(string)
		return a.completeWorkspaceSandboxSet(action, sessionKey, workspaceID, sandboxMode)
	case "workspace.policy.set":
		sessionKey, _ := action.ActionValue["session_key"].(string)
		workspaceID, _ := action.ActionValue["workspace_id"].(string)
		approvalPolicy, _ := action.ActionValue["approval_policy"].(string)
		return a.completeWorkspacePolicySet(action, sessionKey, workspaceID, approvalPolicy)
	case "thread.sandbox.menu":
		sessionKey, _ := action.ActionValue["session_key"].(string)
		return a.completeThreadSandboxMenu(action, sessionKey)
	case "thread.policy.menu":
		sessionKey, _ := action.ActionValue["session_key"].(string)
		return a.completeThreadPolicyMenu(action, sessionKey)
	case "thread.sandbox.set":
		sessionKey, _ := action.ActionValue["session_key"].(string)
		threadID, _ := action.ActionValue["thread_id"].(string)
		sandboxMode, _ := action.ActionValue["sandbox_mode"].(string)
		return a.completeThreadSandboxSet(action, sessionKey, threadID, sandboxMode)
	case "thread.policy.set":
		sessionKey, _ := action.ActionValue["session_key"].(string)
		threadID, _ := action.ActionValue["thread_id"].(string)
		approvalPolicy, _ := action.ActionValue["approval_policy"].(string)
		return a.completeThreadPolicySet(action, sessionKey, threadID, approvalPolicy)
	case "thread.resume":
		sessionKey, _ := action.ActionValue["session_key"].(string)
		threadID, _ := action.ActionValue["thread_id"].(string)
		return a.completeThreadResume(action, sessionKey, threadID)
	case "turn.append":
		sessionKey, _ := action.ActionValue["session_key"].(string)
		turnID, _ := action.ActionValue["turn_id"].(string)
		itemID, _ := action.ActionValue["item_id"].(string)
		return a.completeTurnAppend(action, sessionKey, turnID, itemID)
	case "turn.item.toggle":
		return a.completeTurnItemToggle(action)
	case "user_input.answer":
		return a.completeUserInputAnswer(action)
	case "approval.command.accept", "approval.command.accept_session", "approval.command.decline", "approval.command.cancel",
		"approval.file.accept", "approval.file.accept_session", "approval.file.decline", "approval.file.cancel",
		"approval.permissions.accept_turn", "approval.permissions.accept_session":
		return a.completeApprovalAction(action, name)
	case "pending_form.cancel":
		return a.completePendingFormCancel(action)
	case "elicitation_url.accept", "elicitation_url.decline", "elicitation_url.cancel":
		return a.completeElicitationURLAction(action, name)
	default:
		slog.Warn("unknown feishu card action",
			"name", name,
			"raw_name", action.Name,
			"message_id", action.MessageID,
			"chat_id", action.ChatID,
			"user_id", action.UserID,
			"action_value", fmt.Sprintf("%v", action.ActionValue),
			"form_value", fmt.Sprintf("%v", action.FormValue),
		)
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "warning", Content: "未知操作"},
		}, nil
	}
}

func (a *App) completeMenuNew(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	sess := a.store.GetSession(sessionKey)
	if sess == nil {
		sess = &state.Session{Key: sessionKey, OwnerUserID: action.UserID, ChatID: action.ChatID}
	}
	if sess.ActiveTurnID != "" {
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "warning", Content: "当前任务仍在运行，请先等待结束或中断"},
		}, nil
	}
	discarded := a.discardSessionPendingInputs(sessionKey)
	sess = a.store.GetSession(sessionKey)
	if sess == nil {
		sess = &state.Session{Key: sessionKey, OwnerUserID: action.UserID, ChatID: action.ChatID}
	}
	clearSessionThreadContext(sess)
	a.clearSessionLiveThread(sessionKey)
	sess.ActiveTurnID = ""
	sess.ActiveSubmissionID = ""
	sess.Status = "idle"
	sess.Queue = nil
	sess.StagedImages = nil
	_ = a.store.UpsertSession(sess)
	content := "已切换到新会话"
	if discarded > 0 {
		content = fmt.Sprintf("已切换到新会话，并丢弃 %d 条排队或暂存输入", discarded)
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: content},
	}, nil
}

func (a *App) completeGlobalModelSet(action *feishu.CardAction, modelID string) (*callback.CardActionTriggerResponse, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	result, err := a.fetchModelList(ctx)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: err.Error()}}, nil
	}
	if err := a.updateGlobalModelConfig(func(c *config.CodexConfig) {
		c.Model = strings.TrimSpace(modelID)
	}, result); err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: err.Error()}}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已更新全局模型"},
		Card:  rawCard(a.renderModelConfigCard(result)),
	}, nil
}

func (a *App) completeGlobalReasoningEffortSet(action *feishu.CardAction, reasoningEffort string) (*callback.CardActionTriggerResponse, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	result, err := a.fetchModelList(ctx)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: err.Error()}}, nil
	}
	selectedModel, _ := effectiveConfiguredModelAndEffort(a.cfg, result)
	if strings.TrimSpace(reasoningEffort) != "" && !modelSupportsEffort(selectedModel, reasoningEffort) {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "当前模型不支持这个推理强度"}}, nil
	}
	if err := a.updateGlobalModelConfig(func(c *config.CodexConfig) {
		c.ReasoningEffort = strings.TrimSpace(reasoningEffort)
	}, result); err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: err.Error()}}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已更新全局推理强度"},
		Card:  rawCard(a.renderModelConfigCard(result)),
	}, nil
}

func (a *App) completeMenuQuiet(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	msg := &feishu.InboundMessage{MessageID: action.MessageID, ChatID: action.ChatID, UserID: action.UserID, ChatType: "p2p", Text: "/quiet"}
	go func() {
		_ = a.commandQuiet(msg, nil)
	}()
	return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "info", Content: "正在打开 quiet 配置"}}, nil
}

func (a *App) completeQuietSet(action *feishu.CardAction, enabled bool) (*callback.CardActionTriggerResponse, error) {
	if err := a.updateQuietMode(enabled); err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: err.Error()}}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已更新 quiet 开关"},
		Card:  rawCard(a.renderQuietModeCard()),
	}, nil
}

func (a *App) completeMenuThreads(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	msg := &feishu.InboundMessage{MessageID: action.MessageID, ChatID: action.ChatID, UserID: action.UserID, ChatType: "p2p", Text: "/threads"}
	if strings.Contains(sessionKey, ":group:") {
		msg.ChatType = "group"
	}
	go func() {
		_ = a.commandThreads(msg, false)
	}()
	return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "info", Content: "正在获取线程列表"}}, nil
}

func (a *App) completeMenuModel(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	msg := &feishu.InboundMessage{MessageID: action.MessageID, ChatID: action.ChatID, UserID: action.UserID, ChatType: "p2p", Text: "/model"}
	if strings.Contains(sessionKey, ":group:") {
		msg.ChatType = "group"
	}
	go func() {
		_ = a.commandModel(msg)
	}()
	return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "info", Content: "正在打开模型配置"}}, nil
}

func (a *App) completeMenuStatus(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	msg := &feishu.InboundMessage{MessageID: action.MessageID, ChatID: action.ChatID, UserID: action.UserID, ChatType: "p2p", Text: "/status"}
	if strings.Contains(sessionKey, ":group:") {
		msg.ChatType = "group"
	}
	go func() {
		_ = a.commandStatus(msg)
	}()
	return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "info", Content: "正在打开状态面板"}}, nil
}

func (a *App) completeMenuInterrupt(action *feishu.CardAction, sessionKey, targetTurnID string) (*callback.CardActionTriggerResponse, error) {
	sess := a.store.GetSession(sessionKey)
	discarded := a.discardSessionPendingInputs(sessionKey)
	sess = a.store.GetSession(sessionKey)
	if sess == nil || sess.ActiveTurnID == "" {
		if discarded > 0 {
			return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "success", Content: fmt.Sprintf("已清空 %d 条排队或暂存输入", discarded)}}, nil
		}
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "当前没有运行中的任务"}}, nil
	}
	if strings.TrimSpace(targetTurnID) != "" && sess.ActiveTurnID != targetTurnID {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "这个任务已经结束或已切换到其他任务"}}, nil
	}
	go func() {
		_ = a.codex.Call(context.Background(), "turn/interrupt", map[string]any{
			"threadId": sess.ActiveThreadID,
			"turnId":   sess.ActiveTurnID,
		}, nil)
	}()
	content := "已请求中断"
	if discarded > 0 {
		content = fmt.Sprintf("已请求中断，并清空 %d 条排队或暂存输入", discarded)
	}
	return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "success", Content: content}}, nil
}

func (a *App) completeMenuWorkspace(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	msg := &feishu.InboundMessage{MessageID: action.MessageID, ChatID: action.ChatID, UserID: action.UserID, ChatType: "p2p", Text: "/workspace"}
	if strings.Contains(sessionKey, ":group:") {
		msg.ChatType = "group"
	}
	go func() {
		_ = a.showWorkspaceMenu(msg)
	}()
	return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "info", Content: "正在打开工作区菜单"}}, nil
}

func (a *App) completeTurnAppend(action *feishu.CardAction, sessionKey, targetTurnID, itemID string) (*callback.CardActionTriggerResponse, error) {
	sess := a.store.GetSession(sessionKey)
	if sess == nil || sess.ActiveTurnID == "" || sess.ActiveThreadID == "" {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "当前没有可追加的任务"}}, nil
	}
	if strings.TrimSpace(targetTurnID) != "" && sess.ActiveTurnID != targetTurnID {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "这个任务已经结束或已切换到其他任务"}}, nil
	}
	a.resolvePendingTurnAppendRequests(sessionKey, action.UserID)
	requestID, err := a.store.NextLocalID("turn-append")
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: err.Error()}}, nil
	}
	body := "请直接发送要追加到当前任务的文本。\n\n下一条非命令消息会作为补充输入提交到当前 turn。"
	card := a.feishu.SimpleStatusCard("补充当前任务", "orange", body, []feishu.Button{
		{Text: "取消", Type: "default", Value: map[string]any{"action": "pending_form.cancel", "request_id": requestID}},
	})
	msgID, err := a.feishu.SendCard(context.Background(), action.ChatID, card)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: err.Error()}}, nil
	}
	_ = a.store.UpsertPending(&state.PendingRequest{
		ID:          requestID,
		Kind:        "turn_append",
		SessionKey:  sessionKey,
		ThreadID:    sess.ActiveThreadID,
		TurnID:      sess.ActiveTurnID,
		ItemID:      itemID,
		OwnerUserID: action.UserID,
		FeishuMsgID: msgID,
		Status:      "pending",
		CreatedAt:   time.Now().Unix(),
		ExpiresAt:   time.Now().Add(10 * time.Minute).Unix(),
	})
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "请发送要追加的内容"},
	}, nil
}

func (a *App) completeTurnItemToggle(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	requestID, _ := action.ActionValue["request_id"].(string)
	if strings.TrimSpace(requestID) == "" {
		if parsedID, _, ok := parseTurnItemToggleName(action.Name); ok {
			requestID = parsedID
		}
	}
	pending := a.store.PendingByID(requestID)
	if pending == nil || pending.Kind != "turn_item_card" {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "详情卡已失效"}}, nil
	}
	if pending.OwnerUserID != "" && pending.OwnerUserID != action.UserID {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "你没有权限操作这张卡片"}}, nil
	}
	var payload turnItemCardPayload
	if err := json.Unmarshal([]byte(pending.PayloadJSON), &payload); err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: "详情卡数据损坏"}}, nil
	}
	expanded, _ := action.ActionValue["expanded"].(bool)
	if !expanded {
		if _, parsedExpanded, ok := parseTurnItemToggleName(action.Name); ok {
			expanded = parsedExpanded
		}
	}
	sub := a.store.GetSubmission(payload.SubmissionID)
	includeActions := false
	if sess := a.store.GetSession(payload.SessionKey); sess != nil && sess.ActiveTurnID == payload.TurnID {
		includeActions = true
	}
	card := a.renderTurnItemCard(sub, payload, !expanded, includeActions, requestID)
	return &callback.CardActionTriggerResponse{
		Card: rawCard(card),
	}, nil
}

func parseTurnItemToggleName(name string) (requestID string, expanded bool, ok bool) {
	const prefix = "turn.item.toggle:"
	name = strings.TrimSpace(name)
	if !strings.HasPrefix(name, prefix) {
		return "", false, false
	}
	parts := strings.Split(strings.TrimPrefix(name, prefix), ":")
	if len(parts) != 2 {
		return "", false, false
	}
	requestID = strings.TrimSpace(parts[0])
	state := strings.TrimSpace(parts[1])
	switch state {
	case "expanded":
		return requestID, true, requestID != ""
	case "collapsed":
		return requestID, false, requestID != ""
	default:
		return "", false, false
	}
}

func (a *App) completeWorkspaceUse(action *feishu.CardAction, sessionKey, workspaceID string) (*callback.CardActionTriggerResponse, error) {
	ws := config.FindWorkspace(a.cfg, workspaceID)
	if ws == nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: "工作区不存在"}}, nil
	}
	sess := a.store.GetSession(sessionKey)
	if sess == nil {
		sess = &state.Session{Key: sessionKey, OwnerUserID: action.UserID, ChatID: action.ChatID}
	}
	switchSessionWorkspace(sess, workspaceID)
	_ = a.store.UpsertSession(sess)
	body := "当前工作区: `" + workspaceID + "`\n\ncwd: `" + ws.Cwd + "`"
	if sess.ActiveTurnID != "" {
		body += "\n\n当前运行中的任务仍归属原线程；后续新任务会使用这个工作区。"
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已切换工作区"},
		Card: &callback.Card{
			Type: "raw",
			Data: a.feishu.SimpleStatusCard("已切换工作区", "green", body, []feishu.Button{
				{Text: "查看工作区", Type: "default", Value: map[string]any{"action": "workspace.new", "session_key": sessionKey}},
			}),
		},
	}, nil
}

func (a *App) completeWorkspaceNew(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	msg := &feishu.InboundMessage{MessageID: action.MessageID, ChatID: action.ChatID, UserID: action.UserID, ChatType: "p2p", Text: "/workspace new"}
	if strings.Contains(sessionKey, ":group:") {
		msg.ChatType = "group"
	}
	go func() {
		_ = a.beginWorkspaceNew(msg)
	}()
	return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "info", Content: "请按提示发送工作区信息"}}, nil
}

func (a *App) completeWorkspaceSandboxMenu(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	msg := &feishu.InboundMessage{MessageID: action.MessageID, ChatID: action.ChatID, UserID: action.UserID, ChatType: "p2p", Text: "/workspace sandbox"}
	if strings.Contains(sessionKey, ":group:") {
		msg.ChatType = "group"
	}
	go func() {
		_ = a.showWorkspaceSandboxMenu(msg)
	}()
	return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "info", Content: "正在打开 sandbox 配置"}}, nil
}

func (a *App) completeWorkspacePolicyMenu(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	msg := &feishu.InboundMessage{MessageID: action.MessageID, ChatID: action.ChatID, UserID: action.UserID, ChatType: "p2p", Text: "/workspace policy"}
	if strings.Contains(sessionKey, ":group:") {
		msg.ChatType = "group"
	}
	go func() {
		_ = a.showWorkspacePolicyMenu(msg)
	}()
	return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "info", Content: "正在打开 policy 配置"}}, nil
}

func (a *App) completeThreadSandboxMenu(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	msg := &feishu.InboundMessage{MessageID: action.MessageID, ChatID: action.ChatID, UserID: action.UserID, ChatType: "p2p", Text: "/threads sandbox"}
	if strings.Contains(sessionKey, ":group:") {
		msg.ChatType = "group"
	}
	go func() {
		_ = a.showThreadSandboxMenu(msg)
	}()
	return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "info", Content: "正在打开 thread sandbox 配置"}}, nil
}

func (a *App) completeThreadPolicyMenu(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	msg := &feishu.InboundMessage{MessageID: action.MessageID, ChatID: action.ChatID, UserID: action.UserID, ChatType: "p2p", Text: "/threads policy"}
	if strings.Contains(sessionKey, ":group:") {
		msg.ChatType = "group"
	}
	go func() {
		_ = a.showThreadPolicyMenu(msg)
	}()
	return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "info", Content: "正在打开 thread policy 配置"}}, nil
}

func (a *App) updateWorkspaceDefaults(workspaceID string, mutate func(*config.Workspace)) (*config.Workspace, error) {
	ws := config.FindWorkspace(a.cfg, workspaceID)
	if ws == nil {
		return nil, fmt.Errorf("workspace %q not found", workspaceID)
	}
	mutate(ws)
	if err := a.cfg.Normalize(filepath.Dir(a.cfgPath)); err != nil {
		return nil, err
	}
	if err := config.Save(a.cfgPath, a.cfg); err != nil {
		return nil, err
	}
	return config.FindWorkspace(a.cfg, workspaceID), nil
}

func (a *App) completeWorkspaceSandboxSet(action *feishu.CardAction, sessionKey, workspaceID, sandboxMode string) (*callback.CardActionTriggerResponse, error) {
	valid := false
	for _, opt := range workspaceSandboxOptions() {
		if opt.Value == sandboxMode {
			valid = true
			break
		}
	}
	if !valid {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: "不支持的 sandbox"}}, nil
	}
	ws, err := a.updateWorkspaceDefaults(workspaceID, func(w *config.Workspace) {
		w.SandboxMode = sandboxMode
	})
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: err.Error()}}, nil
	}
	body := "工作区: `" + ws.ID + "`\n默认 sandbox: `" + ws.SandboxMode + "`\n默认 policy: `" + ws.ApprovalPolicy + "`"
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已更新 sandbox"},
		Card: &callback.Card{
			Type: "raw",
			Data: a.feishu.SimpleStatusCard("Sandbox 已更新", "green", body, []feishu.Button{
				{Text: "配置 Policy", Type: "default", Value: map[string]any{"action": "workspace.policy.menu", "session_key": sessionKey}},
			}),
		},
	}, nil
}

func (a *App) completeWorkspacePolicySet(action *feishu.CardAction, sessionKey, workspaceID, approvalPolicy string) (*callback.CardActionTriggerResponse, error) {
	valid := false
	for _, opt := range workspaceApprovalPolicyOptions() {
		if opt.Value == approvalPolicy {
			valid = true
			break
		}
	}
	if !valid {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: "不支持的 policy"}}, nil
	}
	ws, err := a.updateWorkspaceDefaults(workspaceID, func(w *config.Workspace) {
		w.ApprovalPolicy = approvalPolicy
	})
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: err.Error()}}, nil
	}
	body := "工作区: `" + ws.ID + "`\n默认 sandbox: `" + ws.SandboxMode + "`\n默认 policy: `" + ws.ApprovalPolicy + "`"
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已更新 policy"},
		Card: &callback.Card{
			Type: "raw",
			Data: a.feishu.SimpleStatusCard("Policy 已更新", "green", body, []feishu.Button{
				{Text: "配置 Sandbox", Type: "default", Value: map[string]any{"action": "workspace.sandbox.menu", "session_key": sessionKey}},
			}),
		},
	}, nil
}

func (a *App) completeThreadSandboxSet(action *feishu.CardAction, sessionKey, threadID, sandboxMode string) (*callback.CardActionTriggerResponse, error) {
	valid := false
	for _, opt := range workspaceSandboxOptions() {
		if opt.Value == sandboxMode {
			valid = true
			break
		}
	}
	if !valid {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: "不支持的 sandbox"}}, nil
	}
	sess := a.store.GetSession(sessionKey)
	if sess == nil || strings.TrimSpace(sess.ActiveThreadID) == "" || strings.TrimSpace(sess.ActiveThreadID) != strings.TrimSpace(threadID) {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "当前 thread 已失效"}}, nil
	}
	sess.ActiveThreadSandboxMode = sandboxMode
	if err := a.store.UpsertSession(sess); err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: err.Error()}}, nil
	}
	body := "thread: `" + threadID + "`\n默认 sandbox: `" + sandboxMode + "`\n默认 policy: `" + effectiveThreadApprovalPolicy(sess, config.FindWorkspace(a.cfg, sess.WorkspaceID)) + "`"
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已更新 thread sandbox"},
		Card: &callback.Card{
			Type: "raw",
			Data: a.feishu.SimpleStatusCard("Thread Sandbox 已更新", "green", body, []feishu.Button{
				{Text: "配置 Thread Policy", Type: "default", Value: map[string]any{"action": "thread.policy.menu", "session_key": sessionKey}},
			}),
		},
	}, nil
}

func (a *App) completeThreadPolicySet(action *feishu.CardAction, sessionKey, threadID, approvalPolicy string) (*callback.CardActionTriggerResponse, error) {
	valid := false
	for _, opt := range workspaceApprovalPolicyOptions() {
		if opt.Value == approvalPolicy {
			valid = true
			break
		}
	}
	if !valid {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: "不支持的 policy"}}, nil
	}
	sess := a.store.GetSession(sessionKey)
	if sess == nil || strings.TrimSpace(sess.ActiveThreadID) == "" || strings.TrimSpace(sess.ActiveThreadID) != strings.TrimSpace(threadID) {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "当前 thread 已失效"}}, nil
	}
	sess.ActiveThreadApprovalPolicy = approvalPolicy
	if err := a.store.UpsertSession(sess); err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: err.Error()}}, nil
	}
	body := "thread: `" + threadID + "`\n默认 sandbox: `" + effectiveThreadSandboxMode(sess, config.FindWorkspace(a.cfg, sess.WorkspaceID)) + "`\n默认 policy: `" + approvalPolicy + "`"
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已更新 thread policy"},
		Card: &callback.Card{
			Type: "raw",
			Data: a.feishu.SimpleStatusCard("Thread Policy 已更新", "green", body, []feishu.Button{
				{Text: "配置 Thread Sandbox", Type: "default", Value: map[string]any{"action": "thread.sandbox.menu", "session_key": sessionKey}},
			}),
		},
	}, nil
}

func (a *App) completeThreadResume(action *feishu.CardAction, sessionKey, threadID string) (*callback.CardActionTriggerResponse, error) {
	sess := a.store.GetSession(sessionKey)
	if sess == nil {
		sess = &state.Session{Key: sessionKey, OwnerUserID: action.UserID, ChatID: action.ChatID}
	}
	if sess.ActiveTurnID != "" {
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "warning", Content: "当前任务仍在运行，请先等待结束或中断"},
		}, nil
	}
	if strings.TrimSpace(sess.OwnerUserID) == "" {
		sess.OwnerUserID = action.UserID
	}
	if strings.TrimSpace(sess.ChatID) == "" {
		sess.ChatID = action.ChatID
	}
	selectedName, _ := action.ActionValue["thread_name"].(string)
	selectedPreview, _ := action.ActionValue["thread_preview"].(string)
	selectedCWD, _ := action.ActionValue["thread_cwd"].(string)
	workspaceID := sess.WorkspaceID
	if strings.TrimSpace(workspaceID) == "" {
		workspaceID = a.defaultWorkspaceID()
	}
	if ws := config.FindWorkspace(a.cfg, workspaceID); ws != nil && strings.TrimSpace(selectedCWD) != "" && !sameWorkspaceCWD(selectedCWD, ws.Cwd) {
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "warning", Content: "该线程不属于当前工作区，请先切换 workspace"},
		}, nil
	}
	effectiveModel := configuredGlobalModel(a.cfg)
	params := map[string]any{
		"threadId":               threadID,
		"persistExtendedHistory": true,
	}
	if strings.TrimSpace(effectiveModel) != "" {
		params["model"] = effectiveModel
	}
	slog.Debug("manual thread resume request",
		"session_key", sessionKey,
		"thread_id", threadID,
		"model", effectiveModel,
	)
	var result codexrpc.ThreadStartResult
	if err := a.codex.Call(context.Background(), "thread/resume", params, &result); err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: err.Error()}}, nil
	}
	sess.ActiveThreadApprovalPolicy = ""
	sess.ActiveThreadSandboxMode = ""
	setSessionThreadContext(sess, workspaceID, threadID, firstNonEmpty(selectedName, result.Thread.Name), firstNonEmpty(selectedPreview, result.Thread.Preview))
	a.markSessionThreadLive(sessionKey, threadID)
	sess.ActiveTurnID = ""
	sess.ActiveSubmissionID = ""
	sess.Status = "idle"
	_ = a.store.UpsertSession(sess)
	body := "后续消息会继续写入这个线程。\n\n当前线程: " + currentThreadLabel(sess) + "\nthread: `" + threadID + "`"
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已恢复线程"},
		Card: &callback.Card{
			Type: "raw",
			Data: a.feishu.SimpleStatusCard("已切换线程", "green", body, []feishu.Button{
				{Text: "新会话", Type: "default", Value: map[string]any{"action": "menu.new", "session_key": sessionKey}},
				{Text: "线程列表", Type: "default", Value: map[string]any{"action": "menu.threads", "session_key": sessionKey}},
			}),
		},
	}, nil
}

func (a *App) completeApprovalAction(action *feishu.CardAction, actionName string) (*callback.CardActionTriggerResponse, error) {
	requestID, _ := action.ActionValue["request_id"].(string)
	pending := a.store.PendingByID(requestID)
	if pending == nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "审批已过期"}}, nil
	}
	if pending.OwnerUserID != "" && pending.OwnerUserID != action.UserID {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "你没有权限处理这个审批"}}, nil
	}
	switch pending.Kind {
	case "command":
		resp := map[string]any{"decision": "decline"}
		switch actionName {
		case "approval.command.accept":
			resp["decision"] = "accept"
		case "approval.command.accept_session":
			resp["decision"] = "acceptForSession"
		case "approval.command.cancel", "approval.command.decline":
			resp["decision"] = "decline"
		}
		_ = a.codex.Reply(requestIDRaw(requestID), resp)
	case "file":
		resp := map[string]any{"decision": "decline"}
		switch actionName {
		case "approval.file.accept":
			resp["decision"] = "accept"
		case "approval.file.accept_session":
			resp["decision"] = "acceptForSession"
		case "approval.file.cancel", "approval.file.decline":
			resp["decision"] = "decline"
		}
		_ = a.codex.Reply(requestIDRaw(requestID), resp)
	case "permissions":
		var payload struct {
			Permissions map[string]any `json:"permissions"`
		}
		_ = json.Unmarshal([]byte(pending.PayloadJSON), &payload)
		scope := "turn"
		if actionName == "approval.permissions.accept_session" {
			scope = "session"
		}
		_ = a.codex.Reply(requestIDRaw(requestID), map[string]any{
			"permissions": payload.Permissions,
			"scope":       scope,
		})
	}
	_ = a.store.UpdatePending(requestID, func(req *state.PendingRequest) { req.Status = "resolved" })
	a.resumeSubmissionAfterRequest(pending)
	card := a.feishu.SimpleStatusCard("审批已处理", "green", a.approvalDecisionText(actionName), nil)
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "审批已提交"},
		Card: &callback.Card{
			Type: "raw",
			Data: card,
		},
	}, nil
}

func (a *App) completeUserInputAnswer(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	requestID, _ := action.ActionValue["request_id"].(string)
	questionID, _ := action.ActionValue["question_id"].(string)
	answer, _ := action.ActionValue["answer"].(string)
	pending := a.store.PendingByID(requestID)
	if pending == nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "请求已过期"}}, nil
	}
	if pending.OwnerUserID != "" && pending.OwnerUserID != action.UserID {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "你没有权限回答这个问题"}}, nil
	}
	payload := map[string]any{
		"answers": map[string]any{
			questionID: map[string]any{
				"answers": []string{answer},
			},
		},
	}
	_ = a.codex.Reply(requestIDRaw(requestID), payload)
	_ = a.store.UpdatePending(requestID, func(req *state.PendingRequest) { req.Status = "resolved" })
	a.resumeSubmissionAfterRequest(pending)
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已提交"},
		Card: &callback.Card{
			Type: "raw",
			Data: a.feishu.SimpleStatusCard("已提交", "green", answer, nil),
		},
	}, nil
}

func (a *App) approvalDecisionText(action string) string {
	switch action {
	case "approval.command.accept", "approval.file.accept":
		return "已允许本次执行"
	case "approval.command.accept_session", "approval.file.accept_session":
		return "已允许本会话执行"
	case "approval.permissions.accept_turn":
		return "已授权本次权限请求"
	case "approval.permissions.accept_session":
		return "已授权本会话权限请求"
	default:
		return "已拒绝"
	}
}

func (a *App) resumeSubmissionAfterRequest(pending *state.PendingRequest) {
	if pending == nil {
		return
	}
	_, sub := a.findSubmissionByTurn(pending.ThreadID, pending.TurnID)
	if sub == nil {
		return
	}
	_ = a.store.UpdateSubmission(sub.ID, func(s *state.Submission) { s.Status = "running" })
	_ = a.refreshStatusCard(sub.ID)
}
