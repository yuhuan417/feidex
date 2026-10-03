package routing

import (
	"fmt"
	"strings"

	"feidex/internal/application"
	"feidex/internal/domain/routing"
)

// Scope identifies the conversation scope a command was addressed to.  The
// adapter supplies these two transport facts; choosing binding versus profile
// is an application decision.
type Scope struct {
	ChatType string
	ChatID   string
}

func (s Scope) normalized() Scope {
	return Scope{ChatType: strings.ToLower(strings.TrimSpace(s.ChatType)), ChatID: strings.TrimSpace(s.ChatID)}
}

func (s Scope) IsGroup() bool {
	s = s.normalized()
	return s.ChatType == "group" && s.ChatID != ""
}

// ScopedConfigurationService applies a setting to the owner for a chat:
// group conversations own an AgentBinding, while private conversations use
// the frontend BotProfile. It deliberately returns semantic effects so the
// composition layer can execute them after persistence.
type ScopedConfigurationService struct {
	ConfigurationService
	Backend string
}

const FastServiceTier = "fast"

// NormalizeServiceTier is shared by slash commands and card actions. Empty
// means the requested value is unsupported; an empty/default value is handled
// by ClearableValue before this helper is called.
func NormalizeServiceTier(value string) string {
	if strings.EqualFold(strings.TrimSpace(value), FastServiceTier) {
		return FastServiceTier
	}
	return ""
}

func ToggleServiceTier(value string) string {
	if NormalizeServiceTier(value) == FastServiceTier {
		return ""
	}
	return FastServiceTier
}

type ScopedConfigurationResult struct {
	Scope   Scope
	Binding *routing.AgentBinding
	Profile *routing.BotProfile
	Effects []application.Effect
}

func (s ScopedConfigurationService) Set(scope Scope, setting routing.Setting, value string) (ScopedConfigurationResult, error) {
	scope = scope.normalized()
	if scope.IsGroup() {
		binding, err := s.ConfigurationService.EnsureBinding(scope.ChatType, scope.ChatID)
		if err != nil {
			return ScopedConfigurationResult{}, err
		}
		result, err := s.ConfigurationService.SetBinding(binding, setting, value)
		if err != nil {
			return ScopedConfigurationResult{}, err
		}
		return ScopedConfigurationResult{Scope: scope, Binding: result.Binding, Effects: result.Effects}, nil
	}
	if scope.ChatID == "" {
		return ScopedConfigurationResult{}, fmt.Errorf("conversation scope is incomplete")
	}
	profile, err := s.ConfigurationService.SetProfile(s.Backend, setting, value)
	if err != nil {
		return ScopedConfigurationResult{}, err
	}
	return ScopedConfigurationResult{Scope: scope, Profile: profile}, nil
}

func (s ScopedConfigurationService) Ensure(scope Scope) (*routing.AgentBinding, *routing.BotProfile, error) {
	scope = scope.normalized()
	if scope.IsGroup() {
		binding, err := s.ConfigurationService.EnsureBinding(scope.ChatType, scope.ChatID)
		return binding, nil, err
	}
	if scope.ChatID == "" {
		return nil, nil, fmt.Errorf("conversation scope is incomplete")
	}
	profile, err := s.ConfigurationService.EnsureProfile()
	return nil, profile, err
}
