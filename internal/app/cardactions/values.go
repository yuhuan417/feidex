package cardactions

import "strings"

type MenuActionValue struct {
	Action       string
	SessionKey   string
	ParentAction string
}

func (v MenuActionValue) Map() map[string]any {
	out := map[string]any{
		"action":      strings.TrimSpace(v.Action),
		"session_key": strings.TrimSpace(v.SessionKey),
	}
	if value := strings.TrimSpace(v.ParentAction); value != "" {
		out["parent_action"] = value
	}
	return out
}

type RequestActionValue struct {
	Action    string
	RequestID string
}

func (v RequestActionValue) Map() map[string]any {
	return map[string]any{
		"action":     strings.TrimSpace(v.Action),
		"request_id": strings.TrimSpace(v.RequestID),
	}
}

type HistoryPageActionValue struct {
	SessionKey string
	Page       int
}

func (v HistoryPageActionValue) Map() map[string]any {
	return map[string]any{
		"action":      "history.page",
		"session_key": strings.TrimSpace(v.SessionKey),
		"page":        v.Page,
	}
}

type HistoryDetailActionValue struct {
	SessionKey string
	Index      int
}

func (v HistoryDetailActionValue) Map() map[string]any {
	return map[string]any{
		"action":      "history.detail",
		"session_key": strings.TrimSpace(v.SessionKey),
		"index":       v.Index,
	}
}

type HistoryDetailSelectActionValue struct {
	SessionKey string
}

func (v HistoryDetailSelectActionValue) Map() map[string]any {
	return map[string]any{
		"action":      "history.detail.select",
		"session_key": strings.TrimSpace(v.SessionKey),
	}
}

type WorkspaceActionValue struct {
	Action         string
	SessionKey     string
	WorkspaceID    string
	SandboxMode    string
	ApprovalPolicy string
	MultiAgentMode string
	Mode           string
}

func (v WorkspaceActionValue) Map() map[string]any {
	out := map[string]any{
		"action":      strings.TrimSpace(v.Action),
		"session_key": strings.TrimSpace(v.SessionKey),
	}
	if value := strings.TrimSpace(v.WorkspaceID); value != "" {
		out["workspace_id"] = value
	}
	if value := strings.TrimSpace(v.SandboxMode); value != "" {
		out["sandbox_mode"] = value
	}
	if value := strings.TrimSpace(v.ApprovalPolicy); value != "" {
		out["approval_policy"] = value
	}
	if value := strings.TrimSpace(v.MultiAgentMode); value != "" {
		out["multi_agent_mode"] = value
	}
	if value := strings.TrimSpace(v.Mode); value != "" {
		out["mode"] = value
	}
	return out
}

type ThreadActionValue struct {
	Action         string
	SessionKey     string
	ThreadID       string
	SandboxMode    string
	ApprovalPolicy string
	MultiAgentMode string
	Mode           string
	ServiceTier    string
}

func (v ThreadActionValue) Map() map[string]any {
	out := map[string]any{
		"action":      strings.TrimSpace(v.Action),
		"session_key": strings.TrimSpace(v.SessionKey),
	}
	if value := strings.TrimSpace(v.ThreadID); value != "" {
		out["thread_id"] = value
	}
	if value := strings.TrimSpace(v.SandboxMode); value != "" {
		out["sandbox_mode"] = value
	}
	if value := strings.TrimSpace(v.ApprovalPolicy); value != "" {
		out["approval_policy"] = value
	}
	if value := strings.TrimSpace(v.MultiAgentMode); value != "" {
		out["multi_agent_mode"] = value
	}
	if value := strings.TrimSpace(v.Mode); value != "" {
		out["mode"] = value
	}
	if value := strings.TrimSpace(v.ServiceTier); value != "" {
		out["service_tier"] = value
	}
	return out
}

type ModelActionValue struct {
	Action          string
	SessionKey      string
	MenuAction      string
	ModelID         string
	ReasoningEffort string
}

func (v ModelActionValue) Map() map[string]any {
	out := map[string]any{
		"action":      strings.TrimSpace(v.Action),
		"session_key": strings.TrimSpace(v.SessionKey),
	}
	if value := strings.TrimSpace(v.MenuAction); value != "" {
		out["menu_action"] = value
	}
	if value := strings.TrimSpace(v.ModelID); value != "" {
		out["model_id"] = value
	}
	if value := strings.TrimSpace(v.ReasoningEffort); value != "" {
		out["reasoning_effort"] = value
	}
	return out
}
