package routing

import (
	"testing"

	"feidex/internal/application"
	"feidex/internal/domain/identity"
	domain "feidex/internal/domain/routing"
)

type scopedConfigRepo struct {
	binding *domain.AgentBinding
	profile *domain.BotProfile
}

func (r *scopedConfigRepo) AgentBinding(id string) *domain.AgentBinding {
	if r.binding != nil && r.binding.ID == id {
		return r.binding
	}
	return nil
}
func (r *scopedConfigRepo) AgentBindingsForChat(chatType, chatID string) []*domain.AgentBinding {
	if r.binding != nil && r.binding.ChatType == chatType && r.binding.ChatID == chatID {
		return []*domain.AgentBinding{r.binding}
	}
	return nil
}
func (r *scopedConfigRepo) UpdateAgentBinding(binding *domain.AgentBinding, mutate func(*domain.AgentBinding)) (*domain.AgentBinding, error) {
	if r.binding == nil {
		r.binding = binding
	}
	if mutate != nil {
		mutate(r.binding)
	}
	return r.binding, nil
}
func (r *scopedConfigRepo) BotProfile() *domain.BotProfile { return r.profile }
func (r *scopedConfigRepo) UpdateBotProfile(profile *domain.BotProfile, mutate func(*domain.BotProfile)) (*domain.BotProfile, error) {
	if r.profile == nil {
		r.profile = profile
	}
	if mutate != nil {
		mutate(r.profile)
	}
	return r.profile, nil
}

func newScopedConfigService(repo *scopedConfigRepo) ScopedConfigurationService {
	return ScopedConfigurationService{
		ConfigurationService: ConfigurationService{Repository: repo, Frontend: identity.FrontendID("frontend-a")},
		Backend:              "codex",
	}
}

func TestScopedConfigurationUsesGroupBinding(t *testing.T) {
	repo := &scopedConfigRepo{}
	result, err := newScopedConfigService(repo).Set(Scope{ChatType: "group", ChatID: "chat-1"}, domain.Model, " gpt-5 ")
	if err != nil {
		t.Fatalf("Set(group) error = %v", err)
	}
	if result.Binding == nil || result.Binding.ModelOverride != "gpt-5" {
		t.Fatalf("group result = %+v", result.Binding)
	}
	if repo.profile != nil {
		t.Fatalf("group update unexpectedly changed profile: %+v", repo.profile)
	}
	if len(result.Effects) != 1 {
		t.Fatalf("group effects = %d, want refresh effect", len(result.Effects))
	}
	if _, ok := result.Effects[0].(application.RefreshGroupStatus); !ok {
		t.Fatalf("group effect = %T, want RefreshGroupStatus", result.Effects[0])
	}
}

func TestScopedConfigurationUsesProfileForPrivateScope(t *testing.T) {
	repo := &scopedConfigRepo{}
	result, err := newScopedConfigService(repo).Set(Scope{ChatType: "p2p", ChatID: "user-1"}, domain.Model, "opus")
	if err != nil {
		t.Fatalf("Set(p2p) error = %v", err)
	}
	if result.Profile == nil || result.Profile.Model != "opus" {
		t.Fatalf("private result = %+v", result.Profile)
	}
	if repo.binding != nil || len(result.Effects) != 0 {
		t.Fatalf("private scope changed binding/effects: binding=%+v effects=%v", repo.binding, result.Effects)
	}
}

func TestScopedConfigurationRejectsIncompleteScope(t *testing.T) {
	service := newScopedConfigService(&scopedConfigRepo{})
	if _, err := service.Set(Scope{ChatType: "p2p"}, domain.Model, "x"); err == nil {
		t.Fatal("Set() accepted scope without chat id")
	}
	if _, _, err := service.Ensure(Scope{ChatType: "group"}); err == nil {
		t.Fatal("Ensure() accepted group scope without chat id")
	}
}

func TestServiceTierPolicy(t *testing.T) {
	if got := NormalizeServiceTier(" FAST "); got != FastServiceTier {
		t.Fatalf("NormalizeServiceTier() = %q", got)
	}
	if got := NormalizeServiceTier("safe"); got != "" {
		t.Fatalf("NormalizeServiceTier(unsupported) = %q", got)
	}
	if got := ToggleServiceTier(""); got != FastServiceTier {
		t.Fatalf("ToggleServiceTier(empty) = %q", got)
	}
	if got := ToggleServiceTier("fast"); got != "" {
		t.Fatalf("ToggleServiceTier(fast) = %q", got)
	}
}
