package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

const (
	claudeSessionCommandUsage   = "/session | /session list [all] | /session new | /session fork | /session resume SESSION_ID | /session permissions [MODE|inherit]"
	claudeWorkspaceCommandUsage = "/workspace | /workspace list | /workspace new | /workspace clone GIT_URL [ID] [--parent DIR] | /workspace use ID | /workspace delete [ID] | /workspace permissions [MODE|inherit]"
)

type claudePermissionModeOption struct {
	Value string
	Label string
}

func (a *App) isClaudeBypassPermissionsEnabled() bool {
	if a == nil || a.cfg == nil {
		return false
	}
	return a.cfg.Claude.DangerouslySkipPermissions
}

func claudePermissionModeOptions(includeBypass bool) []claudePermissionModeOption {
	options := []claudePermissionModeOption{
		{Value: string(claudePermissionModeDefault), Label: "default"},
		{Value: string(claudePermissionModeAcceptEdits), Label: "acceptEdits"},
	}
	if includeBypass {
		options = append(options, claudePermissionModeOption{Value: string(claudePermissionModeBypass), Label: "bypassPermissions"})
	}
	return options
}

func claudePermissionModeLabel(value string) string {
	value = normalizeClaudePermissionModeValue(value)
	if value == "" {
		value = string(claudePermissionModeDefault)
	}
	return "`" + value + "`"
}

func (a *App) normalizeRequestedClaudePermissionMode(ctx context.Context, raw string) (string, string, error) {
	_ = ctx
	mode := normalizeClaudePermissionModeValue(raw)
	switch mode {
	case string(claudePermissionModeDefault), string(claudePermissionModeAcceptEdits), string(claudePermissionModeBypass):
	default:
		return "", "", fmt.Errorf("不支持的 Claude 权限模式 `%s`", strings.TrimSpace(raw))
	}
	if mode == string(claudePermissionModeBypass) && !a.isClaudeBypassPermissionsEnabled() {
		return "", "", fmt.Errorf("当前未启用 `claude.dangerously_skip_permissions`，不能切到 `bypassPermissions`")
	}
	return mode, "", nil
}

func normalizeClaudePermissionOverrideValue(raw string) (string, bool) {
	switch strings.TrimSpace(raw) {
	case "", "inherit", "follow", "workspace", "global":
		return "", true
	default:
		return "", false
	}
}

func (a *App) applyClaudePermissionModeToRuntime(sessionKey, mode string) error {
	if a == nil || a.claude == nil {
		return nil
	}
	if runtime := backendRuntimeForKind(backendClaude); runtime == nil || !runtime.isActive(a) {
		return nil
	}
	sess := a.appState().session(sessionKey)
	if sess == nil || strings.TrimSpace(sess.ActiveThreadID) == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return a.claude.SetPermissionMode(ctx, sessionKey, mode)
}

