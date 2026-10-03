package app

import (
	"context"
	feishuoutbound "feidex/internal/adapter/feishu/outbound"
	"feidex/internal/application"
	domainbackend "feidex/internal/domain/backend"
	"log/slog"
	"strings"

	storagejson "feidex/internal/adapter/storage/json"
	"feidex/internal/domain/identity"
	appclauderuntime "feidex/internal/runtime/claude"
	"feidex/internal/state"
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
	sessionKey = strings.TrimSpace(sessionKey)
	wanted := make(map[string]bool, len(requestIDs))
	for _, id := range requestIDs {
		if trimmed := strings.TrimSpace(id); trimmed != "" {
			wanted[trimmed] = true
		}
	}
	body := claudeInteractionExpiredBody(reason)
	for _, pending := range a.State().PendingRequests() {
		if pending == nil || normalizeRuntimeBackend(pending.Backend) != domainbackend.BackendClaude {
			continue
		}
		if !storagejson.IsPendingRequestOpen(pending) {
			continue
		}
		if sessionKey != "" && !sessionKeysEqual(a, pending.SessionKey, sessionKey) {
			continue
		}
		if len(wanted) > 0 && !wanted[strings.TrimSpace(pending.ID)] {
			continue
		}
		_ = a.State().UpdatePending(pending.ID, func(req *state.PendingRequest) {
			req.Status = state.PendingRequestStatusExpired.String()
		})
		messageID := strings.TrimSpace(pending.FeishuMsgID)
		if messageID == "" || a.feishu == nil {
			continue
		}
		title := contentCardTitleForSession(a, pending.SessionKey, "", "请求已失效")
		card := a.feishu.SimpleStatusCard(title, "grey", body, nil)
		if err := newEffectRunner(a).Run(context.Background(), []application.Effect{application.PatchCard{
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
