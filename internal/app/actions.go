package app

import (
	"context"
	"encoding/json"
	"strings"

	"feidex/internal/codexrpc"
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
		return a.completeMenuInterrupt(action, sessionKey)
	case "thread.resume":
		sessionKey, _ := action.ActionValue["session_key"].(string)
		threadID, _ := action.ActionValue["thread_id"].(string)
		return a.completeThreadResume(action, sessionKey, threadID)
	case "user_input.answer":
		return a.completeUserInputAnswer(action)
	case "approval.command.accept", "approval.command.decline", "approval.file.accept", "approval.file.decline":
		return a.completeApprovalAction(action, name)
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

func (a *App) completeMenuInterrupt(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	sess := a.store.GetSession(sessionKey)
	if sess == nil || sess.ActiveTurnID == "" {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "当前没有运行中的任务"}}, nil
	}
	go func() {
		_ = a.codex.Call(context.Background(), "turn/interrupt", map[string]any{
			"threadId": sess.ActiveThreadID,
			"turnId":   sess.ActiveTurnID,
		}, nil)
	}()
	return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "success", Content: "已请求中断"}}, nil
}

func (a *App) completeThreadResume(action *feishu.CardAction, sessionKey, threadID string) (*callback.CardActionTriggerResponse, error) {
	sess := a.store.GetSession(sessionKey)
	if sess == nil {
		sess = &state.Session{Key: sessionKey, OwnerUserID: action.UserID, ChatID: action.ChatID}
	}
	var result codexrpc.ThreadStartResult
	if err := a.codex.Call(context.Background(), "thread/resume", map[string]any{
		"threadId":               threadID,
		"persistExtendedHistory": true,
	}, &result); err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: err.Error()}}, nil
	}
	sess.ActiveThreadID = threadID
	sess.ActiveTurnID = ""
	sess.ActiveSubmissionID = ""
	sess.Status = "idle"
	_ = a.store.UpsertSession(sess)
	return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "success", Content: "已恢复线程"}}, nil
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
	decision := "decline"
	if strings.HasSuffix(actionName, ".accept") {
		decision = "accept"
	}
	methodResponse := map[string]any{"decision": decision}
	if strings.HasPrefix(actionName, "approval.command.") {
		_ = a.codex.Reply(json.RawMessage(requestID), methodResponse)
	} else {
		_ = a.codex.Reply(json.RawMessage(requestID), methodResponse)
	}
	_ = a.store.UpdatePending(requestID, func(req *state.PendingRequest) { req.Status = "resolved" })
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "审批已提交"},
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
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已提交"},
	}, nil
}
