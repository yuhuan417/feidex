package feishuapp

import (
	"context"
	"sync"

	"feidex/internal/adapter/feishu/modelconfig"
	"feidex/internal/config"
	"feidex/internal/feishu"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

func BuildModelCommands(app *App) modelconfig.ModelConfigService {
	return modelconfig.ModelConfigService{
		Defaults:    &app.bindings.ModelDefaults,
		Backend:     func() string { return configuredBackend(app) },
		Options:     &app.bindings.ModelOptions,
		GetConfig:   func() *config.Config { return app.cfg },
		GetConfigMu: func() *sync.RWMutex { return app.ConfigMu() },
		ReplyText: func(ctx context.Context, msgID string, text string, replyInThread bool) error {
			return replyTextByAnchorEffect(ctx, app, msgID, text, replyInThread)
		},
		ReplyCard: func(ctx context.Context, msgID string, card map[string]any, replyInThread bool) (string, error) {
			return replyCardWithIDEffect(ctx, app, msgID, card, replyInThread)
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
		MenuBackAction:    menuBackAction,
		FormatMenuBody:    menuCardBody,
		ModelConfigStatus: func(sessionKey string) string { return modelConfigStatus(app, sessionKey) },
		ReplyCommandActionResponse: func(msg *feishu.InboundMessage, resp *callback.CardActionTriggerResponse) error {
			return replyCommandActionResponse(app, msg, resp)
		},
	}
}

func sessionScopedConfigForApp(a *App, sessionKey string) *config.Config {
	if a == nil || a.cfg == nil || !p2pSessionScopeActive(a, sessionKey) {
		return nil
	}
	clone := modelConfigReadCopy(a)
	sess := a.State().Session(normalizeSessionKey(a, sessionKey))
	values := a.bindings.ModelSnapshots.Auxiliary(sess)
	main := a.bindings.ModelSnapshots.Desired(configuredBackend(a), sess)
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
