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
	"feidex/internal/domain/identity"
	"feidex/internal/feishu"
)

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
func patchClaudePermissionMenuRuntimeFailure(a *App, messageID, sessionKey string, applyErr error) {
	messageID = strings.TrimSpace(messageID)
	if a == nil || a.feishu == nil || messageID == "" {
		slog.Warn("claude permission mode runtime apply failed",
			"session_key", sessionKey,
			"error", applyErr,
		)
		return
	}
	card, err := renderClaudeSessionPermissionMenuCard(a, sessionKey)
	if err != nil || card == nil {
		slog.Warn("render claude permission menu for failure patch failed",
			"session_key", sessionKey,
			"error", err,
		)
		return
	}
	warning := "⚠️ 运行时未生效：" + applyErr.Error() + "（设置已保存，将在会话重启后生效）"
	card = cards.PrependMarkdownWarning(card, warning)
	if err := newEffectRunner(a.runtimeOwner).Run(context.Background(), []application.Effect{application.PatchCard{
		Frontend:  identity.FrontendID(a.FrontendID()),
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

func renderClaudeSessionPermissionMenuCard(a *App, sessionKey string) (map[string]any, error) {
	return a.BackendDriver().Permission().RenderConversationPermissionModeMenu(sessionKey, appbackend.ConversationPermissionRenderDeps{
		Permissions:    a,
		Session:        a.State().Session,
		FormatMenuBody: func(action, body string) string { return menuCardBodyForBackend(configuredBackend(a), action, body) },
	})
}

func showClaudeSessionPermissionMenu(a *App, msg *feishu.InboundMessage) error {
	card, err := renderClaudeSessionPermissionMenuCard(a, makeSessionKey(a, msg))
	if err != nil {
		return err
	}
	return newEffectRunner(a.runtimeOwner).Run(context.Background(), []application.Effect{application.SendCard{
		Frontend:       identity.FrontendID(a.FrontendID()),
		Chat:           identity.ChatRef{ID: msg.ChatID, Type: identity.ChatType(msg.ChatType)},
		ReplyMessageID: msg.MessageID,
		View:           feishuoutbound.Card(card),
		InThread:       replyInThreadEnabled(a, msg.ChatType),
	}})
}

func renderClaudeWorkspacePermissionMenuCard(workspacepresentation *workspacecards.Presentation, sessionKey string) (map[string]any, error) {
	return workspacepresentation.RenderWorkspacePermissionModeMenuCard(sessionKey)
}

func showClaudeWorkspacePermissionMenu(a *App, msg *feishu.InboundMessage) error {
	card, err := renderClaudeWorkspacePermissionMenuCard(a.bindings.WorkspacePresentation, makeSessionKey(a, msg))
	if err != nil {
		return err
	}
	return newEffectRunner(a.runtimeOwner).Run(context.Background(), []application.Effect{application.SendCard{
		Frontend:       identity.FrontendID(a.FrontendID()),
		Chat:           identity.ChatRef{ID: msg.ChatID, Type: identity.ChatType(msg.ChatType)},
		ReplyMessageID: msg.MessageID,
		View:           feishuoutbound.Card(card),
		InThread:       replyInThreadEnabled(a, msg.ChatType),
	}})
}
