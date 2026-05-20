package backend

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	appapproval "feidex/internal/app/approval"
	apppendingforms "feidex/internal/app/pendingforms"
	"feidex/internal/app/turnitem"
	"feidex/internal/codexrpc"
	"feidex/internal/state"
)

// CodexEventRouter dispatches Codex notifications and server requests.
// All host-app dependencies are injected as callback function fields.
type CodexEventRouter struct {
	// ---- notification callbacks ----

	// NoteTurnItemStarted records that a turn item started.
	NoteTurnItemStarted func(threadID, turnID string, item turnitem.ProtocolItem)

	// NoteStandaloneCompactItemStarted records that a standalone compact
	// item started.
	NoteStandaloneCompactItemStarted func(threadID, turnID string, item turnitem.ProtocolItem)

	// CompleteTurnItem completes a turn item.
	CompleteTurnItem func(ctx context.Context, threadID, turnID, itemID string, item turnitem.ProtocolItem)

	// UpdateInFlightTurnItem projects a started/progress item snapshot without
	// closing its lifecycle.
	UpdateInFlightTurnItem func(ctx context.Context, threadID, turnID, itemID string, item turnitem.ProtocolItem)

	// UpdatePendingPlan updates the pending plan for a turn.
	UpdatePendingPlan func(turnID, plan string)

	// OnTurnStarted handles a turn/started notification.
	OnTurnStarted func(threadID, turnID string)

	// OnTurnCompleted handles a turn/completed notification.
	OnTurnCompleted func(threadID, turnID, status string)

	// OnThreadTokenUsageUpdated handles token usage updates.
	OnThreadTokenUsageUpdated func(threadID, turnID string, usage codexrpc.ThreadTokenUsage)

	// FailStandaloneCompactTurn fails a standalone compact turn.
	// Returns true if the error was handled.
	FailStandaloneCompactTurn func(threadID, turnID, message string) bool

	// RecordTurnError records an error for a turn.
	RecordTurnError func(threadID, turnID, message string)

	// UpdateSubmissionByTurn updates a submission identified by thread/turn.
	UpdateSubmissionByTurn func(threadID, turnID string, mutate func(*state.Submission))

	// ResolveServerPendingRequest resolves a pending server request by ID.
	ResolveServerPendingRequest func(requestID string) *state.PendingRequest

	// ResumeSubmissionAfterRequest resumes a submission after a request.
	ResumeSubmissionAfterRequest func(pending *state.PendingRequest)

	// ---- server request callbacks ----

	// SendApprovalCardWithPayload sends an approval card.
	SendApprovalCard func(requestID json.RawMessage, presentation appapproval.Presentation)

	// SendUserInputCard sends a simple user input card.
	SendUserInputCard func(requestID json.RawMessage, payload apppendingforms.ToolUserInputPayload)

	// SendUserInputFormCard sends a user input form card.
	SendUserInputFormCard func(requestID json.RawMessage, payload apppendingforms.ToolUserInputPayload)

	// SendElicitationURLCard sends an elicitation URL card.
	SendElicitationURLCard func(requestID json.RawMessage, payload apppendingforms.ElicitationURLPayload)

	// SendElicitationFormCard sends an elicitation form card.
	SendElicitationFormCard func(requestID json.RawMessage, payload apppendingforms.ElicitationFormPayload)

	// MergeRequestPayloadWithTurnItem merges a request payload with turn item data.
	MergeApprovalPresentationWithTurnItem func(presentation appapproval.Presentation) appapproval.Presentation

	// ReplyCodexError sends an error reply for a Codex request.
	ReplyCodexError func(requestID json.RawMessage, code int, message string)

	// ---- workspace / submission lookup ----

	// FindSubmissionByTurn returns a submission by thread/turn ID.
	FindSubmissionByTurn func(threadID, turnID string) (string, *state.Submission)

	// FindWorkspaceCwdForSubmission returns the workspace Cwd for a
	// submission's workspace ID.
	FindWorkspaceCwdForSubmission func(sub *state.Submission) string
}

// NewCodexEventRouter creates a new router.
func NewCodexEventRouter() *CodexEventRouter {
	return &CodexEventRouter{}
}

// HandleNotification dispatches a Codex notification by method.
func (r *CodexEventRouter) HandleNotification(method string, params json.RawMessage) {
	slog.Debug("codex notification", "method", method)
	switch method {
	case "item/started":
		r.handleItemStarted(params)
	case "item/completed":
		r.handleItemCompleted(params)
	case "item/mcpToolCall/progress":
		r.handleMCPToolCallProgress(params)
	case "turn/plan/updated":
		r.handleTurnPlanUpdated(params)
	case "turn/started":
		r.handleTurnStarted(params)
	case "turn/completed":
		r.handleTurnCompleted(params)
	case "thread/tokenUsage/updated":
		r.handleThreadTokenUsageUpdated(params)
	case "error":
		r.handleError(params)
	case "serverRequest/resolved":
		r.handleServerRequestResolved(params)
	}
}

