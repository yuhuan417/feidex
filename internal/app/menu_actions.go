package app

import (
	"context"
	"log/slog"
	"strings"

	"feidex/internal/config"
	"feidex/internal/feishu"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

func (a *App) completeMenuRoot(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "info", Content: "已返回命令菜单"},
		Card:  rawCard(a.renderCommandMenuCard(sessionKey)),
	}, nil
}

func (a *App) completeMenuTools(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "info", Content: "已打开常用工具"},
		Card:  rawCard(a.renderToolsMenuCard(sessionKey)),
	}, nil
}

func (a *App) completeMenuGroupModel(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "info", Content: "已打开 model"},
		Card:  rawCard(a.renderModelMenuCard(sessionKey)),
	}, nil
}

func (a *App) completeMenuGroupSystem(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "info", Content: "已打开 system"},
		Card:  rawCard(a.renderSystemMenuCard(sessionKey)),
	}, nil
}

func (a *App) renderMenuNodeCard(actionName, sessionKey string) (map[string]any, bool) {
	actionName = nearestVisibleMenuAction(actionName, a.configuredBackend())
	renderer := menuNodeRenderers[actionName]
	if renderer == nil {
		return nil, false
	}
	return renderer(a, sessionKey)
}

func (a *App) completeMenuCompact(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return a.completeMenuCommand(action, sessionKey, "/compact", "menu.tools")
}

func (a *App) completeMenuReview(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	if !menuActionVisibleForBackend("menu.review", a.configuredBackend()) {
		return a.completeMenuCommand(action, sessionKey, "/review", "menu.tools")
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "info", Content: "已打开代码审查"},
		Card:  rawCard(a.renderReviewMenuCard(sessionKey)),
	}, nil
}

func (a *App) completeMenuQuiet(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return a.completeMenuCommand(action, sessionKey, "/quiet config", "menu.tools")
}

func (a *App) completeMenuUsage(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return a.completeMenuCommand(action, sessionKey, "/usage", "menu.tools")
}

func (a *App) completeMenuSkills(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return a.completeMenuCommand(action, sessionKey, "/skills", "menu.tools")
}

func (a *App) completeQuietSet(action *feishu.CardAction, mode config.QuietMode) (*callback.CardActionTriggerResponse, error) {
	sessionKey, _ := action.ActionValue["session_key"].(string)
	if err := a.updateQuietMode(mode); err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: err.Error()}}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已更新 quiet 模式为 " + quietModeStatusText(mode)},
		Card:  rawCard(a.renderQuietModeMenuCard(sessionKey)),
	}, nil
}

func (a *App) completeMenuModel(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return a.completeMenuCommand(action, sessionKey, "/model", "menu.group.model")
}

func (a *App) completeMenuStatus(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return a.completeMenuCommand(action, sessionKey, "/status", "menu.group.system")
}

func (a *App) completeMenuHelp(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return a.completeMenuCommand(action, sessionKey, "/help", "menu.group.system")
}

func (a *App) completeMenuHistory(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return a.completeMenuCommand(action, sessionKey, "/history", "menu.tools")
}

func (a *App) completeHistoryPage(action *feishu.CardAction, sessionKey string, page int) (*callback.CardActionTriggerResponse, error) {
	card, err := a.renderHistoryCard(sessionKey, page)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	return &callback.CardActionTriggerResponse{
		Card: rawCard(card),
	}, nil
}

func (a *App) completeHistoryDetail(action *feishu.CardAction, sessionKey string, index int) (*callback.CardActionTriggerResponse, error) {
	card, err := a.renderHistoryDetailCard(sessionKey, index)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	return &callback.CardActionTriggerResponse{
		Card: rawCard(card),
	}, nil
}

func (a *App) completeMenuFast(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return a.completeMenuCommand(action, sessionKey, "/fast config", "menu.group.model")
}

func (a *App) completeServiceTierSet(action *feishu.CardAction, sessionKey, threadID, serviceTier string) (*callback.CardActionTriggerResponse, error) {
	if _, err := a.setThreadServiceTier(sessionKey, threadID, serviceTier); err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已更新 service tier"},
		Card:  rawCard(a.renderServiceTierMenuCard(sessionKey)),
	}, nil
}

func (a *App) completeMenuUpgrade(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	sessionKey, _ := action.ActionValue["session_key"].(string)
	if action != nil && strings.TrimSpace(action.MessageID) != "" {
		messageID := strings.TrimSpace(action.MessageID)
		go func() {
			_, card, err := a.runCommandFromCardAction(action, sessionKey, "/upgrade")
			if err != nil {
				slog.Warn("upgrade panel render failed",
					"session_key", sessionKey,
					"user_id", action.UserID,
					"message_id", messageID,
					"error", err,
				)
				card = a.renderUpgradeFailedCard(sessionKey, err.Error())
			} else if card == nil {
				card = a.renderUpgradeFailedCard(sessionKey, "升级命令没有返回卡片")
			}
			if err := a.feishu.PatchCard(context.Background(), messageID, card); err != nil {
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
			Card:  rawCard(a.renderUpgradePreparingCard(sessionKey)),
		}, nil
	}
	return a.completeMenuCommand(action, sessionKey, "/upgrade", "menu.group.system")
}

func (a *App) completeUpgradeDev(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	return a.completeMenuCommand(action, actionSessionKey(action), "/upgrade dev", "menu.group.system")
}
