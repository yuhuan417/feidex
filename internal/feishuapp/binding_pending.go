package feishuapp

import (
	"log/slog"
	"strings"

	"feidex/internal/application/routing"
)

func discardPendingBindingMessage(pending routing.PendingService, stateReady bool, messageID string) bool {
	if strings.TrimSpace(messageID) == "" || !stateReady {
		return false
	}
	discarded, err := pending.Discard(messageID)
	if err != nil {
		slog.Warn("discard pending binding message failed", "message_id", messageID, "error", err)
	}
	return discarded
}
