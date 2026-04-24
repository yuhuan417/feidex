package app

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"path/filepath"
	"strings"

	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

var backendLookPath = exec.LookPath

type availableBackend struct {
	Kind    string
	Command string
	Path    string
}

func backendDisplayName(backend string) string {
	if runtime := backendRuntimeForKind(backend); runtime != nil {
		return runtime.displayName()
	}
	return "未设置"
}

func (a *App) availableBackends() []availableBackend {
	if a == nil || a.cfg == nil {
		return nil
	}
	out := make([]availableBackend, 0, 2)
	for _, runtime := range backendRuntimeFacades() {
		command := runtime.configuredCommand(a)
		if command == "" {
			continue
		}
		path, err := backendLookPath(command)
		if err != nil {
			continue
		}
		out = append(out, availableBackend{
			Kind:    runtime.kind(),
			Command: command,
			Path:    path,
		})
	}
	return out
}

func (a *App) backendAvailable(target string) bool {
	target = normalizeRuntimeBackend(target)
	if target == "" {
		return false
	}
	for _, candidate := range a.availableBackends() {
		if candidate.Kind == target {
			return true
		}
	}
	return false
}

func (a *App) renderBackendSelectionCard(sessionKey, notice string) map[string]any {
	current := a.configuredBackend()
	choices := a.availableBackends()
	lines := []string{
		"当前 backend: `" + firstNonEmpty(current, "unset") + "`",
		"当前 frontend: `" + firstNonEmpty(strings.TrimSpace(a.frontendID), config.DefaultFrontendID) + "`",
	}
	if len(choices) == 0 {
		lines = append(lines,
			"",
			"本机没有检测到可用 backend。",
			"请先安装 `codex` 或 `claude`，然后重新打开这张卡片。",
		)
	} else {
		names := make([]string, 0, len(choices))
		for _, choice := range choices {
			names = append(names, "`"+choice.Kind+"`")
		}
		lines = append(lines,
			"可选 backend: "+strings.Join(names, " / "),
			"",
			"切换 backend 只允许在当前 frontend 空闲时进行。",
			"切回原 backend 时，会恢复它之前自己的 thread lineage。",
		)
	}
	if strings.TrimSpace(notice) != "" {
		lines = append(lines, "", strings.TrimSpace(notice))
	}
	buttons := make([]feishu.Button, 0, len(choices)+1)
	for _, choice := range choices {
		label := backendDisplayName(choice.Kind)
		btnType := "default"
		if choice.Kind == current && current != "" {
			label = "当前 · " + label
			btnType = "primary"
		}
		buttons = append(buttons, feishu.Button{
			Text:  label,
			Type:  btnType,
			Value: map[string]any{"action": "backend.select", "session_key": sessionKey, "backend": choice.Kind},
		})
	}
	if strings.TrimSpace(sessionKey) != "" {
		buttons = append(buttons, feishu.Button{
			Text:  "返回上一级",
			Type:  "default",
			Value: map[string]any{"action": "menu.group.backend", "session_key": sessionKey},
		})
	}
	color := "blue"
	switch {
	case current == "":
		color = "orange"
	case strings.Contains(notice, "失败"):
		color = "red"
	case strings.Contains(notice, "已切换"):
		color = "green"
	}
	return a.feishu.SimpleStatusCard("后端选择", color, menuCardBody("menu.backend.switch", strings.Join(lines, "\n")), buttons)
}

func (a *App) renderBackendSwitchingCard(sessionKey, target string) map[string]any {
	body := strings.Join([]string{
		"正在切换到 backend: `" + normalizeRuntimeBackend(target) + "`",
		"",
		"会先切换 runtime，再恢复这个 backend 之前的 thread lineage。",
	}, "\n")
	return a.feishu.SimpleStatusCard("切换后端", "orange", menuCardBody("menu.backend.switch", body), []feishu.Button{
		{Text: "处理中", Type: "default", Value: map[string]any{"action": "menu.backend.switch", "session_key": sessionKey}},
	})
}

