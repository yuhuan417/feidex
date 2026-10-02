package app

import (
	"encoding/json"
	"feidex/internal/domain/conversation"
	"fmt"
	"log/slog"
	"strings"

	appcards "feidex/internal/app/cards"
	"feidex/internal/app/pendingforms"
	"feidex/internal/feishu"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

func sendAsyncUserInputCard(a *App, sub *state.Submission, payload pendingforms.ToolUserInputPayload, reuseMessageID string) string {
	payload.ThreadID, payload.TurnID = sub.ThreadID, sub.TurnID
	requestID := pendingforms.AsyncUserInputPendingKind + ":" + sub.TurnID + ":" + payload.ItemID
	if pending := a.State().Pending(requestID); pending != nil {
		return pending.FeishuMsgID
	}
	drafts := pendingforms.FormDrafts{Values: map[string]string{}}
	for _, question := range payload.Questions {
		if len(question.Options) > 0 {
			drafts.Values[question.ID] = question.Options[0].Label
		}
	}
	card := pendingforms.RenderAsyncUserInputFormCard(requestID, payload, drafts, sub.UserID)
	if err := deliverPendingCard(a, sub, card, pendingCardDelivery{
		requestKey: requestID, backend: backendCodex, kind: pendingforms.AsyncUserInputPendingKind,
		sessionKey: sub.SessionKey, threadID: sub.ThreadID, turnID: sub.TurnID,
		itemID: payload.ItemID, ownerUserID: sub.UserID, payloadJSON: mustJSON(payload),
		linkKind: "user_input_card", nonBlocking: true, reuseMessageID: reuseMessageID,
	}); err != nil {
		slog.Error("async user input card failed", "turn_id", sub.TurnID, "item_id", payload.ItemID, "error", err)
		return ""
	}
	return a.State().Pending(requestID).FeishuMsgID
}

func asyncUserInputSession(a *App, pending *state.PendingRequest) (*conversation.Session, error) {
	sess := a.State().Session(pending.SessionKey)
	if sess == nil || sess.ActiveThreadID != pending.ThreadID || configuredBackend(a) != pending.Backend {
		return nil, fmt.Errorf("会话已切换，请在当前会话中回答")
	}
	return sess, nil
}

func completeAsyncUserInput(a *App, action *feishu.CardAction, cancel bool) (*callback.CardActionTriggerResponse, error) {
	warning := func(text string) (*callback.CardActionTriggerResponse, error) {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: text}}, nil
	}
	if action == nil {
		return warning("请求已过期")
	}
	requestID, _ := action.ActionValue["request_id"].(string)
	pending := a.State().Pending(requestID)
	if pending == nil || pending.Kind != pendingforms.AsyncUserInputPendingKind || state.NormalizePendingRequestStatus(pending.Status) != state.PendingRequestStatusPending {
		return warning("请求已处理或过期")
	}
	if pending.OwnerUserID != "" && pending.OwnerUserID != action.UserID {
		return warning("你没有权限回答这个问题")
	}
	if action.MessageID != "" && action.MessageID != pending.FeishuMsgID {
		return warning("问题卡片不匹配")
	}
	var payload pendingforms.ToolUserInputPayload
	if err := json.Unmarshal([]byte(pending.PayloadJSON), &payload); err != nil {
		return warning("问题内容已损坏")
	}
	drafts := pendingforms.ToolUserInputDraftsFromCardAction(payload, action)
	answerText := ""
	if !cancel {
		sess, err := asyncUserInputSession(a, pending)
		if err != nil {
			return warning(err.Error())
		}
		if action.ChatID != "" && action.ChatID != sess.ChatID {
			return warning("问题会话不匹配")
		}
		answerText, err = pendingforms.AsyncUserInputAnswerText(payload, drafts)
		if err != nil {
			return &callback.CardActionTriggerResponse{
				Toast: &callback.Toast{Type: "warning", Content: err.Error()},
				Card:  rawCard(pendingforms.RenderAsyncUserInputFormCard(requestID, payload, drafts, pending.OwnerUserID)),
			}, nil
		}
	}
	claimed := false
	err := a.State().UpdatePending(requestID, func(current *state.PendingRequest) {
		if state.NormalizePendingRequestStatus(current.Status) != state.PendingRequestStatusPending {
			return
		}
		claimed = true
		current.Status = state.PendingRequestStatusReplied.String()
		if cancel {
			current.Status = state.PendingRequestStatusResolved.String()
		}
	})
	if err != nil || !claimed {
		return warning("请求已处理或正在提交")
	}
	if cancel {
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "success", Content: "已取消"},
			Card:  rawCard(a.feishu.SimpleStatusCard("输入请求已取消", "grey", pendingforms.RenderToolUserInputBody(payload), nil)),
		}, nil
	}
	// Only local validation/state changes run in the callback. The backend can
	// take seconds to steer or start a turn, so acknowledge before doing I/O.
	runAsync(a, func() {
		err := submitAsyncUserInput(a, pending, action.UserID, answerText)
		var card map[string]any
		if err != nil {
			_ = a.State().UpdatePending(requestID, func(current *state.PendingRequest) { current.Status = state.PendingRequestStatusPending.String() })
			card = pendingforms.RenderAsyncUserInputFormCard(requestID, payload, drafts, pending.OwnerUserID)
			appcards.AppendMarkdownBodyCardElement(card, map[string]any{"tag": "markdown", "content": "提交失败，请重试。\n" + err.Error()})
			slog.Warn("async user input submission failed", "request_id", requestID, "error", err)
		} else {
			_ = a.State().UpdatePending(requestID, func(current *state.PendingRequest) { current.Status = state.PendingRequestStatusResolved.String() })
			card = a.feishu.SimpleStatusCard("输入已提交", "green", answerText, nil)
		}
		patchMaintenanceCard(a, pending.FeishuMsgID, card, "async user input patch failed", "request_id", requestID)
	})
	return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "info", Content: "正在提交回答"}}, nil
}

func submitAsyncUserInput(a *App, pending *state.PendingRequest, userID, text string) error {
	sess, err := asyncUserInputSession(a, pending)
	if err != nil {
		return err
	}
	if strings.TrimSpace(sess.ActiveTurnID) != "" {
		return conversationBackend(a).ContinueActiveTurn(pending.SessionKey, text)
	}
	msg := &feishu.InboundMessage{
		SessionKey: pending.SessionKey, MessageID: pending.FeishuMsgID,
		ParentMessageID: pending.FeishuMsgID, RootMessageID: pending.FeishuMsgID,
		ChatID: sess.ChatID, ChatType: sess.ChatType, UserID: userID, Text: text,
	}
	return newSubmissionQueueServiceFromApp(a).EnqueueSubmission(msg, pending.SessionKey, true)
}
