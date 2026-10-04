package feishuapp

import (
	retryview "feidex/internal/adapter/feishu/autoretry"
	"feidex/internal/application"
	retry "feidex/internal/application/autoretry"
	"feidex/internal/config"
	"feidex/internal/domain/conversation"
	"feidex/internal/domain/identity"
	"feidex/internal/feishu"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

func AutoRetryView(a *App) retryview.Service {
	view := retryview.Service{
		Context: a.Context, Outbound: newEffectOutbound(a.FrontendID(), newEffectRunner(a.runtimeOwner)), Renderer: simpleStatusCardRenderer{client: a.feishu}, MenuBody: menuCardBody,
		Settings: func() retryview.Settings {
			cfg := a.configView().feishuConfig()
			return retryview.Settings{FrontendID: a.FrontendID(), Backend: a.configView().configuredBackend(), Title: a.BackendDriver().Runtime().AutoRetryTitle(), Enabled: cfg != nil && cfg.AutoRetry}
		},
		SessionKey: func(msg *feishu.InboundMessage) string { return a.configView().makeSessionKey(msg) },
		ReplyAction: func(msg *feishu.InboundMessage, resp *callback.CardActionTriggerResponse) error {
			return replyCommandActionResponse(a, msg, resp)
		},
	}
	return view
}

func AutoRetryPorts(a *App, view retryview.Service, liveThreads retry.LiveThreads) retry.Dependencies {
	return retry.Dependencies{
		Context: a.Context, Tracker: a.AutoRetries(), Repository: a.State(), Live: liveThreads,
		Enabled:     func() bool { return view.Settings().Enabled },
		SaveEnabled: a.bindings.RuntimeSettings.SetAutoRetry,
		Recovering: func() bool {
			runtime := backendRuntime(a)
			return runtime != nil && runtime.DeferQueuedSubmissionsDuringRecovery(backendRuntimeContextForApp(a.BackendRuntimeDeps()))
		},
		DefaultWorkspaceID: func() string { return a.configView().defaultWorkspaceID() },
		Workspace:          func(id string) *config.Workspace { return config.FindWorkspace(a.cfg, id) },
		Starter:            func() retry.SubmissionStarter { return a.bindings.Submissions },
		DispatchTimer: func(key string, seq uint64) {
			runAsync(a, func() {
				_, _ = dispatchInput(a.BackendRuntimeDeps(), application.RetryTimerFired{Frontend: identity.FrontendID(a.FrontendID()), SessionKey: identity.SessionKey(key), Sequence: seq})
			})
		},
		Presenter: view,
	}
}

func (a *App) AutoRetries() *retry.Tracker {
	if a == nil {
		return nil
	}
	owner := a.runtimeView().ensureRuntimeOwner()
	return owner.AutoRetries
}
func (a *App) RunAsync(fn func())                      { runAsync(a, fn) }
func (a *App) MenuCardBody(action, body string) string { return menuCardBody(action, body) }
func (a *App) SessionHasActiveWork(sess *conversation.Session) bool {
	return conversation.HasActiveWork(sess)
}
func (a *App) ReplyCommandActionResponse(msg *feishu.InboundMessage, resp *callback.CardActionTriggerResponse) error {
	return replyCommandActionResponse(a, msg, resp)
}
