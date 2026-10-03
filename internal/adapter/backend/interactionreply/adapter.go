package interactionreply

import (
	"encoding/json"
	interactionapp "feidex/internal/application/interaction"
	"feidex/internal/domain/interaction"
	"fmt"
	"strings"

	"feidex/internal/adapter/feishu/claudesupport"
	"feidex/internal/adapter/feishu/pendingforms"
	appruntime "feidex/internal/runtime"
	"feidex/internal/state"
)

// CodexReplyClient is the narrow interface for Codex RPC reply operations.
type CodexReplyClient interface {
	Reply(json.RawMessage, any) error
	ReplyError(json.RawMessage, int, string) error
}

// ClaudeReplyClient is the narrow interface for Claude pending operations.
type ClaudeReplyClient interface {
	ResolveApproval(string, appruntime.ClaudeApprovalResolution) error
	ResolveUserInput(string, map[string]string) error
	CancelPending(string, string) error
}

// ---------- Unsupported ----------

type unsupportedAdapter struct {
	backend string
}

// NewUnsupportedAdapter returns a interactionapp.BackendReply that returns errors for all methods.
func NewUnsupportedAdapter(backend string) interactionapp.BackendReply {
	return unsupportedAdapter{backend: strings.TrimSpace(backend)}
}

func (u unsupportedAdapter) Kind() string { return strings.TrimSpace(u.backend) }

func (u unsupportedAdapter) err() error {
	backend := strings.TrimSpace(u.backend)
	if backend == "" {
		return fmt.Errorf("backend not configured")
	}
	return fmt.Errorf("unsupported backend %q", backend)
}

func (u unsupportedAdapter) ReplyApproval(*state.PendingRequest, string, json.RawMessage) error {
	return u.err()
}
func (u unsupportedAdapter) ReplyQuickUserInput(*state.PendingRequest, interaction.ToolUserInputPayload, string, string) (string, error) {
	return "", u.err()
}
func (u unsupportedAdapter) ReplyFormUserInput(*state.PendingRequest, interaction.ToolUserInputPayload, map[string]string) (string, error) {
	return "", u.err()
}
func (u unsupportedAdapter) ReplyTextUserInput(*state.PendingRequest, interaction.ToolUserInputPayload, string) (string, error) {
	return "", u.err()
}
func (u unsupportedAdapter) ReplyElicitationAction(*state.PendingRequest, string) error {
	return u.err()
}
func (u unsupportedAdapter) ReplyElicitationContent(*state.PendingRequest, map[string]any) error {
	return u.err()
}
func (u unsupportedAdapter) ReplyElicitationForm(*state.PendingRequest, interaction.ElicitationFormPayload, string) (string, error) {
	return "", u.err()
}
func (u unsupportedAdapter) ReplyElicitationURL(*state.PendingRequest, string) (string, error) {
	return "", u.err()
}
func (u unsupportedAdapter) CancelPending(*state.PendingRequest) error {
	return u.err()
}

// ---------- Codex ----------

type codexAdapter struct {
	client CodexReplyClient
	kind_  string
}

// NewCodexAdapter returns a interactionapp.BackendReply for the Codex backend.
func NewCodexAdapter(client CodexReplyClient, backendKind string) interactionapp.BackendReply {
	return codexAdapter{client: client, kind_: backendKind}
}

func (c codexAdapter) Kind() string { return c.kind_ }

func (c codexAdapter) ReplyApproval(pending *state.PendingRequest, _ string, replyPayload json.RawMessage) error {
	var value any
	if err := json.Unmarshal(replyPayload, &value); err != nil {
		return err
	}
	return c.client.Reply(pendingRequestIDRaw(pending), value)
}

func (c codexAdapter) ReplyQuickUserInput(pending *state.PendingRequest, payload interaction.ToolUserInputPayload, questionID, answer string) (string, error) {
	replyPayload := map[string]any{
		"answers": map[string]any{
			questionID: map[string]any{
				"answers": []string{answer},
			},
		},
	}
	summary := strings.TrimSpace(answer)
	for _, q := range payload.Questions {
		if strings.TrimSpace(q.ID) == strings.TrimSpace(questionID) {
			summary = pendingforms.ToolUserInputSummaryText(q, []string{answer}, q.IsSecret)
			break
		}
	}
	return summary, c.client.Reply(pendingRequestIDRaw(pending), replyPayload)
}

func (c codexAdapter) ReplyFormUserInput(pending *state.PendingRequest, payload interaction.ToolUserInputPayload, selections map[string]string) (string, error) {
	replyPayload, summary, err := pendingforms.BuildToolUserInputResponseFromSelections(payload, selections)
	if err != nil {
		return "", interactionapp.Warning{Message: err.Error()}
	}
	return summary, c.client.Reply(pendingRequestIDRaw(pending), replyPayload)
}

func (c codexAdapter) ReplyTextUserInput(pending *state.PendingRequest, payload interaction.ToolUserInputPayload, text string) (string, error) {
	replyPayload, summary, err := pendingforms.ParseToolUserInputResponse(strings.TrimSpace(text), payload)
	if err != nil {
		return "", err
	}
	return summary, c.client.Reply(pendingRequestIDRaw(pending), replyPayload)
}

func (c codexAdapter) ReplyElicitationAction(pending *state.PendingRequest, action string) error {
	action = strings.TrimSpace(action)
	switch action {
	case "accept", "decline", "cancel":
		return c.client.Reply(pendingRequestIDRaw(pending), map[string]any{"action": action})
	default:
		return fmt.Errorf("unsupported elicitation action %q", action)
	}
}

