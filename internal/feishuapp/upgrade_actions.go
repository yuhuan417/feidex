package feishuapp

import (
	"context"
	"feidex/internal/application/backendmaintenance"
	"time"

	appbackend "feidex/internal/adapter/feishu/backend"
	"feidex/internal/adapter/feishu/upgraderender"
	"feidex/internal/feishu"

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
	spec        upgraderender.Spec
	pendingKind string
	rawCommand  string
	patchLog    string
	loadView    func(ctx context.Context, includeLatest bool) (upgraderender.UpgradeView, error)
}

func upgradeHooksFor(backendupgrades backendUpgradeService, kind backendUpgradeKind) backendUpgradeHooks {
	svc := backendupgrades
	if kind == backendUpgradeClaude {
		return backendUpgradeHooks{
			spec:        upgraderender.ClaudeSpec,
			pendingKind: claudeUpgradePendingKind,
			rawCommand:  "/claude",
			patchLog:    "claude upgrade panel patch failed",
			loadView:    svc.loadClaudeUpgradeView,
		}
	}
	return backendUpgradeHooks{
		spec:        upgraderender.CodexSpec,
		pendingKind: codexUpgradePendingKind,
		rawCommand:  "/codex",
		patchLog:    "codex upgrade panel patch failed",
		loadView:    svc.loadCodexUpgradeView,
	}
}

func (s backendUpgradeService) completeMenuUpgrade(kind backendUpgradeKind, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	h := upgradeHooksFor(s.app.bindings.BackendUpgrades, kind)
	return s.app.bindings.BackendUpgrades.completeUpgradeAsyncAction(h, action,
		h.rawCommand,
		"正在加载 "+h.spec.Name+" 状态",
		"正在读取本机 "+h.spec.Name+" 状态，请稍候。")
}

func (s backendUpgradeService) completeUpgradeRefresh(kind backendUpgradeKind, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	h := upgradeHooksFor(s.app.bindings.BackendUpgrades, kind)
	return s.app.bindings.BackendUpgrades.completeUpgradeAsyncAction(h, action,
		h.rawCommand,
		"正在刷新 "+h.spec.Name+" 状态",
		"正在刷新本机 "+h.spec.Name+" 状态，请稍候。")
}

func (s backendUpgradeService) completeUpgradeCheck(kind backendUpgradeKind, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	h := upgradeHooksFor(s.app.bindings.BackendUpgrades, kind)
	return s.app.bindings.BackendUpgrades.completeUpgradeAsyncAction(h, action,
		h.rawCommand+" check",
		"正在检查 "+h.spec.Name+" 自升级命令",
		"正在检查 "+h.spec.Name+" 自升级命令，请稍候。")
}

func (s backendUpgradeService) completeUpgradePrepare(kind backendUpgradeKind, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	h := upgradeHooksFor(s.app.bindings.BackendUpgrades, kind)
	return s.app.bindings.BackendUpgrades.completeUpgradeAsyncAction(h, action,
		h.rawCommand+" upgrade",
		"正在准备自升级确认",
		"正在准备自升级确认，请稍候。")
}

func (s backendUpgradeService) completeRestartRun(kind backendUpgradeKind, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	h := upgradeHooksFor(s.app.bindings.BackendUpgrades, kind)
	service := s.app.bindings.BackendMaintenance[string(kind)]
	render := s.app.bindings.UpgradePresentation
	return completeMaintenanceRestartRun(
		s.app,
		action,
		service.BeginRestart,
		func(messageID, sessionKey string) {
			_ = s.app.bindings.MaintenanceRunners[string(kind)].Start(backendmaintenance.Operation{MessageID: messageID, SessionKey: sessionKey, Restart: true})
		},
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
	render := s.app.bindings.UpgradePresentation
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
	h := upgradeHooksFor(s.app.bindings.BackendUpgrades, kind)
	render := s.app.bindings.UpgradePresentation
	service := s.app.bindings.BackendMaintenance[string(kind)]
	requestID := actionStringValue(action, "request_id")
	if actionName == h.spec.ActionPrefix+".cancel" {
		request, err := service.Cancel(requestID, action.UserID)
		if err != nil {
			return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
		}
		sessionKey := request.SessionKey
		runAsync(s.app, func() {
			ctx, cancel := context.WithTimeout(s.app.Context(), 20*time.Second)
			defer cancel()
			view, err := h.loadView(ctx, false)
			if err != nil {
				patchMaintenanceCard(s.app, action.MessageID, render.renderUpgradeFailedCard(h.spec, sessionKey, err.Error()), h.patchLog)
				return
			}
			patchMaintenanceCard(s.app, action.MessageID, render.renderUpgradeStatusCard(h.spec, sessionKey, view, false), h.patchLog)
		})
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "success", Content: "已取消升级"}, Card: rawCard(render.renderUpgradePreparingCard(h.spec, sessionKey, "已取消升级"))}, nil
	}
	operation, err := service.Confirm(requestID, action.UserID, action.MessageID)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	snapshot := service.State.UpgradeState()
	if err := s.app.bindings.MaintenanceRunners[string(kind)].Start(operation); err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "info", Content: h.spec.Name + " 升级已开始"}, Card: rawCard(render.renderUpgradeOperationCard(h.spec, operation.SessionKey, snapshot))}, nil
}