// HandleServerRequest dispatches a Codex server request by method.
func (r *CodexEventRouter) HandleServerRequest(req codexrpc.RequestEnvelope) {
	slog.Debug("codex server request", "method", req.Method)
	switch req.Method {
	case "item/commandExecution/requestApproval":
		r.OnCommandApproval(req)
	case "item/fileChange/requestApproval":
		r.OnFileApproval(req)
	case "item/permissions/requestApproval":
		r.OnPermissionsApproval(req)
	case "item/tool/requestUserInput":
		r.OnToolUserInput(req)
	case "mcpServer/elicitation/request":
		r.OnMcpElicitationRequest(req)
	default:
		if r.ReplyCodexError != nil {
			r.ReplyCodexError(req.ID, -32601, "unsupported server request")
		}
	}
}

// ---- Notification handlers ----

func (r *CodexEventRouter) handleItemStarted(params json.RawMessage) {
	var p struct {
		ThreadID string         `json:"threadId"`
		TurnID   string         `json:"turnId"`
		Item     map[string]any `json:"item"`
	}
	if json.Unmarshal(params, &p) != nil {
		return
	}
	item := turnitem.NewProtocolItem(p.Item)
	if r.NoteTurnItemStarted != nil {
		r.NoteTurnItemStarted(p.ThreadID, p.TurnID, item)
	}
	if r.NoteStandaloneCompactItemStarted != nil {
		r.NoteStandaloneCompactItemStarted(p.ThreadID, p.TurnID, item)
	}
}

func (r *CodexEventRouter) handleItemCompleted(params json.RawMessage) {
	var p struct {
		ThreadID string         `json:"threadId"`
		TurnID   string         `json:"turnId"`
		ItemID   string         `json:"itemId"`
		Item     map[string]any `json:"item"`
	}
	if json.Unmarshal(params, &p) != nil {
		return
	}
	if p.ItemID == "" {
		p.ItemID = strings.TrimSpace(stringValue(p.Item["id"]))
	}
	if r.CompleteTurnItem != nil {
		r.CompleteTurnItem(context.Background(), p.ThreadID, p.TurnID, p.ItemID, turnitem.NewProtocolItemWithID(p.ItemID, p.Item))
	}
}

func (r *CodexEventRouter) handleMCPToolCallProgress(params json.RawMessage) {
	var p struct {
		ThreadID string `json:"threadId"`
		TurnID   string `json:"turnId"`
		ItemID   string `json:"itemId"`
		Message  string `json:"message"`
	}
	if json.Unmarshal(params, &p) != nil {
		return
	}
	if strings.TrimSpace(p.ItemID) == "" || strings.TrimSpace(p.Message) == "" || r.UpdateInFlightTurnItem == nil {
		return
	}
	item := turnitem.NewProtocolItemWithID(p.ItemID, map[string]any{
		"id":      p.ItemID,
		"type":    "mcp_tool_call",
		"status":  "in_progress",
		"message": strings.TrimSpace(p.Message),
	})
	r.UpdateInFlightTurnItem(context.Background(), p.ThreadID, p.TurnID, p.ItemID, item)
}

func (r *CodexEventRouter) handleTurnPlanUpdated(params json.RawMessage) {
	var p struct {
		TurnID string `json:"turnId"`
		Plan   []struct {
			Step   string `json:"step"`
			Status string `json:"status"`
		} `json:"plan"`
	}
	if json.Unmarshal(params, &p) != nil {
		return
	}
	plan := make([]string, 0, len(p.Plan))
	for _, item := range p.Plan {
		plan = append(plan, fmt.Sprintf("- [%s] %s", item.Status, item.Step))
	}
	if r.UpdatePendingPlan != nil {
		r.UpdatePendingPlan(p.TurnID, strings.Join(plan, "\n"))
	}
}

func (r *CodexEventRouter) handleTurnStarted(params json.RawMessage) {
	var p struct {
		ThreadID string `json:"threadId"`
		TurnID   string `json:"turnId"`
		Turn     struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"turn"`
	}
	if json.Unmarshal(params, &p) != nil {
		return
	}
	turnID := strings.TrimSpace(firstNonEmpty(p.Turn.ID, p.TurnID))
	if turnID != "" && r.OnTurnStarted != nil {
		r.OnTurnStarted(p.ThreadID, turnID)
	}
}

func (r *CodexEventRouter) handleTurnCompleted(params json.RawMessage) {
	var p struct {
		ThreadID string `json:"threadId"`
		Turn     struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"turn"`
	}
	if json.Unmarshal(params, &p) != nil {
		return
	}
	slog.Debug("turn completed",
		"thread_id", p.ThreadID,
		"turn_id", p.Turn.ID,
		"status", p.Turn.Status,
	)
	if r.OnTurnCompleted != nil {
		r.OnTurnCompleted(p.ThreadID, p.Turn.ID, p.Turn.Status)
	}
}

