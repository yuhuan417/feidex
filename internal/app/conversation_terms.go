package app

func primaryConversationSlash(backend string) string {
	return backendCapabilityForKind(backend).Conversation.Slash
}

func primaryConversationMissingLabel(backend string) string {
	return backendCapabilityForKind(backend).MissingConversationLabel()
}
