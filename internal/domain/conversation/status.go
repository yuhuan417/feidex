package conversation

import "strings"

type SessionStatus string

const (
	SessionStatusIdle           SessionStatus = "idle"
	SessionStatusQueued         SessionStatus = "queued"
	SessionStatusTurnStarting   SessionStatus = "turn_starting"
	SessionStatusTurnInProgress SessionStatus = "turn_in_progress"
	SessionStatusCompacting     SessionStatus = "compacting"
)

func (s SessionStatus) String() string {
	return string(s)
}

func NormalizeSessionStatus(value string) SessionStatus {
	trimmed := strings.TrimSpace(value)
	switch trimmed {
	case "":
		return SessionStatusIdle
	case SessionStatusIdle.String():
		return SessionStatusIdle
	case SessionStatusQueued.String():
		return SessionStatusQueued
	case SessionStatusTurnStarting.String():
		return SessionStatusTurnStarting
	case SessionStatusTurnInProgress.String():
		return SessionStatusTurnInProgress
	case SessionStatusCompacting.String():
		return SessionStatusCompacting
	default:
		return SessionStatus(trimmed)
	}
}
