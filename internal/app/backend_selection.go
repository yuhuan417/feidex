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

func (s backendSelectionService) availableBackends() []availableBackend {
	if s.app == nil || s.app.cfg == nil {
		return nil
	}
	out := make([]availableBackend, 0, 2)
	for _, runtime := range backendRuntimeFacades() {
		command := runtime.configuredCommand(s.app)
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

func (s backendSelectionService) backendAvailable(target string) bool {
	target = normalizeRuntimeBackend(target)
	if target == "" {
		return false
	}
	for _, candidate := range newBackendSelectionService(s.app).availableBackends() {
		if candidate.Kind == target {
			return true
		}
	}
	return false
}

func (s backendSelectionService) renderBackendSelectionCard(sessionKey, notice string) map[string]any {
	current := configuredBackend(s.app)
	choices := newBackendSelectionService(s.app).availableBackends()
	lines := []string{
		"当前 backend: `" + firstNonEmpty(current, "unset") + "`",
		"当前 frontend: `" + firstNonEmpty(strings.TrimSpace(s.app.frontendID), config.DefaultFrontendID) + "`",
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
	return s.app.feishu.SimpleStatusCard("后端选择", color, menuCardBody("menu.backend.switch", strings.Join(lines, "\n")), buttons)
}

func (s backendSelectionService) renderBackendSwitchingCard(sessionKey, target string) map[string]any {
	body := strings.Join([]string{
		"正在切换到 backend: `" + normalizeRuntimeBackend(target) + "`",
		"",
		"会先切换 runtime，再恢复这个 backend 之前的 thread lineage。",
	}, "\n")
	return s.app.feishu.SimpleStatusCard("切换后端", "orange", menuCardBody("menu.backend.switch", body), []feishu.Button{
		{Text: "处理中", Type: "default", Value: map[string]any{"action": "menu.backend.switch", "session_key": sessionKey}},
	})
}

func (s backendSelectionService) replyBackendSelectionCard(msg *feishu.InboundMessage, reason string) error {
	if s.app == nil || s.app.feishu == nil {
		return fmt.Errorf("backend not configured")
	}
	sessionKey := ""
	if msg != nil {
		sessionKey = makeSessionKey(s.app, msg)
	}
	card := newBackendSelectionService(s.app).renderBackendSelectionCard(sessionKey, firstNonEmpty(strings.TrimSpace(reason), "当前 frontend 还没有设置 backend，请先选择。"))
	if msg != nil && strings.TrimSpace(msg.MessageID) != "" {
		_, err := s.app.feishu.ReplyCard(context.Background(), msg.MessageID, card, replyInThreadEnabled(s.app, msg.ChatType))
		return err
	}
	if msg != nil && strings.TrimSpace(msg.ChatID) != "" {
		_, err := s.app.feishu.SendCard(context.Background(), msg.ChatID, card)
		return err
	}
	return fmt.Errorf("backend not configured")
}

func (s backendSelectionService) commandBackend(msg *feishu.InboundMessage, args []string) error {
	if len(args) == 0 {
		return newBackendSelectionService(s.app).replyBackendSelectionCard(msg, "")
	}
	switch strings.TrimSpace(args[0]) {
	case "retry":
		return newAutoRetryService(s.app).commandAutoRetry(msg, args[1:])
	default:
		return fmt.Errorf("usage: /backend | /backend retry | /backend retry status | /backend retry on | /backend retry off")
	}
}

func (s backendSelectionService) completeMenuBackend(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "info", Content: "已打开后端选择"},
		Card:  rawCard(newCommandService(s.app).renderBackendMenuCard(sessionKey)),
	}, nil
}

func (s backendSelectionService) completeBackendSelect(action *feishu.CardAction, sessionKey, target string) (*callback.CardActionTriggerResponse, error) {
	target = normalizeRuntimeBackend(target)
	if target == "" {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "未收到有效 backend"}}, nil
	}
	if !newBackendSelectionService(s.app).backendAvailable(target) {
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "warning", Content: backendDisplayName(target) + " 当前不可用"},
			Card:  rawCard(newBackendSelectionService(s.app).renderBackendSelectionCard(sessionKey, backendDisplayName(target)+" 当前不可用，请先确认本机安装。")),
		}, nil
	}
	if current := configuredBackend(s.app); current == target && newBackendSelectionService(s.app).backendRuntimeReady(target) {
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "success", Content: "当前已经在使用 " + backendDisplayName(target)},
			Card:  rawCard(newBackendSelectionService(s.app).renderBackendSelectionCard(sessionKey, "当前已经在使用 `"+target+"`。")),
		}, nil
	}
	if reason := newBackendSelectionService(s.app).backendSwitchBlockedReason(); reason != "" {
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "warning", Content: reason},
			Card:  rawCard(newBackendSelectionService(s.app).renderBackendSelectionCard(sessionKey, reason)),
		}, nil
	}
	if action == nil || strings.TrimSpace(action.MessageID) == "" {
		if err := newBackendSelectionService(s.app).switchBackend(context.Background(), target); err != nil {
			return &callback.CardActionTriggerResponse{
				Toast: &callback.Toast{Type: "error", Content: err.Error()},
				Card:  rawCard(newBackendSelectionService(s.app).renderBackendSelectionCard(sessionKey, "切换失败: "+err.Error())),
			}, nil
		}
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "success", Content: "已切换到 " + backendDisplayName(target)},
			Card:  rawCard(newBackendSelectionService(s.app).renderBackendSelectionCard(sessionKey, "已切换到 `"+target+"`。")),
		}, nil
	}

	messageID := strings.TrimSpace(action.MessageID)
	go func() {
		err := newBackendSelectionService(s.app).switchBackend(context.Background(), target)
		notice := "已切换到 `" + target + "`。"
		if err != nil {
			notice = "切换失败: " + err.Error()
			slog.Warn("backend switch failed",
				"frontend_id", s.app.frontendID,
				"target_backend", target,
				"message_id", messageID,
				"error", err,
			)
		}
		if patchErr := s.app.feishu.PatchCard(context.Background(), messageID, newBackendSelectionService(s.app).renderBackendSelectionCard(sessionKey, notice)); patchErr != nil {
			slog.Warn("backend switch patch failed",
				"frontend_id", s.app.frontendID,
				"target_backend", target,
				"message_id", messageID,
				"error", patchErr,
			)
		}
	}()
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "info", Content: "正在切换到 " + backendDisplayName(target)},
		Card:  rawCard(newBackendSelectionService(s.app).renderBackendSwitchingCard(sessionKey, target)),
	}, nil
}

