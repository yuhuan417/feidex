package announcement

import (
	"feidex/internal/domain/conversation"
	"feidex/internal/domain/identity"
	"feidex/internal/domain/routing"
	"feidex/internal/domain/workspace"
	"sort"
	"strings"
)

type QueryRepository interface {
	Sessions() []*conversation.Session
	AgentBindings() []*routing.AgentBinding
	GroupAnnouncementBlocks() []*routing.GroupAnnouncementBlock
	AgentBindingsForChat(string, string) []*routing.AgentBinding
}
type Workspaces interface {
	Get(string) (*workspace.Workspace, error)
}
type Query struct {
	Repository QueryRepository
	Workspaces Workspaces
	HasPrimary func(string) bool
}

func (q Query) GroupSession(sess *conversation.Session, chatID string) bool {
	if sess == nil || chatID == "" {
		return false
	}
	typ, id := strings.TrimSpace(sess.ChatType), strings.TrimSpace(sess.ChatID)
	_, keyType, keyChat, _, _ := identity.ParseSessionKey(sess.Key)
	if typ == "" {
		typ = keyType
	}
	if id == "" {
		id = keyChat
	}
	return id == chatID && (typ == "group" || typ == "" && (len(q.Repository.AgentBindingsForChat("group", chatID)) > 0 || q.HasPrimary(chatID)))
}
func (q Query) Chats() []string {
	seen := map[string]bool{}
	for _, binding := range q.Repository.AgentBindings() {
		if binding != nil && binding.ChatType == "group" && binding.ChatID != "" {
			seen[binding.ChatID] = true
		}
	}
	for _, sess := range q.Repository.Sessions() {
		if sess == nil {
			continue
		}
		id := sess.ChatID
		if id == "" {
			_, _, id, _, _ = identity.ParseSessionKey(sess.Key)
		}
		if q.GroupSession(sess, id) {
			seen[id] = true
		}
	}
	for _, record := range q.Repository.GroupAnnouncementBlocks() {
		if record != nil && record.ChatID != "" && (record.ChatType == "group" || record.ChatType == "group_common") {
			seen[record.ChatID] = true
		}
	}
	var out []string
	for id := range seen {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}
func (q Query) WorkspaceDirectory(chatID string) string {
	for _, binding := range q.Repository.AgentBindingsForChat("group", chatID) {
		if binding == nil || strings.TrimSpace(binding.WorkspaceID) == "" {
			continue
		}
		ws, err := q.Workspaces.Get(binding.WorkspaceID)
		if err == nil && ws != nil && strings.TrimSpace(ws.Cwd) != "" {
			return ws.Cwd
		}
		return "unavailable:" + binding.WorkspaceID
	}
	return "unconfigured"
}
