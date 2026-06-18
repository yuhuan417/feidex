package app

import (
	"feidex/internal/config"
	"feidex/internal/feishu"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

func (s modelConfigService) renderClaudeModelConfigCard(sessionKey, menuAction string) map[string]any {
	return s.inner.RenderClaudeModelConfigCard(sessionKey, menuAction)
}

func (s modelConfigService) ensureClaudeRuntimeConfigChangeSafe() error {
	return s.inner.EnsureClaudeRuntimeConfigChangeSafe()
}

func (s modelConfigService) updateClaudeModelConfig(mutate func(*config.ClaudeConfig)) error {
	return s.inner.UpdateClaudeModelConfig(mutate)
}

func (s modelConfigService) hotApplyClaudeModelToCurrentSession(sessionKey, model string) (bool, error) {
	return s.inner.HotApplyClaudeModelToCurrentSession(sessionKey, model)
}

func (s modelConfigService) hotApplyClaudeEffortToCurrentSession(sessionKey, effort string) (bool, error) {
	return s.inner.HotApplyClaudeEffortToCurrentSession(sessionKey, effort)
}

func (s modelConfigService) completeClaudeModelSet(action *feishu.CardAction, modelID string) (*callback.CardActionTriggerResponse, error) {
	return s.inner.CompleteClaudeModelSet(action, modelID)
}

func (s modelConfigService) completeClaudeModelOptionAdd(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	return s.inner.CompleteClaudeModelOptionAdd(action)
}

func (s modelConfigService) completeClaudeModelOptionRemove(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	return s.inner.CompleteClaudeModelOptionRemove(action)
}

func (s modelConfigService) completeClaudeEffortSet(action *feishu.CardAction, effort string) (*callback.CardActionTriggerResponse, error) {
	return s.inner.CompleteClaudeEffortSet(action, effort)
}

func (s modelConfigService) commandClaudeModel(msg *feishu.InboundMessage, args []string) error {
	return s.inner.CommandClaudeModel(msg, args)
}

func (s modelConfigService) commandEffort(msg *feishu.InboundMessage, args []string) error {
	return s.inner.CommandEffort(msg, args)
}
