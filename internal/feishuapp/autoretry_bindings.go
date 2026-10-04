package feishuapp

import (
	"context"
	"sync"

	retryview "feidex/internal/adapter/feishu/autoretry"
	appstate "feidex/internal/adapter/storage/json/scoped"
	"feidex/internal/application"
	retry "feidex/internal/application/autoretry"
	"feidex/internal/config"
	"feidex/internal/domain/conversation"
	"feidex/internal/domain/identity"
	"feidex/internal/feishu"
	frontendruntime "feidex/internal/runtime"

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

type AutoRetryPortInputs struct {
	Context      func() context.Context
	Tracker      *retry.Tracker
	Repository   *appstate.Store
	Live         retry.LiveThreads
	Enabled      func() bool
	SaveEnabled  func(bool) error
	RuntimeDeps  *BackendRuntimeDeps
	RuntimeOwner *frontendruntime.FrontendOwner
	RunAsync     func(func())
	FrontendID   string
	Config       *config.Config
	ConfigMu     *sync.RWMutex
	Starter      retry.SubmissionStarter
	Presenter    retryview.Service
}

func AutoRetryPorts(inputs AutoRetryPortInputs) retry.Dependencies {
	configView := frontendConfigView{cfg: inputs.Config, mu: inputs.ConfigMu}
	return retry.Dependencies{
		Context: inputs.Context, Tracker: inputs.Tracker, Repository: inputs.Repository, Live: inputs.Live,
		Enabled:     inputs.Enabled,
		SaveEnabled: inputs.SaveEnabled,
		Recovering: func() bool {
			runtime := frontendruntime.BackendForKind(inputs.RuntimeOwner.Backend())
			return runtime != nil && runtime.DeferQueuedSubmissionsDuringRecovery(backendRuntimeContextForApp(inputs.RuntimeDeps.currentBackend()))
		},
		DefaultWorkspaceID: configView.defaultWorkspaceID,
		Workspace:          func(id string) *config.Workspace { return config.FindWorkspace(inputs.Config, id) },
		Starter:            func() retry.SubmissionStarter { return inputs.Starter },
		DispatchTimer: func(key string, seq uint64) {
			inputs.RunAsync(func() {
				_, _ = dispatchInput(inputs.RuntimeDeps.currentBackend(), application.RetryTimerFired{Frontend: identity.FrontendID(inputs.FrontendID), SessionKey: identity.SessionKey(key), Sequence: seq})
			})
		},
		Presenter: inputs.Presenter,
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