func (a *App) replyBackendSelectionCard(msg *feishu.InboundMessage, reason string) error {
	if a == nil || a.feishu == nil {
		return fmt.Errorf("backend not configured")
	}
	sessionKey := ""
	if msg != nil {
		sessionKey = a.makeSessionKey(msg)
	}
	card := a.renderBackendSelectionCard(sessionKey, firstNonEmpty(strings.TrimSpace(reason), "当前 frontend 还没有设置 backend，请先选择。"))
	if msg != nil && strings.TrimSpace(msg.MessageID) != "" {
		_, err := a.feishu.ReplyCard(context.Background(), msg.MessageID, card, a.replyInThreadEnabled(msg.ChatType))
		return err
	}
	if msg != nil && strings.TrimSpace(msg.ChatID) != "" {
		_, err := a.feishu.SendCard(context.Background(), msg.ChatID, card)
		return err
	}
	return fmt.Errorf("backend not configured")
}

func (a *App) commandBackend(msg *feishu.InboundMessage, args []string) error {
	if len(args) == 0 {
		return a.replyBackendSelectionCard(msg, "")
	}
	switch strings.TrimSpace(args[0]) {
	case "retry":
		return newAutoRetryService(a).commandAutoRetry(msg, args[1:])
	default:
		return fmt.Errorf("usage: /backend | /backend retry | /backend retry status | /backend retry on | /backend retry off")
	}
}

func (a *App) completeMenuBackend(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "info", Content: "已打开后端选择"},
		Card:  rawCard(a.renderBackendMenuCard(sessionKey)),
	}, nil
}

func (a *App) completeBackendSelect(action *feishu.CardAction, sessionKey, target string) (*callback.CardActionTriggerResponse, error) {
	target = normalizeRuntimeBackend(target)
	if target == "" {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "未收到有效 backend"}}, nil
	}
	if !a.backendAvailable(target) {
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "warning", Content: backendDisplayName(target) + " 当前不可用"},
			Card:  rawCard(a.renderBackendSelectionCard(sessionKey, backendDisplayName(target)+" 当前不可用，请先确认本机安装。")),
		}, nil
	}
	if current := a.configuredBackend(); current == target && a.backendRuntimeReady(target) {
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "success", Content: "当前已经在使用 " + backendDisplayName(target)},
			Card:  rawCard(a.renderBackendSelectionCard(sessionKey, "当前已经在使用 `"+target+"`。")),
		}, nil
	}
	if reason := a.backendSwitchBlockedReason(); reason != "" {
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "warning", Content: reason},
			Card:  rawCard(a.renderBackendSelectionCard(sessionKey, reason)),
		}, nil
	}
	if action == nil || strings.TrimSpace(action.MessageID) == "" {
		if err := a.switchBackend(context.Background(), target); err != nil {
			return &callback.CardActionTriggerResponse{
				Toast: &callback.Toast{Type: "error", Content: err.Error()},
				Card:  rawCard(a.renderBackendSelectionCard(sessionKey, "切换失败: "+err.Error())),
			}, nil
		}
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "success", Content: "已切换到 " + backendDisplayName(target)},
			Card:  rawCard(a.renderBackendSelectionCard(sessionKey, "已切换到 `"+target+"`。")),
		}, nil
	}

	messageID := strings.TrimSpace(action.MessageID)
	go func() {
		err := a.switchBackend(context.Background(), target)
		notice := "已切换到 `" + target + "`。"
		if err != nil {
			notice = "切换失败: " + err.Error()
			slog.Warn("backend switch failed",
				"frontend_id", a.frontendID,
				"target_backend", target,
				"message_id", messageID,
				"error", err,
			)
		}
		if patchErr := a.feishu.PatchCard(context.Background(), messageID, a.renderBackendSelectionCard(sessionKey, notice)); patchErr != nil {
			slog.Warn("backend switch patch failed",
				"frontend_id", a.frontendID,
				"target_backend", target,
				"message_id", messageID,
				"error", patchErr,
			)
		}
	}()
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "info", Content: "正在切换到 " + backendDisplayName(target)},
		Card:  rawCard(a.renderBackendSwitchingCard(sessionKey, target)),
	}, nil
}

