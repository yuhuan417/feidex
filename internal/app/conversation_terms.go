package app

import "strings"

func primaryConversationSlash(backend string) string {
	if normalizeRuntimeBackend(backend) == backendClaude {
		return "/session"
	}
	return "/thread"
}

func primaryConversationPluralSlash(backend string) string {
	if normalizeRuntimeBackend(backend) == backendClaude {
		return "/session list"
	}
	return "/threads"
}

func primaryConversationNoun(backend string) string {
	if normalizeRuntimeBackend(backend) == backendClaude {
		return "会话"
	}
	return "线程"
}

func primaryConversationMenuLabel(backend string) string {
	if normalizeRuntimeBackend(backend) == backendClaude {
		return "会话管理"
	}
	return "线程管理"
}

func primaryConversationCurrentLabel(backend string) string {
	return "当前" + primaryConversationNoun(backend)
}

func primaryConversationMissingLabel(backend string) string {
	return "当前没有活动" + primaryConversationNoun(backend)
}

func primaryConversationIDLabel(backend string) string {
	if normalizeRuntimeBackend(backend) == backendClaude {
		return "session id"
	}
	return "thread id"
}

func primaryConversationSummaryLabel(backend string) string {
	if normalizeRuntimeBackend(backend) == backendClaude {
		return "session"
	}
	return "thread"
}

func backendSpecificThreadSummary(text, backend string) string {
	if normalizeRuntimeBackend(backend) != backendClaude {
		return text
	}
	text = strings.ReplaceAll(text, "线程", "会话")
	text = strings.ReplaceAll(text, "thread", "session")
	return text
}
