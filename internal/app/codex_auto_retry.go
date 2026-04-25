package app

import (
	appautoretry "feidex/internal/app/autoretry"
)

// Type aliases for backward compatibility with app/ code and tests.
type autoRetryService = appautoretry.Service
type delayedTask = appautoretry.DelayedTask
type autoRetryTracker = appautoretry.Tracker
type autoRetryState = appautoretry.RetryState

var newAutoRetryService = appautoretry.NewService
var newAutoRetryTracker = appautoretry.NewTracker
var autoRetryDelayForStep = appautoretry.DelayForStep
