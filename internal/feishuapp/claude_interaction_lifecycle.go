package feishuapp

import (
	"context"
	feishuoutbound "feidex/internal/adapter/feishu/outbound"
	"feidex/internal/application"
	domainbackend "feidex/internal/domain/backend"
	"log/slog"
	"strings"

	"feidex/internal/domain/identity"
	domaininteraction "feidex/internal/domain/interaction"
	appclauderuntime "feidex/internal/runtime/claude"
)

// detachedCardAnchor converts a runtime interaction target into the delivery
// anchor used by the pending-card scaffold.
func detachedCardAnchor(target appclauderuntime.InteractionTarget) pendingCardAnchor {
	return pendingCardAnchor{
		sessionKey:       strings.TrimSpace(target.SessionKey),
		chatID:           strings.TrimSpace(target.ChatID),
		triggerMessageID: strings.TrimSpace(target.TriggerMessageID),
		threadID:         strings.TrimSpace(target.ThreadID),
		turnID:           strings.TrimSpace(target.TurnID),
		ownerUserID:      strings.TrimSpace(target.UserID),
	}
}

// ExpireClaudeInteractionCards closes out Claude cards whose backend request
// died with the session (reset, backend switch, restart, process exit). The
// request can no longer be answered, so the card is replaced with a status
// card instead of staying clickable and failing later.
//
// An empty requestIDs slice targets every open Claude interaction of the
// session, which is what transport failures need.
func ExpireClaudeInteractionCards(a *App, sessionKey string, requestIDs []string, reason string) {
	if a == nil {
		return
	}
	a.bindings.InteractionLifecycle.ExpireAndPresent(domainbackend.BackendClaude, sessionKey, requestIDs, reason)
}

type interactionExpiryPresentation struct{ app *App }

func InteractionExpiryPresentation(a *App) interface {
	ExpiredInteraction(*domaininteraction.PendingRequest, string)
} {
	return interactionExpiryPresentation{app: a}
}
func (p interactionExpiryPresentation) ExpiredInteraction(pending *domaininteraction.PendingRequest, reason string) {
	a := p.app
	body := claudeInteractionExpiredBody(reason)
	messageID := strings.TrimSpace(pending.FeishuMsgID)
	if messageID == "" || a.feishu == nil {
		return
	}
	title := contentCardTitleForSession(a, pending.SessionKey, "", "请求已失效")
	card := a.feishu.SimpleStatusCard(title, "grey", body, nil)
	if err := newEffectRunner(a.runtimeOwner).Run(context.Background(), []application.Effect{application.PatchCard{
		Frontend:  identity.FrontendID(a.FrontendID()),
		MessageID: messageID,
		View:      feishuoutbound.Card(card),
	}}); err != nil {
		slog.Warn("expire claude interaction card failed",
			"request_id", pending.ID,
			"message_id", messageID,
			"error", err,
		)
	}
}

func claudeInteractionExpiredBody(reason string) string {
	detail := "Claude 会话已结束。"
	switch strings.TrimSpace(reason) {
	case "session reset":
		detail = "Claude 会话已重置（模型或权限设置变更、手动重启）。"
	case "session ended":
		detail = "Claude 会话已结束。"
	case "transport failure":
		detail = "Claude 会话异常结束。"
	case "request withdrawn":
		detail = "Claude 已撤回这次请求（任务被中断或已结束）。"
	}
	return detail + "这张请求已无法继续处理，请重新发起。"
}
