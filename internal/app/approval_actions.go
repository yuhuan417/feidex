package app

import (
	"encoding/json"
	appapprovalview "feidex/internal/app/approvalview"
	"log/slog"
	"strings"

	"feidex/internal/feishu"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

func completeApprovalAction(a *App, action *feishu.CardAction, actionName string) (*callback.CardActionTriggerResponse, error) {
	appState := appState(a)
	requestID, _ := action.ActionValue["request_id"].(string)
	pending := appState.pending(requestID)
	if pending == nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "审批已过期"}}, nil
	}
	if pending.OwnerUserID != "" && pending.OwnerUserID != action.UserID {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "你没有权限处理这个审批"}}, nil
	}
	var replyPayload any
	var warning string
	switch pending.Kind {
	case "command":
		replyPayload, warning = commandApprovalReplyPayload(pending, action, actionName)
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
		replyPayload = resp
	case "permissions":
		var payload struct {
			Permissions map[string]any `json:"permissions"`
		}
		_ = json.Unmarshal([]byte(pending.PayloadJSON), &payload)
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
	adapter := serverRequestAdapterForPending(a, pending)
	if err := adapter.replyApproval(pending, actionName, replyPayload); err != nil {
		if isUIWarningError(err) {
			return &callback.CardActionTriggerResponse{
				Toast: &callback.Toast{Type: "warning", Content: err.Error()},
			}, nil
		}
		slog.Error("approval reply failed",
			"backend", adapter.kind(),
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
	_ = newRuntimeStateService(a).finalizePendingReply(pending)
	card := renderResolvedApprovalCard(a, pending, action, actionName)
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "审批已提交"},
		Card: &callback.Card{
			Type: "raw",
			Data: card,
		},
	}, nil
}

var commandApprovalReplyPayload = appapprovalview.CommandApprovalReplyPayload

var approvalRequestPayload = appapprovalview.ApprovalRequestPayload

func renderResolvedApprovalCard(a *App, pending *state.PendingRequest, action *feishu.CardAction, actionName string) map[string]any {
	decision := appapprovalview.ApprovalDecisionText(actionName)
	body := strings.TrimSpace(appapprovalview.ApprovalBodyText(pending))
	lines := []string{"处理结果: " + decision}
	if detail := strings.TrimSpace(appapprovalview.ApprovalDecisionDetail(pending, action, actionName)); detail != "" {
		lines = append(lines, detail)
	}
	if body != "" {
		lines = append(lines, "", body)
	}
	color := appapprovalview.ApprovalDecisionColor(actionName)
	return a.feishu.SimpleStatusCard("审批已处理", color, strings.Join(lines, "\n"), nil)
}

var approvalBodyText = appapprovalview.ApprovalBodyText

var approvalDecisionText = appapprovalview.ApprovalDecisionText

var approvalDecisionDetail = appapprovalview.ApprovalDecisionDetail

var approvalDecisionColor = appapprovalview.ApprovalDecisionColor

func resumeSubmissionAfterRequest(a *App, pending *state.PendingRequest) {
	appState := appState(a)
	if pending == nil {
		return
	}
	if newRuntimeStateService(a).hasOpenPendingRequestForTurn(pending.ThreadID, pending.TurnID, pending.ID) {
		return
	}
	_, sub := findSubmissionByTurn(a, pending.ThreadID, pending.TurnID)
	if sub == nil {
		return
	}
	_ = appState.setSubmissionStatus(sub.ID, "running")
}
