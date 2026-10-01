package app

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"feidex/internal/app/apputil"
	appbackend "feidex/internal/app/backend"
	appruntime "feidex/internal/app/runtime"
	"feidex/internal/app/upgraderender"
	"feidex/internal/feishu"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

// backendUpgradeKind selects which backend's runtime hooks an upgrade card
// action targets. The Claude and Codex card actions are otherwise identical,
// so they share one implementation driven by these hooks.
type backendUpgradeKind string

const (
	backendUpgradeClaude backendUpgradeKind = "claude"
	backendUpgradeCodex  backendUpgradeKind = "codex"
)

// backendUpgradeHooks are the per-backend runtime operations the shared card
// actions call. Everything else is derived from spec.
type backendUpgradeHooks struct {
	spec         upgraderender.Spec
	pendingKind  string
	rawCommand   string
	patchLog     string
	loadView     func(ctx context.Context, includeLatest bool) (backendUpgradeView, error)
	beginUpgrade func(appbackend.BackendUpgradeSnapshot) bool
	upgradeState func() appbackend.BackendUpgradeSnapshot
	beginRestart func() (appbackend.BackendRestartSnapshot, error)
	runRestart   func(messageID, sessionKey string)
	runUpgrade   func(messageID, sessionKey string, payload appruntime.BackendUpgradePendingPayload)
}

func upgradeHooksFor(a *App, kind backendUpgradeKind) backendUpgradeHooks {
	svc := newBackendUpgradeService(a)
	if kind == backendUpgradeClaude {
		return backendUpgradeHooks{
			spec:        upgraderender.ClaudeSpec,
			pendingKind: claudeUpgradePendingKind,
			rawCommand:  "/claude",
			patchLog:    "claude upgrade panel patch failed",
			loadView:    svc.loadClaudeUpgradeView,
			beginUpgrade: func(snapshot appbackend.BackendUpgradeSnapshot) bool {
				return appbackend.NewMaintenanceStateService(a).BeginClaudeUpgrade(snapshot)
			},
			upgradeState: func() appbackend.BackendUpgradeSnapshot {
				return appbackend.NewMaintenanceStateService(a).ClaudeUpgradeState()
			},
			beginRestart: svc.beginClaudeRestartOperation,
			runRestart:   svc.runClaudeRestartOperation,
			runUpgrade:   svc.runClaudeUpgradeOperation,
		}
	}
	return backendUpgradeHooks{
		spec:        upgraderender.CodexSpec,
		pendingKind: codexUpgradePendingKind,
		rawCommand:  "/codex",
		patchLog:    "codex upgrade panel patch failed",
		loadView:    svc.loadCodexUpgradeView,
		beginUpgrade: func(snapshot appbackend.BackendUpgradeSnapshot) bool {
			return appbackend.NewMaintenanceStateService(a).BeginCodexUpgrade(snapshot)
		},
		upgradeState: func() appbackend.BackendUpgradeSnapshot {
			return appbackend.NewMaintenanceStateService(a).CodexUpgradeState()
		},
		beginRestart: svc.beginCodexRestartOperation,
		runRestart:   svc.runCodexRestartOperation,
		runUpgrade:   svc.runCodexUpgradeOperation,
	}
}

func (s backendUpgradeService) completeMenuUpgrade(kind backendUpgradeKind, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	h := upgradeHooksFor(s.app, kind)
	return newBackendUpgradeService(s.app).completeUpgradeAsyncAction(h, action,
		h.rawCommand,
		"正在加载 "+h.spec.Name+" 状态",
		"正在读取本机 "+h.spec.Name+" 状态，请稍候。")
}

func (s backendUpgradeService) completeUpgradeRefresh(kind backendUpgradeKind, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	h := upgradeHooksFor(s.app, kind)
	return newBackendUpgradeService(s.app).completeUpgradeAsyncAction(h, action,
		h.rawCommand,
		"正在刷新 "+h.spec.Name+" 状态",
		"正在刷新本机 "+h.spec.Name+" 状态，请稍候。")
}

func (s backendUpgradeService) completeUpgradeCheck(kind backendUpgradeKind, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	h := upgradeHooksFor(s.app, kind)
	return newBackendUpgradeService(s.app).completeUpgradeAsyncAction(h, action,
		h.rawCommand+" check",
		"正在检查 "+h.spec.Name+" 自升级命令",
		"正在检查 "+h.spec.Name+" 自升级命令，请稍候。")
}

func (s backendUpgradeService) completeUpgradePrepare(kind backendUpgradeKind, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	h := upgradeHooksFor(s.app, kind)
	return newBackendUpgradeService(s.app).completeUpgradeAsyncAction(h, action,
		h.rawCommand+" upgrade",
		"正在准备自升级确认",
		"正在准备自升级确认，请稍候。")
}

