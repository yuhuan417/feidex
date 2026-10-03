package feishuapp

import "feidex/internal/application/backendcaps"

func primaryConversationSlash(backend string) string {
	return backendcaps.ForKind(backend).Conversation.Slash
}

func primaryConversationMissingLabel(backend string) string {
	return backendcaps.ForKind(backend).MissingConversationLabel()
}
