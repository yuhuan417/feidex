package appstate

import (
	"fmt"
	"strings"

	"feidex/internal/state"
)

// BotProfile returns the default profile owned by this frontend.
func (s *Store) BotProfile() *state.BotProfile {
	if s == nil || s.stateStore() == nil {
		return nil
	}
	frontendID := strings.TrimSpace(s.scopeFrontendID())
	if frontendID == "" {
		frontendID = "default"
	}
	profile := s.stateStore().GetBotProfile(frontendID)
	return profile
}

// SaveBotProfile persists a profile in the current frontend scope.
func (s *Store) SaveBotProfile(profile *state.BotProfile) error {
	if s == nil || s.stateStore() == nil || profile == nil {
		return nil
	}
	cp := *profile
	if strings.TrimSpace(cp.FrontendID) == "" {
		cp.FrontendID = strings.TrimSpace(s.scopeFrontendID())
		if cp.FrontendID == "" {
			cp.FrontendID = "default"
		}
	}
	return s.stateStore().UpsertBotProfile(&cp)
}

// DeleteBotProfile removes the current frontend's default profile.
func (s *Store) DeleteBotProfile() error {
	if s == nil || s.stateStore() == nil {
		return nil
	}
	return s.stateStore().DeleteBotProfile(s.scopeFrontendID())
}

// AgentBinding returns a binding owned by the current frontend.
func (s *Store) AgentBinding(id string) *state.AgentBinding {
	if s == nil || s.stateStore() == nil {
		return nil
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return nil
	}
	if binding := s.stateStore().GetScopedAgentBinding(s.scopeFrontendID(), id); binding != nil {
		return binding
	}
	if s.scopeLegacyFallback() && s.scopeFrontendID() != "" {
		binding := s.stateStore().GetAgentBinding(id)
		if binding != nil && strings.TrimSpace(binding.FrontendID) == "" {
			return binding
		}
	}
	return nil
}

// AgentBindings returns all bindings visible to the current frontend.
func (s *Store) AgentBindings() []*state.AgentBinding {
	if s == nil || s.stateStore() == nil {
		return nil
	}
	all := s.stateStore().AllAgentBindings()
	out := make([]*state.AgentBinding, 0, len(all))
	for _, binding := range all {
		if binding == nil || !s.matchesFrontend(binding.FrontendID) {
			continue
		}
		out = append(out, binding)
	}
	return out
}

// AgentBindingsForChat returns this frontend's bindings for one logical chat.
func (s *Store) AgentBindingsForChat(chatType, chatID string) []*state.AgentBinding {
	if s == nil || s.stateStore() == nil {
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
	if s == nil || s.stateStore() == nil {
		return nil
	}
	return s.stateStore().UpsertScopedAgentBinding(s.scopeFrontendID(), binding)
}

// DeleteAgentBinding removes a binding from the current frontend scope.
func (s *Store) DeleteAgentBinding(id string) error {
	if s == nil || s.stateStore() == nil {
		return nil
	}
	id = strings.TrimSpace(id)
	if s.scopeLegacyFallback() && s.scopeFrontendID() != "" {
		binding := s.stateStore().GetAgentBinding(id)
		if binding != nil && strings.TrimSpace(binding.FrontendID) == "" {
			return s.stateStore().DeleteScopedAgentBinding("", id)
		}
	}
	return s.stateStore().DeleteScopedAgentBinding(s.scopeFrontendID(), id)
}

func (s *Store) UpdateAgentBinding(fallback *state.AgentBinding, mutate func(*state.AgentBinding)) (*state.AgentBinding, error) {
	if s == nil || s.stateStore() == nil || fallback == nil {
		return nil, fmt.Errorf("binding repository or fallback is nil")
	}
	mu := s.revisionMutex()
	mu.Lock()
	defer mu.Unlock()
	binding := fallback
	if latest := s.AgentBinding(fallback.ID); latest != nil {
		binding = latest
	}
	current := *binding
	if mutate != nil {
		mutate(&current)
	}
	if err := s.SaveAgentBinding(&current); err != nil {
		return nil, err
	}
	return s.AgentBinding(current.ID), nil
}
func (s *Store) UpdateBotProfile(fallback *state.BotProfile, mutate func(*state.BotProfile)) (*state.BotProfile, error) {
	if s == nil || s.stateStore() == nil || fallback == nil {
		return nil, fmt.Errorf("profile repository or fallback is nil")
	}
	mu := s.revisionMutex()
	mu.Lock()
	defer mu.Unlock()
	profile := fallback
	if latest := s.BotProfile(); latest != nil {
		profile = latest
	}
	current := *profile
	if mutate != nil {
		mutate(&current)
	}
	if err := s.SaveBotProfile(&current); err != nil {
		return nil, err
	}
	return s.BotProfile(), nil
}
