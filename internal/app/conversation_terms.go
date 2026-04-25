package app

func primaryConversationSlash(backend string) string {
	return backendCapabilityForKind(backend).Conversation.Slash
}

func primaryConversationNoun(backend string) string {
	return backendCapabilityForKind(backend).Conversation.Noun
}

func primaryConversationMenuLabel(backend string) string {
	return backendCapabilityForKind(backend).Conversation.MenuLabel
}

func primaryConversationCurrentLabel(backend string) string {
	return backendCapabilityForKind(backend).CurrentConversationLabel()
}

func primaryConversationMissingLabel(backend string) string {
	return backendCapabilityForKind(backend).MissingConversationLabel()
}

func primaryConversationIDLabel(backend string) string {
	return backendCapabilityForKind(backend).Conversation.IDLabel
}

func primaryConversationSummaryLabel(backend string) string {
	return backendCapabilityForKind(backend).Conversation.SummaryLabel
}