func (s backendSelectionService) backendRuntimeReady(target string) bool {
	if runtime := backendRuntimeForKind(target); runtime != nil {
		return runtime.runtimeReady(s.app)
	}
	return false
}

func (s backendSelectionService) backendSwitchBlockedReason() string {
	return frontendIdleBlockedReason(s.app)
}

func (s backendSelectionService) switchBackend(ctx context.Context, target string) error {
	if s.app == nil {
		return fmt.Errorf("app not initialized")
	}
	target = normalizeRuntimeBackend(target)
	if target == "" {
		return fmt.Errorf("missing backend")
	}
	if !newBackendSelectionService(s.app).backendAvailable(target) {
		return fmt.Errorf("%s backend 当前不可用", backendDisplayName(target))
	}

	s.app.backendSwitchMu.Lock()
	defer s.app.backendSwitchMu.Unlock()

	if reason := newBackendSelectionService(s.app).backendSwitchBlockedReason(); reason != "" {
		return fmt.Errorf("%s", reason)
	}

	current := configuredBackend(s.app)
	if current == target && newBackendSelectionService(s.app).backendRuntimeReady(target) {
		return nil
	}
	newRuntimeStateService(s.app).beginBackendSwitchState(target)
	defer newRuntimeStateService(s.app).finishBackendSwitchState()
	slog.Info("backend switch begin",
		"frontend_id", s.app.frontendID,
		"current_backend", current,
		"target_backend", target,
	)

	nextSessions := newBackendSelectionService(s.app).frontendSessionsAfterBackendSwitch(current, target)
	newHandle, err := prepareBackendRuntime(s.app,ctx, target)
	if err != nil {
		return err
	}

	oldHandle := currentBackendRuntimeHandle(s.app)
	oldBackend := currentRuntimeBackend(s.app)
	if err := newBackendSelectionService(s.app).setConfiguredBackend(target); err != nil {
		_ = newHandle.close()
		return err
	}

	newHandle.install(s.app)
	for _, sess := range nextSessions {
		if err := s.app.store.UpsertSession(sess); err != nil {
			oldHandle.install(s.app)
			setRuntimeBackend(s.app, oldBackend)
			_ = newBackendSelectionService(s.app).setConfiguredBackend(current)
			_ = newHandle.close()
			return err
		}
	}
	slog.Info("backend switch runtime installed",
		"frontend_id", s.app.frontendID,
		"target_backend", target,
	)
	recoverFrontendRuntimeState(s.app)
	_ = oldHandle.close()
	slog.Info("backend switch completed",
		"frontend_id", s.app.frontendID,
		"current_backend", current,
		"target_backend", target,
	)
	return nil
}

func (s backendSelectionService) frontendSessionsAfterBackendSwitch(current, target string) []*state.Session {
	appState := appState(s.app)
	out := make([]*state.Session, 0, 8)
	for _, sess := range appState.sessions() {
		if sess == nil || !sessionBelongsToFrontend(s.app, sess.Key) {
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

func (s backendSelectionService) setConfiguredBackend(target string) error {
	if s.app == nil || s.app.cfg == nil {
		return fmt.Errorf("nil config")
	}
	target = normalizeRuntimeBackend(target)
	s.app.configMu.Lock()
	defer s.app.configMu.Unlock()
	cfg := feishuConfigUnlocked(s.app)
	if cfg == nil {
		return fmt.Errorf("frontend config not found")
	}
	cfg.Backend = target
	if err := s.app.cfg.Normalize(filepath.Dir(s.app.cfgPath)); err != nil {
		return err
	}
	if strings.TrimSpace(s.app.cfgPath) == "" {
		return nil
	}
	return config.Save(s.app.cfgPath, s.app.cfg)
}
