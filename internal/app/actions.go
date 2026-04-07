package app

import (
	"context"
	"encoding/json"
	"log/slog"
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
	switch name {
	case "menu.new":
		sessionKey, _ := action.ActionValue["session_key"].(string)
		return a.completeMenuNew(action, sessionKey)
	case "menu.threads":
		sessionKey, _ := action.ActionValue["session_key"].(string)
		return a.completeMenuThreads(action, sessionKey)
	case "menu.interrupt":
		sessionKey, _ := action.ActionValue["session_key"].(string)
		turnID, _ := action.ActionValue["turn_id"].(string)
		return a.completeMenuInterrupt(action, sessionKey, turnID)
	case "workspace.use":
		sessionKey, _ := action.ActionValue["session_key"].(string)
		workspaceID, _ := action.ActionValue["workspace_id"].(string)
		return a.completeWorkspaceUse(action, sessionKey, workspaceID)
	case "workspace.new":
		sessionKey, _ := action.ActionValue["session_key"].(string)
		return a.completeWorkspaceNew(action, sessionKey)
	case "thread.resume":
		sessionKey, _ := action.ActionValue["session_key"].(string)
		threadID, _ := action.ActionValue["thread_id"].(string)
		return a.completeThreadResume(action, sessionKey, threadID)
	case "turn.append":
		sessionKey, _ := action.ActionValue["session_key"].(string)
		turnID, _ := action.ActionValue["turn_id"].(string)
		itemID, _ := action.ActionValue["item_id"].(string)
		return a.completeTurnAppend(action, sessionKey, turnID, itemID)
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
	case "outbound_files.send", "outbound_files.cancel":
		return a.completeOutboundFilesAction(action, name)
	default:
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
	sess.ActiveThreadID = ""
	sess.ActiveTurnID = ""
	sess.ActiveSubmissionID = ""
	sess.Status = "idle"
	_ = a.store.UpsertSession(sess)
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已切换到新会话"},
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

func (a *App) completeMenuInterrupt(action *feishu.CardAction, sessionKey, targetTurnID string) (*callback.CardActionTriggerResponse, error) {
	sess := a.store.GetSession(sessionKey)
	if sess == nil || sess.ActiveTurnID == "" {
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
	return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "success", Content: "已请求中断"}}, nil
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

func (a *App) completeWorkspaceUse(action *feishu.CardAction, sessionKey, workspaceID string) (*callback.CardActionTriggerResponse, error) {
	ws := config.FindWorkspace(a.cfg, workspaceID)
	if ws == nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: "工作区不存在"}}, nil
	}
	sess := a.store.GetSession(sessionKey)
	if sess == nil {
		sess = &state.Session{Key: sessionKey, OwnerUserID: action.UserID, ChatID: action.ChatID}
	}
	sess.WorkspaceID = workspaceID
	_ = a.store.UpsertSession(sess)
	body := "当前工作区: `" + workspaceID + "`\n\ncwd: `" + ws.Cwd + "`"
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

func (a *App) completeThreadResume(action *feishu.CardAction, sessionKey, threadID string) (*callback.CardActionTriggerResponse, error) {
	sess := a.store.GetSession(sessionKey)
	if sess == nil {
		sess = &state.Session{Key: sessionKey, OwnerUserID: action.UserID, ChatID: action.ChatID}
	}
	if strings.TrimSpace(sess.OwnerUserID) == "" {
		sess.OwnerUserID = action.UserID
	}
	if strings.TrimSpace(sess.ChatID) == "" {
		sess.ChatID = action.ChatID
	}
	selectedName, _ := action.ActionValue["thread_name"].(string)
	selectedPreview, _ := action.ActionValue["thread_preview"].(string)
	workspaceID := sess.WorkspaceID
	if strings.TrimSpace(workspaceID) == "" {
		workspaceID = a.defaultWorkspaceID()
	}
	effectiveModel := a.defaultModel()
	if ws := config.FindWorkspace(a.cfg, workspaceID); ws != nil {
		effectiveModel = firstNonEmpty(sess.ModelOverride, ws.Model, a.defaultModel())
	}
	params := map[string]any{
		"threadId":               threadID,
		"persistExtendedHistory": true,
	}
	if strings.TrimSpace(effectiveModel) != "" {
		params["model"] = effectiveModel
	}
	slog.Info("manual thread resume request",
		"session_key", sessionKey,
		"thread_id", threadID,
		"model", effectiveModel,
	)
	var result codexrpc.ThreadStartResult
	if err := a.codex.Call(context.Background(), "thread/resume", params, &result); err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: err.Error()}}, nil
	}
	sess.ActiveThreadID = threadID
	sess.ActiveThreadName = strings.TrimSpace(firstNonEmpty(selectedName, result.Thread.Name))
	sess.ActiveThreadPreview = strings.TrimSpace(firstNonEmpty(selectedPreview, result.Thread.Preview))
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
		case "approval.command.cancel":
			resp["decision"] = "cancel"
		case "approval.command.decline":
			resp["decision"] = "decline"
		}
		_ = a.codex.Reply(json.RawMessage(requestID), resp)
	case "file":
		resp := map[string]any{"decision": "decline"}
		switch actionName {
		case "approval.file.accept":
			resp["decision"] = "accept"
		case "approval.file.accept_session":
			resp["decision"] = "acceptForSession"
		case "approval.file.cancel":
			resp["decision"] = "cancel"
		case "approval.file.decline":
			resp["decision"] = "decline"
		}
		_ = a.codex.Reply(json.RawMessage(requestID), resp)
	case "permissions":
		var payload struct {
			Permissions map[string]any `json:"permissions"`
		}
		_ = json.Unmarshal([]byte(pending.PayloadJSON), &payload)
		scope := "turn"
		if actionName == "approval.permissions.accept_session" {
			scope = "session"
		}
		_ = a.codex.Reply(json.RawMessage(requestID), map[string]any{
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
	_ = a.codex.Reply(json.RawMessage(requestID), payload)
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
	case "approval.command.cancel", "approval.file.cancel":
		return "已取消"
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
