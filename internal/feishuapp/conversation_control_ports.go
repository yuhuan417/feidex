package feishuapp

import (
	"context"

	retryview "feidex/internal/adapter/feishu/autoretry"
	retry "feidex/internal/application/autoretry"
	conversationapp "feidex/internal/application/conversation"
	"feidex/internal/domain/conversation"
)

// conversationRetryControl holds the two values it uses rather than the
// frontend aggregate.
type conversationRetryControl struct {
	tracker *retry.Tracker
	service retryview.Service
}

func (r conversationRetryControl) Lock(key string) func() {
	return r.tracker.LockDispatch(key)
}
func (r conversationRetryControl) Cancel(key string, keep bool, notice string) bool {
	return r.service.CancelAutoRetry(key, keep, notice)
}

type conversationRuntimeControl struct{ app *App }

func (r conversationRuntimeControl) Reconcile(key string, sess *conversation.Session) *conversation.Session {
	runtime := backendRuntime(r.app)
	if runtime == nil {
		return sess
	}
	return runtime.ReconcileCompletedTurnFromFinalOutput(backendRuntimeContextForApp(r.app.BackendRuntimeDeps()), key, sess)
}
func (r conversationRuntimeControl) Interrupted(key string, sess *conversation.Session) *conversation.Session {
	runtime := backendRuntime(r.app)
	if runtime == nil {
		return sess
	}
	return runtime.ClearActiveOperationsAfterInterruptContext(backendRuntimeContextForApp(r.app.BackendRuntimeDeps()), key, sess)
}
func (r conversationRuntimeControl) Interrupt(ctx context.Context, key string, sess *conversation.Session) error {
	return interruptConversation(r.app, ctx, key, sess)
}

func ConversationControlPorts(a *App) conversationapp.ControlDependencies {
	return conversationapp.ControlDependencies{Repository: a.State(), Workspaces: planWorkspaces{view: a.configView()}, Conversations: a.bindings.Conversations, Pending: a.bindings.PendingQueue, Retry: conversationRetryControl{tracker: a.AutoRetries(), service: a.bindings.AutoRetry}, Runtime: conversationRuntimeControl{app: a}, Context: a.Context}
}
