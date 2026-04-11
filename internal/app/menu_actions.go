package app

import (
	"context"
	"log/slog"
	"strings"
	"time"

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
	renderer := menuNodeRenderers[strings.TrimSpace(actionName)]
	if renderer == nil {
		return nil, false
	}
	return renderer(a, sessionKey)
}

func (a *App) completeMenuCompact(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	parentAction := "menu.tools"
	if action != nil {
		if value, ok := action.ActionValue["parent_action"].(string); ok && strings.TrimSpace(value) != "" {
			parentAction = value
		}
	}
	if _, err := a.startThreadCompaction(sessionKey); err != nil {
		if card, ok := a.renderMenuNodeCard(parentAction, sessionKey); ok {
			return &callback.CardActionTriggerResponse{
				Toast: &callback.Toast{Type: "warning", Content: err.Error()},
				Card:  rawCard(card),
			}, nil
		}
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	if card, ok := a.renderMenuNodeCard(parentAction, sessionKey); ok {
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "success", Content: "已请求压缩当前线程上下文"},
			Card:  rawCard(card),
		}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已请求压缩当前线程上下文"},
	}, nil
}

func (a *App) completeGlobalModelSet(action *feishu.CardAction, modelID string) (*callback.CardActionTriggerResponse, error) {
	sessionKey, _ := action.ActionValue["session_key"].(string)
	menuAction, _ := action.ActionValue["menu_action"].(string)
	if strings.TrimSpace(menuAction) == "" {
		menuAction = "menu.model"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	result, err := a.fetchModelList(ctx)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: err.Error()}}, nil
	}
	if err := a.updateGlobalModelConfig(func(c *config.CodexConfig) {
		c.Model = strings.TrimSpace(modelID)
	}, result); err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: err.Error()}}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已更新全局模型"},
		Card:  rawCard(a.renderModelConfigCard(result, sessionKey, menuAction)),
	}, nil
}

func (a *App) completeGlobalReasoningEffortSet(action *feishu.CardAction, reasoningEffort string) (*callback.CardActionTriggerResponse, error) {
	sessionKey, _ := action.ActionValue["session_key"].(string)
	menuAction, _ := action.ActionValue["menu_action"].(string)
	if strings.TrimSpace(menuAction) == "" {
		menuAction = "menu.model"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	result, err := a.fetchModelList(ctx)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: err.Error()}}, nil
	}
	selectedModel, _ := effectiveConfiguredModelAndEffort(a.cfg, result)
	if strings.TrimSpace(reasoningEffort) != "" && !modelSupportsEffort(selectedModel, reasoningEffort) {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "当前模型不支持这个推理强度"}}, nil
	}
	if err := a.updateGlobalModelConfig(func(c *config.CodexConfig) {
		c.ReasoningEffort = strings.TrimSpace(reasoningEffort)
	}, result); err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: err.Error()}}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已更新全局推理强度"},
		Card:  rawCard(a.renderModelConfigCard(result, sessionKey, menuAction)),
	}, nil
}

func (a *App) completeMenuQuiet(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "info", Content: "已打开 quiet 配置"},
		Card:  rawCard(a.renderQuietModeMenuCard(sessionKey)),
	}, nil
}

func (a *App) completeMenuUsage(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "info", Content: "已打开 token usage"},
		Card:  rawCard(a.renderUsageCard(sessionKey)),
	}, nil
}

func (a *App) completeQuietSet(action *feishu.CardAction, enabled bool) (*callback.CardActionTriggerResponse, error) {
	sessionKey, _ := action.ActionValue["session_key"].(string)
	if err := a.updateQuietMode(enabled); err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: err.Error()}}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已更新 quiet 开关"},
		Card:  rawCard(a.renderQuietModeMenuCard(sessionKey)),
	}, nil
}

func (a *App) completeMenuModel(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	result, err := a.fetchModelList(ctx)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "info", Content: "已打开模型配置"},
		Card:  rawCard(a.renderModelConfigCard(result, sessionKey, "menu.model")),
	}, nil
}

func (a *App) completeMenuStatus(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "info", Content: "已打开状态面板"},
		Card:  rawCard(a.renderStatusCard(sessionKey)),
	}, nil
}

func (a *App) completeMenuHelp(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "info", Content: "已打开帮助说明"},
		Card:  rawCard(a.renderHelpCard(sessionKey)),
	}, nil
}

func (a *App) completeMenuHistory(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	card, err := a.renderHistoryCard(sessionKey, 0)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "info", Content: "已打开历史记录"},
		Card:  rawCard(card),
	}, nil
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
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "info", Content: "已打开 service tier 配置"},
		Card:  rawCard(a.renderServiceTierMenuCard(sessionKey)),
	}, nil
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
		userID := action.UserID
		go func() {
			card, err := a.renderUpgradeCard(sessionKey, userID)
			if err != nil {
				slog.Warn("upgrade panel render failed",
					"session_key", sessionKey,
					"user_id", userID,
					"message_id", messageID,
					"error", err,
				)
				card = a.renderUpgradeFailedCard(sessionKey, err.Error())
			}
			if err := a.feishu.PatchCard(context.Background(), messageID, card); err != nil {
				slog.Warn("upgrade panel patch failed",
					"session_key", sessionKey,
					"user_id", userID,
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
	card, err := a.renderUpgradeCard(sessionKey, action.UserID)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "info", Content: "已打开升级面板"},
		Card:  rawCard(card),
	}, nil
}
