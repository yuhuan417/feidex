package app

import (
	"encoding/json"
	"log/slog"
	"sort"
	"strings"

	appreview "feidex/internal/app/review"
	"feidex/internal/feishu"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

type toolUserInputOption struct {
	Label       string `json:"label"`
	Description string `json:"description"`
}

type toolUserInputQuestion struct {
	Header      string                `json:"header"`
	ID          string                `json:"id"`
	Question    string                `json:"question"`
	IsOther     bool                  `json:"isOther"`
	IsSecret    bool                  `json:"isSecret"`
	MultiSelect bool                  `json:"multiSelect,omitempty"`
	Options     []toolUserInputOption `json:"options"`
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
	pending := appState(a).pendingRequests()
	sort.Slice(pending, func(i, j int) bool { return pending[i].CreatedAt > pending[j].CreatedAt })
	for _, req := range pending {
		if req == nil || req.Status != "pending" || req.SessionKey != sessionKey {
			continue
		}
		if req.OwnerUserID != "" && req.OwnerUserID != userID {
			continue
		}
		switch req.Kind {
		case "tool_request_user_input_form", "mcp_elicitation_form", "workspace_new", claudePlanModePendingKind:
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

func (s pendingInputService) handlePendingTextResponse(msg *feishu.InboundMessage, pending *state.PendingRequest) error {
	if msg == nil || pending == nil {
		return nil
	}
	switch pending.Kind {
	case "tool_request_user_input_form":
		return s.completeToolUserInputText(msg, pending)
	case "mcp_elicitation_form":
		return s.completeElicitationFormText(msg, pending)
	case "workspace_new":
		return s.completeWorkspaceNewText(msg, pending)
	case claudePlanModePendingKind:
		return s.completeClaudePlanModeText(msg, pending)
	default:
		return nil
	}
}

func (a *App) completePendingFormCancel(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	appState := appState(a)
	requestID, _ := action.ActionValue["request_id"].(string)
	pending := appState.pending(requestID)
	if pending == nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "请求已过期"}}, nil
	}
	if pending.OwnerUserID != "" && pending.OwnerUserID != action.UserID {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "你没有权限处理这个请求"}}, nil
	}
	adapter := serverRequestAdapterForPending(a, pending)
	replyErr := adapter.cancelPending(pending)
	if replyErr != nil {
		slog.Error("pending form cancel reply failed",
			"backend", adapter.kind(),
			"request_id", requestID,
			"pending_kind", pending.Kind,
			"user_id", action.UserID,
			"error", replyErr,
		)
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "warning", Content: "取消提交失败，请重试"},
		}, nil
	}
	_ = newRuntimeStateService(a).finalizePendingReply(pending)
	if pending.Kind == "workspace_new" || pending.Kind == "workspace_clone" {
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "success", Content: "已返回工作区"},
			Card:  rawCard(newWorkspaceConfigService(a).renderWorkspaceMenuCard(pending.SessionKey)),
		}, nil
	}
	if body := cancelledPendingBody(pending); body != "" {
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "success", Content: "已取消"},
			Card:  rawCard(a.feishu.SimpleStatusCard(cancelledPendingTitle(pending), "grey", body, nil)),
		}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已取消"},
		Card:  rawCard(a.feishu.SimpleStatusCard("已取消", "grey", "该请求已取消。", nil)),
	}, nil
}

func cancelledPendingTitle(pending *state.PendingRequest) string {
	if pending == nil {
		return "已取消"
	}
	switch pending.Kind {
	case claudePlanModePendingKind:
		return "计划确认已取消"
	case "tool_request_user_input_form":
		return "输入请求已取消"
	case "mcp_elicitation_form":
		return "表单请求已取消"
	case pendingKindReview:
		return "Review 已取消"
	default:
		return "已取消"
	}
}

func cancelledPendingBody(pending *state.PendingRequest) string {
	if pending == nil {
		return ""
	}
	switch pending.Kind {
	case claudePlanModePendingKind:
		return claudePlanCancelledBody(pending)
	case "tool_request_user_input_form":
		var payload toolUserInputPayload
		if err := json.Unmarshal([]byte(pending.PayloadJSON), &payload); err != nil {
			return ""
		}
		return strings.TrimSpace(strings.Join([]string{
			"已取消本次补充输入。",
			"",
			"原请求：",
			renderToolUserInputBody(payload),
		}, "\n"))
	case "mcp_elicitation_form":
		var payload elicitationFormPayload
		if err := json.Unmarshal([]byte(pending.PayloadJSON), &payload); err != nil {
			return ""
		}
		return strings.TrimSpace(strings.Join([]string{
			"已取消本次表单请求。",
			"",
			"原请求：",
			renderElicitationFormBody(payload),
		}, "\n"))
	case pendingKindReview:
		payload := reviewPendingPayloadFromPending(pending)
		lines := []string{"已取消本次 review 请求。", "", "原请求："}
		switch payload.Mode {
		case reviewFormModeBase:
			lines = append(lines, "模式: base branch")
			if branch := strings.TrimSpace(payload.Branch); branch != "" {
				lines = append(lines, "当前选择: `"+branch+"`")
			}
		case reviewFormModeCommit:
			lines = append(lines, "模式: commit")
			if sha := strings.TrimSpace(payload.CommitSHA); sha != "" {
				lines = append(lines, "当前选择: `"+appreview.ShortCommitSHA(sha)+"`")
			}
			if title := strings.TrimSpace(payload.CommitTitle); title != "" {
				lines = append(lines, title)
			}
		case reviewFormModeCustom:
			lines = append(lines, "模式: custom")
			if instructions := strings.TrimSpace(payload.Instructions); instructions != "" {
				lines = append(lines, "Instructions:", instructions)
			}
		default:
			return ""
		}
		return strings.TrimSpace(strings.Join(lines, "\n"))
	default:
		return ""
	}
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