func (c codexAdapter) ReplyElicitationContent(pending *state.PendingRequest, content map[string]any) error {
	return c.client.Reply(pendingRequestIDRaw(pending), map[string]any{
		"action":  "accept",
		"content": content,
	})
}

func (c codexAdapter) ReplyElicitationForm(pending *state.PendingRequest, payload interaction.ElicitationFormPayload, text string) (string, error) {
	content, summary, err := pendingforms.ParseElicitationFormResponse(strings.TrimSpace(text), payload)
	if err != nil {
		return "", err
	}
	return summary, c.ReplyElicitationContent(pending, content)
}

func (c codexAdapter) ReplyElicitationURL(pending *state.PendingRequest, actionName string) (string, error) {
	decision := "cancel"
	switch strings.TrimSpace(actionName) {
	case "elicitation_url.accept":
		decision = "accept"
	case "elicitation_url.decline":
		decision = "decline"
	}
	return decision, c.ReplyElicitationAction(pending, decision)
}

func (c codexAdapter) CancelPending(pending *state.PendingRequest) error {
	switch pending.Kind {
	case "tool_request_user_input_form":
		return c.client.ReplyError(pendingRequestIDRaw(pending), -32800, "cancelled by user")
	case "mcp_elicitation_form":
		return c.client.Reply(pendingRequestIDRaw(pending), map[string]any{"action": "cancel"})
	default:
		return nil
	}
}

// ---------- Claude ----------

type claudeAdapter struct {
	client ClaudeReplyClient
	kind_  string
}

// NewClaudeAdapter returns a interactionapp.BackendReply for the Claude backend.
func NewClaudeAdapter(client ClaudeReplyClient, backendKind string) interactionapp.BackendReply {
	return claudeAdapter{client: client, kind_: backendKind}
}

func (c claudeAdapter) Kind() string { return c.kind_ }

func (c claudeAdapter) ReplyApproval(pending *state.PendingRequest, actionName string, _ json.RawMessage) error {
	resolution, resolutionWarning := claudesupport.ClaudeApprovalResolutionForAction(actionName)
	if strings.TrimSpace(resolutionWarning) != "" {
		return interactionapp.Warning{Message: resolutionWarning}
	}
	return c.client.ResolveApproval(strings.TrimSpace(pending.ID), resolution)
}

func (c claudeAdapter) ReplyQuickUserInput(pending *state.PendingRequest, payload interaction.ToolUserInputPayload, questionID, answer string) (string, error) {
	answers, _, err := claudesupport.ClaudeAnswersFromSelections(payload, map[string]string{
		strings.TrimSpace(questionID): strings.TrimSpace(answer),
	})
	if err != nil {
		return "", interactionapp.Warning{Message: err.Error()}
	}
	summary := strings.TrimSpace(answer)
	for _, q := range payload.Questions {
		if strings.TrimSpace(q.ID) == strings.TrimSpace(questionID) {
			summary = pendingforms.ToolUserInputSummaryText(q, []string{answer}, q.IsSecret)
			break
		}
	}
	return summary, c.client.ResolveUserInput(strings.TrimSpace(pending.ID), answers)
}

func (c claudeAdapter) ReplyFormUserInput(pending *state.PendingRequest, payload interaction.ToolUserInputPayload, selections map[string]string) (string, error) {
	answers, summary, err := claudesupport.ClaudeAnswersFromSelections(payload, selections)
	if err != nil {
		return "", interactionapp.Warning{Message: err.Error()}
	}
	return summary, c.client.ResolveUserInput(strings.TrimSpace(pending.ID), answers)
}

func (c claudeAdapter) ReplyTextUserInput(pending *state.PendingRequest, payload interaction.ToolUserInputPayload, text string) (string, error) {
	answers, summary, err := claudesupport.ParseClaudeToolUserInputResponse(strings.TrimSpace(text), payload)
	if err != nil {
		return "", err
	}
	return summary, c.client.ResolveUserInput(strings.TrimSpace(pending.ID), answers)
}

func (claudeAdapter) ReplyElicitationAction(*state.PendingRequest, string) error {
	return fmt.Errorf("claude backend does not support elicitation form replies")
}

func (claudeAdapter) ReplyElicitationContent(*state.PendingRequest, map[string]any) error {
	return fmt.Errorf("claude backend does not support elicitation form replies")
}

func (claudeAdapter) ReplyElicitationForm(*state.PendingRequest, interaction.ElicitationFormPayload, string) (string, error) {
	return "", fmt.Errorf("claude backend does not support elicitation form replies")
}

func (claudeAdapter) ReplyElicitationURL(*state.PendingRequest, string) (string, error) {
	return "", fmt.Errorf("claude backend does not support elicitation url replies")
}

func (c claudeAdapter) CancelPending(pending *state.PendingRequest) error {
	switch pending.Kind {
	case "tool_request_user_input_form", "mcp_elicitation_form", "claude_exit_plan_mode":
		return c.client.CancelPending(strings.TrimSpace(pending.ID), "cancelled by user")
	default:
		return nil
	}
}

func pendingRequestIDRaw(pending *state.PendingRequest) json.RawMessage {
	if pending == nil {
		return nil
	}
	if raw := strings.TrimSpace(pending.RequestIDRaw); raw != "" {
		return json.RawMessage(raw)
	}
	value := strings.TrimSpace(pending.ID)
	if value == "" {
		return nil
	}
	encoded, _ := json.Marshal(value)
	return encoded
}
