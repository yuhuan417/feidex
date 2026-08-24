package appstate

import (
	"strings"

	"feidex/internal/state"
)

// AgentBinding returns a binding owned by the current frontend.
func (s *Store) AgentBinding(id string) *state.AgentBinding {
	if s == nil || s.Store == nil {
		return nil
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return nil
	}
	if binding := s.Store.GetScopedAgentBinding(s.FrontendID, id); binding != nil {
		return binding
	}
	if s.LegacyFallback && s.FrontendID != "" {
		binding := s.Store.GetAgentBinding(id)
		if binding != nil && strings.TrimSpace(binding.FrontendID) == "" {
			return binding
		}
	}
	return nil
}

// AgentBindings returns all bindings visible to the current frontend.
func (s *Store) AgentBindings() []*state.AgentBinding {
	if s == nil || s.Store == nil {
		return nil
	}
	all := s.Store.AllAgentBindings()
	out := make([]*state.AgentBinding, 0, len(all))
	for _, binding := range all {
		if binding == nil || !s.MatchesFrontend(binding.FrontendID) {
			continue
		}
		out = append(out, binding)
	}
	return out
}

// AgentBindingsForChat returns this frontend's bindings for one logical chat.
func (s *Store) AgentBindingsForChat(chatType, chatID string) []*state.AgentBinding {
	if s == nil || s.Store == nil {
		return nil
	}
	chatType = strings.ToLower(strings.TrimSpace(chatType))
	chatID = strings.TrimSpace(chatID)
	if chatID == "" {
		return nil
	}
	bindings := s.AgentBindings()
	out := make([]*state.AgentBinding, 0, len(bindings))
	for _, binding := range bindings {
		if binding.ChatID != chatID || (chatType != "" && binding.ChatType != chatType) {
			continue
		}
		out = append(out, binding)
	}
	return out
}

// SaveAgentBinding persists a binding in the current frontend scope.
func (s *Store) SaveAgentBinding(binding *state.AgentBinding) error {
	if s == nil || s.Store == nil {
		return nil
	}
	return s.Store.UpsertScopedAgentBinding(s.FrontendID, binding)
}

// DeleteAgentBinding removes a binding from the current frontend scope.
func (s *Store) DeleteAgentBinding(id string) error {
	if s == nil || s.Store == nil {
		return nil
	}
	id = strings.TrimSpace(id)
	if s.LegacyFallback && s.FrontendID != "" {
		binding := s.Store.GetAgentBinding(id)
		if binding != nil && strings.TrimSpace(binding.FrontendID) == "" {
			return s.Store.DeleteScopedAgentBinding("", id)
		}
	}
	return s.Store.DeleteScopedAgentBinding(s.FrontendID, id)
}
