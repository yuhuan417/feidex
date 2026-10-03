package workspace

import (
	"fmt"
	"strings"

	"feidex/internal/domain/conversation"
	"feidex/internal/domain/identity"
	"feidex/internal/domain/routing"
)

// SelectionRepository exposes only persisted workspace selections and the
// current frontend's profile/bindings. It cannot select another frontend.
type SelectionRepository interface {
	Session(string) *conversation.Session
	SaveSession(*conversation.Session) error
	BotProfile() *routing.BotProfile
	SetProfileWorkspace(string) error
	AgentBindingsForChat(string, string) []*routing.AgentBinding
}
type SelectionService struct {
	Frontend           identity.FrontendID
	DefaultWorkspace   string
	DefaultWorkspaceID func() string
	Repository         SelectionRepository
}

func SelectionKey(frontend identity.FrontendID, chatType, chatID, userID string) string {
	chatType, chatID, userID = strings.ToLower(strings.TrimSpace(chatType)), strings.TrimSpace(chatID), strings.TrimSpace(userID)
	if chatType == "" || chatID == "" || (chatType != "group" && userID == "") {
		return ""
	}
	prefix := "feishu:"
	if value := strings.TrimSpace(string(frontend)); value != "" {
		prefix += "frontend:" + value + ":"
	}
	key := fmt.Sprintf("%s%s:%s:workspace", prefix, chatType, chatID)
	if chatType != "group" {
		key += ":" + userID
	}
	return key
}

func (s SelectionService) Resolve(chatType, chatID, userID string, fallback *conversation.Session) string {
	if s.Repository != nil {
		if key := SelectionKey(s.Frontend, chatType, chatID, userID); key != "" {
			if selected := s.Repository.Session(key); selected != nil && strings.TrimSpace(selected.WorkspaceID) != "" {
				return strings.TrimSpace(selected.WorkspaceID)
			}
		}
		if chatType != "" && !strings.EqualFold(strings.TrimSpace(chatType), "group") {
			if profile := s.Repository.BotProfile(); profile != nil && strings.TrimSpace(profile.WorkspaceID) != "" {
				return strings.TrimSpace(profile.WorkspaceID)
			}
		}
	}
	if fallback != nil && strings.TrimSpace(fallback.WorkspaceID) != "" {
		return strings.TrimSpace(fallback.WorkspaceID)
	}
	value := s.DefaultWorkspace
	if s.DefaultWorkspaceID != nil {
		value = s.DefaultWorkspaceID()
	}
	if value := strings.TrimSpace(value); value != "" {
		return value
	}
	return "default"
}
func (s SelectionService) ResolveSession(sess *conversation.Session) string {
	if sess == nil {
		return s.Resolve("", "", "", nil)
	}
	return s.Resolve(sess.ChatType, sess.ChatID, sess.OwnerUserID, sess)
}
func (s SelectionService) BindingWorkspace(sessionKey string, sess *conversation.Session) string {
	if s.Repository == nil {
		return ""
	}
	var chatType, chatID, bindingID string
	if sess != nil {
		chatType, chatID, bindingID = strings.TrimSpace(sess.ChatType), strings.TrimSpace(sess.ChatID), strings.TrimSpace(sess.BindingID)
	}
	if chatType == "" || chatID == "" {
		_, parsedType, parsedID, _, _ := identity.ParseSessionKey(sessionKey)
		if chatType == "" {
			chatType = parsedType
		}
		if chatID == "" {
			chatID = parsedID
		}
	}
	if chatType == "" && chatID != "" && len(s.Repository.AgentBindingsForChat("group", chatID)) > 0 {
		chatType = "group"
	}
	if !strings.EqualFold(chatType, "group") || chatID == "" {
		return ""
	}
	bindings := s.Repository.AgentBindingsForChat(chatType, chatID)
	if bindingID != "" {
		for _, binding := range bindings {
			if binding != nil && strings.TrimSpace(binding.ID) == bindingID {
				return strings.TrimSpace(binding.WorkspaceID)
			}
		}
	}
	for _, binding := range bindings {
		if binding != nil && strings.TrimSpace(binding.WorkspaceID) != "" {
			return strings.TrimSpace(binding.WorkspaceID)
		}
	}
	return ""
}
func (s SelectionService) Transition(chatType, chatID, userID, workspaceID string) (*conversation.Session, *routing.BotProfile) {
	workspaceID = strings.TrimSpace(workspaceID)
	key := SelectionKey(s.Frontend, chatType, chatID, userID)
	if s.Repository == nil || workspaceID == "" || key == "" {
		return nil, nil
	}
	sess := conversation.CloneSession(s.Repository.Session(key))
	if sess == nil {
		sess = &conversation.Session{Key: key, Status: conversation.SessionStatusIdle.String()}
	}
	sess.ChatType, sess.ChatID, sess.OwnerUserID = strings.TrimSpace(chatType), strings.TrimSpace(chatID), strings.TrimSpace(userID)
	if strings.EqualFold(sess.ChatType, "group") {
		sess.OwnerUserID = ""
	}
	sess.WorkspaceID = workspaceID
	if strings.TrimSpace(sess.Status) == "" {
		sess.Status = conversation.SessionStatusIdle.String()
	} else {
		sess.Status = strings.TrimSpace(sess.Status)
	}
	recent := []string{workspaceID}
	for _, id := range sess.RecentWorkspaceIDs {
		if strings.TrimSpace(id) != workspaceID {
			recent = append(recent, id)
		}
	}
	sess.RecentWorkspaceIDs = recent
	var profile *routing.BotProfile
	if !strings.EqualFold(sess.ChatType, "group") {
		profile = s.Repository.BotProfile()
		if profile == nil {
			frontend := string(s.Frontend)
			if frontend == "" {
				frontend = "default"
			}
			profile = &routing.BotProfile{ID: "bot-profile-" + routing.ProfileID(frontend), FrontendID: frontend}
		} else {
			cp := *profile
			profile = &cp
		}
		profile.WorkspaceID = workspaceID
	}
	return sess, profile
}

func (s SelectionService) Select(chatType, chatID, userID, workspaceID string) error {
	sess, profile := s.Transition(chatType, chatID, userID, workspaceID)
	if sess == nil {
		return nil
	}
	if err := s.Repository.SaveSession(sess); err != nil {
		return err
	}
	if profile != nil {
		return s.Repository.SetProfileWorkspace(workspaceID)
	}
	return nil
}
