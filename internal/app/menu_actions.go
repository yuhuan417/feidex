package app

import (
	"context"
	"log/slog"
	"strings"

	"feidex/internal/config"
	"feidex/internal/feishu"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

type menuActionService struct {
	app *App
}

func newMenuActionService(app *App) menuActionService {
	return menuActionService{app: app}
}

func (s menuActionService) completeMenuRoot(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "info", Content: "已返回命令菜单"},
		Card:  rawCard(renderCommandMenuCard(s.app, sessionKey)),
	}, nil
}

func (s menuActionService) completeMenuTools(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "info", Content: "已打开常用工具"},
		Card:  rawCard(renderToolsMenuCard(s.app, sessionKey)),
	}, nil
}

func (s menuActionService) completeMenuGroupModel(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "info", Content: "已打开 model"},
		Card:  rawCard(newBackendConfigurationService(s.app).renderModelMenuCard(sessionKey)),
	}, nil
}

func (s menuActionService) completeMenuGroupSystem(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "info", Content: "已打开 system"},
		Card:  rawCard(renderSystemMenuCard(s.app, sessionKey)),
	}, nil
}

func (s menuActionService) completeMenuBackendSwitch(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "info", Content: "已打开切换后端"},
		Card:  rawCard(newBackendSelectionService(s.app).renderBackendSelectionCard(sessionKey, "")),
	}, nil
}

func (s menuActionService) renderMenuNodeCard(actionName, sessionKey string) (map[string]any, bool) {
	actionName = nearestVisibleMenuAction(actionName, configuredBackend(s.app))
	renderer := menuNodeRenderers()[actionName]
	if renderer == nil {
		return nil, false
	}
	return renderer(s.app, sessionKey)
}

func (s menuActionService) completeMenuCompact(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
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
		card := renderCompactAcceptedCard(s.app, sessionKey)
		if err := runMenuCompactAction(s.app, action, sessionKey); err != nil {
			card = renderCompactFailedCard(s.app, sessionKey, err.Error())
		}
		patchMaintenanceCard(s.app, messageID, card, "compact menu patch failed",
			"session_key", sessionKey,
			"message_id", messageID,
			"user_id", userID,
		)
	})
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "info", Content: "正在请求压缩当前线程上下文"},
		Card:  rawCard(renderCompactPreparingCard(s.app, sessionKey)),
	}, nil
}

func (s menuActionService) completeMenuReview(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	if !menuActionVisibleForBackend("menu.review", configuredBackend(s.app)) {
		return completeMenuCommand(s.app, action, sessionKey, "/review", "menu.tools")
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "info", Content: "已打开代码审查"},
		Card:  rawCard(newReviewFormServiceInner(s.app).RenderReviewMenuCard(sessionKey)),
	}, nil
}

func (s menuActionService) completeMenuQuiet(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return completeMenuCommand(s.app, action, sessionKey, "/quiet config", "menu.tools")
}

func (s menuActionService) completeMenuUsage(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return completeMenuCommand(s.app, action, sessionKey, "/usage", "menu.tools")
}

func (s menuActionService) completeMenuSkills(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return completeMenuCommand(s.app, action, sessionKey, "/skills", "menu.tools")
}

func (s menuActionService) completeQuietSet(action *feishu.CardAction, mode config.QuietMode) (*callback.CardActionTriggerResponse, error) {
	sessionKey, _ := action.ActionValue["session_key"].(string)
	if err := updateQuietMode(s.app, mode); err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: err.Error()}}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已更新 quiet 模式为 " + quietModeStatusText(mode)},
		Card:  rawCard(renderQuietModeMenuCard(s.app, sessionKey)),
	}, nil
}

func (s menuActionService) completeMenuModel(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return completeMenuCommand(s.app, action, sessionKey, "/model", "menu.group.model")
}

func (s menuActionService) completeMenuStatus(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return completeMenuCommand(s.app, action, sessionKey, "/status", "menu.group.system")
}

func (s menuActionService) completeMenuHelp(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return completeMenuCommand(s.app, action, sessionKey, "/help", "menu.group.system")
}

func (s menuActionService) completeMenuHistory(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return completeMenuCommand(s.app, action, sessionKey, "/history", "menu.tools")
}

func (s menuActionService) completeHistoryPage(action *feishu.CardAction, sessionKey string, page int) (*callback.CardActionTriggerResponse, error) {
	card, err := newHistoryServiceInner(s.app).RenderHistoryCard(sessionKey, page)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	return &callback.CardActionTriggerResponse{
		Card: rawCard(card),
	}, nil
}

func (s menuActionService) completeHistoryDetail(action *feishu.CardAction, sessionKey string, index int) (*callback.CardActionTriggerResponse, error) {
	card, err := newHistoryServiceInner(s.app).RenderHistoryDetailCard(sessionKey, index)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	return &callback.CardActionTriggerResponse{
		Card: rawCard(card),
	}, nil
}

func (s menuActionService) completeMenuFast(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return completeMenuCommand(s.app, action, sessionKey, "/fast config", "menu.group.model")
}

func (s menuActionService) completeServiceTierSet(action *feishu.CardAction, sessionKey, threadID, serviceTier string) (*callback.CardActionTriggerResponse, error) {
	if _, err := setThreadServiceTier(s.app, sessionKey, threadID, serviceTier); err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已更新 service tier"},
		Card:  rawCard(renderServiceTierMenuCard(s.app, sessionKey)),
	}, nil
}

func (s menuActionService) completeMenuUpgrade(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	sessionKey, _ := action.ActionValue["session_key"].(string)
	if action != nil && strings.TrimSpace(action.MessageID) != "" {
		messageID := strings.TrimSpace(action.MessageID)
		go func() {
			_, card, err := runCommandFromCardAction(s.app, action, sessionKey, "/upgrade")
			if err != nil {
				slog.Warn("upgrade panel render failed",
					"session_key", sessionKey,
					"user_id", action.UserID,
					"message_id", messageID,
					"error", err,
				)
				card = newUpgradeServiceInner(s.app).RenderUpgradeFailedCard(sessionKey, err.Error())
			} else if card == nil {
				card = newUpgradeServiceInner(s.app).RenderUpgradeFailedCard(sessionKey, "升级命令没有返回卡片")
			}
			if err := s.app.feishu.PatchCard(context.Background(), messageID, card); err != nil {
				slog.Warn("upgrade panel patch failed",
					"session_key", sessionKey,
					"user_id", action.UserID,
					"message_id", messageID,
					"error", err,
				)
			}
		}()
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "info", Content: "正在检查可升级版本"},
			Card:  rawCard(newUpgradeServiceInner(s.app).RenderUpgradePreparingCard(sessionKey)),
		}, nil
	}
	return completeMenuCommand(s.app, action, sessionKey, "/upgrade", "menu.group.system")
}

func (s menuActionService) completeUpgradeDev(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	sessionKey := actionSessionKey(action)
	return completeAsyncCommandAction(s.app,
		action,
		sessionKey,
		"/upgrade dev",
		"menu.group.system",
		"正在检查开发版升级信息",
		newUpgradeServiceInner(s.app).RenderUpgradePreparingCard(sessionKey),
		nil,
		newUpgradeServiceInner(s.app).RenderUpgradeFailedCard,
		"upgrade dev patch failed",
	)
}
