package app

import (
	"feidex/internal/app/autoretry"
	"feidex/internal/app/maintenance"
	applicationturn "feidex/internal/application/turn"
)

func newTurnLifecycleService(app *App) applicationturn.Service {
	return applicationturn.NewService(applicationturn.Dependencies{
		State: app.State(), Bindings: newRuntimeStateService(app),
		Replies: newReplyContinuationService(app), Streams: newTurnStreamService(app),
		Reactions: newPendingQueueService(app), Cards: newOutboundCardService(app),
		Queue: newSubmissionQueueServiceFromApp(app), Retry: autoretry.NewService(app),
		Cleanup: maintenance.NewRuntimeMaintenanceService(app),
		Runtime: app, Continuations: app, Delivery: app, Diagnostics: app,
	})
}
