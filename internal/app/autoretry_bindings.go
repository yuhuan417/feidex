package app

import (
	appautoretry "feidex/internal/app/autoretry"
	appconvbackend "feidex/internal/app/convbackend"
	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

// ---------------------------------------------------------------------------
// Provider adapters — satisfy autoretry narrow interfaces
// ---------------------------------------------------------------------------

type backendRuntimeAdapter struct {
	app     *App
	runtime backendRuntimeFacade
}

func (a backendRuntimeAdapter) DeferQueuedSubmissionsDuringRecovery() bool {
	if a.runtime == nil {
		return false
	}
	return a.runtime.deferQueuedSubmissionsDuringRecovery(a.app)
}

type conversationBackendAdapter struct {
	backend appconvbackend.ConversationBackendFacade
}

func (a conversationBackendAdapter) StartQueuedSubmission(sessionKey string, sess *state.Session, sub *state.Submission, ws *config.Workspace, notifyFailure bool) error {
	return a.backend.StartQueuedSubmission(sessionKey, sess, sub, ws, notifyFailure)
}

// ---------------------------------------------------------------------------
// *App methods satisfying autoretry.App
// ---------------------------------------------------------------------------

func (a *App) AutoRetries() *appautoretry.Tracker {
	if a == nil {
		return nil
	}
	if a.autoRetries == nil {
		a.autoRetries = appautoretry.NewTracker()
	}
	return a.autoRetries
}

func (a *App) AppState() appautoretry.AppStateProvider {
	if a == nil {
		return nil
	}
	return a.State()
}

func (a *App) AutoRetryBackendRuntime() appautoretry.BackendRuntimeProvider {
	return backendRuntimeAdapter{app: a, runtime: backendRuntime(a)}
}

func (a *App) AutoRetryConversationBackend() appautoretry.ConversationBackendProvider {
	return conversationBackendAdapter{backend: conversationBackend(a)}
}

func (a *App) RunAsync(fn func()) {
	runAsync(a, fn)
}

func (a *App) MenuCardBody(action, body string) string {
	return menuCardBody(action, body)
}

func (a *App) SessionHasActiveWork(sess *state.Session) bool {
	return sessionHasActiveWork(sess)
}

func (a *App) SessionHasLiveThread(sessionKey, threadID string) bool {
	return sessionHasLiveThread(a, sessionKey, threadID)
}

func (a *App) ClearSessionLiveThread(sessionKey string) {
	clearSessionLiveThread(a, sessionKey)
}

func (a *App) ReplyCommandActionResponse(msg *feishu.InboundMessage, resp *callback.CardActionTriggerResponse) error {
	return replyCommandActionResponse(a, msg, resp)
}
