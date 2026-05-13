package serverrequest

import (
	"encoding/json"
	"log/slog"
	"strings"

	appapproval "feidex/internal/app/approval"
	"feidex/internal/app/approvalview"
	"feidex/internal/app/pendingforms"
	"feidex/internal/feishu"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

// CompleteApprovalAction handles all approval card button clicks.
func (s *Service) CompleteApprovalAction(action *feishu.CardAction, actionName string) (*callback.CardActionTriggerResponse, error) {
	requestID, _ := action.ActionValue["request_id"].(string)
	pending := s.Pending(requestID)
	if pending == nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "审批已过期"}}, nil
	}
	if pending.OwnerUserID != "" && pending.OwnerUserID != action.UserID {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "你没有权限处理这个审批"}}, nil
	}
	var replyPayload any
	var warning string
	switch appapproval.NormalizeKind(pending.Kind) {
	case appapproval.KindCommand:
		replyPayload, warning = approvalview.CommandApprovalReplyPayload(pending, action, actionName)
	case appapproval.KindFile:
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
		replyPayload = resp
	case appapproval.KindPermissions:
		payload := appapproval.ParseStoredPayload(pending.PayloadJSON)
		scope := "turn"
		if actionName == "approval.permissions.accept_session" {
			scope = "session"
		}
		replyPayload = map[string]any{
			"permissions": payload.Permissions,
			"scope":       scope,
		}
	default:
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "不支持的审批类型"}}, nil
	}
	if strings.TrimSpace(warning) != "" {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: warning}}, nil
	}
	adapter := s.AdapterForPending(pending)
	if err := adapter.ReplyApproval(pending, actionName, replyPayload); err != nil {
		if IsUIWarningError(err) {
			return &callback.CardActionTriggerResponse{
				Toast: &callback.Toast{Type: "warning", Content: err.Error()},
			}, nil
		}
		slog.Error("approval reply failed",
			"backend", adapter.Kind(),
			"request_id", requestID,
			"pending_kind", pending.Kind,
			"action", actionName,
			"user_id", action.UserID,
			"error", err,
		)
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "warning", Content: "审批结果提交失败，请重试"},
		}, nil
	}
	_ = s.FinalizePendingReply(pending)
	card := s.renderResolvedApprovalCard(pending, action, actionName)
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "审批已提交"},
		Card:  rawCard(card),
	}, nil
}

func (s *Service) renderResolvedApprovalCard(pending *state.PendingRequest, action *feishu.CardAction, actionName string) map[string]any {
	decision := approvalview.ApprovalDecisionText(actionName)
	body := strings.TrimSpace(approvalview.ApprovalBodyText(pending))
	lines := []string{"处理结果: " + decision}
	if detail := strings.TrimSpace(approvalview.ApprovalDecisionDetail(pending, action, actionName)); detail != "" {
		lines = append(lines, detail)
	}
	if body != "" {
		lines = append(lines, "", body)
	}
	color := approvalview.ApprovalDecisionColor(actionName)
	title := "审批已处理"
	workspaceID := ""
	if s.Session != nil && pending != nil {
		if sess := s.Session(strings.TrimSpace(pending.SessionKey)); sess != nil {
			workspaceID = strings.TrimSpace(sess.WorkspaceID)
		}
	}
	if s.ContentCardTitle != nil {
		title = s.ContentCardTitle(strings.TrimSpace(pending.SessionKey), workspaceID, title)
	}
	return s.SimpleStatusCard(title, color, strings.Join(lines, "\n"), nil)
}

// ResumeSubmissionAfterRequest resumes a submission if no other open
// pending requests remain for the same turn.
func (s *Service) ResumeSubmissionAfterRequest(pending *state.PendingRequest) {
	if pending == nil {
		return
	}
	if s.HasOpenPendingRequestForTurn(pending.ThreadID, pending.TurnID, pending.ID) {
		return
	}
	_, sub := s.FindSubmissionByTurn(pending.ThreadID, pending.TurnID)
	if sub == nil {
		return
	}
	_ = s.SetSubStatus(sub.ID, state.SubmissionStatusRunning.String())
}

// ---------- Outbound card methods ----------

// SendApprovalCard sends a simple approval card without extra payload.
func (s *Service) SendApprovalCard(kind string, requestID json.RawMessage, threadID, turnID, itemID, body string) {
	s.SendApprovalCardWithPayload(kind, requestID, threadID, turnID, itemID, body, nil)
}

// SendApprovalCardWithPayload sends an approval card with an optional request payload.
func (s *Service) SendApprovalCardWithPayload(kind string, requestID json.RawMessage, threadID, turnID, itemID, body string, requestPayload map[string]any) {
	s.SendApprovalCardPresentation(requestID, appapproval.Presentation{
		Kind:     appapproval.NormalizeKind(kind),
		ThreadID: threadID,
		TurnID:   turnID,
		ItemID:   itemID,
		Body:     body,
		Payload: appapproval.RequestPayload{
			Body:    strings.TrimSpace(body),
			Request: requestPayload,
		},
	})
}

