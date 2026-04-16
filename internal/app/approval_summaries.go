package app

import (
	"strings"

	"feidex/internal/feishu"
	"feidex/internal/state"
)

func renderCommandApprovalBody(params map[string]any) string {
	lines := []string{"命令审批"}
	if command := strings.TrimSpace(firstNonEmpty(
		stringValue(params["command"]),
		stringValue(params["commandLine"]),
		stringValue(params["command_line"]),
	)); command != "" {
		lines = append(lines, markdownCodeBlock(command))
	}
	if cwd := strings.TrimSpace(firstNonEmpty(
		stringValue(params["cwd"]),
		stringValue(params["workingDirectory"]),
		stringValue(params["working_directory"]),
	)); cwd != "" {
		lines = append(lines, "工作目录: `"+strings.ReplaceAll(cwd, "`", "'")+"`")
	}
	if target := strings.TrimSpace(commandApprovalNetworkTarget(params)); target != "" {
		lines = append(lines, "网络访问: `"+strings.ReplaceAll(target, "`", "'")+"`")
	}
	if reason := strings.TrimSpace(stringValue(params["reason"])); reason != "" {
		if len(lines) > 1 {
			lines = append(lines, "")
		}
		lines = append(lines, "说明:", reason)
	}
	if len(lines) == 1 {
		if summary := strings.TrimSpace(truncatedApprovalRequestJSON(params)); summary != "" {
			lines = append(lines, markdownCodeBlock(summary))
		}
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func approvalButtons(kind, requestKey string, requestPayload ...map[string]any) []feishu.Button {
	switch strings.TrimSpace(kind) {
	case "command":
		return []feishu.Button{
			{Text: "允许一次", Type: "primary", Value: map[string]any{"action": "approval.command.accept", "request_id": requestKey}},
			{Text: "本会话允许", Type: "default", Value: map[string]any{"action": "approval.command.accept_session", "request_id": requestKey}},
			feishu.Button{Text: "拒绝", Type: "danger", Value: map[string]any{"action": "approval.command.decline", "request_id": requestKey}},
			feishu.Button{Text: "拒绝并中断", Type: "danger", Value: map[string]any{"action": "approval.command.cancel", "request_id": requestKey}},
		}
	case "file":
		return []feishu.Button{
			{Text: "允许一次", Type: "primary", Value: map[string]any{"action": "approval.file.accept", "request_id": requestKey}},
			{Text: "本会话允许", Type: "default", Value: map[string]any{"action": "approval.file.accept_session", "request_id": requestKey}},
			{Text: "拒绝", Type: "danger", Value: map[string]any{"action": "approval.file.decline", "request_id": requestKey}},
			{Text: "拒绝并中断", Type: "danger", Value: map[string]any{"action": "approval.file.cancel", "request_id": requestKey}},
		}
	default:
		return []feishu.Button{
			{Text: "允许一次", Type: "primary", Value: map[string]any{"action": "approval." + kind + ".accept", "request_id": requestKey}},
			{Text: "本会话允许", Type: "default", Value: map[string]any{"action": "approval." + kind + ".accept_session", "request_id": requestKey}},
			{Text: "拒绝", Type: "danger", Value: map[string]any{"action": "approval." + kind + ".decline", "request_id": requestKey}},
		}
	}
}

func (a *App) renderApprovalCard(_ string, _ *state.Submission, title, color, body string, buttons []feishu.Button) map[string]any {
	return a.feishu.SimpleStatusCard(title, color, strings.TrimSpace(body), buttons)
}

func commandApprovalNetworkTarget(params map[string]any) string {
	ctx, _ := params["networkApprovalContext"].(map[string]any)
	if len(ctx) == 0 {
		return ""
	}
	host := strings.TrimSpace(stringValue(ctx["host"]))
	protocol := strings.TrimSpace(stringValue(ctx["protocol"]))
	switch {
	case host == "":
		return ""
	case protocol == "":
		return host
	default:
		return protocol + "://" + host
	}
}

func firstNonEmptyValue(values ...any) any {
	for _, value := range values {
		switch x := value.(type) {
		case nil:
			continue
		case string:
			if strings.TrimSpace(x) != "" {
				return value
			}
		case []any:
			if len(x) > 0 {
				return value
			}
		case []string:
			if len(x) > 0 {
				return value
			}
		case map[string]any:
			if len(x) > 0 {
				return value
			}
		default:
			return value
		}
	}
	return nil
}
