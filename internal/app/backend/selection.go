package backend

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	"feidex/internal/app/appcore"
	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

// AvailableBackend describes a backend binary found on this machine.
type AvailableBackend struct {
	Kind    string
	Command string
	Path    string
}

// BackendRuntimeHandle is an opaque handle to a prepared backend runtime.
// The caller invokes Close when done and Install to activate it.
type BackendRuntimeHandle struct {
	Close   func() error
	Install func()
}

type SelectionRuntimeDeps struct {
	ListAvailableBackends func() []AvailableBackend
	PrepareRuntime        func(ctx context.Context, target string) (*BackendRuntimeHandle, error)
	SnapshotRuntime       func() *BackendRuntimeHandle
	RecoverState          func()
	IdleBlockedReason     func() string
	RuntimeReady          func(target string) bool
}

type SelectionRenderDeps struct {
	BuildMenuCard func(sessionKey string) map[string]any
	BuildCardBody func(action, body string) string
}

type SelectionCommandDeps struct {
	CommandAutoRetry func(msg *feishu.InboundMessage, args []string) error
}

type SelectionDeps struct {
	App      App
	Runtime  SelectionRuntimeDeps
	Render   SelectionRenderDeps
	Commands SelectionCommandDeps
}

// SelectionService manages backend selection, switching, and configuration
// display.
type SelectionService struct {
	App  App
	deps SelectionDeps
}

// NewSelectionService creates a new SelectionService.
func NewSelectionService(deps SelectionDeps) SelectionService {
	return SelectionService{App: deps.App, deps: deps}
}

// RawCard wraps a card map as a raw callback Card.
func RawCard(card map[string]any) *callback.Card {
	return &callback.Card{Type: "raw", Data: card}
}

// AvailableBackends returns the list of backends available on this machine.
func (s SelectionService) AvailableBackends() []AvailableBackend {
	if s.deps.Runtime.ListAvailableBackends != nil {
		return s.deps.Runtime.ListAvailableBackends()
	}
	return nil
}

// BackendAvailable reports whether the named backend is available.
func (s SelectionService) BackendAvailable(target string) bool {
	target = NormalizeRuntimeBackend(target)
	if target == "" {
		return false
	}
	for _, candidate := range s.AvailableBackends() {
		if candidate.Kind == target {
			return true
		}
	}
	return false
}

// BackendRuntimeReady reports whether the named backend's runtime is ready.
func (s SelectionService) BackendRuntimeReady(target string) bool {
	if s.deps.Runtime.RuntimeReady != nil {
		return s.deps.Runtime.RuntimeReady(target)
	}
	return false
}

// BackendSwitchBlockedReason returns a reason if a backend switch is blocked.
func (s SelectionService) BackendSwitchBlockedReason() string {
	if s.deps.Runtime.IdleBlockedReason != nil {
		return s.deps.Runtime.IdleBlockedReason()
	}
	return ""
}

