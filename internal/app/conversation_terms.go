package app

func primaryConversationSlash(backend string) string {
	return backendCapabilityForKind(backend).conversation.Slash
}

func primaryConversationPluralSlash(backend string) string {
	return backendCapabilityForKind(backend).conversation.PluralSlash
}

func primaryConversationNoun(backend string) string {
	return backendCapabilityForKind(backend).conversation.Noun
}

func primaryConversationMenuLabel(backend string) string {
	return backendCapabilityForKind(backend).conversation.MenuLabel
}

func primaryConversationCurrentLabel(backend string) string {
	return backendCapabilityForKind(backend).currentConversationLabel()
}

func primaryConversationMissingLabel(backend string) string {
	return backendCapabilityForKind(backend).missingConversationLabel()
}

func primaryConversationIDLabel(backend string) string {
	return backendCapabilityForKind(backend).conversation.IDLabel
}

func primaryConversationSummaryLabel(backend string) string {
	return backendCapabilityForKind(backend).conversation.SummaryLabel
}

func backendSpecificThreadSummary(text, backend string) string {
	return backendCapabilityForKind(backend).rewriteConversationText(text)
}
