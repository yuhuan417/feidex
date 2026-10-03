package feishuapp

import (
	"context"

	"feidex/internal/claudecli"
	"feidex/internal/config"
	appruntime "feidex/internal/runtime"
	appclauderuntime "feidex/internal/runtime/claude"
)

const claudePlanModePendingKind = "claude_exit_plan_mode"

func (r *claudeRuntime) sessionState(sessionKey string) (*appclauderuntime.SessionState, error) {
	return r.service.SessionState(sessionKey)
}

func (r *claudeRuntime) handleTurnComplete(state *appclauderuntime.SessionState, event claudecli.TurnCompleteEvent) {
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

func claudePlanFilePathFromTool(toolName string, input map[string]interface{}) string {
	return appclauderuntime.PlanFilePathFromTool(toolName, input)
}

func (r *claudeRuntime) CanRetryFreshSession(sessionKey string) bool {
	return r.service.CanRetryFreshSession(sessionKey)
}