// RenderBackendSelectionCard builds the backend selection interactive card.
func (s SelectionService) RenderBackendSelectionCard(sessionKey, notice string) map[string]any {
	current := appcore.ConfiguredBackend(s.App)
	choices := s.AvailableBackends()
	frontendID := appcore.FirstNonEmpty(strings.TrimSpace(s.App.FrontendID()), config.DefaultFrontendID)
	lines := []string{
		"当前 backend: `" + appcore.FirstNonEmpty(current, "unset") + "`",
		"当前 frontend: `" + frontendID + "`",
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
		label := BackendDisplayName(choice.Kind)
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
	body := strings.Join(lines, "\n")
	if s.deps.Render.BuildCardBody != nil {
		body = s.deps.Render.BuildCardBody("menu.backend.switch", body)
	}
	return s.App.Feishu().SimpleStatusCard("后端选择", color, body, buttons)
}

// RenderBackendSwitchingCard builds the in-progress switching card.
func (s SelectionService) RenderBackendSwitchingCard(sessionKey, target string) map[string]any {
	body := strings.Join([]string{
		"正在切换到 backend: `" + NormalizeRuntimeBackend(target) + "`",
		"",
		"会先切换 runtime，再恢复这个 backend 之前的 thread lineage。",
	}, "\n")
	cardBody := body
	if s.deps.Render.BuildCardBody != nil {
		cardBody = s.deps.Render.BuildCardBody("menu.backend.switch", body)
	}
	return s.App.Feishu().SimpleStatusCard("切换后端", "orange", cardBody, []feishu.Button{
		{Text: "处理中", Type: "default", Value: map[string]any{"action": "menu.backend.switch", "session_key": sessionKey}},
	})
}

// ReplyBackendSelectionCard sends the backend selection card as a reply.
func (s SelectionService) ReplyBackendSelectionCard(msg *feishu.InboundMessage, reason string) error {
	if s.App == nil || s.App.Feishu() == nil {
		return fmt.Errorf("backend not configured")
	}
	sessionKey := ""
	if msg != nil {
		sessionKey = appcore.MakeSessionKey(s.App, msg)
	}
	card := s.RenderBackendSelectionCard(sessionKey, appcore.FirstNonEmpty(strings.TrimSpace(reason), "当前 frontend 还没有设置 backend，请先选择。"))
	if msg != nil && strings.TrimSpace(msg.MessageID) != "" {
		_, err := s.App.Feishu().ReplyCard(context.Background(), msg.MessageID, card, appcore.ReplyInThreadEnabled(s.App, msg.ChatType))
		return err
	}
	if msg != nil && strings.TrimSpace(msg.ChatID) != "" {
		_, err := s.App.Feishu().SendCard(context.Background(), msg.ChatID, card)
		return err
	}
	return fmt.Errorf("backend not configured")
}

// CommandBackend handles the /backend command.
func (s SelectionService) CommandBackend(msg *feishu.InboundMessage, args []string) error {
	if len(args) == 0 {
		return s.ReplyBackendSelectionCard(msg, "")
	}
	switch strings.TrimSpace(args[0]) {
	case "retry":
		if s.deps.Commands.CommandAutoRetry != nil {
			return s.deps.Commands.CommandAutoRetry(msg, args[1:])
		}
		return fmt.Errorf("auto-retry not available")
	default:
		return fmt.Errorf("usage: /backend | /backend retry | /backend retry status | /backend retry on | /backend retry off")
	}
}

// CompleteMenuBackend handles the menu.backend card action.
func (s SelectionService) CompleteMenuBackend(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	var card map[string]any
	if s.deps.Render.BuildMenuCard != nil {
		card = s.deps.Render.BuildMenuCard(sessionKey)
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "info", Content: "已打开后端选择"},
		Card:  RawCard(card),
	}, nil
}

// CompleteBackendSelect handles the backend.select card action.
func (s SelectionService) CompleteBackendSelect(action *feishu.CardAction, sessionKey, target string) (*callback.CardActionTriggerResponse, error) {
	target = NormalizeRuntimeBackend(target)
	if target == "" {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "未收到有效 backend"}}, nil
	}
	if !s.BackendAvailable(target) {
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "warning", Content: BackendDisplayName(target) + " 当前不可用"},
			Card:  RawCard(s.RenderBackendSelectionCard(sessionKey, BackendDisplayName(target)+" 当前不可用，请先确认本机安装。")),
		}, nil
	}
	if current := appcore.ConfiguredBackend(s.App); current == target && s.BackendRuntimeReady(target) {
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "success", Content: "当前已经在使用 " + BackendDisplayName(target)},
			Card:  RawCard(s.RenderBackendSelectionCard(sessionKey, "当前已经在使用 `"+target+"`。")),
		}, nil
	}
	if reason := s.BackendSwitchBlockedReason(); reason != "" {
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "warning", Content: reason},
			Card:  RawCard(s.RenderBackendSelectionCard(sessionKey, reason)),
		}, nil
	}
	if action == nil || strings.TrimSpace(action.MessageID) == "" {
		if err := s.SwitchBackend(context.Background(), target); err != nil {
			return &callback.CardActionTriggerResponse{
				Toast: &callback.Toast{Type: "error", Content: err.Error()},
				Card:  RawCard(s.RenderBackendSelectionCard(sessionKey, "切换失败: "+err.Error())),
			}, nil
		}
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "success", Content: "已切换到 " + BackendDisplayName(target)},
			Card:  RawCard(s.RenderBackendSelectionCard(sessionKey, "已切换到 `"+target+"`。")),
		}, nil
	}

	messageID := strings.TrimSpace(action.MessageID)
	go func() {
		err := s.SwitchBackend(context.Background(), target)
		notice := "已切换到 `" + target + "`。"
		if err != nil {
			notice = "切换失败: " + err.Error()
			slog.Warn("backend switch failed",
				"frontend_id", s.App.FrontendID(),
				"target_backend", target,
				"message_id", messageID,
				"error", err,
			)
		}
		if patchErr := s.App.Feishu().PatchCard(context.Background(), messageID, s.RenderBackendSelectionCard(sessionKey, notice)); patchErr != nil {
			slog.Warn("backend switch patch failed",
				"frontend_id", s.App.FrontendID(),
				"target_backend", target,
				"message_id", messageID,
				"error", patchErr,
			)
		}
	}()
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "info", Content: "正在切换到 " + BackendDisplayName(target)},
		Card:  RawCard(s.RenderBackendSwitchingCard(sessionKey, target)),
	}, nil
}

