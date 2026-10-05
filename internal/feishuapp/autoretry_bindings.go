package feishuapp

import (
	"context"
	"sync"

	retryview "feidex/internal/adapter/feishu/autoretry"
	appbackend "feidex/internal/adapter/feishu/backend"
	appstate "feidex/internal/adapter/storage/json/scoped"
	"feidex/internal/application"
	retry "feidex/internal/application/autoretry"
	"feidex/internal/config"
	"feidex/internal/domain/identity"
	"feidex/internal/feishu"
	frontendruntime "feidex/internal/runtime"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

type AutoRetryViewInputs struct {
	Context             func() context.Context
	Config              *config.Config
	ConfigMu            *sync.RWMutex
	ConfiguredBackend   func() string
	FrontendID          string
	FrontendConfigIndex int
	BackendDriver       appbackend.Driver
	EffectRunner        frontendruntime.EffectRunner
	Feishu              FeishuClient
}

func AutoRetryView(inputs AutoRetryViewInputs) retryview.Service {
	configView := frontendConfigView{
		cfg: inputs.Config, mu: inputs.ConfigMu,
		frontendID: inputs.FrontendID, frontendConfigIndex: inputs.FrontendConfigIndex,
	}
	service := retryview.Service{
		Context: inputs.Context, Outbound: newEffectOutbound(inputs.FrontendID, inputs.EffectRunner), Renderer: simpleStatusCardRenderer{client: inputs.Feishu}, MenuBody: menuCardBody,
		Settings: func() retryview.Settings {
			cfg := configView.feishuConfig()
			backend, title := "", ""
			if inputs.ConfiguredBackend != nil {
				backend = inputs.ConfiguredBackend()
			}
			if inputs.BackendDriver != nil {
				title = inputs.BackendDriver.Runtime().AutoRetryTitle()
			}
			return retryview.Settings{FrontendID: inputs.FrontendID, Backend: backend, Title: title, Enabled: cfg != nil && cfg.AutoRetry}
		},
		SessionKey: configView.makeSessionKey,
		ReplyAction: func(msg *feishu.InboundMessage, resp *callback.CardActionTriggerResponse) error {
			return replyCommandActionResponseWith(inputs.EffectRunner, inputs.FrontendID, configView.replyInThreadEnabled(), msg, resp)
		},
	}
	return service
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
