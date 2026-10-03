package feishuapp

import (
	"context"
	conversationapp "feidex/internal/application/conversation"
	"feidex/internal/domain/conversation"
)

type conversationRetryControl struct{ app *App }

func (r conversationRetryControl) Lock(key string) func() {
	return r.app.AutoRetries().LockDispatch(key)
}
func (r conversationRetryControl) Cancel(key string, keep bool, notice string) bool {
	return r.app.bindings.AutoRetry.CancelAutoRetry(key, keep, notice)
}

type conversationRuntimeControl struct{ app *App }

func (r conversationRuntimeControl) Reconcile(key string, sess *conversation.Session) *conversation.Session {
	runtime := backendRuntime(r.app)
	if runtime == nil {
		return sess
	}
	return runtime.ReconcileCompletedTurnFromFinalOutput(backendRuntimeContextForApp(r.app), key, sess)
}
func (r conversationRuntimeControl) Interrupted(key string, sess *conversation.Session) *conversation.Session {
	runtime := backendRuntime(r.app)
	if runtime == nil {
		return sess
	}
	return runtime.ClearActiveOperationsAfterInterruptContext(backendRuntimeContextForApp(r.app), key, sess)
}
func (r conversationRuntimeControl) Interrupt(ctx context.Context, key string, sess *conversation.Session) error {
	return interruptConversation(r.app, ctx, key, sess)
}

func ConversationControlPorts(a *App) conversationapp.ControlDependencies {
	return conversationapp.ControlDependencies{Repository: a.State(), Workspaces: planWorkspaces{app: a}, Conversations: a.bindings.Conversations, Pending: a.bindings.PendingQueue, Retry: conversationRetryControl{app: a}, Runtime: conversationRuntimeControl{app: a}, Context: a.Context}
}
