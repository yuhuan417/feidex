package application

import "strings"

// SessionActorKey returns the serialization key for an input. Inputs that
// carry a session are serialized together; transport-only events are scoped
// to their chat so unrelated sessions retain concurrency.
func SessionActorKey(input Input) string {
	const transportPrefix = "transport:"
	switch event := input.(type) {
	case MessageReceived:
		if key := strings.TrimSpace(event.Message.SessionKey); key != "" {
			return "session:" + key
		}
		return transportPrefix + string(event.Chat.Type) + ":" + strings.TrimSpace(event.Chat.ID)
	case CardActionReceived:
		if key, ok := event.Action.ActionValue.String("session_key"); ok && strings.TrimSpace(key) != "" {
			return "session:" + strings.TrimSpace(key)
		}
		return transportPrefix + strings.TrimSpace(event.Action.ChatID)
	case BackendEventReceived:
		if key := strings.TrimSpace(string(event.SessionKey)); key != "" {
			return "session:" + key
		}
		return transportPrefix + "backend"
	case RetryTimerFired:
		return "session:" + strings.TrimSpace(string(event.SessionKey))
	case MessageRecalled:
		return transportPrefix + strings.TrimSpace(event.ChatID)
	case MessageReacted:
		return transportPrefix + strings.TrimSpace(event.ChatID)
	default:
		return transportPrefix + "unknown"
	}
}
