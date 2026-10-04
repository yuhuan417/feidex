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
		core := a.runtimeView().currentClaudeCore()
		return core != nil && a.configView().configuredBackend() == "claude" && core.SessionStopped(key)
	}
}
func (p turnReconciliationGateway) Available() bool {
	return p.app.configView().configuredBackend() == "codex" && p.app.runtimeView().currentCodexClient() != nil
}
func (p turnReconciliationGateway) ReadThreadTurns(ctx context.Context, threadID string) (backendops.ThreadTurns, error) {
	gateway, err := p.app.runtimeView().requireCodexGateway()
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