// SendApprovalCardPresentation sends an approval card from a presentation.
func (s *Service) SendApprovalCardPresentation(requestID json.RawMessage, presentation appapproval.Presentation) {
	sessionKey, sub := s.FindSubmissionByTurn(presentation.ThreadID, presentation.TurnID)
	if sub == nil {
		s.ReplyCodexError(requestID, -32602, "no active session for approval")
		return
	}
	requestKey := requestIDKey(requestID)
	kind := presentation.Kind
	if kind == "" {
		kind = appapproval.NormalizeKind(sub.Kind)
	}
	title := "等待审批"
	linkKind := "approval_card"
	if kind == appapproval.KindPermissions {
		title = "权限请求"
		linkKind = "permissions_card"
	}
	buttons := appapproval.Buttons(kind.String(), requestKey, presentation.Payload.Request)
	card := s.RenderApprovalCard(sub, title, "orange", strings.TrimSpace(presentation.Body), buttons)
	err := s.DeliverPendingCard(sub, card, PendingCardDelivery{
		RequestKey:      requestKey,
		RequestIDStored: requestIDStored(requestID),
		Backend:         s.BackendCodex,
		Kind:            kind.String(),
		SessionKey:      sessionKey,
		ThreadID:        presentation.ThreadID,
		TurnID:          presentation.TurnID,
		ItemID:          presentation.ItemID,
		OwnerUserID:     sub.UserID,
		PayloadJSON:     presentation.Payload.MarshalJSONText(),
		WaitingStatus:   state.SubmissionStatusWaitingApproval.String(),
		LinkKind:        linkKind,
	})
	if err == nil {
		return
	}
	s.ReplyCodexError(requestID, -32603, err.Error())
}

// SendPermissionsCard sends a permissions approval card.
func (s *Service) SendPermissionsCard(requestID json.RawMessage, threadID, turnID, itemID, body string, permissions map[string]any) {
	s.SendPermissionsCardWithPayload(requestID, threadID, turnID, itemID, body, permissions, nil)
}

// SendPermissionsCardWithPayload sends a permissions approval card with payload.
func (s *Service) SendPermissionsCardWithPayload(requestID json.RawMessage, threadID, turnID, itemID, body string, permissions map[string]any, requestPayload map[string]any) {
	s.SendApprovalCardPresentation(requestID, appapproval.Presentation{
		Kind:     appapproval.KindPermissions,
		ThreadID: threadID,
		TurnID:   turnID,
		ItemID:   itemID,
		Body:     body,
		Payload: appapproval.RequestPayload{
			Body:        strings.TrimSpace(body),
			Request:     requestPayload,
			Permissions: permissions,
		},
	})
}

// SendUserInputCard sends a simple single-question user input card with option buttons.
func (s *Service) SendUserInputCard(requestID json.RawMessage, payload ToolUserInputPayload) {
	sessionKey, sub := s.FindSubmissionByTurn(payload.ThreadID, payload.TurnID)
	if sub == nil || len(payload.Questions) == 0 {
		s.ReplyCodexError(requestID, -32602, "no active session for request_user_input")
		return
	}
	q := payload.Questions[0]
	buttons := make([]feishu.Button, 0, len(q.Options))
	for _, opt := range q.Options {
		buttons = append(buttons, feishu.Button{
			Text: opt.Label,
			Type: "default",
			Value: map[string]any{
				"action":      "user_input.answer",
				"request_id":  requestIDKey(requestID),
				"question_id": q.ID,
				"answer":      opt.Label,
			},
		})
	}
	card := s.SimpleStatusCard("需要补充输入", "orange", s.PrepareMentionText(pendingforms.RenderToolUserInputQuickBody(q), sub.UserID), buttons)
	requestKey := requestIDKey(requestID)
	err := s.DeliverPendingCard(sub, card, PendingCardDelivery{
		RequestKey:      requestKey,
		RequestIDStored: requestIDStored(requestID),
		Backend:         s.BackendCodex,
		Kind:            "tool_request_user_input",
		SessionKey:      sessionKey,
		ThreadID:        payload.ThreadID,
		TurnID:          payload.TurnID,
		ItemID:          payload.ItemID,
		OwnerUserID:     sub.UserID,
		PayloadJSON:     mustJSON(payload),
		WaitingStatus:   state.SubmissionStatusWaitingUserInput.String(),
		LinkKind:        "user_input_card",
	})
	if err == nil {
		return
	}
	s.ReplyCodexError(requestID, -32603, err.Error())
}