func (a *App) renderClaudeSessionPermissionMenuCard(sessionKey string) (map[string]any, error) {
	sess := a.appState().session(sessionKey)
	workspaceID := a.defaultWorkspaceID()
	if sess != nil && strings.TrimSpace(sess.WorkspaceID) != "" {
		workspaceID = sess.WorkspaceID
	}
	ws := config.FindWorkspace(a.cfg, workspaceID)
	if sess == nil || strings.TrimSpace(sess.ActiveThreadID) == "" {
		return nil, fmt.Errorf("当前没有活动会话")
	}
	threadID := strings.TrimSpace(sess.ActiveThreadID)
	effective := effectiveClaudePermissionMode(sess, ws, a.cfg.Claude)
	override := strings.TrimSpace(sess.ActiveClaudePermissionMode)
	bodyLines := []string{
		"配置当前 Claude 会话权限模式。",
		"",
		"session: `" + threadID + "`",
		"生效值: " + claudePermissionModeLabel(effective),
	}
	if override == "" {
		bodyLines = append(bodyLines, "当前覆盖: 跟随工作区")
	} else {
		bodyLines = append(bodyLines, "当前覆盖: "+claudePermissionModeLabel(override))
	}
	buttons := make([]feishu.Button, 0, 6)
	followType := "default"
	followLabel := "跟随工作区"
	if override == "" {
		followType = "primary"
		followLabel = "当前 · 跟随工作区"
	}
	buttons = append(buttons, feishu.Button{
		Text: followLabel,
		Type: followType,
		Value: map[string]any{
			"action":      "thread.permission_mode.set",
			"session_key": sessionKey,
			"thread_id":   threadID,
			"mode":        "",
		},
	})
	for _, opt := range claudePermissionModeOptions(a.isClaudeBypassPermissionsEnabled()) {
		btnType := "default"
		label := opt.Label
		if opt.Value == override {
			btnType = "primary"
			label = "当前 · " + label
		}
		buttons = append(buttons, feishu.Button{
			Text: label,
			Type: btnType,
			Value: map[string]any{
				"action":      "thread.permission_mode.set",
				"session_key": sessionKey,
				"thread_id":   threadID,
				"mode":        opt.Value,
			},
		})
	}
	buttons = append(buttons, feishu.Button{
		Text: commandLabel("返回会话", "/session"),
		Type: "default",
		Value: map[string]any{
			"action":      "menu.thread",
			"session_key": sessionKey,
		},
	})
	return a.feishu.SimpleStatusCard("配置会话权限", "blue", menuCardBodyForBackend(a.configuredBackend(), "thread.permission_mode.menu", strings.Join(bodyLines, "\n")), buttons), nil
}

func (a *App) showClaudeSessionPermissionMenu(msg *feishu.InboundMessage) error {
	card, err := a.renderClaudeSessionPermissionMenuCard(a.makeSessionKey(msg))
	if err != nil {
		return err
	}
	_, err = a.feishu.ReplyCard(context.Background(), msg.MessageID, card, a.replyInThreadEnabled(msg.ChatType))
	return err
}

func (s threadActionService) completeClaudeSessionPermissionModeSet(action *feishu.CardAction, sessionKey, threadID, rawMode string) (*callback.CardActionTriggerResponse, error) {
	appState := s.app.appState()
	sess := appState.session(sessionKey)
	if sess == nil || strings.TrimSpace(sess.ActiveThreadID) == "" || strings.TrimSpace(sess.ActiveThreadID) != strings.TrimSpace(threadID) {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "当前会话已失效"}}, nil
	}
	mode := ""
	warning := ""
	if override, ok := normalizeClaudePermissionOverrideValue(rawMode); ok {
		mode = override
	} else {
		var err error
		mode, warning, err = s.app.normalizeRequestedClaudePermissionMode(context.Background(), rawMode)
		if err != nil {
			return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
		}
	}
	sess.ActiveClaudePermissionMode = mode
	if err := appState.saveSession(sess); err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: err.Error()}}, nil
	}
	effective := effectiveClaudePermissionMode(sess, config.FindWorkspace(s.app.cfg, sess.WorkspaceID), s.app.cfg.Claude)
	if err := s.app.applyClaudePermissionModeToRuntime(sessionKey, effective); err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: err.Error()}}, nil
	}
	card, err := s.app.renderClaudeSessionPermissionMenuCard(sessionKey)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	content := "已更新 Claude 会话权限模式"
	if warning != "" {
		content = warning
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: content},
		Card:  rawCard(card),
	}, nil
}

