package workspace

import (
	"context"
	"feidex/internal/domain/conversation"
	"feidex/internal/domain/identity"
	"feidex/internal/domain/routing"
	"fmt"
	"strings"
)

type GroupRepository interface {
	Session(string) *conversation.Session
}
type GroupService struct {
	Frontend   identity.FrontendID
	Repository GroupRepository
	Creation   *CreationService
	Planning   *PlanningService
	Effects    EffectService
}

func (s GroupService) Request(binding *routing.AgentBinding) (SwitchRequest, error) {
	if binding == nil {
		return SwitchRequest{}, fmt.Errorf("binding is required")
	}
	key := identity.CanonicalSessionKey(string(s.Frontend), "feishu:chat:"+binding.ChatID)
	return SwitchRequest{Session: s.Repository.Session(key), Binding: binding}, nil
}
func (s GroupService) Select(binding *routing.AgentBinding, id string) (*routing.AgentBinding, error) {
	req, err := s.Request(binding)
	if err != nil {
		return nil, err
	}
	req.WorkspaceID = id
	effects, err := s.Creation.Lifecycle.Switch(req)
	if err == nil {
		s.Effects.Apply(effects)
	}
	return effects.Binding, err
}
func (s GroupService) Unbind(key string, binding *routing.AgentBinding) error {
	effects, err := s.Creation.Lifecycle.Switch(SwitchRequest{Session: s.Repository.Session(key), Binding: binding, Unbind: true})
	if err == nil {
		s.Effects.Apply(effects)
	}
	return err
}
func (s GroupService) New(binding *routing.AgentBinding, id, name, cwd string) (CreationResult, error) {
	req, err := s.Request(binding)
	if err != nil {
		return CreationResult{}, err
	}
	result, err := s.Creation.CreateAndSwitch(req, id, name, cwd)
	if err == nil {
		s.Effects.Apply(result.Effects)
	}
	return result, err
}
func (s GroupService) Clone(ctx context.Context, binding *routing.AgentBinding, args []string) (CreationResult, error) {
	req, err := s.Request(binding)
	if err != nil {
		return CreationResult{}, err
	}
	repo, id, parent, err := ParseCloneArgs(append([]string{"clone"}, args...))
	if err != nil {
		return CreationResult{}, err
	}
	if strings.TrimSpace(parent) == "" {
		parent = s.Planning.DefaultWorkspaceCloneParent(s.Planning.WorkspaceByID(binding.WorkspaceID))
	}
	plan, err := s.Planning.PrepareWorkspaceClone(repo, id, s.Creation.Filesystem.ResolvePath(parent))
	if err != nil {
		return CreationResult{}, err
	}
	result, err := s.Creation.Clone(ctx, req, repo, plan, nil)
	if err == nil {
		s.Effects.Apply(result.Effects)
	}
	return result, err
}
