package app

import (
	feishuoutbound "feidex/internal/adapter/feishu/outbound"
	"feidex/internal/application"
	appruntime "feidex/internal/runtime"

	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"feidex/internal/adapter/feishu/cards"
	appbackend "feidex/internal/app/backend"
	"feidex/internal/config"
	"feidex/internal/domain/identity"
	"feidex/internal/feishu"
)

const ()

func isClaudeBypassPermissionsEnabled(cfg *config.Config) bool {
	if cfg == nil {
		return false
	}
	return cfg.Claude.DangerouslySkipPermissions
}

func claudePermissionModeOptions(includeBypass bool) []appruntime.ClaudePermissionModeOption {
	options := []appruntime.ClaudePermissionModeOption{
		{Value: string(claudePermissionModeDefault), Label: "default"},
		{Value: string(claudePermissionModeAcceptEdits), Label: "acceptEdits"},
	}
	if includeBypass {
		options = append(options, appruntime.ClaudePermissionModeOption{Value: string(claudePermissionModeBypass), Label: "bypassPermissions"})
	}
	return options
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

func applyClaudePermissionModeToRuntime(a *App, sessionKey, mode string) error {
	if a == nil || currentClaudeCore(a) == nil {
		return nil
	}
	if runtime := backendRuntimeForKind(backendClaude); runtime == nil || !runtime.isActive(backendRuntimeContextForApp(a)) {
		return nil
	}
	sess := a.State().Session(sessionKey)
	if sess == nil || strings.TrimSpace(sess.ActiveThreadID) == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return currentClaudeCore(a).SetPermissionMode(ctx, sessionKey, mode)
}

// applyClaudePermissionModeToRuntimeAsync applies the stored permission mode
// off the Feishu ack path.
//
// Card callbacks must answer within the platform's callback deadline, and this
// apply is a CLI round-trip (set_permission_mode + its control response). The
// mode is re-read from the session when the task runs, so two rapid changes
// converge on the latest stored value instead of racing.
func applyClaudePermissionModeToRuntimeAsync(a *App, messageID, sessionKey, fallbackMode string) {
	runAsync(a, func() {
		mode := strings.TrimSpace(fallbackMode)
		if cfg := a.Config(); cfg != nil {
			if sess := a.State().Session(sessionKey); sess != nil {
				mode = effectiveBindingClaudePermissionMode(a, sess, config.FindWorkspace(cfg, sess.WorkspaceID), cfg.Claude)
			}
		}
		if err := applyClaudePermissionModeToRuntime(a, sessionKey, mode); err != nil {
			patchClaudePermissionMenuRuntimeFailure(a, messageID, sessionKey, err)
		}
	})
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
	if err := newEffectRunner(a).Run(context.Background(), []application.Effect{application.PatchCard{
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
		App:            a,
		Session:        a.State().Session,
		FormatMenuBody: func(action, body string) string { return menuCardBodyForBackend(configuredBackend(a), action, body) },
	})
}

func showClaudeSessionPermissionMenu(a *App, msg *feishu.InboundMessage) error {
	card, err := renderClaudeSessionPermissionMenuCard(a, makeSessionKey(a, msg))
	if err != nil {
		return err
	}
	return newEffectRunner(a).Run(context.Background(), []application.Effect{application.SendCard{
		Frontend:       identity.FrontendID(a.FrontendID()),
		Chat:           identity.ChatRef{ID: msg.ChatID, Type: identity.ChatType(msg.ChatType)},
		ReplyMessageID: msg.MessageID,
		View:           feishuoutbound.Card(card),
		InThread:       replyInThreadEnabled(a, msg.ChatType),
	}})
}

func renderClaudeWorkspacePermissionMenuCard(a *App, sessionKey string) (map[string]any, error) {
	return a.BackendDriver().Permission().RenderWorkspacePermissionModeMenu(sessionKey, appbackend.WorkspacePermissionRenderDeps{
		App:            a,
		FormatMenuBody: func(action, body string) string { return menuCardBodyForBackend(configuredBackend(a), action, body) },
	})
}

func showClaudeWorkspacePermissionMenu(a *App, msg *feishu.InboundMessage) error {
	card, err := renderClaudeWorkspacePermissionMenuCard(a, makeSessionKey(a, msg))
	if err != nil {
		return err
	}
	return newEffectRunner(a).Run(context.Background(), []application.Effect{application.SendCard{
		Frontend:       identity.FrontendID(a.FrontendID()),
		Chat:           identity.ChatRef{ID: msg.ChatID, Type: identity.ChatType(msg.ChatType)},
		ReplyMessageID: msg.MessageID,
		View:           feishuoutbound.Card(card),
		InThread:       replyInThreadEnabled(a, msg.ChatType),
	}})
}
