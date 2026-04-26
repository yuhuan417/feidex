package app

import (
	"context"
	"fmt"
	"strings"

	appclauderuntime "feidex/internal/app/clauderuntime"
	appruntime "feidex/internal/app/runtime"
	"feidex/internal/claudecli"
	"feidex/internal/config"
	"feidex/internal/state"
)

const claudePlanModePendingKind = "claude_exit_plan_mode"

// Type aliases — exported types from the clauderuntime sub-package.
type (
	claudeSessionState       = appclauderuntime.SessionState
	claudeTurnState          = appclauderuntime.TurnState
	claudePendingInteraction = appclauderuntime.PendingInteraction
	claudePendingResponse    = appclauderuntime.PendingResponse
)

// ---------------------------------------------------------------------------
// Wrapper methods on *claudeRuntime — delegate to the service
// ---------------------------------------------------------------------------

func (r *claudeRuntime) sessionState(sessionKey string) (*claudeSessionState, error) {
	return r.service.SessionState(sessionKey)
}

func (r *claudeRuntime) handleTurnComplete(state *claudeSessionState, event claudecli.TurnCompleteEvent) {
	r.service.HandleTurnComplete(state, event)
}

func (r *claudeRuntime) CancelPending(requestID, message string) error {
	return r.service.CancelPending(requestID, message)
}

func (r *claudeRuntime) Close() error {
	return r.service.Close()
}

func (r *claudeRuntime) EnsureSession(ctx context.Context, sessionKey string, ws *config.Workspace, resumeID, model string) (string, error) {
	return r.service.EnsureSession(ctx, sessionKey, ws, resumeID, model)
}

func (r *claudeRuntime) ForkSession(ctx context.Context, sessionKey string, ws *config.Workspace, sourceSessionID, model string) (string, error) {
	return r.service.ForkSession(ctx, sessionKey, ws, sourceSessionID, model)
}

func (r *claudeRuntime) StartTurn(ctx context.Context, sessionKey, threadID, turnID, prompt string) error {
	return r.service.StartTurn(ctx, sessionKey, threadID, turnID, prompt)
}

func (r *claudeRuntime) StartSteerTurn(ctx context.Context, sessionKey, threadID, turnID, prompt, steerSubmissionID string) error {
	return r.service.StartSteerTurn(ctx, sessionKey, threadID, turnID, prompt, steerSubmissionID)
}

func (r *claudeRuntime) Interrupt(ctx context.Context, sessionKey string) error {
	return r.service.Interrupt(ctx, sessionKey)
}

func (r *claudeRuntime) SetModel(ctx context.Context, sessionKey, model string) (bool, error) {
	return r.service.SetModel(ctx, sessionKey, model)
}

func (r *claudeRuntime) SetEffort(ctx context.Context, sessionKey, effort string) (bool, error) {
	return r.service.SetEffort(ctx, sessionKey, effort)
}

func (r *claudeRuntime) SetPermissionMode(ctx context.Context, sessionKey, mode string) error {
	return r.service.SetPermissionMode(ctx, sessionKey, mode)
}

func (r *claudeRuntime) ResetSession(sessionKey string) error {
	return r.service.ResetSession(sessionKey)
}

func (r *claudeRuntime) ResolveApproval(requestID string, resolution appruntime.ClaudeApprovalResolution) error {
	return r.service.ResolveApproval(requestID, resolution)
}

func (r *claudeRuntime) ResolveUserInput(requestID string, answers map[string]string) error {
	return r.service.ResolveUserInput(requestID, answers)
}

func (r *claudeRuntime) ResolvePlanFeedback(requestID, feedback string) error {
	return r.service.ResolvePlanFeedback(requestID, feedback)
}

func (r *claudeRuntime) SessionStopped(sessionKey string) bool {
	return r.service.SessionStopped(sessionKey)
}

func (r *claudeRuntime) UpdateConfig(cfg config.ClaudeConfig) {
	r.service.UpdateConfig(cfg)
}

func (r *claudeRuntime) handleSessionError(state *claudeSessionState, event claudecli.ErrorEvent) {
	r.service.HandleSessionError(state, event)
}

func (r *claudeRuntime) handleTextEvent(state *claudeSessionState, event claudecli.TextEvent) {
	r.service.HandleTextEvent(state, event)
}

// ---------------------------------------------------------------------------
// Function aliases — clauderuntime helper functions
// ---------------------------------------------------------------------------

var copyPermissionUpdates = appclauderuntime.CopyPermissionUpdates

var claudeTurnUsageAsThreadUsage = appclauderuntime.TurnUsageAsThreadUsage

var claudeTurnContextUsagePercent = appclauderuntime.TurnContextUsagePercent

var claudeQuestionsAsToolUserInput = appclauderuntime.QuestionsAsToolUserInput

var claudePlanModeBody = appclauderuntime.PlanModeBody

var isClaudeInternalTool = appclauderuntime.IsInternalTool

var isClaudePlanFilePath = appclauderuntime.IsPlanFilePath

var enrichClaudePlanForDisplay = appclauderuntime.EnrichPlanForDisplay

var readClaudePlanText = appclauderuntime.ReadPlanText

var claudePlanFileCandidates = appclauderuntime.PlanFileCandidates

var latestClaudePlanFile = appclauderuntime.LatestPlanFile

var claudePermissionModeValue = appclauderuntime.PermissionModeValue

var safeClaudeSessionPermissionUpdates = appclauderuntime.SafeClaudeSessionPermissionUpdates

var describeClaudeSessionPermissionUpdates = appclauderuntime.DescribeClaudeSessionPermissionUpdates

// ---------------------------------------------------------------------------
// Standalone functions — kept in app/ for backward compatibility
// ---------------------------------------------------------------------------

func claudePlanFilePathFromTool(toolName string, input map[string]interface{}) string {
	return appclauderuntime.PlanFilePathFromTool(toolName, input)
}

func isFatalClaudeSessionError(state *claudeSessionState, event claudecli.ErrorEvent) bool {
	return appclauderuntime.IsFatalSessionErrorFromState(state, event)
}

func buildClaudePrompt(sub *state.Submission) string {
	if sub == nil {
		return ""
	}
	parts := make([]string, 0, len(sub.Skills)+1+len(sub.Attachments))
	for _, skill := range sub.Skills {
		if strings.TrimSpace(skill.Name) == "" && strings.TrimSpace(skill.Path) == "" {
			continue
		}
		parts = append(parts, fmt.Sprintf("Use skill `%s` (`%s`) if it is available in this Claude session.", firstNonEmpty(strings.TrimSpace(skill.Name), "skill"), firstNonEmpty(strings.TrimSpace(skill.Path), "-")))
	}
	if text := strings.TrimSpace(sub.InputText); text != "" {
		parts = append(parts, text)
	}
	for _, attachment := range sub.Attachments {
		if prompt := attachmentPrompt(attachment); prompt != "" {
			parts = append(parts, prompt)
		}
	}
	return strings.TrimSpace(strings.Join(parts, "\n\n"))
}
