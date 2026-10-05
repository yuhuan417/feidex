package feishuapp

import (
	"context"
	"strings"
	"sync"

	"feidex/internal/adapter/feishu/modelconfig"
	appstate "feidex/internal/adapter/storage/json/scoped"
	applicationmodelconfig "feidex/internal/application/modelconfig"
	"feidex/internal/config"
	"feidex/internal/domain/conversation"
	"feidex/internal/feishu"
	"feidex/internal/textutil"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

func BuildModelCommands(app *App) modelconfig.ModelConfigService {
	statusSnapshots := app.bindings.ModelSnapshots
	statusStore := app.State()
	statusConfig := app.cfg
	statusConfigMu := app.ConfigMu()
	statusBackend := app.runtimeOwner.Backend
	statusFrontendID := app.frontendID
	statusFrontendConfigIndex := app.frontendConfigIndex
	replyRunner := newEffectRunner(app.runtimeOwner)
	replyInThread := app.configView().replyInThreadEnabled()
	sessionConfig := sessionModelConfigSource{
		cfg: app.cfg, configMu: app.ConfigMu(), store: app.State(), snapshots: app.bindings.ModelSnapshots,
		view:    frontendConfigView{cfg: app.cfg, mu: app.ConfigMu(), frontendID: app.frontendID, frontendConfigIndex: app.frontendConfigIndex},
		backend: ConfiguredBackendBuilder(app.cfg, app.ConfigMu(), app.runtimeOwner.Backend, app.frontendID, app.frontendConfigIndex),
	}
	return modelconfig.ModelConfigService{
		Defaults:    &app.bindings.ModelDefaults,
		Backend:     func() string { return app.configView().configuredBackend() },
		Options:     &app.bindings.ModelOptions,
		GetConfig:   func() *config.Config { return app.cfg },
		GetConfigMu: func() *sync.RWMutex { return app.ConfigMu() },
		ReplyText: func(ctx context.Context, msgID string, text string, replyInThread bool) error {
			return newEffectOutbound(app.FrontendID(), newEffectRunner(app.runtimeOwner)).ReplyText(ctx, msgID, text, replyInThread)
		},
		ReplyCard: func(ctx context.Context, msgID string, card map[string]any, replyInThread bool) (string, error) {
			return replyCardWithIDEffect(ctx, replyRunner, statusFrontendID, msgID, card, replyInThread)
		},
		RequireCodexClient: func() (modelconfig.CodexClient, error) {
			return runtimeViewOf(app.runtimeOwner).requireCodexGateway()
		},
		MakeSessionKey: func(msg *feishu.InboundMessage) string {
			return app.configView().makeSessionKey(msg)
		},
		NormalizeSessionKey: func(sessionKey string) string {
			return app.configView().normalizeSessionKey(sessionKey)
		},
		SessionBelongsToFrontend: func(sessionKey string) bool {
			return app.configView().sessionBelongsToFrontend(sessionKey)
		},
		ReplyInThreadEnabled: func(chatType string) bool {
			return app.configView().replyInThreadEnabled()
		},
		SessionConfig:  sessionConfig.ForSession,
		MenuBackAction: menuBackAction,
		FormatMenuBody: menuCardBody,
		ModelConfigStatus: func(sessionKey string) string {
			view := frontendConfigView{
				cfg: statusConfig, mu: statusConfigMu, backend: statusBackend(),
				frontendID: statusFrontendID, frontendConfigIndex: statusFrontendConfigIndex,
			}
			return modelConfigStatus(statusSnapshots, statusStore, view, sessionKey)
		},
		ReplyCommandActionResponse: func(msg *feishu.InboundMessage, resp *callback.CardActionTriggerResponse) error {
			return replyCommandActionResponseWith(replyRunner, app.frontendID, replyInThread, msg, resp)
		},
	}
}

type sessionModelConfigSource struct {
	cfg       *config.Config
	configMu  *sync.RWMutex
	store     *appstate.Store
	snapshots applicationmodelconfig.SnapshotService
	view      frontendConfigView
	backend   func() string
}

func (s sessionModelConfigSource) ForSession(sessionKey string) *config.Config {
	view := s.view
	if s.backend != nil {
		view.backend = s.backend()
	}
	if s.cfg == nil || !p2pSessionScopeActiveForConfig(view, s.store, sessionKey) {
		return nil
	}
	clone := configReadCopy(s.cfg, s.configMu)
	var sess *conversation.Session
	if s.store != nil {
		sess = s.store.Session(view.normalizeSessionKey(sessionKey))
	}
	values := s.snapshots.Auxiliary(sess)
	main := s.snapshots.Desired(view.configuredBackend(), sess)
	if main.Backend == "claude" {
		clone.Claude.Model, clone.Claude.Effort = main.Model, main.Effort
	} else {
		clone.Codex.Model, clone.Codex.ReasoningEffort = main.Model, main.Effort
	}
	clone.Codex.PlanModel, clone.Codex.PlanReasoningEffort = values.PlanModel, values.PlanEffort
	clone.Codex.ReviewModel, clone.Codex.SubagentModel, clone.Codex.SubagentReasoningEffort = values.ReviewModel, values.SubagentModel, values.SubagentEffort
	clone.Claude.SmallModel, clone.Claude.SubagentModel = values.ClaudeSmallModel, values.ClaudeSubagent
	return clone
}

func p2pSessionScopeActiveForConfig(view frontendConfigView, store *appstate.Store, sessionKey string) bool {
	chatType, chatID := sessionKeyChat(sessionKey)
	if store != nil {
		for _, key := range []string{strings.TrimSpace(sessionKey), view.normalizeSessionKey(sessionKey)} {
			if sess := store.Session(key); sess != nil {
				chatType = textutil.FirstNonEmpty(chatType, strings.TrimSpace(sess.ChatType))
				chatID = textutil.FirstNonEmpty(chatID, strings.TrimSpace(sess.ChatID))
			}
		}
	}
	return strings.TrimSpace(chatID) != "" && strings.EqualFold(strings.TrimSpace(chatType), "p2p")
}
