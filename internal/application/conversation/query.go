package conversation

import (
	"feidex/internal/domain/conversation"
	"feidex/internal/domain/identity"
	"strings"
)

type QueryRepository interface {
	Session(string) *conversation.Session
	Sessions() []*conversation.Session
}
type Query struct{ Repository QueryRepository }

func (q Query) SessionForBackendThread(threadID string) string {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return ""
	}
	for _, sess := range q.Repository.Sessions() {
		if sess == nil {
			continue
		}
		if strings.TrimSpace(sess.ActiveThreadID) == threadID {
			return sess.Key
		}
		for _, lineage := range sess.BackendThreads {
			if strings.TrimSpace(lineage.ThreadID) == threadID {
				return sess.Key
			}
		}
	}
	return ""
}

func (q Query) LatestGroupSession(chatID, bindingID string) *conversation.Session {
	var best *conversation.Session
	for _, sess := range q.Repository.Sessions() {
		if sess == nil || strings.TrimSpace(sess.Key) == "" || strings.TrimSpace(sess.ActiveThreadID) == "" {
			continue
		}
		typ, chat := strings.TrimSpace(sess.ChatType), strings.TrimSpace(sess.ChatID)
		if typ == "" || chat == "" {
			_, parsedType, parsedChat, _, _ := identity.ParseSessionKey(sess.Key)
			if typ == "" {
				typ = parsedType
			}
			if chat == "" {
				chat = parsedChat
			}
		}
		if typ != "group" || chat != strings.TrimSpace(chatID) || (bindingID != "" && strings.TrimSpace(sess.BindingID) != bindingID) {
			continue
		}
		if best == nil || sess.UpdatedAt > best.UpdatedAt || (sess.UpdatedAt == best.UpdatedAt && strings.TrimSpace(sess.Key) > strings.TrimSpace(best.Key)) {
			best = sess
		}
	}
	return best
}

func (q Query) EffectiveGroupSessionKey(key, chatID, bindingID string) string {
	key = strings.TrimSpace(key)
	if sess := q.Repository.Session(key); sess != nil && strings.TrimSpace(sess.ActiveThreadID) != "" {
		return key
	}
	if latest := q.LatestGroupSession(chatID, bindingID); latest != nil {
		return strings.TrimSpace(latest.Key)
	}
	return key
}