func (a *App) renderClaudeWorkspacePermissionMenuCard(sessionKey string) (map[string]any, error) {
	var sess *state.Session
	if a.store != nil {
		sess = a.appState().session(sessionKey)
	}
	workspaceID := a.defaultWorkspaceID()
	if sess != nil && strings.TrimSpace(sess.WorkspaceID) != "" {
		workspaceID = sess.WorkspaceID
	}
	ws := config.FindWorkspace(a.cfg, workspaceID)
	if ws == nil {
		return nil, fmt.Errorf("current workspace not found")
	}
	effective := effectiveClaudePermissionMode(nil, ws, a.cfg.Claude)
	override := strings.TrimSpace(ws.ClaudePermissionMode)
	bodyLines := []string{
		"配置当前工作区默认 Claude 权限模式。",
		"",
		"当前工作区: `" + ws.ID + "`",
		"生效值: " + claudePermissionModeLabel(effective),
	}
	if override == "" {
		bodyLines = append(bodyLines, "当前覆盖: 跟随全局")
	} else {
		bodyLines = append(bodyLines, "当前覆盖: "+claudePermissionModeLabel(override))
	}
	buttons := make([]feishu.Button, 0, 6)
	followType := "default"
	followLabel := "跟随全局"
	if override == "" {
		followType = "primary"
		followLabel = "当前 · 跟随全局"
	}
	buttons = append(buttons, feishu.Button{
		Text: followLabel,
		Type: followType,
		Value: map[string]any{
			"action":       "workspace.permission_mode.set",
			"session_key":  sessionKey,
			"workspace_id": ws.ID,
			"mode":         "",
		},
	})
	for _, opt := range claudePermissionModeOptions(a.isClaudeBypassPermissionsEnabled()) {
		btnType := "default"
		label := opt.Label
		if opt.Value == override {
			btnType = "primary"
			label = "当前 · " + label
		}
		buttons = append(buttons, feishu.Button{
			Text: label,
			Type: btnType,
			Value: map[string]any{
				"action":       "workspace.permission_mode.set",
				"session_key":  sessionKey,
				"workspace_id": ws.ID,
				"mode":         opt.Value,
			},
		})
	}
	buttons = append(buttons, feishu.Button{
		Text: commandLabel("返回工作区", "/workspace"),
		Type: "default",
		Value: map[string]any{
			"action":      "menu.workspace",
			"session_key": sessionKey,
		},
	})
	return a.feishu.SimpleStatusCard("配置默认权限", "blue", menuCardBodyForBackend(a.configuredBackend(), "workspace.permission_mode.menu", strings.Join(bodyLines, "\n")), buttons), nil
}

func (a *App) showClaudeWorkspacePermissionMenu(msg *feishu.InboundMessage) error {
	card, err := a.renderClaudeWorkspacePermissionMenuCard(a.makeSessionKey(msg))
	if err != nil {
		return err
	}
	_, err = a.feishu.ReplyCard(context.Background(), msg.MessageID, card, a.replyInThreadEnabled(msg.ChatType))
	return err
}

func (s workspaceActionService) completeClaudeWorkspacePermissionModeSet(action *feishu.CardAction, sessionKey, workspaceID, rawMode string) (*callback.CardActionTriggerResponse, error) {
	mode := ""
	warning := ""
	if override, ok := normalizeClaudePermissionOverrideValue(rawMode); ok {
		mode = override
	} else {
		var err error
		mode, warning, err = s.app.normalizeRequestedClaudePermissionMode(context.Background(), rawMode)
		if err != nil {
			return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
		}
	}
	_, err := s.app.updateWorkspaceDefaults(workspaceID, func(w *config.Workspace) {
		w.ClaudePermissionMode = mode
	})
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: err.Error()}}, nil
	}
	sess := s.app.appState().session(sessionKey)
	if sess != nil && strings.TrimSpace(sess.WorkspaceID) == strings.TrimSpace(workspaceID) && strings.TrimSpace(sess.ActiveClaudePermissionMode) == "" {
		effective := effectiveClaudePermissionMode(sess, config.FindWorkspace(s.app.cfg, workspaceID), s.app.cfg.Claude)
		if err := s.app.applyClaudePermissionModeToRuntime(sessionKey, effective); err != nil {
			return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: err.Error()}}, nil
		}
	}
	card, renderErr := s.app.renderClaudeWorkspacePermissionMenuCard(sessionKey)
	if renderErr != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: renderErr.Error()}}, nil
	}
	content := "已更新 Claude 默认权限模式"
	if warning != "" {
		content = warning
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: content},
		Card:  rawCard(card),
	}, nil
}
