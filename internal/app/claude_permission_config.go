package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	appbackend "feidex/internal/app/backend"
	"feidex/internal/config"
	"feidex/internal/feishu"
)

const (
	claudeSessionCommandUsage   = "/session | /session list [all] | /session new | /session fork | /session resume SESSION_ID | /session permissions [MODE|inherit]"
	claudeWorkspaceCommandUsage = "/workspace | /workspace list | /workspace new | /workspace clone GIT_URL [ID] [--parent DIR] | /workspace use ID | /workspace delete [ID] | /workspace permissions [MODE|inherit]"
)

func isClaudeBypassPermissionsEnabled(cfg *config.Config) bool {
	if cfg == nil {
		return false
	}
	return cfg.Claude.DangerouslySkipPermissions
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

func normalizeRequestedClaudePermissionMode(a *App, ctx context.Context, raw string) (string, string, error) {
	_ = ctx
	mode := normalizeClaudePermissionModeValue(raw)
	switch mode {
	case string(claudePermissionModeDefault), string(claudePermissionModeAcceptEdits), string(claudePermissionModeBypass):
	default:
		return "", "", fmt.Errorf("不支持的 Claude 权限模式 `%s`", strings.TrimSpace(raw))
	}
	if mode == string(claudePermissionModeBypass) && !isClaudeBypassPermissionsEnabled(a.cfg) {
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

func applyClaudePermissionModeToRuntime(a *App, sessionKey, mode string) error {
	if a == nil || a.claude == nil {
		return nil
	}
	if runtime := backendRuntimeForKind(backendClaude); runtime == nil || !runtime.isActive(a) {
		return nil
	}
	sess := a.State().Session(sessionKey)
	if sess == nil || strings.TrimSpace(sess.ActiveThreadID) == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return a.claude.SetPermissionMode(ctx, sessionKey, mode)
}

func renderClaudeSessionPermissionMenuCard(a *App, sessionKey string) (map[string]any, error) {
	return appbackend.DriverForApp(a).Permission().RenderConversationPermissionModeMenu(sessionKey, appbackend.ConversationPermissionRenderDeps{
		App:            a,
		Session:        a.State().Session,
		FormatMenuBody: func(action, body string) string { return menuCardBodyForBackend(configuredBackend(a), action, body) },
		CommandLabel:   commandLabel,
	})
}

func showClaudeSessionPermissionMenu(a *App, msg *feishu.InboundMessage) error {
	card, err := renderClaudeSessionPermissionMenuCard(a, makeSessionKey(a, msg))
	if err != nil {
		return err
	}
	_, err = a.feishu.ReplyCard(context.Background(), msg.MessageID, card, replyInThreadEnabled(a, msg.ChatType))
	return err
}

func renderClaudeWorkspacePermissionMenuCard(a *App, sessionKey string) (map[string]any, error) {
	return appbackend.DriverForApp(a).Permission().RenderWorkspacePermissionModeMenu(sessionKey, appbackend.WorkspacePermissionRenderDeps{
		App:            a,
		FormatMenuBody: func(action, body string) string { return menuCardBodyForBackend(configuredBackend(a), action, body) },
	})
}

func showClaudeWorkspacePermissionMenu(a *App, msg *feishu.InboundMessage) error {
	card, err := renderClaudeWorkspacePermissionMenuCard(a, makeSessionKey(a, msg))
	if err != nil {
		return err
	}
	_, err = a.feishu.ReplyCard(context.Background(), msg.MessageID, card, replyInThreadEnabled(a, msg.ChatType))
	return err
}