// SwitchBackend performs the backend switch.
func (s SelectionService) SwitchBackend(ctx context.Context, target string) error {
	if s.App == nil {
		return fmt.Errorf("app not initialized")
	}
	target = NormalizeRuntimeBackend(target)
	if target == "" {
		return fmt.Errorf("missing backend")
	}
	if !s.BackendAvailable(target) {
		return fmt.Errorf("%s backend 当前不可用", BackendDisplayName(target))
	}

	s.App.BackendSwitchMu().Lock()
	defer s.App.BackendSwitchMu().Unlock()

	if reason := s.BackendSwitchBlockedReason(); reason != "" {
		return fmt.Errorf("%s", reason)
	}

	current := appcore.ConfiguredBackend(s.App)
	if current == target && s.BackendRuntimeReady(target) {
		return nil
	}
	ts := NewRuntimeStateService(s.App)
	ts.BeginBackendSwitchState(target)
	defer ts.FinishBackendSwitchState()
	slog.Info("backend switch begin",
		"frontend_id", s.App.FrontendID(),
		"current_backend", current,
		"target_backend", target,
	)

	nextSessions := s.FrontendSessionsAfterBackendSwitch(current, target)

	if s.deps.Runtime.PrepareRuntime == nil {
		return fmt.Errorf("PrepareRuntime callback not set")
	}
	newHandle, err := s.deps.Runtime.PrepareRuntime(ctx, target)
	if err != nil {
		return err
	}

	if s.deps.Runtime.SnapshotRuntime == nil {
		_ = newHandle.Close()
		return fmt.Errorf("SnapshotRuntime callback not set")
	}
	oldHandle := s.deps.Runtime.SnapshotRuntime()
	oldBackend := appcore.CurrentRuntimeBackend(s.App)
	if err := s.SetConfiguredBackend(target); err != nil {
		_ = newHandle.Close()
		return err
	}

	newHandle.Install()
	for _, sess := range nextSessions {
		if err := s.App.Store().UpsertSession(sess); err != nil {
			if oldHandle != nil {
				oldHandle.Install()
			}
			s.App.SetBackend(oldBackend)
			_ = s.SetConfiguredBackend(current)
			_ = newHandle.Close()
			return err
		}
	}
	slog.Info("backend switch runtime installed",
		"frontend_id", s.App.FrontendID(),
		"target_backend", target,
	)
	if s.deps.Runtime.RecoverState != nil {
		s.deps.Runtime.RecoverState()
	}
	if oldHandle != nil {
		_ = oldHandle.Close()
	}
	slog.Info("backend switch completed",
		"frontend_id", s.App.FrontendID(),
		"current_backend", current,
		"target_backend", target,
	)
	return nil
}

// FrontendSessionsAfterBackendSwitch returns session copies with thread lineage
// transferred from the current backend to the target backend.
func (s SelectionService) FrontendSessionsAfterBackendSwitch(current, target string) []*state.Session {
	store := s.App.Store()
	if store == nil {
		return nil
	}
	out := make([]*state.Session, 0, 8)
	for _, sess := range store.AllSessions() {
		if sess == nil || !appcore.SessionBelongsToFrontend(s.App, sess.Key) {
			continue
		}
		cp := appcore.StateCloneSession(sess)
		if cp == nil {
			continue
		}
		if current != "" {
			appcore.SessionStoreBackendThread(cp, current)
		}
		if !appcore.SessionRestoreBackendThread(cp, target) {
			appcore.ClearSessionThreadContext(cp)
		}
		cp.Status = appcore.FirstNonEmpty(strings.TrimSpace(cp.Status), "idle")
		out = append(out, cp)
	}
	return out
}

// SetConfiguredBackend persists the target backend to config.
func (s SelectionService) SetConfiguredBackend(target string) error {
	if s.App == nil || s.App.Config() == nil {
		return fmt.Errorf("nil config")
	}
	target = NormalizeRuntimeBackend(target)
	s.App.ConfigMu().Lock()
	defer s.App.ConfigMu().Unlock()
	cfg := appcore.FeishuConfigUnlocked(s.App)
	if cfg == nil {
		return fmt.Errorf("frontend config not found")
	}
	cfg.Backend = target
	if err := s.App.Config().Normalize(filepath.Dir(s.App.ConfigPath())); err != nil {
		return err
	}
	if strings.TrimSpace(s.App.ConfigPath()) == "" {
		return nil
	}
	return config.Save(s.App.ConfigPath(), s.App.Config())
}
