package compositionkit

import (
	"context"

	applicationrouting "feidex/internal/application/routing"
	domainrouting "feidex/internal/domain/routing"
	"feidex/internal/runtime"
)

// ScopedRoutingConfiguration is the composition boundary for command
// adapters. The application service chooses the state owner; this wrapper is
// responsible only for running the resulting runtime effects.
type ScopedRoutingConfiguration struct {
	Service applicationrouting.ScopedConfigurationService
	Runner  runtime.EffectRunner
	Context context.Context
}

func (s ScopedRoutingConfiguration) Set(scope applicationrouting.Scope, setting domainrouting.Setting, value string) (applicationrouting.ScopedConfigurationResult, error) {
	result, err := s.Service.Set(scope, setting, value)
	if err != nil {
		return applicationrouting.ScopedConfigurationResult{}, err
	}
	if err := s.Runner.Run(s.Context, result.Effects); err != nil {
		return result, err
	}
	return result, nil
}

func (s ScopedRoutingConfiguration) Ensure(scope applicationrouting.Scope) (*domainrouting.AgentBinding, *domainrouting.BotProfile, error) {
	return s.Service.Ensure(scope)
}
