package app

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"feidex/internal/feishu"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

func (a *App) completeMenuClaudeUpgrade(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	return a.completeClaudeUpgradeAsyncAction(action, "/claude", "正在加载 Claude 状态", "正在读取本机 Claude 状态，请稍候。")
}

func (a *App) completeClaudeUpgradeRefresh(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	return a.completeClaudeUpgradeAsyncAction(action, "/claude", "正在刷新 Claude 状态", "正在刷新本机 Claude 状态，请稍候。")
}

func (a *App) completeClaudeUpgradeCheck(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	return a.completeClaudeUpgradeAsyncAction(action, "/claude check", "正在检查最新稳定版", "正在检查最新稳定版，请稍候。")
}

func (a *App) completeClaudeUpgradePrepare(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	return a.completeClaudeUpgradeAsyncAction(action, "/claude upgrade", "正在准备升级确认", "正在准备升级确认，请稍候。")
}

func (a *App) completeClaudeRestartRun(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	return completeMaintenanceRestartRun(
		a,
		action,
		a.beginClaudeRestartOperation,
		a.runClaudeRestartOperation,
		a.renderClaudeRestartOperationCard,
		func(ctx context.Context) (map[string]any, error) {
			view, err := a.loadClaudeUpgradeView(ctx, false)
			if err != nil {
				return nil, err
			}
			return a.renderClaudeUpgradeStatusCard(actionSessionKey(action), view, false), nil
		},
		a.renderClaudeUpgradeFailedCard,
		"正在重启 Claude runtime",
	)
}

func (a *App) completeClaudeUpgradeAsyncAction(action *feishu.CardAction, rawCommand, toastText, preparingText string) (*callback.CardActionTriggerResponse, error) {
	return a.completeMaintenanceAsyncAction(
		action,
		rawCommand,
		toastText,
		func(sessionKey string) map[string]any {
			return a.renderClaudeUpgradePreparingCard(sessionKey, preparingText)
		},
		a.renderClaudeUpgradeFailedCard,
		"claude upgrade panel patch failed",
	)
}

func (a *App) completeClaudeUpgradeAction(action *feishu.CardAction, actionName string) (*callback.CardActionTriggerResponse, error) {
	appState := a.appState()
	requestID := actionStringValue(action, "request_id")
	pending := appState.pending(requestID)
	if pending == nil || pending.Kind != claudeUpgradePendingKind || strings.TrimSpace(pending.Status) != "pending" {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "升级请求已过期"}}, nil
	}
	if pending.OwnerUserID != "" && pending.OwnerUserID != action.UserID {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "你没有权限处理这个升级请求"}}, nil
	}
	sessionKey := firstNonEmpty(actionSessionKey(action), pending.SessionKey)
	if actionName == "claude_upgrade.cancel" {
		_ = appState.updatePending(requestID, func(req *state.PendingRequest) { req.Status = "resolved" })
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		view, err := a.loadClaudeUpgradeView(ctx, false)
		if err != nil {
			return &callback.CardActionTriggerResponse{
				Toast: &callback.Toast{Type: "success", Content: "已取消升级"},
				Card:  rawCard(a.renderClaudeUpgradeFailedCard(sessionKey, err.Error())),
			}, nil
		}
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "success", Content: "已取消升级"},
			Card:  rawCard(a.renderClaudeUpgradeStatusCard(sessionKey, view, false)),
		}, nil
	}

	var payload claudeUpgradePendingPayload
	if err := json.Unmarshal([]byte(pending.PayloadJSON), &payload); err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "升级参数损坏"}}, nil
	}
	snapshot := claudeUpgradeSnapshot{
		Running:         true,
		Phase:           "preflight",
		Message:         "正在校验升级前置条件",
		CurrentVersion:  payload.CurrentVersion,
		PreviousVersion: payload.CurrentVersion,
		TargetVersion:   payload.TargetVersion,
		LatestVersion:   payload.TargetVersion,
	}
	if !a.beginClaudeUpgrade(snapshot) {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		view, err := a.loadClaudeUpgradeView(ctx, false)
		if err == nil {
			return &callback.CardActionTriggerResponse{
				Toast: &callback.Toast{Type: "warning", Content: "Claude 正在维护中"},
				Card:  rawCard(a.renderClaudeUpgradeStatusCard(sessionKey, view, false)),
			}, nil
		}
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "warning", Content: "Claude 正在维护中"},
			Card:  rawCard(a.renderClaudeUpgradeOperationCard(sessionKey, a.claudeUpgradeState())),
		}, nil
	}
	_ = appState.updatePending(requestID, func(req *state.PendingRequest) { req.Status = "resolved" })
	messageID := firstNonEmpty(strings.TrimSpace(action.MessageID), strings.TrimSpace(pending.FeishuMsgID))
	go a.runClaudeUpgradeOperation(messageID, sessionKey, payload)
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "info", Content: "Claude 升级已开始"},
		Card:  rawCard(a.renderClaudeUpgradeOperationCard(sessionKey, a.claudeUpgradeState())),
	}, nil
}
