package app

import (
	"feidex/internal/application/modelconfig"
	"feidex/internal/application/routing"
	"feidex/internal/composition"
	"feidex/internal/domain/identity"
)

func newRoutingConfiguration(a *App) composition.RoutingConfiguration {
	return composition.RoutingConfiguration{ConfigurationService: routing.ConfigurationService{Repository: a.State(), Frontend: identity.FrontendID(a.FrontendID())}, Runner: newEffectRunner(a), Context: a.Context()}
}

func newScopedRoutingConfiguration(a *App) composition.ScopedRoutingConfiguration {
	return composition.ScopedRoutingConfiguration{
		Service: routing.ScopedConfigurationService{
			ConfigurationService: routing.ConfigurationService{Repository: a.State(), Frontend: identity.FrontendID(a.FrontendID())},
			Backend:              configuredBackend(a),
		},
		Runner:  newEffectRunner(a),
		Context: a.Context(),
	}
}

type modelWriteAdmission struct{ app *App }

func (s modelWriteAdmission) ModelConfigBlockedReason() string {
	return modelConfigBlockedReason(s.app)
}

func newModelSettingsService(a *App) modelconfig.SettingsService {
	return modelconfig.SettingsService{Repository: a.State(), Admission: modelWriteAdmission{app: a}, Frontend: identity.FrontendID(a.FrontendID())}
}