func (a *App) backendRuntimeReady(target string) bool {
	if runtime := backendRuntimeForKind(target); runtime != nil {
		return runtime.runtimeReady(a)
	}
	return false
}

func (a *App) backendSwitchBlockedReason() string {
	return a.frontendIdleBlockedReason()
}

func (a *App) switchBackend(ctx context.Context, target string) error {
	if a == nil {
		return fmt.Errorf("app not initialized")
	}
	target = normalizeRuntimeBackend(target)
	if target == "" {
		return fmt.Errorf("missing backend")
	}
	if !a.backendAvailable(target) {
		return fmt.Errorf("%s backend 当前不可用", backendDisplayName(target))
	}

	a.backendSwitchMu.Lock()
	defer a.backendSwitchMu.Unlock()

	if reason := a.backendSwitchBlockedReason(); reason != "" {
		return fmt.Errorf("%s", reason)
	}

	current := a.configuredBackend()
	if current == target && a.backendRuntimeReady(target) {
		return nil
	}
	newRuntimeStateService(a).beginBackendSwitchState(target)
	defer newRuntimeStateService(a).finishBackendSwitchState()
	slog.Info("backend switch begin",
		"frontend_id", a.frontendID,
		"current_backend", current,
		"target_backend", target,
	)

	nextSessions := a.frontendSessionsAfterBackendSwitch(current, target)
	newHandle, err := a.prepareBackendRuntime(ctx, target)
	if err != nil {
		return err
	}

	oldHandle := a.currentBackendRuntimeHandle()
	oldBackend := a.currentRuntimeBackend()
	if err := a.setConfiguredBackend(target); err != nil {
		_ = newHandle.close()
		return err
	}

	newHandle.install(a)
	for _, sess := range nextSessions {
		if err := a.store.UpsertSession(sess); err != nil {
			oldHandle.install(a)
			a.setRuntimeBackend(oldBackend)
			_ = a.setConfiguredBackend(current)
			_ = newHandle.close()
			return err
		}
	}
	slog.Info("backend switch runtime installed",
		"frontend_id", a.frontendID,
		"target_backend", target,
	)
	a.recoverFrontendRuntimeState()
	_ = oldHandle.close()
	slog.Info("backend switch completed",
		"frontend_id", a.frontendID,
		"current_backend", current,
		"target_backend", target,
	)
	return nil
}

func (a *App) frontendSessionsAfterBackendSwitch(current, target string) []*state.Session {
	appState := a.appState()
	out := make([]*state.Session, 0, 8)
	for _, sess := range appState.sessions() {
		if sess == nil || !a.sessionBelongsToFrontend(sess.Key) {
			continue
		}
		cp := stateCloneSession(sess)
		if cp == nil {
			continue
		}
		if current != "" {
			sessionStoreBackendThread(cp, current)
		}
		if !sessionRestoreBackendThread(cp, target) {
			clearSessionThreadContext(cp)
		}
		cp.Status = firstNonEmpty(strings.TrimSpace(cp.Status), "idle")
		out = append(out, cp)
	}
	return out
}

func (a *App) setConfiguredBackend(target string) error {
	if a == nil || a.cfg == nil {
		return fmt.Errorf("nil config")
	}
	target = normalizeRuntimeBackend(target)
	a.configMu.Lock()
	defer a.configMu.Unlock()
	cfg := a.feishuConfigUnlocked()
	if cfg == nil {
		return fmt.Errorf("frontend config not found")
	}
	cfg.Backend = target
	if err := a.cfg.Normalize(filepath.Dir(a.cfgPath)); err != nil {
		return err
	}
	if strings.TrimSpace(a.cfgPath) == "" {
		return nil
	}
	return config.Save(a.cfgPath, a.cfg)
}
