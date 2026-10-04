package feishuapp

import (
	feishuoutbound "feidex/internal/adapter/feishu/outbound"
	"feidex/internal/application"
	"feidex/internal/application/submission"
	"feidex/internal/domain/identity"

	"context"
	appstate "feidex/internal/adapter/storage/json/scoped"
	"fmt"
	"strings"
	"sync"
	"time"

	"feidex/internal/config"
	"feidex/internal/feishu"

	frontendruntime "feidex/internal/runtime"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

type App struct {
	cfg                 *config.Config
	cfgPath             string
	store               *state.Store
	frontendID          string
	frontendConfigIndex int
	configMu            sync.RWMutex
	sharedConfigMu      *sync.RWMutex
	feishu              FeishuClient
	started             time.Time
	runtimeOwner        *frontendruntime.FrontendOwner
	stateView           *appstate.Store
	bindings            *Bindings
	transport           FeishuClient
	asyncRunner         func(func())
	waitAsync           func()
}

func (a *App) configMutex() *sync.RWMutex {
	if a == nil {
		return nil
	}
	if a.sharedConfigMu != nil {
		return a.sharedConfigMu
	}
	return &a.configMu
}

// NewFeishuShell validates the composition output and creates the thin
// frontend entrypoint object. Runtime parts are attached by
// internal/composition after the shell is created.
func NewFeishuShell(scope frontendruntime.FrontendScope) (*App, error) {
	cfg, cfgPath, store, frontend := scope.Config, scope.ConfigPath, scope.Store, scope.Frontend
	if cfg == nil {
		return nil, fmt.Errorf("nil config")
	}
	if store == nil {
		return nil, fmt.Errorf("nil store")
	}
	backend := normalizeRuntimeBackend(frontend.Backend)
	feishuTransport, ok := scope.FeishuTransport.(FeishuClient)
	if !ok || feishuTransport == nil {
		return nil, fmt.Errorf("nil Feishu transport")
	}
	if scope.RuntimeOwner == nil {
		return nil, fmt.Errorf("frontend composition is incomplete")
	}
	owner := scope.RuntimeOwner
	owner.SetBackend(backend)
	app := &App{
		cfg:                 cfg,
		sharedConfigMu:      scope.ConfigMutex,
		cfgPath:             cfgPath,
		store:               store,
		frontendID:          strings.TrimSpace(frontend.ID),
		frontendConfigIndex: frontend.ConfigIndex,
		transport:           feishuTransport,
		feishu:              feishuTransport,
		started:             time.Now(),
		runtimeOwner:        owner,
	}
	return app, nil
}

func (a *App) Start(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	return (frontendruntime.FrontendGroup{Frontends: []frontendruntime.ManagedFrontend{a}}).Start(ctx)
}

func (a *App) beginLifecycle(ctx context.Context) {
	a.runtimeView().ensureRuntimeOwner().Lifecycle.Begin(ctx)
}

func (a *App) Stop(ctx context.Context) error {
	if a == nil {
		return nil
	}
	return a.runtimeOwner.Shutdown(ctx, func() {
		if a.feishu != nil {
			a.feishu.Stop()
		}
	})
}

// Context returns the application lifecycle context for background work.
// It is cancelled before runtime shutdown so external calls can stop promptly.
func (a *App) Context() context.Context {
	if a == nil {
		return context.Background()
	}
	return a.runtimeView().ensureRuntimeOwner().Lifecycle.Context()
}

func runAsync(a *App, fn func()) bool {
	if fn == nil {
		return false
	}
	return a.runtimeView().ensureRuntimeOwner().Lifecycle.Run(fn, a.asyncRunner)
}

func (a *App) HandleFeishuMessage(msg *feishu.InboundMessage) {
	if msg != nil {
		_, _ = dispatchInput(a.BackendRuntimeDeps(), application.MessageReceived{Frontend: identity.FrontendID(a.FrontendID()), Chat: identity.ChatRef{Type: identity.ChatType(msg.ChatType), ID: msg.ChatID}, Message: *msg})
	}
}

func (a *App) HandleFeishuRecall(recall *feishu.MessageRecall) {
	if recall != nil {
		_, _ = dispatchInput(a.BackendRuntimeDeps(), application.MessageRecalled{Frontend: identity.FrontendID(a.FrontendID()), MessageID: recall.MessageID, ChatID: recall.ChatID})
	}
}

func (a *App) HandleFeishuReaction(reaction *feishu.MessageReaction) {
	if reaction != nil {
		_, _ = dispatchInput(a.BackendRuntimeDeps(), application.MessageReacted{Frontend: identity.FrontendID(a.FrontendID()), MessageID: reaction.MessageID, ChatID: reaction.ChatID, UserID: reaction.UserID, EmojiType: reaction.EmojiType})
	}
}

func isStaleInboundMessage(started time.Time, msg *feishu.InboundMessage) bool {
	if msg == nil || msg.CreatedAt == 0 {
		return false
	}
	return msg.CreatedAt < started.Add(-30*time.Second).Unix()
}

func nonZero(values ...int64) int64 {
	for _, value := range values {
		if value != 0 {
			return value
		}
	}
	return 0
}

func (a *App) HandleCardAction(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	return dispatchCardAction(a, action)
}

func enqueueSubmission(a *App, msg *feishu.InboundMessage) error {
	return enqueueSubmissionWithSessionKey(a.bindings.Submissions, msg, a.configView().makeSessionKey(msg), false)
}

func enqueueSubmissionWithSessionKey(submissions *submission.SubmissionQueueService, msg *feishu.InboundMessage, sessionKey string, bindOnlyCurrentRoot bool) error {
	if err := submissions.EnqueueSubmission(msg, sessionKey, bindOnlyCurrentRoot); err != nil {
		return err
	}
	return nil
}

func startNextSubmission(submissions *submission.SubmissionQueueService, sessionKey string) error {
	return submissions.StartNextSubmission(sessionKey)
}

func replyErrorWith(contextFn func() context.Context, frontend identity.FrontendID, runner frontendruntime.EffectRunner, msg *feishu.InboundMessage, err error) error {
	if msg == nil || err == nil {
		return nil
	}
	ctx := context.Background()
	if contextFn != nil {
		ctx = contextFn()
	}
	return runner.Run(ctx, []application.Effect{application.SendMessage{Frontend: frontend, Chat: identity.ChatRef{ID: msg.ChatID, Type: identity.ChatType(msg.ChatType)}, ReplyMessageID: msg.MessageID, Text: "执行失败: " + err.Error()}})
}

func sendCommandMenu(a *App, msg *feishu.InboundMessage) error {
	sessionKey := a.configView().makeSessionKey(msg)
	card := renderCommandMenuCardData(a.configView().configuredBackend(), planModeTitleForSession(a.State(), a != nil, sessionKey, "主菜单"), a.feishu, sessionKey)
	return newEffectRunner(a.runtimeOwner).Run(context.Background(), []application.Effect{application.SendCard{
		Frontend:       identity.FrontendID(a.FrontendID()),
		Chat:           identity.ChatRef{ID: msg.ChatID, Type: identity.ChatType(msg.ChatType)},
		ReplyMessageID: msg.MessageID,
		View:           feishuoutbound.Card(card),
		InThread:       a.configView().replyInThreadEnabled(),
	}})
}
