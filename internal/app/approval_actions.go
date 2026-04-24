package app

import (
	"encoding/json"
	appapproval "feidex/internal/app/approval"
	"log/slog"
	"strings"

	"feidex/internal/feishu"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

func (a *App) completeApprovalAction(action *feishu.CardAction, actionName string) (*callback.CardActionTriggerResponse, error) {
	appState := a.appState()
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
	adapter := a.serverRequestBackendAdapter(pending)
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
	card := a.renderResolvedApprovalCard(pending, action, actionName)
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "审批已提交"},
		Card: &callback.Card{
			Type: "raw",
			Data: card,
		},
	}, nil
}

func commandApprovalReplyPayload(pending *state.PendingRequest, action *feishu.CardAction, actionName string) (any, string) {
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
	default:
		return nil, "不支持的审批动作"
	}
	return resp, ""
}

func approvalRequestPayload(pending *state.PendingRequest) map[string]any {
	if pending == nil || strings.TrimSpace(pending.PayloadJSON) == "" {
		return nil
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(pending.PayloadJSON), &payload); err != nil {
		return nil
	}
	if request, ok := payload["request"].(map[string]any); ok {
		return request
	}
	return payload
}

func (a *App) renderResolvedApprovalCard(pending *state.PendingRequest, action *feishu.CardAction, actionName string) map[string]any {
	decision := a.approvalDecisionText(actionName)
	body := strings.TrimSpace(a.approvalBodyText(pending))
	lines := []string{"处理结果: " + decision}
	if detail := strings.TrimSpace(a.approvalDecisionDetail(pending, action, actionName)); detail != "" {
		lines = append(lines, detail)
	}
	if body != "" {
		lines = append(lines, "", body)
	}
	color := approvalDecisionColor(actionName)
	return a.feishu.SimpleStatusCard("审批已处理", color, strings.Join(lines, "\n"), nil)
}

func (a *App) approvalBodyText(pending *state.PendingRequest) string {
	if pending == nil {
		return ""
	}
	var payload map[string]any
	if strings.TrimSpace(pending.PayloadJSON) != "" {
		if err := json.Unmarshal([]byte(pending.PayloadJSON), &payload); err == nil {
			if body := strings.TrimSpace(stringValue(payload["body"])); body != "" {
				return body
			}
			if pending.Kind == "command" {
				if request, ok := payload["request"].(map[string]any); ok {
					if body := strings.TrimSpace(appapproval.RenderCommandBody(request)); body != "" {
						return body
					}
				}
			}
			if pending.Kind == "file" {
				if request, ok := payload["request"].(map[string]any); ok {
					if body := strings.TrimSpace(appapproval.RenderFileBody(request)); body != "" {
						return body
					}
				}
			}
			if pending.Kind == "permissions" {
				if request, ok := payload["request"].(map[string]any); ok {
					if body := strings.TrimSpace(renderPermissionsApprovalBody(request)); body != "" {
						return body
					}
				}
				if permissions, ok := payload["permissions"]; ok {
					if rendered := strings.TrimSpace(prettyJSON(permissions)); rendered != "" {
						return "权限审批\n" + rendered
					}
				}
			}
		}
	}
	switch pending.Kind {
	case "command":
		return "命令审批"
	case "file":
		return "文件变更审批"
	case "permissions":
		return "权限审批"
	default:
		return ""
	}
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
		return "已拒绝并中断任务"
	default:
		return "已拒绝"
	}
}

func (a *App) approvalDecisionDetail(pending *state.PendingRequest, action *feishu.CardAction, actionName string) string {
	switch actionName {
	case "approval.command.cancel", "approval.file.cancel":
		return "该 turn 会立即中断。"
	}
	return ""
}

func approvalDecisionColor(action string) string {
	switch action {
	case "approval.command.cancel", "approval.file.cancel":
		return "red"
	case "approval.command.decline", "approval.file.decline":
		return "grey"
	default:
		return "green"
	}
}

func (a *App) resumeSubmissionAfterRequest(pending *state.PendingRequest) {
	appState := a.appState()
	if pending == nil {
		return
	}
	if newRuntimeStateService(a).hasOpenPendingRequestForTurn(pending.ThreadID, pending.TurnID, pending.ID) {
		return
	}
	_, sub := a.findSubmissionByTurn(pending.ThreadID, pending.TurnID)
	if sub == nil {
		return
	}
	_ = appState.setSubmissionStatus(sub.ID, "running")
}
