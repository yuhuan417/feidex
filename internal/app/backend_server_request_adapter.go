package app

import (
	"fmt"
	"strings"

	"feidex/internal/state"
)

type serverRequestBackendAdapter interface {
	kind() string
	replyApproval(pending *state.PendingRequest, actionName string, replyPayload any) error
	replyQuickUserInput(pending *state.PendingRequest, payload toolUserInputPayload, questionID, answer string) (string, error)
	replyFormUserInput(pending *state.PendingRequest, payload toolUserInputPayload, selections map[string]string) (string, error)
	cancelPending(pending *state.PendingRequest) error
}

func (a *App) serverRequestBackendAdapter(pending *state.PendingRequest) serverRequestBackendAdapter {
	if pendingBackend(a, pending) == backendClaude {
		return claudeServerRequestAdapter{app: a}
	}
	return codexServerRequestAdapter{app: a}
}

type codexServerRequestAdapter struct {
	app *App
}

func (codexServerRequestAdapter) kind() string { return backendCodex }

func (c codexServerRequestAdapter) replyApproval(pending *state.PendingRequest, _ string, replyPayload any) error {
	if c.app == nil || c.app.codex == nil {
		return fmt.Errorf("codex client not initialized")
	}
	return c.app.codex.Reply(pendingRequestIDRaw(pending), replyPayload)
}

func (c codexServerRequestAdapter) replyQuickUserInput(pending *state.PendingRequest, _ toolUserInputPayload, questionID, answer string) (string, error) {
	if c.app == nil || c.app.codex == nil {
		return "", fmt.Errorf("codex client not initialized")
	}
	replyPayload := map[string]any{
		"answers": map[string]any{
			questionID: map[string]any{
				"answers": []string{answer},
			},
		},
	}
	return strings.TrimSpace(answer), c.app.codex.Reply(pendingRequestIDRaw(pending), replyPayload)
}

func (c codexServerRequestAdapter) replyFormUserInput(pending *state.PendingRequest, payload toolUserInputPayload, selections map[string]string) (string, error) {
	if c.app == nil || c.app.codex == nil {
		return "", fmt.Errorf("codex client not initialized")
	}
	replyPayload, summary, err := buildToolUserInputResponseFromSelections(payload, selections)
	if err != nil {
		return "", newUIWarningError(err.Error())
	}
	return summary, c.app.codex.Reply(pendingRequestIDRaw(pending), replyPayload)
}

func (c codexServerRequestAdapter) cancelPending(pending *state.PendingRequest) error {
	switch pending.Kind {
	case "tool_request_user_input_form":
		if c.app == nil || c.app.codex == nil {
			return fmt.Errorf("codex client not initialized")
		}
		return c.app.codex.ReplyError(pendingRequestIDRaw(pending), -32800, "cancelled by user")
	case "mcp_elicitation_form":
		if c.app == nil || c.app.codex == nil {
			return fmt.Errorf("codex client not initialized")
		}
		return c.app.codex.Reply(pendingRequestIDRaw(pending), map[string]any{"action": "cancel"})
	default:
		return nil
	}
}

type claudeServerRequestAdapter struct {
	app *App
}

func (claudeServerRequestAdapter) kind() string { return backendClaude }

func (c claudeServerRequestAdapter) replyApproval(pending *state.PendingRequest, actionName string, _ any) error {
	if c.app == nil || c.app.claude == nil {
		return fmt.Errorf("claude backend not initialized")
	}
	resolution, resolutionWarning := claudeApprovalResolutionForAction(actionName)
	if strings.TrimSpace(resolutionWarning) != "" {
		return newUIWarningError(resolutionWarning)
	}
	return c.app.claude.ResolveApproval(strings.TrimSpace(pending.ID), resolution)
}

func (c claudeServerRequestAdapter) replyQuickUserInput(pending *state.PendingRequest, payload toolUserInputPayload, questionID, answer string) (string, error) {
	if c.app == nil || c.app.claude == nil {
		return "", fmt.Errorf("claude backend not initialized")
	}
	answers, _, err := claudeAnswersFromSelections(payload, map[string]string{
		strings.TrimSpace(questionID): strings.TrimSpace(answer),
	})
	if err != nil {
		return "", newUIWarningError(err.Error())
	}
	return strings.TrimSpace(answer), c.app.claude.ResolveUserInput(strings.TrimSpace(pending.ID), answers)
}

func (c claudeServerRequestAdapter) replyFormUserInput(pending *state.PendingRequest, payload toolUserInputPayload, selections map[string]string) (string, error) {
	if c.app == nil || c.app.claude == nil {
		return "", fmt.Errorf("claude backend not initialized")
	}
	answers, summary, err := claudeAnswersFromSelections(payload, selections)
	if err != nil {
		return "", newUIWarningError(err.Error())
	}
	return summary, c.app.claude.ResolveUserInput(strings.TrimSpace(pending.ID), answers)
}

func (c claudeServerRequestAdapter) cancelPending(pending *state.PendingRequest) error {
	switch pending.Kind {
	case "tool_request_user_input_form", "mcp_elicitation_form", claudePlanModePendingKind:
		if c.app == nil || c.app.claude == nil {
			return fmt.Errorf("claude backend not initialized")
		}
		return c.app.claude.CancelPending(strings.TrimSpace(pending.ID), "cancelled by user")
	default:
		return nil
	}
}
