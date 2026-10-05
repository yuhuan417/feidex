package feishuapp

import (
	"log/slog"
	"strings"

	"feidex/internal/feishu"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

func (s cardActionService) renderMenuNodeCard(actionName, sessionKey string) (map[string]any, bool) {
	actionName = nearestVisibleMenuAction(actionName, s.app.configView().configuredBackend())
	renderer := menuNodeRenderers()[actionName]
	if renderer == nil {
		return nil, false
	}
	return renderer(s.app, sessionKey)
}

func (s cardActionService) completeMenuCompact(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	messageID := ""
	userID := ""
	if action != nil {
		messageID = strings.TrimSpace(action.MessageID)
		userID = strings.TrimSpace(action.UserID)
	}
	if messageID == "" {
		return completeMenuCommand(s.app, action, sessionKey, "/compact", "menu.tools")
	}
	runAsync(&s.app.runtimeOwner.Lifecycle, s.app.asyncRunner, func() {
		card := renderCompactAcceptedCard(s.app.State(), sessionKey)
		if err := s.app.bindings.BackendActions.RunMenuCompactAction(action, sessionKey, s.app.bindings.Compaction); err != nil {
			card = renderCompactFailedCard(s.app.State(), sessionKey, err.Error())
		}
		patchMaintenanceCard(s.app.Context(), s.app.FrontendID(), newEffectRunner(s.app.runtimeOwner), messageID, card, "compact menu patch failed",
			"session_key", sessionKey,
			"message_id", messageID,
			"user_id", userID,
		)
	})
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "info", Content: "正在请求压缩当前线程上下文"},
		Card:  rawCard(renderCompactPreparingCard(s.app.State(), sessionKey)),
	}, nil
}

func (s cardActionService) completeMenuReview(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	if !menuActionVisibleForBackend("menu.review", s.app.configView().configuredBackend()) {
		return completeMenuCommand(s.app, action, sessionKey, "/review", "menu.tools")
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "info", Content: "已打开代码审查"},
		Card:  rawCard(s.app.bindings.ReviewCommands.RenderReviewMenuCard(sessionKey)),
	}, nil
}

func (s cardActionService) completeMenuModel(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return completeMenuCommand(s.app, action, sessionKey, "/model", "menu.group.model")
}

func (s cardActionService) completeMenuFast(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return completeMenuCommand(s.app, action, sessionKey, "/fast config", "menu.group.model")
}

func (s cardActionService) completeServiceTierSet(action *feishu.CardAction, sessionKey, threadID, serviceTier string) (*callback.CardActionTriggerResponse, error) {
	if _, err := setThreadServiceTier(s.app.bindings.ThreadSettings, sessionKey, threadID, serviceTier); err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已更新 service tier"},
		Card:  rawCard(renderServiceTierMenuCard(s.app.State(), sessionKey)),
	}, nil
}

func (s cardActionService) completeMenuUpgrade(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	sessionKey, _ := action.ActionValue["session_key"].(string)
	if action != nil && strings.TrimSpace(action.MessageID) != "" {
		messageID := strings.TrimSpace(action.MessageID)
		runAsync(&s.app.runtimeOwner.Lifecycle, s.app.asyncRunner, func() {
			_, card, err := runCommandFromCardAction(s.app, action, sessionKey, "/upgrade")
			if err != nil {
				slog.Warn("upgrade panel render failed",
					"session_key", sessionKey,
					"user_id", action.UserID,
					"message_id", messageID,
					"error", err,
				)
				card = s.app.bindings.Upgrades.RenderUpgradeFailedCard(sessionKey, err.Error())
			} else if card == nil {
				card = s.app.bindings.Upgrades.RenderUpgradeFailedCard(sessionKey, "升级命令没有返回卡片")
			}
			if err := patchCardEffect(s.app.Context(), newEffectRunner(s.app.runtimeOwner), s.app.FrontendID(), messageID, card); err != nil {
				slog.Warn("upgrade panel patch failed",
					"session_key", sessionKey,
					"user_id", action.UserID,
					"message_id", messageID,
					"error", err,
				)
			}
		})
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "info", Content: "正在检查可升级版本"},
			Card:  rawCard(s.app.bindings.Upgrades.RenderUpgradePreparingCard(sessionKey)),
		}, nil
	}
	return completeMenuCommand(s.app, action, sessionKey, "/upgrade", "menu.group.system")
}
