package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"feidex/internal/codexrpc"
	"feidex/internal/config"
	"feidex/internal/state"
)

// codexEventRouter groups Codex notifications and server requests.
type codexEventRouter struct {
	app *App
}

func newCodexEventRouter(app *App) *codexEventRouter {
	return &codexEventRouter{app: app}
}

func (r *codexEventRouter) handleNotification(method string, params json.RawMessage) {
	a := r.app
	appState := a.appState()
	slog.Debug("codex notification", "method", method)
	switch method {
	case "item/completed":
		var p struct {
			ThreadID string         `json:"threadId"`
			TurnID   string         `json:"turnId"`
			ItemID   string         `json:"itemId"`
			Item     map[string]any `json:"item"`
		}
		if json.Unmarshal(params, &p) == nil {
			if p.ItemID == "" {
				p.ItemID = strings.TrimSpace(stringValue(p.Item["id"]))
			}
			a.completeTurnItem(context.Background(), p.ThreadID, p.TurnID, p.ItemID, p.Item)
		}
	case "turn/plan/updated":
		var p struct {
			TurnID string `json:"turnId"`
			Plan   []struct {
				Step   string `json:"step"`
				Status string `json:"status"`
			} `json:"plan"`
		}
		if json.Unmarshal(params, &p) == nil {
			plan := make([]string, 0, len(p.Plan))
			for _, item := range p.Plan {
				plan = append(plan, fmt.Sprintf("- [%s] %s", item.Status, item.Step))
			}
			a.updatePendingPlan(p.TurnID, strings.Join(plan, "\n"))
		}
	case "turn/started":
		var p struct {
			ThreadID string `json:"threadId"`
			TurnID   string `json:"turnId"`
			Turn     struct {
				ID     string `json:"id"`
				Status string `json:"status"`
			} `json:"turn"`
		}
		if json.Unmarshal(params, &p) == nil {
			turnID := strings.TrimSpace(firstNonEmpty(p.Turn.ID, p.TurnID))
			if turnID != "" {
				a.onTurnStartedNotification(p.ThreadID, turnID)
			}
		}
	case "turn/completed":
		var p struct {
			ThreadID string `json:"threadId"`
			Turn     struct {
				ID     string `json:"id"`
				Status string `json:"status"`
			} `json:"turn"`
		}
		if json.Unmarshal(params, &p) == nil {
			slog.Debug("turn completed",
				"thread_id", p.ThreadID,
				"turn_id", p.Turn.ID,
				"status", p.Turn.Status,
			)
			a.finishTurn(p.ThreadID, p.Turn.ID, p.Turn.Status)
		}
	case "thread/compacted":
		var p struct {
			ThreadID string `json:"threadId"`
			TurnID   string `json:"turnId"`
		}
		if json.Unmarshal(params, &p) == nil {
			a.completeStandaloneCompactTurn(p.ThreadID, p.TurnID)
		}
	case "thread/tokenUsage/updated":
		var p codexrpc.ThreadTokenUsageUpdatedNotification
		if json.Unmarshal(params, &p) == nil {
			a.onThreadTokenUsageUpdated(p.ThreadID, p.TurnID, p.TokenUsage)
		}
	case "error":
		var p struct {
			ThreadID string `json:"threadId"`
			TurnID   string `json:"turnId"`
			Error    struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(params, &p) == nil {
			slog.Error("codex turn error",
				"thread_id", p.ThreadID,
				"turn_id", p.TurnID,
				"message", p.Error.Message,
			)
			if a.failStandaloneCompactTurn(p.ThreadID, p.TurnID, p.Error.Message) {
				return
			}
			a.recordTurnError(p.ThreadID, p.TurnID, p.Error.Message)
			a.updateSubmissionByTurn(p.ThreadID, p.TurnID, func(sub *state.Submission) {
				sub.Status = "failed"
			})
		}
	case "serverRequest/resolved":
		var p struct {
			ThreadID  string          `json:"threadId"`
			RequestID json.RawMessage `json:"requestId"`
		}
		if json.Unmarshal(params, &p) == nil {
			reqID := requestIDKey(p.RequestID)
			pending := appState.resolvePending(reqID)
			a.resumeSubmissionAfterRequest(pending)
		}
	}
}

func (r *codexEventRouter) handleServerRequest(req codexrpc.RequestEnvelope) {
	a := r.app
	slog.Debug("codex server request", "method", req.Method)
	switch req.Method {
	case "item/commandExecution/requestApproval":
		a.onCommandApproval(req)
	case "item/fileChange/requestApproval":
		a.onFileApproval(req)
	case "item/permissions/requestApproval":
		a.onPermissionsApproval(req)
	case "item/tool/requestUserInput":
		a.onToolUserInput(req)
	case "mcpServer/elicitation/request":
		a.onMcpElicitationRequest(req)
	default:
		_ = a.codex.ReplyError(req.ID, -32601, "unsupported server request")
	}
}

func (r *codexEventRouter) onCommandApproval(req codexrpc.RequestEnvelope) {
	a := r.app
	var raw map[string]any
	if err := json.Unmarshal(req.Params, &raw); err != nil {
		_ = a.codex.ReplyError(req.ID, -32602, "invalid params")
		return
	}
	threadID := strings.TrimSpace(stringValue(raw["threadId"]))
	turnID := strings.TrimSpace(stringValue(raw["turnId"]))
	itemID := strings.TrimSpace(stringValue(raw["itemId"]))
	a.sendApprovalCardWithPayload("command", req.ID, threadID, turnID, itemID, renderCommandApprovalBody(raw), raw)
}

func (r *codexEventRouter) onFileApproval(req codexrpc.RequestEnvelope) {
	a := r.app
	var raw map[string]any
	if err := json.Unmarshal(req.Params, &raw); err != nil {
		_ = a.codex.ReplyError(req.ID, -32602, "invalid params")
		return
	}
	threadID := strings.TrimSpace(stringValue(raw["threadId"]))
	turnID := strings.TrimSpace(stringValue(raw["turnId"]))
	itemID := strings.TrimSpace(stringValue(raw["itemId"]))
	workspaceCwd := ""
	if _, sub := a.findSubmissionByTurn(threadID, turnID); sub != nil {
		if ws := config.FindWorkspace(a.cfg, sub.WorkspaceID); ws != nil {
			workspaceCwd = ws.Cwd
		}
	}
	a.sendApprovalCardWithPayload("file", req.ID, threadID, turnID, itemID, renderFileApprovalBodyWithWorkspace(raw, workspaceCwd), raw)
}

func (r *codexEventRouter) onPermissionsApproval(req codexrpc.RequestEnvelope) {
	a := r.app
	var raw map[string]any
	if err := json.Unmarshal(req.Params, &raw); err != nil {
		_ = a.codex.ReplyError(req.ID, -32602, "invalid params")
		return
	}
	threadID := strings.TrimSpace(stringValue(raw["threadId"]))
	turnID := strings.TrimSpace(stringValue(raw["turnId"]))
	itemID := strings.TrimSpace(stringValue(raw["itemId"]))
	permissions, _ := raw["permissions"].(map[string]any)
	a.sendPermissionsCardWithPayload(req.ID, threadID, turnID, itemID, renderPermissionsApprovalBody(raw), permissions, raw)
}

func (r *codexEventRouter) onToolUserInput(req codexrpc.RequestEnvelope) {
	a := r.app
	var p toolUserInputPayload
	if err := json.Unmarshal(req.Params, &p); err != nil {
		_ = a.codex.ReplyError(req.ID, -32602, "invalid params")
		return
	}
	if len(p.Questions) == 1 && len(p.Questions[0].Options) > 0 && len(p.Questions[0].Options) <= 3 {
		a.sendUserInputCard(req.ID, p)
		return
	}
	a.sendUserInputFormCard(req.ID, p)
}

func (r *codexEventRouter) onMcpElicitationRequest(req codexrpc.RequestEnvelope) {
	a := r.app
	var header struct {
		ServerName string `json:"serverName"`
		ThreadID   string `json:"threadId"`
		TurnID     string `json:"turnId"`
		Mode       string `json:"mode"`
		Message    string `json:"message"`
		URL        string `json:"url"`
	}
	if err := json.Unmarshal(req.Params, &header); err != nil {
		_ = a.codex.ReplyError(req.ID, -32602, "invalid params")
		return
	}
	switch header.Mode {
	case "url":
		var payload elicitationURLPayload
		if err := json.Unmarshal(req.Params, &payload); err != nil {
			_ = a.codex.ReplyError(req.ID, -32602, "invalid params")
			return
		}
		a.sendElicitationURLCard(req.ID, payload)
	case "form":
		var payload elicitationFormPayload
		if err := json.Unmarshal(req.Params, &payload); err != nil {
			_ = a.codex.ReplyError(req.ID, -32602, "invalid params")
			return
		}
		a.sendElicitationFormCard(req.ID, payload)
	default:
		_ = a.codex.ReplyError(req.ID, -32601, "unsupported elicitation mode")
	}
}
