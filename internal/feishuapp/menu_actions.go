package feishuapp

import (
	"log/slog"
	"strings"

	"feidex/internal/adapter/feishu/quietmode"
	"feidex/internal/config"
	"feidex/internal/feishu"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

func (s cardActionService) completeMenuRoot(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "info", Content: "已返回命令菜单"},
		Card:  rawCard(renderCommandMenuCard(s.app, sessionKey)),
	}, nil
}

func (s cardActionService) completeMenuTools(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "info", Content: "已打开常用工具"},
		Card:  rawCard(renderToolsMenuCard(s.app, sessionKey)),
	}, nil
}

func (s cardActionService) completeMenuGroupModel(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	// 直接打开模型配置界面，而不是显示中间菜单
	return s.completeMenuModel(action, sessionKey)
}

func (s cardActionService) completeMenuGroupSystem(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "info", Content: "已打开 system"},
		Card:  rawCard(renderSystemMenuCard(s.app, sessionKey)),
	}, nil
}

func (s cardActionService) completeMenuBackendSwitch(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "info", Content: "已打开切换后端"},
		Card:  rawCard(s.app.bindings.BackendSelection.RenderBackendSelectionCard(sessionKey, "")),
	}, nil
}

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
	runAsync(s.app, func() {
		card := renderCompactAcceptedCard(s.app.State(), sessionKey)
		if err := runMenuCompactAction(s.app, action, sessionKey); err != nil {
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

func (s cardActionService) completeMenuQuiet(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return completeMenuCommand(s.app, action, sessionKey, "/quiet config", "menu.tools")
}

func (s cardActionService) completeMenuUsage(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return completeMenuCommand(s.app, action, sessionKey, "/usage", "menu.tools")
}

func (s cardActionService) completeQuietSet(action *feishu.CardAction, mode config.QuietMode) (*callback.CardActionTriggerResponse, error) {
	sessionKey, _ := action.ActionValue["session_key"].(string)
	if err := updateQuietMode(s.app.bindings.RuntimeSettings, mode); err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: err.Error()}}, nil
	}
	configView := s.app.configView()
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已更新 quiet 模式为 " + quietmode.StatusText(mode)},
		Card: rawCard(renderQuietModeMenuCard(
			quietmode.Mode(configView.feishuConfig()), sessionKey,
			planModeTitleForSession(s.app, sessionKey, "Quiet Mode"), s.app.feishu,
		)),
	}, nil
}

func (s cardActionService) completeMenuModel(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return completeMenuCommand(s.app, action, sessionKey, "/model", "menu.group.model")
}

func (s cardActionService) completeMenuStatus(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return completeMenuCommand(s.app, action, sessionKey, "/status", "menu.group.system")
}

func (s cardActionService) completeMenuHelp(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return completeMenuCommand(s.app, action, sessionKey, "/help", "menu.group.system")
}

func (s cardActionService) completeMenuHistory(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return completeMenuCommand(s.app, action, sessionKey, "/history", "menu.tools")
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
		runAsync(s.app, func() {
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
			if err := patchCardEffect(s.app.Context(), s.app, messageID, card); err != nil {
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

func (s cardActionService) completeUpgradeDev(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	sessionKey := actionSessionKey(action)
	return completeAsyncCommandAction(s.app,
		action,
		sessionKey,
		"/upgrade dev",
		"menu.group.system",
		"正在检查开发版升级信息",
		s.app.bindings.Upgrades.RenderUpgradePreparingCard(sessionKey),
		nil,
		s.app.bindings.Upgrades.RenderUpgradeFailedCard,
		"upgrade dev patch failed",
	)
}
