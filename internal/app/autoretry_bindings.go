package app

import (
	configadapter "feidex/internal/adapter/config"
	retryview "feidex/internal/adapter/feishu/autoretry"
	"feidex/internal/application"
	"feidex/internal/application/runtimeconfig"
	"feidex/internal/config"
	"feidex/internal/domain/conversation"
	"feidex/internal/domain/identity"
	"feidex/internal/feishu"
	retry "feidex/internal/runtime/autoretry"
	"fmt"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

func newAutoRetryService(a *App) retryview.Service {
	view := retryview.Service{
		Context: a.Context, Client: a.feishu, MenuBody: menuCardBody,
		Settings: func() retryview.Settings {
			cfg := feishuConfig(a)
			return retryview.Settings{FrontendID: a.FrontendID(), Backend: configuredBackend(a), Title: a.BackendDriver().Runtime().AutoRetryTitle(), Enabled: cfg != nil && cfg.AutoRetry}
		},
		SaveEnabled: func(enabled bool) error {
			if a.cfg == nil {
				return fmt.Errorf("nil config")
			}
			return (runtimeconfig.Service{Repository: configadapter.NewRuntimeRepository(a)}).SetAutoRetry(enabled)
		},
		SessionKey: func(msg *feishu.InboundMessage) string { return makeSessionKey(a, msg) },
		ReplyAction: func(msg *feishu.InboundMessage, resp *callback.CardActionTriggerResponse) error {
			return replyCommandActionResponse(a, msg, resp)
		},
	}
	view.Engine = retry.NewEngine(retry.Dependencies{
		Context: a.Context, Tracker: a.AutoRetries(), Repository: a.State(), Live: sqLiveThreadAdapter{app: a},
		Enabled: func() bool { return view.Settings().Enabled },
		Recovering: func() bool {
			runtime := backendRuntime(a)
			return runtime != nil && runtime.deferQueuedSubmissionsDuringRecovery(backendRuntimeContextForApp(a))
		},
		DefaultWorkspaceID: func() string { return defaultWorkspaceID(a) },
		Workspace:          func(id string) *config.Workspace { return config.FindWorkspace(a.cfg, id) },
		Starter:            func() retry.SubmissionStarter { return newSubmissionQueueServiceFromApp(a) },
		DispatchTimer: func(key string, seq uint64) {
			runAsync(a, func() {
				_, _ = dispatchInput(a, application.RetryTimerFired{Frontend: identity.FrontendID(a.FrontendID()), SessionKey: identity.SessionKey(key), Sequence: seq})
			})
		},
		Presenter: view,
	})
	return view
}

func (a *App) AutoRetries() *retry.Tracker {
	if a == nil {
		return nil
	}
	ensureCompositionState(a)
	if a.composition.autoRetries == nil {
		a.composition.autoRetries = retry.NewTracker()
	}
	return a.composition.autoRetries
}
func (a *App) RunAsync(fn func())                      { runAsync(a, fn) }
func (a *App) MenuCardBody(action, body string) string { return menuCardBody(action, body) }
func (a *App) SessionHasActiveWork(sess *conversation.Session) bool {
	return conversation.HasActiveWork(sess)
}
func (a *App) ReplyCommandActionResponse(msg *feishu.InboundMessage, resp *callback.CardActionTriggerResponse) error {
	return replyCommandActionResponse(a, msg, resp)
}
