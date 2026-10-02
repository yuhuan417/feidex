package app

import (
	"feidex/internal/application/routing"
	"feidex/internal/composition"
	"feidex/internal/domain/identity"
)

func newRoutingConfiguration(a *App) composition.RoutingConfiguration {
	return composition.RoutingConfiguration{ConfigurationService: routing.ConfigurationService{Repository: a.State(), Frontend: identity.FrontendID(a.FrontendID())}, Runner: newEffectRunner(a), Context: a.Context()}
}
