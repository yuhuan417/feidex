package routing

import (
	"fmt"
	"strings"

	"feidex/internal/application"
	"feidex/internal/domain/identity"
	domain "feidex/internal/domain/routing"
)

type ConfigurationRepository interface {
	AgentBinding(string) *domain.AgentBinding
	AgentBindingsForChat(string, string) []*domain.AgentBinding
	UpdateAgentBinding(*domain.AgentBinding, func(*domain.AgentBinding)) (*domain.AgentBinding, error)
	BotProfile() *domain.BotProfile
	UpdateBotProfile(*domain.BotProfile, func(*domain.BotProfile)) (*domain.BotProfile, error)
}
type ConfigurationService struct {
	Repository ConfigurationRepository
	Frontend   identity.FrontendID
}
type BindingResult struct {
	Binding *domain.AgentBinding
	Effects []application.Effect
}

func (s ConfigurationService) EnsureBinding(chatType, chatID string) (*domain.AgentBinding, error) {
	chatType, chatID = strings.TrimSpace(chatType), strings.TrimSpace(chatID)
	if chatType != "group" || chatID == "" {
		return nil, fmt.Errorf("该命令只能在群聊中使用")
	}
	for _, binding := range s.Repository.AgentBindingsForChat(chatType, chatID) {
		if binding != nil {
			return binding, nil
		}
	}
	binding := &domain.AgentBinding{ID: domain.DefaultBindingID(string(s.Frontend), chatType, chatID), FrontendID: string(s.Frontend), ChatType: chatType, ChatID: chatID, Status: "pending"}
	return s.Repository.UpdateAgentBinding(binding, nil)
}
func (s ConfigurationService) UpdateBinding(binding *domain.AgentBinding, mutate func(*domain.AgentBinding)) (BindingResult, error) {
	if binding == nil {
		return BindingResult{}, fmt.Errorf("当前 Bot 工作区配置未初始化")
	}
	updated, err := s.Repository.UpdateAgentBinding(binding, func(current *domain.AgentBinding) {
		if mutate != nil {
			mutate(current)
		}
		if strings.TrimSpace(current.WorkspaceID) != "" {
			current.Status = "active"
		}
	})
	if err != nil {
		return BindingResult{}, err
	}
	if updated == nil {
		return BindingResult{}, fmt.Errorf("当前 Bot 工作区配置 %q 更新后未找到", binding.ID)
	}
	result := BindingResult{Binding: updated}
	if strings.EqualFold(strings.TrimSpace(updated.ChatType), "group") {
		result.Effects = []application.Effect{application.RefreshGroupStatus{Frontend: s.Frontend, ChatID: updated.ChatID, Reason: "binding_updated"}}
	}
	return result, nil
}
func (s ConfigurationService) EnsureProfile() (*domain.BotProfile, error) {
	if profile := s.Repository.BotProfile(); profile != nil {
		return profile, nil
	}
	profile := &domain.BotProfile{ID: "bot-profile-" + domain.ProfileID(string(s.Frontend)), FrontendID: string(s.Frontend)}
	return s.Repository.UpdateBotProfile(profile, nil)
}
func (s ConfigurationService) UpdateProfile(mutate func(*domain.BotProfile)) (*domain.BotProfile, error) {
	profile, err := s.EnsureProfile()
	if err != nil {
		return nil, err
	}
	return s.Repository.UpdateBotProfile(profile, mutate)
}
