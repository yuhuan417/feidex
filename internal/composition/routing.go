package composition

import (
	"context"
	"feidex/internal/application/routing"
	domain "feidex/internal/domain/routing"
	"feidex/internal/runtime"
)

// RoutingConfiguration connects use-case results to their runtime effects.
type RoutingConfiguration struct {
	routing.ConfigurationService
	Runner  runtime.EffectRunner
	Context context.Context
}

func (s RoutingConfiguration) UpdateBinding(binding *domain.AgentBinding, mutate func(*domain.AgentBinding)) (*domain.AgentBinding, error) {
	result, err := s.ConfigurationService.UpdateBinding(binding, mutate)
	if err != nil {
		return nil, err
	}
	if err := s.Runner.Run(s.Context, result.Effects); err != nil {
		return result.Binding, err
	}
	return result.Binding, nil
}
