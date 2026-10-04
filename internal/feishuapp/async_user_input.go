package feishuapp

import (
	"encoding/json"
	domainbackend "feidex/internal/domain/backend"
	domainsubmission "feidex/internal/domain/submission"
	"log/slog"
	"strings"

	appcards "feidex/internal/adapter/feishu/cards"
	"feidex/internal/adapter/feishu/pendingforms"
	"feidex/internal/feishu"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

type asyncUserInputCardSender struct {
	pending interface {
		Pending(string) *state.PendingRequest
	}
	delivery pendingCardDeliveryService
}

func (s asyncUserInputCardSender) Send(sub *domainsubmission.Submission, payload pendingforms.ToolUserInputPayload, reuseMessageID string) string {
	payload.ThreadID, payload.TurnID = sub.ThreadID, sub.TurnID
	requestID := pendingforms.AsyncUserInputPendingKind + ":" + sub.TurnID + ":" + payload.ItemID
	if pending := s.pending.Pending(requestID); pending != nil && (strings.TrimSpace(pending.FeishuMsgID) != "" || state.NormalizePendingRequestStatus(pending.Status) != state.PendingRequestStatusPending) {
		return pending.FeishuMsgID
	}
	drafts := pendingforms.FormDrafts{Values: map[string]string{}}
	for _, question := range payload.Questions {
		if len(question.Options) > 0 {
			drafts.Values[question.ID] = question.Options[0].Label
		}
	}
	card := pendingforms.RenderAsyncUserInputFormCard(requestID, payload, drafts, sub.UserID)
	if err := s.delivery.Deliver(anchorForSubmission(sub), card, pendingCardDelivery{
		requestKey: requestID, backend: domainbackend.BackendCodex, kind: pendingforms.AsyncUserInputPendingKind,
		sessionKey: sub.SessionKey, threadID: sub.ThreadID, turnID: sub.TurnID,
		itemID: payload.ItemID, ownerUserID: sub.UserID, payloadJSON: mustJSON(payload),
		linkKind: "user_input_card", nonBlocking: true, reuseMessageID: reuseMessageID,
	}); err != nil {
		slog.Error("async user input card failed", "turn_id", sub.TurnID, "item_id", payload.ItemID, "error", err)
		return ""
	}
	return s.pending.Pending(requestID).FeishuMsgID
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
	if err := a.bindings.AsyncInputs.Validate(pending, action.UserID, action.MessageID, action.ChatID, cancel); err != nil {
		return warning(err.Error())
	}
	var payload pendingforms.ToolUserInputPayload
	if err := json.Unmarshal([]byte(pending.PayloadJSON), &payload); err != nil {
		return warning("问题内容已损坏")
	}
	drafts := pendingforms.ToolUserInputDraftsFromCardAction(payload, action)
	answerText := ""
	if !cancel {
		var err error
		answerText, err = pendingforms.AsyncUserInputAnswerText(payload, drafts)
		if err != nil {
			return &callback.CardActionTriggerResponse{
				Toast: &callback.Toast{Type: "warning", Content: err.Error()},
				Card:  rawCard(pendingforms.RenderAsyncUserInputFormCard(requestID, payload, drafts, pending.OwnerUserID)),
			}, nil
		}
	}
	if err := a.bindings.AsyncInputs.Claim(pending, cancel); err != nil {
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
	if err := a.bindings.AsyncInputs.Dispatch(pending, action.UserID, answerText, func(err error) {
		var card map[string]any
		if err != nil {
			card = pendingforms.RenderAsyncUserInputFormCard(requestID, payload, drafts, pending.OwnerUserID)
			appcards.AppendMarkdownBodyCardElement(card, map[string]any{"tag": "markdown", "content": "提交失败，请重试。\n" + err.Error()})
			slog.Warn("async user input submission failed", "request_id", requestID, "error", err)
		} else {
			card = a.feishu.SimpleStatusCard("输入已提交", "green", answerText, nil)
		}
		patchMaintenanceCard(a.Context(), a.FrontendID(), newEffectRunner(a.runtimeOwner), pending.FeishuMsgID, card, "async user input patch failed", "request_id", requestID)
	}); err != nil {
		return warning(err.Error())
	}
	return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "info", Content: "正在提交回答"}}, nil
}
