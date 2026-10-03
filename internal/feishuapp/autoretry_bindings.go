package feishuapp

import (
	"context"
	retryview "feidex/internal/adapter/feishu/autoretry"
	"feidex/internal/application"
	retry "feidex/internal/application/autoretry"
	"feidex/internal/config"
	"feidex/internal/domain/conversation"
	"feidex/internal/domain/identity"
	"feidex/internal/feishu"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

type autoRetryOutbound struct{ app *App }

func (o autoRetryOutbound) PatchCard(ctx context.Context, messageID string, card map[string]any) error {
	return patchCardEffect(ctx, o.app, messageID, card)
}
func (o autoRetryOutbound) ReplyCard(ctx context.Context, messageID string, card map[string]any, inThread bool) (string, error) {
	return replyCardWithIDEffect(ctx, o.app, messageID, card, inThread)
}
func (o autoRetryOutbound) SendCard(ctx context.Context, chatID string, card map[string]any) (string, error) {
	return sendCardWithIDEffect(ctx, o.app, chatID, card)
}

type autoRetryCardRenderer struct{ app *App }

func (r autoRetryCardRenderer) SimpleStatusCard(title, color, body string, buttons []feishu.Button) map[string]any {
	if r.app == nil || r.app.feishu == nil {
		return nil
	}
	return r.app.feishu.SimpleStatusCard(title, color, body, buttons)
}

func AutoRetryView(a *App) retryview.Service {
	view := retryview.Service{
		Context: a.Context, Outbound: autoRetryOutbound{app: a}, Renderer: autoRetryCardRenderer{app: a}, MenuBody: menuCardBody,
		Settings: func() retryview.Settings {
			cfg := feishuConfig(a)
			return retryview.Settings{FrontendID: a.FrontendID(), Backend: configuredBackend(a), Title: a.BackendDriver().Runtime().AutoRetryTitle(), Enabled: cfg != nil && cfg.AutoRetry}
		},
		SessionKey: func(msg *feishu.InboundMessage) string { return makeSessionKey(a, msg) },
		ReplyAction: func(msg *feishu.InboundMessage, resp *callback.CardActionTriggerResponse) error {
			return replyCommandActionResponse(a, msg, resp)
		},
	}
	return view
}

func AutoRetryPorts(a *App, view retryview.Service) retry.Dependencies {
	return retry.Dependencies{
		Context: a.Context, Tracker: a.AutoRetries(), Repository: a.State(), Live: sqLiveThreadAdapter{app: a},
		Enabled:     func() bool { return view.Settings().Enabled },
		SaveEnabled: a.bindings.RuntimeSettings.SetAutoRetry,
		Recovering: func() bool {
			runtime := backendRuntime(a)
			return runtime != nil && runtime.DeferQueuedSubmissionsDuringRecovery(backendRuntimeContextForApp(a))
		},
		DefaultWorkspaceID: func() string { return defaultWorkspaceID(a) },
		Workspace:          func(id string) *config.Workspace { return config.FindWorkspace(a.cfg, id) },
		Starter:            func() retry.SubmissionStarter { return a.bindings.Submissions },
		DispatchTimer: func(key string, seq uint64) {
			runAsync(a, func() {
				_, _ = dispatchInput(a, application.RetryTimerFired{Frontend: identity.FrontendID(a.FrontendID()), SessionKey: identity.SessionKey(key), Sequence: seq})
			})
		},
		Presenter: view,
	}
}

func (a *App) AutoRetries() *retry.Tracker {
	if a == nil {
		return nil
	}
	owner := ensureRuntimeOwner(a)
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
