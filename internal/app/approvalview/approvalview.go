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
	if pending == nil {
		return nil
	}
	payload := appapproval.ParseStoredPayload(pending.PayloadJSON)
	if len(payload.Request) > 0 {
		return payload.Request
	}
	if len(payload.Permissions) > 0 {
		return payload.Permissions
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(pending.PayloadJSON), &raw); err == nil && len(raw) > 0 {
		return raw
	}
	return nil
}

func ApprovalBodyText(pending *state.PendingRequest) string {
	if pending == nil {
		return ""
	}
	payload := appapproval.ParseStoredPayload(pending.PayloadJSON)
	if body := strings.TrimSpace(payload.Body); body != "" {
		return body
	}
	switch appapproval.NormalizeKind(pending.Kind) {
	case appapproval.KindCommand:
		if body := strings.TrimSpace(appapproval.RenderCommandBody(payload.Request)); body != "" {
			return body
		}
	case appapproval.KindFile:
		if body := strings.TrimSpace(appapproval.RenderFileBody(payload.Request)); body != "" {
			return body
		}
	case appapproval.KindPermissions:
		if body := strings.TrimSpace(appapproval.RenderPermissionsApprovalBody(payload.Request)); body != "" {
			return body
		}
		if rendered := strings.TrimSpace(apputil.PrettyJSON(payload.Permissions)); rendered != "" {
			return "权限审批\n" + rendered
		}
	}
	switch appapproval.NormalizeKind(pending.Kind) {
	case appapproval.KindCommand:
		return "命令审批"
	case appapproval.KindFile:
		return "文件变更审批"
	case appapproval.KindPermissions:
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
