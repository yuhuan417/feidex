package app

import (
	applicationturn "feidex/internal/application/turn"
)

func newTurnLifecycleService(app *App) applicationturn.Service {
	return applicationturn.NewService(applicationturn.Dependencies{
		State: app.State(), Bindings: newRuntimeStateService(app),
		Replies: newReplyContinuationService(app), Streams: newTurnStreamService(app),
		Reactions: newPendingQueueService(app), Cards: newOutboundCardService(app),
		Queue: newSubmissionQueueServiceFromApp(app), Retry: newAutoRetryService(app),
		Cleanup: newSubmissionCleanup(app),
		Runtime: app, Continuations: app, Delivery: app, Diagnostics: app,
	})
}
