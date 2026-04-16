package app

import (
	"encoding/json"
	"log/slog"
	"sort"
	"strings"

	"feidex/internal/feishu"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

type toolUserInputOption struct {
	Label       string `json:"label"`
	Description string `json:"description"`
}

type toolUserInputQuestion struct {
	Header   string                `json:"header"`
	ID       string                `json:"id"`
	Question string                `json:"question"`
	IsOther  bool                  `json:"isOther"`
	IsSecret bool                  `json:"isSecret"`
	Options  []toolUserInputOption `json:"options"`
}

type toolUserInputPayload struct {
	ThreadID  string                  `json:"threadId"`
	TurnID    string                  `json:"turnId"`
	ItemID    string                  `json:"itemId"`
	Questions []toolUserInputQuestion `json:"questions"`
}

type elicitationFormPayload struct {
	ServerName string         `json:"serverName"`
	ThreadID   string         `json:"threadId"`
	TurnID     string         `json:"turnId,omitempty"`
	Message    string         `json:"message"`
	Schema     map[string]any `json:"requestedSchema"`
}

type elicitationURLPayload struct {
	ServerName    string `json:"serverName"`
	ThreadID      string `json:"threadId"`
	TurnID        string `json:"turnId,omitempty"`
	ElicitationID string `json:"elicitationId"`
	Message       string `json:"message"`
	URL           string `json:"url"`
}

func (a *App) pendingTextRequest(sessionKey, userID string) *state.PendingRequest {
	pending := a.appState().pendingRequests()
	sort.Slice(pending, func(i, j int) bool { return pending[i].CreatedAt > pending[j].CreatedAt })
	for _, req := range pending {
		if req == nil || req.Status != "pending" || req.SessionKey != sessionKey {
			continue
		}
		if req.OwnerUserID != "" && req.OwnerUserID != userID {
			continue
		}
		switch req.Kind {
		case "tool_request_user_input_form", "mcp_elicitation_form", "workspace_new":
			return req
		}
	}
	return nil
}

func (a *App) shouldRedactInboundText(sessionKey, userID string) bool {
	req := a.pendingTextRequest(sessionKey, userID)
	if req == nil {
		return false
	}
	switch req.Kind {
	case "mcp_elicitation_form":
		return true
	case "tool_request_user_input_form":
		var payload toolUserInputPayload
		if err := json.Unmarshal([]byte(req.PayloadJSON), &payload); err != nil {
			return true
		}
		for _, q := range payload.Questions {
			if q.IsSecret {
				return true
			}
		}
	}
	return false
}

func (a *App) handlePendingTextResponse(msg *feishu.InboundMessage, pending *state.PendingRequest) error {
	if msg == nil || pending == nil {
		return nil
	}
	switch pending.Kind {
	case "tool_request_user_input_form":
		return a.completeToolUserInputText(msg, pending)
	case "mcp_elicitation_form":
		return a.completeElicitationFormText(msg, pending)
	case "workspace_new":
		return a.completeWorkspaceNewText(msg, pending)
	default:
		return nil
	}
}

func (a *App) completePendingFormCancel(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	appState := a.appState()
	requestID, _ := action.ActionValue["request_id"].(string)
	pending := appState.pending(requestID)
	if pending == nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "请求已过期"}}, nil
	}
	if pending.OwnerUserID != "" && pending.OwnerUserID != action.UserID {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "你没有权限处理这个请求"}}, nil
	}
	var replyErr error
	switch pending.Kind {
	case "tool_request_user_input_form":
		replyErr = a.codex.ReplyError(pendingRequestIDRaw(pending), -32800, "cancelled by user")
	case "mcp_elicitation_form":
		replyErr = a.codex.Reply(pendingRequestIDRaw(pending), map[string]any{"action": "cancel"})
	}
	if replyErr != nil {
		slog.Error("pending form cancel reply to codex failed",
			"request_id", requestID,
			"pending_kind", pending.Kind,
			"user_id", action.UserID,
			"error", replyErr,
		)
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "warning", Content: "取消提交失败，请重试"},
		}, nil
	}
	if isServerResolvedPendingKind(pending.Kind) {
		_ = a.markPendingRequestReplied(requestID)
	} else {
		_ = appState.updatePending(requestID, func(req *state.PendingRequest) { req.Status = "resolved" })
		a.resumeSubmissionAfterRequest(pending)
	}
	if pending.Kind == "workspace_new" {
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "success", Content: "已返回工作区"},
			Card:  rawCard(a.renderWorkspaceMenuCard(pending.SessionKey)),
		}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已取消"},
		Card:  rawCard(a.feishu.SimpleStatusCard("已取消", "grey", "该请求已取消。", nil)),
	}, nil
}

func parseStructuredLines(text string) map[string]string {
	lines := strings.Split(text, "\n")
	out := map[string]string{}
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		out[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
	}
	return out
}
