package feishuapp

import (
	"context"
	"feidex/internal/application/backendops"
	"feidex/internal/application/turn"
	"feidex/internal/domain/conversation"
)

type turnReconciliationGateway struct{ runtimeDeps BackendRuntimeDeps }

func TurnReconciliationGateway(runtimeDeps BackendRuntimeDeps) turn.ReconciliationGateway {
	return turnReconciliationGateway{runtimeDeps: runtimeDeps}
}

func ClaudeSessionStopped(backend func() string, core func() ClaudeCore) func(string) bool {
	return func(key string) bool {
		current := core()
		return current != nil && backend() == "claude" && current.SessionStopped(key)
	}
}
func (p turnReconciliationGateway) Available() bool {
	deps := p.runtimeDeps.currentBackend()
	return deps.view.configuredBackend() == "codex" && deps.runtime.currentCodexClient() != nil
}
func (p turnReconciliationGateway) ReadThreadTurns(ctx context.Context, threadID string) (backendops.ThreadTurns, error) {
	gateway, err := p.runtimeDeps.runtime.requireCodexGateway()
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
