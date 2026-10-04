package feishuapp

import (
	"context"
	"feidex/internal/application/backendops"
	"feidex/internal/application/turn"
	"feidex/internal/domain/conversation"
)

type turnReconciliationGateway struct{ app *App }

func TurnReconciliationGateway(a *App) turn.ReconciliationGateway {
	return turnReconciliationGateway{app: a}
}

func ClaudeSessionStopped(a *App) func(string) bool {
	return func(key string) bool {
		core := currentClaudeCore(a)
		return core != nil && configuredBackend(a) == "claude" && core.SessionStopped(key)
	}
}
func (p turnReconciliationGateway) Available() bool {
	return configuredBackend(p.app) == "codex" && currentCodexClient(p.app) != nil
}
func (p turnReconciliationGateway) ReadThreadTurns(ctx context.Context, threadID string) (backendops.ThreadTurns, error) {
	gateway, err := requireCodexGateway(p.app)
	if err != nil {
		return backendops.ThreadTurns{}, err
	}
	return gateway.ReadThreadTurns(ctx, threadID)
}
func reconcileCompletedCodexTurnFromFinalOutput(turnreconciliation turn.Reconciliation, key string, sess *conversation.Session) *conversation.Session {
	return turnreconciliation.AfterFinal(key, sess)
}
func reconcileCompletedCodexTurn(turnreconciliation turn.Reconciliation, key string, sess *conversation.Session) *conversation.Session {
	return turnreconciliation.Reconcile(key, sess)
}
