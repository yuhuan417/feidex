package app

import (
	"context"
	"sync"

	"feidex/internal/app/modelconfig"
	"feidex/internal/codexrpc"
	"feidex/internal/config"
	"feidex/internal/feishu"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

type modelConfigService struct {
	inner modelconfig.ModelConfigService
}

func newModelConfigService(app *App) modelConfigService {
	return modelConfigService{
		inner: modelconfig.ModelConfigService{
			GetConfig:   func() *config.Config { return app.cfg },
			GetCfgPath:  func() string { return app.cfgPath },
			GetConfigMu: func() *sync.RWMutex { return &app.configMu },
			ReplyText: func(ctx context.Context, msgID string, text string, replyInThread bool) error {
				return app.feishu.ReplyText(ctx, msgID, text, replyInThread)
			},
			ReplyCard: func(ctx context.Context, msgID string, card map[string]any, replyInThread bool) (string, error) {
				return app.feishu.ReplyCard(ctx, msgID, card, replyInThread)
			},
			UpdateClaudeConfig: func(cfg config.ClaudeConfig) {
				if app.claude != nil {
					app.claude.UpdateConfig(cfg)
				}
			},
			ClaudeSetModel: func(ctx context.Context, sessionKey, model string) (bool, error) {
				if app.claude == nil {
					return false, nil
				}
				return app.claude.SetModel(ctx, sessionKey, model)
			},
			ClaudeSetEffort: func(ctx context.Context, sessionKey, effort string) (bool, error) {
				if app.claude == nil {
					return false, nil
				}
				return app.claude.SetEffort(ctx, sessionKey, effort)
			},
			IsClaudeAvailable: func() bool {
				return app.claude != nil
			},
			RequireCodexClient: func() (modelconfig.CodexClient, error) {
				return requireCodexClient(app)
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
			CompleteGlobalModelSet: func(action *feishu.CardAction, modelID string) (*callback.CardActionTriggerResponse, error) {
				return newBackendConfigurationService(app).completeGlobalModelSet(action, modelID)
			},
			CompleteGlobalReasoningEffortSet: func(action *feishu.CardAction, effort string) (*callback.CardActionTriggerResponse, error) {
				return newBackendConfigurationService(app).completeGlobalReasoningEffortSet(action, effort)
			},
			HandleBackendModelCommand: func(msg *feishu.InboundMessage, args []string) error {
				return newBackendConfigurationService(app).handleBackendModelCommand(msg, args)
			},
			FormatMenuBody: menuCardBody,
			FrontendIdleBlockedReason: func() string {
				return frontendIdleBlockedReason(app)
			},
			FrontendIdleBlockedReasonIgnoringCurrentMessage: func() string {
				return frontendIdleBlockedReasonIgnoringCurrentMessage(app)
			},
			ReplyCommandActionResponse: func(msg *feishu.InboundMessage, resp *callback.CardActionTriggerResponse) error {
				return replyCommandActionResponse(app, msg, resp)
			},
		},
	}
}

func (s modelConfigService) fetchModelList(ctx context.Context) (codexrpc.ModelListResult, error) {
	return s.inner.FetchModelList(ctx)
}

func (s modelConfigService) fetchPlanCollaborationModePreset(ctx context.Context) (*codexrpc.CollaborationModeMask, error) {
	return s.inner.FetchPlanCollaborationModePreset(ctx)
}

func (s modelConfigService) renderModelConfigCard(result codexrpc.ModelListResult, planPreset *codexrpc.CollaborationModeMask, sessionKey, menuAction string) map[string]any {
	return s.inner.RenderModelConfigCard(result, planPreset, sessionKey, menuAction)
}

func (s modelConfigService) updateGlobalModelConfig(mutate func(*config.CodexConfig), result codexrpc.ModelListResult) error {
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
