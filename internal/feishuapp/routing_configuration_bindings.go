package feishuapp

import (
	"feidex/internal/application/modelconfig"
	"feidex/internal/application/routing"
	"feidex/internal/compositionkit"
	"feidex/internal/domain/identity"
)

func newRoutingConfiguration(a *App) compositionkit.RoutingConfiguration {
	return compositionkit.RoutingConfiguration{ConfigurationService: routing.ConfigurationService{Repository: a.State(), Frontend: identity.FrontendID(a.FrontendID())}, Runner: newEffectRunner(a), Context: a.Context()}
}

func newScopedRoutingConfiguration(a *App) compositionkit.ScopedRoutingConfiguration {
	return compositionkit.ScopedRoutingConfiguration{
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
