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

func (s backendUpgradeService) completeMenuCodexUpgrade(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	return newBackendUpgradeService(s.app).completeCodexUpgradeAsyncAction(action, "/codex", "正在加载 Codex 状态", "正在读取本机 Codex 状态，请稍候。")
}

func (s backendUpgradeService) completeCodexUpgradeRefresh(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	return newBackendUpgradeService(s.app).completeCodexUpgradeAsyncAction(action, "/codex", "正在刷新 Codex 状态", "正在刷新本机 Codex 状态，请稍候。")
}

func (s backendUpgradeService) completeCodexUpgradeCheck(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	return newBackendUpgradeService(s.app).completeCodexUpgradeAsyncAction(action, "/codex check", "正在检查最新稳定版", "正在检查最新稳定版，请稍候。")
}

func (s backendUpgradeService) completeCodexUpgradePrepare(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	return newBackendUpgradeService(s.app).completeCodexUpgradeAsyncAction(action, "/codex upgrade", "正在准备升级确认", "正在准备升级确认，请稍候。")
}

func (s backendUpgradeService) completeCodexRestartRun(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	return completeMaintenanceRestartRun(
		s.app,
		action,
		newBackendUpgradeService(s.app).beginCodexRestartOperation,
		newBackendUpgradeService(s.app).runCodexRestartOperation,
		newUpgradeRenderService(s.app).renderCodexRestartOperationCard,
		func(ctx context.Context) (map[string]any, error) {
			view, err := newBackendUpgradeService(s.app).loadCodexUpgradeView(ctx, false)
			if err != nil {
				return nil, err
			}
			return newUpgradeRenderService(s.app).renderCodexUpgradeStatusCard(actionSessionKey(action), view, false), nil
		},
		newUpgradeRenderService(s.app).renderCodexUpgradeFailedCard,
		"正在重启 Codex runtime",
	)
}

func (s backendUpgradeService) completeCodexUpgradeAsyncAction(action *feishu.CardAction, rawCommand, toastText, preparingText string) (*callback.CardActionTriggerResponse, error) {
	return completeMaintenanceAsyncAction(s.app,
		action,
		rawCommand,
		toastText,
		func(sessionKey string) map[string]any {
			return newUpgradeRenderService(s.app).renderCodexUpgradePreparingCard(sessionKey, preparingText)
		},
		newUpgradeRenderService(s.app).renderCodexUpgradeFailedCard,
		"codex upgrade panel patch failed",
	)
}

func (s backendUpgradeService) completeCodexUpgradeAction(action *feishu.CardAction, actionName string) (*callback.CardActionTriggerResponse, error) {
	appState := appState(s.app)
	requestID := actionStringValue(action, "request_id")
	pending := appState.pending(requestID)
	if pending == nil || pending.Kind != codexUpgradePendingKind || strings.TrimSpace(pending.Status) != "pending" {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "升级请求已过期"}}, nil
	}
	if pending.OwnerUserID != "" && pending.OwnerUserID != action.UserID {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "你没有权限处理这个升级请求"}}, nil
	}
	sessionKey := firstNonEmpty(actionSessionKey(action), pending.SessionKey)
	if actionName == "codex_upgrade.cancel" {
		_ = appState.updatePending(requestID, func(req *state.PendingRequest) { req.Status = "resolved" })
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		view, err := newBackendUpgradeService(s.app).loadCodexUpgradeView(ctx, false)
		if err != nil {
			return &callback.CardActionTriggerResponse{
				Toast: &callback.Toast{Type: "success", Content: "已取消升级"},
				Card:  rawCard(newUpgradeRenderService(s.app).renderCodexUpgradeFailedCard(sessionKey, err.Error())),
			}, nil
		}
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "success", Content: "已取消升级"},
			Card:  rawCard(newUpgradeRenderService(s.app).renderCodexUpgradeStatusCard(sessionKey, view, false)),
		}, nil
	}

	var payload codexUpgradePendingPayload
	if err := json.Unmarshal([]byte(pending.PayloadJSON), &payload); err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "升级参数损坏"}}, nil
	}
	snapshot := backendUpgradeSnapshot{
		Running:         true,
		Phase:           "preflight",
		Message:         "正在校验升级前置条件",
		CurrentVersion:  payload.CurrentVersion,
		PreviousVersion: payload.CurrentVersion,
		TargetVersion:   payload.TargetVersion,
		LatestVersion:   payload.TargetVersion,
	}
	if !newMaintenanceStateService(s.app).beginCodexUpgrade(snapshot) {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		view, err := newBackendUpgradeService(s.app).loadCodexUpgradeView(ctx, false)
		if err == nil {
			return &callback.CardActionTriggerResponse{
				Toast: &callback.Toast{Type: "warning", Content: "Codex 正在维护中"},
				Card:  rawCard(newUpgradeRenderService(s.app).renderCodexUpgradeStatusCard(sessionKey, view, false)),
			}, nil
		}
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "warning", Content: "Codex 正在维护中"},
			Card:  rawCard(newUpgradeRenderService(s.app).renderCodexUpgradeOperationCard(sessionKey, newMaintenanceStateService(s.app).codexUpgradeState())),
		}, nil
	}
	_ = appState.updatePending(requestID, func(req *state.PendingRequest) { req.Status = "resolved" })
	messageID := firstNonEmpty(strings.TrimSpace(action.MessageID), strings.TrimSpace(pending.FeishuMsgID))
	go newBackendUpgradeService(s.app).runCodexUpgradeOperation(messageID, sessionKey, payload)
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "info", Content: "Codex 升级已开始"},
		Card:  rawCard(newUpgradeRenderService(s.app).renderCodexUpgradeOperationCard(sessionKey, newMaintenanceStateService(s.app).codexUpgradeState())),
	}, nil
}
