package feishuapp

import (
	feishuoutbound "feidex/internal/adapter/feishu/outbound"
	workspacecards "feidex/internal/adapter/feishu/workspace"
	"feidex/internal/application"
	appruntime "feidex/internal/runtime"

	"context"
	"log/slog"
	"strings"

	appbackend "feidex/internal/adapter/feishu/backend"
	"feidex/internal/adapter/feishu/cards"
	"feidex/internal/config"
	"feidex/internal/domain/conversation"
	"feidex/internal/domain/identity"
)

func renderClaudeWorkspacePermissionMenuCard(workspacepresentation *workspacecards.Presentation, sessionKey string) (map[string]any, error) {
	return workspacepresentation.RenderWorkspacePermissionModeMenuCard(sessionKey)
}

func isClaudeBypassPermissionsEnabled(cfg *config.Config) bool {
	if cfg == nil {
		return false
	}
	return cfg.Claude.DangerouslySkipPermissions
}

func claudePermissionModeOptions(includeBypass bool) []appruntime.ClaudePermissionModeOption {
	options := []appruntime.ClaudePermissionModeOption{
		{Value: string(appruntime.ClaudePermissionModeDefault), Label: "default"},
		{Value: string(appruntime.ClaudePermissionModeAcceptEdits), Label: "acceptEdits"},
	}
	if includeBypass {
		options = append(options, appruntime.ClaudePermissionModeOption{Value: string(appruntime.ClaudePermissionModeBypass), Label: "bypassPermissions"})
	}
	return options
}

// patchClaudePermissionMenuRuntimeFailure re-renders the permission menu with a
// warning so the card does not claim a runtime change that never landed.
func patchClaudePermissionMenuRuntimeFailure(render func(string) (map[string]any, error), frontendID identity.FrontendID, runner appruntime.EffectRunner, messageID, sessionKey string, applyErr error) {
	messageID = strings.TrimSpace(messageID)
	if render == nil || messageID == "" {
		slog.Warn("claude permission mode runtime apply failed",
			"session_key", sessionKey,
			"error", applyErr,
		)
		return
	}
	card, err := render(sessionKey)
	if err != nil || card == nil {
		slog.Warn("render claude permission menu for failure patch failed",
			"session_key", sessionKey,
			"error", err,
		)
		return
	}
	warning := "⚠️ 运行时未生效：" + applyErr.Error() + "（设置已保存，将在会话重启后生效）"
	card = cards.PrependMarkdownWarning(card, warning)
	if err := runner.Run(context.Background(), []application.Effect{application.PatchCard{
		Frontend:  frontendID,
		MessageID: messageID,
		View:      feishuoutbound.Card(card),
	}}); err != nil {
		slog.Warn("patch claude permission menu failed",
			"session_key", sessionKey,
			"message_id", messageID,
			"error", err,
		)
	}
}

type permissionConfig struct{ config *config.Config }

func (p permissionConfig) Config() *config.Config { return p.config }

func ClaudePermissionMenuRenderer(cfg *config.Config, backend func() string, session func(string) *conversation.Session) func(string) (map[string]any, error) {
	return func(sessionKey string) (map[string]any, error) {
		return (appbackend.SelectedDriver{Selected: backend}).Permission().RenderConversationPermissionModeMenu(sessionKey, appbackend.ConversationPermissionRenderDeps{
			Permissions: permissionConfig{config: cfg},
			Session:     session,
			FormatMenuBody: func(action, body string) string {
				return menuCardBodyForBackend(backend(), action, body)
			},
		})
	}
}
