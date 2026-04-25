// Package approvalview provides pure approval action formatting functions
// extracted from the app package.
package approvalview

import (
	"encoding/json"
	"strings"

	appapproval "feidex/internal/app/approval"
	"feidex/internal/app/apputil"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

func CommandApprovalReplyPayload(pending *state.PendingRequest, action *feishu.CardAction, actionName string) (any, string) {
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

func ApprovalRequestPayload(pending *state.PendingRequest) map[string]any {
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

func ApprovalBodyText(pending *state.PendingRequest) string {
	if pending == nil {
		return ""
	}
	var payload map[string]any
	if strings.TrimSpace(pending.PayloadJSON) != "" {
		if err := json.Unmarshal([]byte(pending.PayloadJSON), &payload); err == nil {
			if body := strings.TrimSpace(apputil.StringValue(payload["body"])); body != "" {
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
					if body := strings.TrimSpace(appapproval.RenderPermissionsApprovalBody(request)); body != "" {
						return body
					}
				}
				if permissions, ok := payload["permissions"]; ok {
					if rendered := strings.TrimSpace(apputil.PrettyJSON(permissions)); rendered != "" {
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

func ApprovalDecisionText(action string) string {
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

func ApprovalDecisionDetail(pending *state.PendingRequest, action *feishu.CardAction, actionName string) string {
	switch actionName {
	case "approval.command.cancel", "approval.file.cancel":
		return "该 turn 会立即中断。"
	}
	return ""
}

func ApprovalDecisionColor(action string) string {
	switch action {
	case "approval.command.cancel", "approval.file.cancel":
		return "red"
	case "approval.command.decline", "approval.file.decline":
		return "grey"
	default:
		return "green"
	}
}
