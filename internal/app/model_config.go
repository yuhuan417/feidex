package app

import (
	"context"
	catalog "feidex/internal/domain/modelconfig"
	"feidex/internal/textutil"
	"sync"
	"time"

	"feidex/internal/app/modelconfig"
	"feidex/internal/config"
	"feidex/internal/feishu"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

type modelConfigService struct {
	inner modelconfig.ModelConfigService
	app   *App
}

func newModelConfigService(app *App) modelConfigService {
	return modelConfigService{
		app: app,
		inner: modelconfig.ModelConfigService{
			Backend:     func() string { return configuredBackend(app) },
			GetConfig:   func() *config.Config { return app.cfg },
			GetCfgPath:  func() string { return app.cfgPath },
			GetConfigMu: func() *sync.RWMutex { return app.ConfigMu() },
			ReplyText: func(ctx context.Context, msgID string, text string, replyInThread bool) error {
				return replyTextByAnchorEffect(ctx, app, msgID, text, replyInThread)
			},
			ReplyCard: func(ctx context.Context, msgID string, card map[string]any, replyInThread bool) (string, error) {
				return replyCardWithIDEffect(ctx, app, msgID, card, replyInThread)
			},
			UpdateClaudeConfig: func(cfg config.ClaudeConfig) {
				if currentClaudeCore(app) != nil {
					currentClaudeCore(app).UpdateConfig(cfg)
				}
			},
			IsClaudeAvailable: func() bool {
				return currentClaudeCore(app) != nil
			},
			RequireCodexClient: func() (modelconfig.CodexClient, error) {
				return requireCodexGateway(app)
			},
			MakeSessionKey: func(msg *feishu.InboundMessage) string {
				return makeSessionKey(app, msg)
			},
			NormalizeSessionKey: func(sessionKey string) string {
				return normalizeSessionKey(app, sessionKey)
			},
			SessionBelongsToFrontend: func(sessionKey string) bool {
				return sessionBelongsToFrontend(app, sessionKey)
			},
			ReplyInThreadEnabled: func(chatType string) bool {
				return replyInThreadEnabled(app, chatType)
			},
			SessionConfig: func(sessionKey string) *config.Config {
				return sessionScopedConfigForApp(app, sessionKey)
			},
			MenuBackAction:           menuBackAction,
			FormatMenuBody:           menuCardBody,
			ModelConfigBlockedReason: func() string { return modelConfigBlockedReason(app) },
			ModelConfigStatus:        func(sessionKey string) string { return modelConfigStatus(app, sessionKey) },
			ReplyCommandActionResponse: func(msg *feishu.InboundMessage, resp *callback.CardActionTriggerResponse) error {
				return replyCommandActionResponse(app, msg, resp)
			},
		},
	}
}

func (s modelConfigService) fetchModelList(ctx context.Context) (catalog.ModelListResult, error) {
	return s.inner.FetchModelList(ctx)
}

func (s modelConfigService) fetchPlanCollaborationModePreset(ctx context.Context) (*catalog.CollaborationModeMask, error) {
	return s.inner.FetchPlanCollaborationModePreset(ctx)
}

func (s modelConfigService) renderModelConfigCard(result catalog.ModelListResult, planPreset *catalog.CollaborationModeMask, sessionKey, menuAction string) map[string]any {
	return s.inner.RenderModelConfigCard(result, planPreset, sessionKey, menuAction)
}

func (s modelConfigService) renderCodexAuxiliaryModelConfigCard(result catalog.ModelListResult, planPreset *catalog.CollaborationModeMask, sessionKey, menuAction string) map[string]any {
	service := s.inner
	if cfg := s.auxiliaryConfigForSession(sessionKey); cfg != nil {
		service.GetConfig = func() *config.Config { return cfg }
	}
	return service.RenderCodexAuxiliaryModelConfigCard(result, planPreset, sessionKey, menuAction)
}

func (s modelConfigService) renderCodexAuxiliaryModelConfigCardForSession(sessionKey, menuAction string) (map[string]any, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	result, err := s.fetchModelList(ctx)
	if err != nil {
		return nil, err
	}
	preset, _ := s.fetchPlanCollaborationModePreset(ctx)
	return s.renderCodexAuxiliaryModelConfigCard(result, preset, sessionKey, menuAction), nil
}

func (s modelConfigService) renderClaudeAuxiliaryModelConfigCard(sessionKey, menuAction string) map[string]any {
	service := s.inner
	if cfg := s.auxiliaryConfigForSession(sessionKey); cfg != nil {
		service.GetConfig = func() *config.Config { return cfg }
	}
	return service.RenderClaudeAuxiliaryModelConfigCard(sessionKey, menuAction)
}

func (s modelConfigService) auxiliaryConfigForSession(sessionKey string) *config.Config {
	return sessionScopedConfigForApp(s.app, sessionKey)
}

// sessionScopedConfigForApp clones the global config with session- and
// profile-level overrides merged in. It returns nil for sessions that are not
// scoped to a p2p frontend, or when the app has no config yet.
func sessionScopedConfigForApp(a *App, sessionKey string) *config.Config {
	if a == nil || a.cfg == nil || !p2pSessionScopeActive(a, sessionKey) {
		return nil
	}
	clone := *modelConfigReadCopy(a)
	if profile := a.State().BotProfile(); profile != nil {
		clone.Codex.PlanModel = textutil.FirstNonEmpty(profile.PlanModel, clone.Codex.PlanModel)
		clone.Codex.PlanReasoningEffort = textutil.FirstNonEmpty(profile.PlanReasoningEffort, clone.Codex.PlanReasoningEffort)
		clone.Codex.ReviewModel = textutil.FirstNonEmpty(profile.ReviewModel, clone.Codex.ReviewModel)
		clone.Codex.SubagentModel = textutil.FirstNonEmpty(profile.SubagentModel, clone.Codex.SubagentModel)
		clone.Codex.SubagentReasoningEffort = textutil.FirstNonEmpty(profile.SubagentReasoningEffort, clone.Codex.SubagentReasoningEffort)
		clone.Claude.SmallModel = textutil.FirstNonEmpty(profile.ClaudeSmallModel, clone.Claude.SmallModel)
		clone.Claude.SubagentModel = textutil.FirstNonEmpty(profile.ClaudeSubagentModel, clone.Claude.SubagentModel)
	}
	if sess := a.State().Session(normalizeSessionKey(a, sessionKey)); sess != nil {
		clone.Codex.PlanModel = textutil.FirstNonEmpty(sess.PlanModelOverride, clone.Codex.PlanModel)
		clone.Codex.PlanReasoningEffort = textutil.FirstNonEmpty(sess.PlanReasoningEffortOverride, clone.Codex.PlanReasoningEffort)
		clone.Codex.ReviewModel = textutil.FirstNonEmpty(sess.ReviewModelOverride, clone.Codex.ReviewModel)
		clone.Codex.SubagentModel = textutil.FirstNonEmpty(sess.SubagentModelOverride, clone.Codex.SubagentModel)
		clone.Codex.SubagentReasoningEffort = textutil.FirstNonEmpty(sess.SubagentReasoningEffortOverride, clone.Codex.SubagentReasoningEffort)
		clone.Claude.SmallModel = textutil.FirstNonEmpty(sess.SmallModelOverride, clone.Claude.SmallModel)
		clone.Claude.SubagentModel = textutil.FirstNonEmpty(sess.SubagentModelOverride, clone.Claude.SubagentModel)
	}
	return &clone
}

func (s modelConfigService) completeCodexAuxiliaryModelSet(action *feishu.CardAction, role, value string) (*callback.CardActionTriggerResponse, error) {
	return s.inner.CompleteCodexAuxiliaryModelSet(action, role, value)
}

func (s modelConfigService) completeClaudeAuxiliaryModelSet(action *feishu.CardAction, role, value string) (*callback.CardActionTriggerResponse, error) {
	return s.inner.CompleteClaudeAuxiliaryModelSet(action, role, value)
}

func (s modelConfigService) updateGlobalModelConfig(mutate func(*config.CodexConfig), result catalog.ModelListResult) error {
	return s.inner.UpdateGlobalModelConfig(mutate, result)
}

func (s modelConfigService) completeCodexPlanModelSet(action *feishu.CardAction, modelID string) (*callback.CardActionTriggerResponse, error) {
	return s.inner.CompleteCodexPlanModelSet(action, modelID)
}

func (s modelConfigService) completeCodexPlanReasoningEffortSet(action *feishu.CardAction, reasoningEffort string) (*callback.CardActionTriggerResponse, error) {
	return s.inner.CompleteCodexPlanReasoningEffortSet(action, reasoningEffort)
}

func (s modelConfigService) commandCodexModel(msg *feishu.InboundMessage, args []string) error {
	return s.inner.CommandCodexModel(msg, args)
}
