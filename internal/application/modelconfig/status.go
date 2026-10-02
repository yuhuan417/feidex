// Package modelconfig coordinates desired and acknowledged model settings.
package modelconfig

import (
	"strings"

	domain "feidex/internal/domain/modelconfig"
)

// Status is a transport-independent view of one session's model settings.
// Renderers receive confirmed application and pending changes explicitly;
// they must not infer them from a global or another chat's configuration.
type Status struct {
	NextModel, NextEffort       string
	AppliedModel, AppliedEffort string
	HasApplied                  bool
	Pending                     bool
	Error                       string
}

func SessionStatus(backend, activeThreadID string, desired, applied domain.Snapshot, applyError string) Status {
	result := Status{Error: applyError}
	result.NextModel, result.NextEffort = domain.TurnSettings(desired)
	result.HasApplied = applied.Valid && applied.Backend == backend && strings.TrimSpace(activeThreadID) != ""
	if result.HasApplied {
		result.AppliedModel, result.AppliedEffort = domain.TurnSettings(applied)
		result.Pending = desired != applied
	}
	return result
}
