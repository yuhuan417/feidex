package serverrequest

import (
	"encoding/json"
	"log/slog"
	"strings"

	"feidex/internal/app/pendingforms"
	"feidex/internal/feishu"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

// CompletePendingFormCancel handles the pending_form.cancel button click
// for serverrequest-owned kinds (tool_request_user_input_form, mcp_elicitation_form).
func (s *Service) CompletePendingFormCancel(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	requestID, _ := action.ActionValue["request_id"].(string)
	pending := s.Pending(requestID)
	if pending == nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "请求已过期"}}, nil
	}
	if pending.OwnerUserID != "" && pending.OwnerUserID != action.UserID {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "你没有权限处理这个请求"}}, nil
	}
	adapter := s.AdapterForPending(pending)
	replyErr := adapter.CancelPending(pending)
	if replyErr != nil {
		slog.Error("pending form cancel reply failed",
			"backend", adapter.Kind(),
			"request_id", requestID,
			"pending_kind", pending.Kind,
			"user_id", action.UserID,
			"error", replyErr,
		)
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "warning", Content: "取消提交失败，请重试"},
		}, nil
	}
	_ = s.FinalizePendingReply(pending)
	if body := s.cancelledPendingBody(pending); body != "" {
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "success", Content: "已取消"},
			Card:  rawCard(s.SimpleStatusCard(cancelledPendingTitle(pending), "grey", body, nil)),
		}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已取消"},
		Card:  rawCard(s.SimpleStatusCard("已取消", "grey", "该请求已取消。", nil)),
	}, nil
}

func cancelledPendingTitle(pending *state.PendingRequest) string {
	if pending == nil {
		return "已取消"
	}
	switch pending.Kind {
	case "tool_request_user_input_form":
		return "输入请求已取消"
	case "mcp_elicitation_form":
		return "表单请求已取消"
	default:
		return "已取消"
	}
}

func (s *Service) cancelledPendingBody(pending *state.PendingRequest) string {
	if pending == nil {
		return ""
	}
	switch pending.Kind {
	case "tool_request_user_input_form":
		var payload ToolUserInputPayload
		if err := json.Unmarshal([]byte(pending.PayloadJSON), &payload); err != nil {
			return ""
		}
		return strings.TrimSpace(strings.Join([]string{
			"已取消本次补充输入。",
			"",
			"原请求：",
			pendingforms.RenderToolUserInputBody(payload),
		}, "\n"))
	case "mcp_elicitation_form":
		var payload ElicitationFormPayload
		if err := json.Unmarshal([]byte(pending.PayloadJSON), &payload); err != nil {
			return ""
		}
		return strings.TrimSpace(strings.Join([]string{
			"已取消本次表单请求。",
			"",
			"原请求：",
			pendingforms.RenderElicitationFormBody(payload),
		}, "\n"))
	default:
		return ""
	}
}