func (s backendUpgradeService) completeRestartRun(kind backendUpgradeKind, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	h := upgradeHooksFor(s.app, kind)
	render := newUpgradeRenderService(s.app)
	return completeMaintenanceRestartRun(
		s.app,
		action,
		h.beginRestart,
		h.runRestart,
		func(sessionKey string, snapshot appbackend.BackendRestartSnapshot) map[string]any {
			return render.renderRestartOperationCard(h.spec, sessionKey, snapshot)
		},
		func(ctx context.Context) (map[string]any, error) {
			view, err := h.loadView(ctx, false)
			if err != nil {
				return nil, err
			}
			return render.renderUpgradeStatusCard(h.spec, actionSessionKey(action), view, false), nil
		},
		func(sessionKey, errText string) map[string]any {
			return render.renderUpgradeFailedCard(h.spec, sessionKey, errText)
		},
		"正在重启 "+h.spec.Name+" runtime",
	)
}

func (s backendUpgradeService) completeUpgradeAsyncAction(h backendUpgradeHooks, action *feishu.CardAction, rawCommand, toastText, preparingText string) (*callback.CardActionTriggerResponse, error) {
	render := newUpgradeRenderService(s.app)
	return completeMaintenanceAsyncAction(s.app,
		action,
		rawCommand,
		toastText,
		func(sessionKey string) map[string]any {
			return render.renderUpgradePreparingCard(h.spec, sessionKey, preparingText)
		},
		func(sessionKey, errText string) map[string]any {
			return render.renderUpgradeFailedCard(h.spec, sessionKey, errText)
		},
		h.patchLog,
	)
}

func (s backendUpgradeService) completeUpgradeAction(kind backendUpgradeKind, action *feishu.CardAction, actionName string) (*callback.CardActionTriggerResponse, error) {
	h := upgradeHooksFor(s.app, kind)
	render := newUpgradeRenderService(s.app)
	appState := s.app.State()
	requestID := actionStringValue(action, "request_id")
	pending := appState.Pending(requestID)
	if pending == nil || pending.Kind != h.pendingKind || state.NormalizePendingRequestStatus(pending.Status) != state.PendingRequestStatusPending {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "升级请求已过期"}}, nil
	}
	if pending.OwnerUserID != "" && pending.OwnerUserID != action.UserID {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "你没有权限处理这个升级请求"}}, nil
	}
	sessionKey := apputil.FirstNonEmpty(actionSessionKey(action), pending.SessionKey)
	if actionName == h.spec.ActionPrefix+".cancel" {
		_ = appState.UpdatePending(requestID, func(req *state.PendingRequest) { req.Status = state.PendingRequestStatusResolved.String() })
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		view, err := h.loadView(ctx, false)
		if err != nil {
			return &callback.CardActionTriggerResponse{
				Toast: &callback.Toast{Type: "success", Content: "已取消升级"},
				Card:  rawCard(render.renderUpgradeFailedCard(h.spec, sessionKey, err.Error())),
			}, nil
		}
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "success", Content: "已取消升级"},
			Card:  rawCard(render.renderUpgradeStatusCard(h.spec, sessionKey, view, false)),
		}, nil
	}

	var payload appruntime.BackendUpgradePendingPayload
	if err := json.Unmarshal([]byte(pending.PayloadJSON), &payload); err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "升级参数损坏"}}, nil
	}
	snapshot := appbackend.BackendUpgradeSnapshot{
		Running:         true,
		Phase:           "preflight",
		Message:         "正在校验升级前置条件",
		CurrentVersion:  payload.CurrentVersion,
		PreviousVersion: payload.CurrentVersion,
		TargetVersion:   payload.TargetVersion,
		LatestVersion:   payload.TargetVersion,
	}
	if !h.beginUpgrade(snapshot) {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		view, err := h.loadView(ctx, false)
		if err == nil {
			return &callback.CardActionTriggerResponse{
				Toast: &callback.Toast{Type: "warning", Content: h.spec.Name + " 正在维护中"},
				Card:  rawCard(render.renderUpgradeStatusCard(h.spec, sessionKey, view, false)),
			}, nil
		}
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "warning", Content: h.spec.Name + " 正在维护中"},
			Card:  rawCard(render.renderUpgradeOperationCard(h.spec, sessionKey, h.upgradeState())),
		}, nil
	}
	_ = appState.UpdatePending(requestID, func(req *state.PendingRequest) { req.Status = state.PendingRequestStatusResolved.String() })
	messageID := apputil.FirstNonEmpty(strings.TrimSpace(action.MessageID), strings.TrimSpace(pending.FeishuMsgID))
	go h.runUpgrade(messageID, sessionKey, payload)
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "info", Content: h.spec.Name + " 升级已开始"},
		Card:  rawCard(render.renderUpgradeOperationCard(h.spec, sessionKey, h.upgradeState())),
	}, nil
}
