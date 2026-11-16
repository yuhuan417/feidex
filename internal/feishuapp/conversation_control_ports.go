package feishuapp

import (
	"context"
	"strings"
	"sync"

	retryview "feidex/internal/adapter/feishu/autoretry"
	appstate "feidex/internal/adapter/storage/json/scoped"
	retry "feidex/internal/application/autoretry"
	conversationapp "feidex/internal/application/conversation"
	"feidex/internal/config"
	"feidex/internal/domain/conversation"
	backendruntime "feidex/internal/runtime"
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

type conversationRuntimeControl struct {
	runtime       BackendRuntimeDeps
	conversations *conversationapp.Service
}

func (r conversationRuntimeControl) Reconcile(key string, sess *conversation.Session) *conversation.Session {
	deps := r.runtime.currentBackend()
	selected := backendruntime.BackendForKind(deps.view.configuredBackend())
	if selected == nil {
		return sess
	}
	return selected.ReconcileCompletedTurnFromFinalOutput(backendRuntimeContextForApp(deps), key, sess)
}
func (r conversationRuntimeControl) Interrupted(key string, sess *conversation.Session) *conversation.Session {
	deps := r.runtime.currentBackend()
	selected := backendruntime.BackendForKind(deps.view.configuredBackend())
	if selected == nil {
		return sess
	}
	return selected.ClearActiveOperationsAfterInterruptContext(backendRuntimeContextForApp(deps), key, sess)
}
func (r conversationRuntimeControl) Interrupt(ctx context.Context, key string, sess *conversation.Session) error {
	return interruptConversation(r.conversations, r.runtime, ctx, key, sess)
}
func (r conversationRuntimeControl) CancelBackgroundWorkflow(key string, sess *conversation.Session) bool {
	if sess == nil {
		return false
	}
	var submissionID string
	for _, op := range sess.ActiveOperations {
		if strings.TrimSpace(op.Kind) == conversation.OpKindBackgroundWorkflow {
			submissionID = strings.TrimSpace(op.SubmissionID)
			break
		}
	}
	if submissionID == "" {
		return false
	}
	core := r.runtime.currentBackend().runtime.currentClaudeCore()
	canceller, ok := core.(interface {
		CancelBackgroundWorkflow(string, string) bool
	})
	return ok && canceller.CancelBackgroundWorkflow(key, submissionID)
}

type ConversationControlInputs struct {
	Repository          *appstate.Store
	Config              *config.Config
	ConfigMu            *sync.RWMutex
	FrontendID          string
	FrontendConfigIndex int
	Conversations       *conversationapp.Service
	Pending             conversationapp.PendingInputs
	RetryTracker        *retry.Tracker
	AutoRetry           retryview.Service
	Runtime             BackendRuntimeDeps
	Context             func() context.Context
}

func ConversationControlPorts(inputs ConversationControlInputs) conversationapp.ControlDependencies {
	view := frontendConfigView{cfg: inputs.Config, mu: inputs.ConfigMu, frontendID: inputs.FrontendID, frontendConfigIndex: inputs.FrontendConfigIndex}
	return conversationapp.ControlDependencies{
		Repository: inputs.Repository, Workspaces: planWorkspaces{view: view}, Conversations: inputs.Conversations,
		Pending: inputs.Pending, Retry: conversationRetryControl{tracker: inputs.RetryTracker, service: inputs.AutoRetry},
		Runtime: conversationRuntimeControl{runtime: inputs.Runtime, conversations: inputs.Conversations}, Context: inputs.Context,
	}
}
