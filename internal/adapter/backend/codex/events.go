package codex

import (
	"encoding/json"
	"feidex/internal/application"
	"feidex/internal/codexrpc"
	"feidex/internal/domain/interaction"
	"feidex/internal/domain/turn"
	"fmt"
	"strings"
)

// DecodeNotification completes the wire boundary before dispatch. Every
// payload is an application/domain value, never a protocol envelope or SDK object.
func DecodeNotification(method string, params json.RawMessage) (application.BackendEvent, bool, error) {
	if e, handled, err := DecodeLifecycle(method, params); handled {
		return e, handled, err
	}
	var e application.BackendEvent
	switch method {
	case "item/started", "item/completed":
		var p struct {
			ThreadID string         `json:"threadId"`
			TurnID   string         `json:"turnId"`
			ItemID   string         `json:"itemId"`
			Item     map[string]any `json:"item"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			return e, true, err
		}
		item := turn.NewProtocolItemWithID(p.ItemID, p.Item)
		kind := application.EventItemStarted
		if method == "item/completed" {
			kind = application.EventItemCompleted
		}
		return application.BackendEvent{Kind: kind, ThreadID: p.ThreadID, TurnID: p.TurnID, Item: &item, Payload: item}, true, nil
	case "item/mcpToolCall/progress":
		var p struct {
			ThreadID string `json:"threadId"`
			TurnID   string `json:"turnId"`
			ItemID   string `json:"itemId"`
			Message  string `json:"message"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			return e, true, err
		}
		if strings.TrimSpace(p.ItemID) == "" || strings.TrimSpace(p.Message) == "" {
			return e, true, nil
		}
		item := turn.NewProtocolItemWithID(p.ItemID, map[string]any{"id": p.ItemID, "type": "mcp_tool_call", "status": "in_progress", "message": strings.TrimSpace(p.Message)})
		return application.BackendEvent{Kind: application.EventItemProgress, ThreadID: p.ThreadID, TurnID: p.TurnID, Item: &item, Payload: item}, true, nil
	case "turn/plan/updated":
		var p struct {
			ThreadID string                          `json:"threadId"`
			TurnID   string                          `json:"turnId"`
			Plan     []struct{ Step, Status string } `json:"plan"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			return e, true, err
		}
		lines := make([]string, 0, len(p.Plan))
		for _, step := range p.Plan {
			lines = append(lines, fmt.Sprintf("- [%s] %s", step.Status, step.Step))
		}
		return application.BackendEvent{Kind: application.EventPlanUpdated, ThreadID: p.ThreadID, TurnID: p.TurnID, Message: strings.Join(lines, "\n")}, true, nil
	case "thread/tokenUsage/updated":
		var p codexrpc.ThreadTokenUsageUpdatedNotification
		if err := json.Unmarshal(params, &p); err != nil {
			return e, true, err
		}
		usage := ThreadUsage(p.TokenUsage)
		return application.BackendEvent{Kind: application.EventUsageUpdated, ThreadID: p.ThreadID, TurnID: p.TurnID, Usage: &usage, Payload: usage}, true, nil
	case "thread/goal/updated":
		var p codexrpc.ThreadGoalUpdatedNotification
		if err := json.Unmarshal(params, &p); err != nil {
			return e, true, err
		}
		turnID := ""
		if p.TurnID != nil {
			turnID = *p.TurnID
		}
		goal := p.Goal
		return application.BackendEvent{Kind: application.EventGoalUpdated, ThreadID: p.ThreadID, TurnID: turnID, Goal: &goal, Payload: goal}, true, nil
	case "thread/goal/cleared":
		var p codexrpc.ThreadGoalClearedNotification
		if err := json.Unmarshal(params, &p); err != nil {
			return e, true, err
		}
		return application.BackendEvent{Kind: application.EventGoalCleared, ThreadID: p.ThreadID}, true, nil
	default:
		return e, false, nil
	}
}

// DecodeRequest keeps the response token opaque to use cases. The backend
// adapter alone interprets that token when replying, preserving numeric IDs.
func DecodeRequest(req codexrpc.RequestEnvelope) application.BackendEvent {
	e := application.BackendEvent{RequestID: RequestIDKey(req.ID), ResponseToken: string(req.ID)}
	invalid := func(code int, message string) application.BackendEvent {
		e.Kind = application.EventRequestRejected
		e.Message = message
		rejected := application.RequestRejected{Code: code}
		e.Rejected = &rejected
		e.Payload = rejected // compatibility mirror for legacy event consumers.
		return e
	}
	switch req.Method {
	case "item/commandExecution/requestApproval", "item/fileChange/requestApproval", "item/permissions/requestApproval":
		var raw map[string]any
		if err := json.Unmarshal(req.Params, &raw); err != nil {
			return invalid(-32602, "invalid params")
		}
		kind := "command"
		if req.Method == "item/fileChange/requestApproval" {
			kind = "file"
		}
		if req.Method == "item/permissions/requestApproval" {
			kind = "permissions"
		}
		e.Kind = application.EventApprovalRequested
		e.ThreadID = stringValue(raw["threadId"])
		e.TurnID = stringValue(raw["turnId"])
		approval := application.ApprovalRequested{Kind: kind, ItemID: stringValue(raw["itemId"]), Request: raw}
		e.Approval = &approval
		e.Payload = approval // compatibility mirror for legacy event consumers.
		return e
	case "item/tool/requestUserInput":
		var p interaction.ToolUserInputPayload
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return invalid(-32602, "invalid params")
		}
		e.Kind = application.EventUserInputRequested
		e.ThreadID = p.ThreadID
		e.TurnID = p.TurnID
		e.UserInput = &p
		e.Payload = p
		return e
	case "mcpServer/elicitation/request":
		var header struct {
			Mode string `json:"mode"`
		}
		if err := json.Unmarshal(req.Params, &header); err != nil {
			return invalid(-32602, "invalid params")
		}
		switch header.Mode {
		case "url":
			var p interaction.ElicitationURLPayload
			if err := json.Unmarshal(req.Params, &p); err != nil {
				return invalid(-32602, "invalid params")
			}
			e.Kind = application.EventElicitationURLRequested
			e.ThreadID = p.ThreadID
			e.TurnID = p.TurnID
			e.ElicitationURL = &p
			e.Payload = p
			return e
		case "form":
			var p interaction.ElicitationFormPayload
			if err := json.Unmarshal(req.Params, &p); err != nil {
				return invalid(-32602, "invalid params")
			}
			e.Kind = application.EventElicitationFormRequested
			e.ThreadID = p.ThreadID
			e.TurnID = p.TurnID
			e.ElicitationForm = &p
			e.Payload = p
			return e
		default:
			return invalid(-32601, "unsupported elicitation mode")
		}
	default:
		return invalid(-32601, "unsupported server request")
	}
}
func stringValue(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return strings.TrimSpace(s)
	}
	return fmt.Sprint(v)
}
