package feishuapp

import (
	"log/slog"
	"strings"
	"time"

	"feidex/internal/feishu"
)

func (s bindingService) gatePendingGroupMessage(msg *feishu.InboundMessage) (bool, error) {
	if s.app == nil || msg == nil || !isGroupMessage(msg) {
		return false, nil
	}
	result, err := s.app.bindings.BindingPending.Gate(msg, s.app.configView().makeSessionKey(msg), isGroupPrimary(s.app, msg.ChatType, msg.ChatID), time.Now().Unix())
	if err != nil || !result.Handled {
		return result.Handled, err
	}
	if err := newEffectRunner(s.app.runtimeOwner).Run(s.app.Context(), result.Effects); err != nil {
		return false, err
	}
	card := s.app.bindings.WorkspacePresentation.RenderWorkspaceMenuCard(s.app.configView().makeSessionKey(msg))
	_, err = replyCardWithIDEffect(s.app.Context(), s.app, msg.MessageID, card, s.app.configView().replyInThreadEnabled())
	return true, err
}
func discardPendingBindingMessageByID(a *App, messageID string) bool {
	if a == nil || strings.TrimSpace(messageID) == "" || a.State() == nil {
		return false
	}
	discarded, err := a.bindings.BindingPending.Discard(messageID)
	if err != nil {
		slog.Warn("discard pending binding message failed", "message_id", messageID, "error", err)
	}
	return discarded
}