func (r *CodexEventRouter) handleThreadTokenUsageUpdated(params json.RawMessage) {
	var p codexrpc.ThreadTokenUsageUpdatedNotification
	if json.Unmarshal(params, &p) != nil {
		return
	}
	if r.OnThreadTokenUsageUpdated != nil {
		r.OnThreadTokenUsageUpdated(p.ThreadID, p.TurnID, p.TokenUsage)
	}
}

func (r *CodexEventRouter) handleError(params json.RawMessage) {
	var p struct {
		ThreadID string `json:"threadId"`
		TurnID   string `json:"turnId"`
		Error    struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(params, &p) != nil {
		return
	}
	slog.Error("codex turn error",
		"thread_id", p.ThreadID,
		"turn_id", p.TurnID,
		"message", p.Error.Message,
	)
	if r.FailStandaloneCompactTurn != nil && r.FailStandaloneCompactTurn(p.ThreadID, p.TurnID, p.Error.Message) {
		return
	}
	if r.RecordTurnError != nil {
		r.RecordTurnError(p.ThreadID, p.TurnID, p.Error.Message)
	}
	if r.UpdateSubmissionByTurn != nil {
		r.UpdateSubmissionByTurn(p.ThreadID, p.TurnID, func(sub *state.Submission) {
			sub.Status = state.SubmissionStatusFailed.String()
		})
	}
}

func (r *CodexEventRouter) handleServerRequestResolved(params json.RawMessage) {
	var p struct {
		ThreadID  string          `json:"threadId"`
		RequestID json.RawMessage `json:"requestId"`
	}
	if json.Unmarshal(params, &p) != nil {
		return
	}
	reqID := RequestIDKey(p.RequestID)
	var pending *state.PendingRequest
	if r.ResolveServerPendingRequest != nil {
		pending = r.ResolveServerPendingRequest(reqID)
	}
	if r.ResumeSubmissionAfterRequest != nil {
		r.ResumeSubmissionAfterRequest(pending)
	}
}

// ---- Server request handlers ----

// OnCommandApproval handles command execution approval requests.
func (r *CodexEventRouter) OnCommandApproval(req codexrpc.RequestEnvelope) {
	var raw map[string]any
	if err := json.Unmarshal(req.Params, &raw); err != nil {
		r.replyError(req.ID, -32602, "invalid params")
		return
	}
	threadID := strings.TrimSpace(stringValue(raw["threadId"]))
	turnID := strings.TrimSpace(stringValue(raw["turnId"]))
	itemID := strings.TrimSpace(stringValue(raw["itemId"]))
	presentation := appapproval.Presentation{
		Kind:     appapproval.KindCommand,
		ThreadID: threadID,
		TurnID:   turnID,
		ItemID:   itemID,
		Payload:  appapproval.RequestPayload{Request: raw},
	}
	if r.MergeApprovalPresentationWithTurnItem != nil {
		presentation = r.MergeApprovalPresentationWithTurnItem(presentation)
	}
	presentation.Body = appapproval.RenderCommandBody(presentation.Payload.Request)
	presentation.Payload.Body = presentation.Body
	if r.SendApprovalCard != nil {
		r.SendApprovalCard(req.ID, presentation)
	}
}

// OnFileApproval handles file change approval requests.
func (r *CodexEventRouter) OnFileApproval(req codexrpc.RequestEnvelope) {
	var raw map[string]any
	if err := json.Unmarshal(req.Params, &raw); err != nil {
		r.replyError(req.ID, -32602, "invalid params")
		return
	}
	threadID := strings.TrimSpace(stringValue(raw["threadId"]))
	turnID := strings.TrimSpace(stringValue(raw["turnId"]))
	itemID := strings.TrimSpace(stringValue(raw["itemId"]))
	presentation := appapproval.Presentation{
		Kind:     appapproval.KindFile,
		ThreadID: threadID,
		TurnID:   turnID,
		ItemID:   itemID,
		Payload:  appapproval.RequestPayload{Request: raw},
	}
	if r.MergeApprovalPresentationWithTurnItem != nil {
		presentation = r.MergeApprovalPresentationWithTurnItem(presentation)
	}
	workspaceCwd := ""
	if r.FindSubmissionByTurn != nil && r.FindWorkspaceCwdForSubmission != nil {
		if _, sub := r.FindSubmissionByTurn(threadID, turnID); sub != nil {
			workspaceCwd = r.FindWorkspaceCwdForSubmission(sub)
		}
	}
	presentation.Body = appapproval.RenderFileBodyWithWorkspace(presentation.Payload.Request, workspaceCwd)
	presentation.Payload.Body = presentation.Body
	if r.SendApprovalCard != nil {
		r.SendApprovalCard(req.ID, presentation)
	}
}

// OnPermissionsApproval handles permissions approval requests.
func (r *CodexEventRouter) OnPermissionsApproval(req codexrpc.RequestEnvelope) {
	var raw map[string]any
	if err := json.Unmarshal(req.Params, &raw); err != nil {
		r.replyError(req.ID, -32602, "invalid params")
		return
	}
	threadID := strings.TrimSpace(stringValue(raw["threadId"]))
	turnID := strings.TrimSpace(stringValue(raw["turnId"]))
	itemID := strings.TrimSpace(stringValue(raw["itemId"]))
	presentation := appapproval.Presentation{
		Kind:     appapproval.KindPermissions,
		ThreadID: threadID,
		TurnID:   turnID,
		ItemID:   itemID,
		Payload:  appapproval.RequestPayload{Request: raw},
	}
	if r.MergeApprovalPresentationWithTurnItem != nil {
		presentation = r.MergeApprovalPresentationWithTurnItem(presentation)
	}
	if permissions, ok := presentation.Payload.Request["permissions"].(map[string]any); ok && len(presentation.Payload.Permissions) == 0 {
		presentation.Payload.Permissions = appapproval.CloneJSONMap(permissions)
	}
	presentation.Body = appapproval.RenderPermissionsApprovalBody(presentation.Payload.Request)
	presentation.Payload.Body = presentation.Body
	if r.SendApprovalCard != nil {
		r.SendApprovalCard(req.ID, presentation)
	}
}

// OnToolUserInput handles tool user input requests.
func (r *CodexEventRouter) OnToolUserInput(req codexrpc.RequestEnvelope) {
	var p apppendingforms.ToolUserInputPayload
	if err := json.Unmarshal(req.Params, &p); err != nil {
		r.replyError(req.ID, -32602, "invalid params")
		return
	}
	if len(p.Questions) == 1 && len(p.Questions[0].Options) > 0 && len(p.Questions[0].Options) <= 3 && !p.Questions[0].MultiSelect && !p.Questions[0].IsOther {
		if r.SendUserInputCard != nil {
			r.SendUserInputCard(req.ID, p)
		}
		return
	}
	if r.SendUserInputFormCard != nil {
		r.SendUserInputFormCard(req.ID, p)
	}
}

// OnMcpElicitationRequest handles MCP elicitation requests.
func (r *CodexEventRouter) OnMcpElicitationRequest(req codexrpc.RequestEnvelope) {
	var header struct {
		ServerName string `json:"serverName"`
		ThreadID   string `json:"threadId"`
		TurnID     string `json:"turnId"`
		Mode       string `json:"mode"`
		Message    string `json:"message"`
		URL        string `json:"url"`
	}
	if err := json.Unmarshal(req.Params, &header); err != nil {
		r.replyError(req.ID, -32602, "invalid params")
		return
	}
	switch header.Mode {
	case "url":
		var payload apppendingforms.ElicitationURLPayload
		if err := json.Unmarshal(req.Params, &payload); err != nil {
			r.replyError(req.ID, -32602, "invalid params")
			return
		}
		if r.SendElicitationURLCard != nil {
			r.SendElicitationURLCard(req.ID, payload)
		}
	case "form":
		var payload apppendingforms.ElicitationFormPayload
		if err := json.Unmarshal(req.Params, &payload); err != nil {
			r.replyError(req.ID, -32602, "invalid params")
			return
		}
		if r.SendElicitationFormCard != nil {
			r.SendElicitationFormCard(req.ID, payload)
		}
	default:
		r.replyError(req.ID, -32601, "unsupported elicitation mode")
	}
}

func (r *CodexEventRouter) replyError(requestID json.RawMessage, code int, message string) {
	if r.ReplyCodexError != nil {
		r.ReplyCodexError(requestID, code, message)
	}
}

// ---------------------------------------------------------------------------
// Pure helpers
// ---------------------------------------------------------------------------

// RequestIDKey extracts a string key from a JSON raw message request ID.
func RequestIDKey(raw json.RawMessage) string {
	raw = json.RawMessage(strings.TrimSpace(string(raw)))
	if len(raw) == 0 {
		return ""
	}
	var value string
	if err := json.Unmarshal(raw, &value); err == nil {
		return strings.TrimSpace(value)
	}
	return strings.TrimSpace(string(raw))
}

// stringValue extracts a string from an any value.
func stringValue(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}
