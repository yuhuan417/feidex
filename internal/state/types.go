package state

import "strings"

type AgentBindingStatus string

const (
	AgentBindingStatusPending AgentBindingStatus = "pending"
	AgentBindingStatusActive  AgentBindingStatus = "active"
)

func (s AgentBindingStatus) String() string {
	return string(s)
}

func NormalizeAgentBindingStatus(value string) AgentBindingStatus {
	trimmed := strings.TrimSpace(value)
	switch trimmed {
	case "":
		return AgentBindingStatusPending
	case AgentBindingStatusPending.String():
		return AgentBindingStatusPending
	case AgentBindingStatusActive.String():
		return AgentBindingStatusActive
	default:
		return AgentBindingStatus(trimmed)
	}
}

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

type SubmissionStatus string

const (
	SubmissionStatusQueued           SubmissionStatus = "queued"
	SubmissionStatusRunning          SubmissionStatus = "running"
	SubmissionStatusWaitingApproval  SubmissionStatus = "waiting_approval"
	SubmissionStatusWaitingUserInput SubmissionStatus = "waiting_user_input"
	SubmissionStatusCompleted        SubmissionStatus = "completed"
	SubmissionStatusInterrupted      SubmissionStatus = "interrupted"
	SubmissionStatusFailed           SubmissionStatus = "failed"
	SubmissionStatusDiscarded        SubmissionStatus = "discarded"
)

func (s SubmissionStatus) String() string {
	return string(s)
}

func NormalizeSubmissionStatus(value string) SubmissionStatus {
	trimmed := strings.TrimSpace(value)
	switch trimmed {
	case "":
		return SubmissionStatus("")
	case SubmissionStatusQueued.String():
		return SubmissionStatusQueued
	case SubmissionStatusRunning.String():
		return SubmissionStatusRunning
	case SubmissionStatusWaitingApproval.String():
		return SubmissionStatusWaitingApproval
	case SubmissionStatusWaitingUserInput.String():
		return SubmissionStatusWaitingUserInput
	case SubmissionStatusCompleted.String():
		return SubmissionStatusCompleted
	case SubmissionStatusInterrupted.String():
		return SubmissionStatusInterrupted
	case SubmissionStatusFailed.String():
		return SubmissionStatusFailed
	case SubmissionStatusDiscarded.String():
		return SubmissionStatusDiscarded
	default:
		return SubmissionStatus(trimmed)
	}
}

type PendingRequestStatus string

const (
	PendingRequestStatusPending    PendingRequestStatus = "pending"
	PendingRequestStatusReplied    PendingRequestStatus = "replied"
	PendingRequestStatusResolved   PendingRequestStatus = "resolved"
	PendingRequestStatusExpired    PendingRequestStatus = "expired"
	PendingRequestStatusProcessing PendingRequestStatus = "processing"
	PendingRequestStatusCancelling PendingRequestStatus = "cancelling"
	PendingRequestStatusUpgrading  PendingRequestStatus = "upgrading"
)

func (s PendingRequestStatus) String() string {
	return string(s)
}

func NormalizePendingRequestStatus(value string) PendingRequestStatus {
	trimmed := strings.TrimSpace(value)
	switch trimmed {
	case "":
		return PendingRequestStatus("")
	case PendingRequestStatusPending.String():
		return PendingRequestStatusPending
	case PendingRequestStatusReplied.String():
		return PendingRequestStatusReplied
	case PendingRequestStatusResolved.String():
		return PendingRequestStatusResolved
	case PendingRequestStatusExpired.String():
		return PendingRequestStatusExpired
	case PendingRequestStatusProcessing.String():
		return PendingRequestStatusProcessing
	case PendingRequestStatusCancelling.String():
		return PendingRequestStatusCancelling
	case PendingRequestStatusUpgrading.String():
		return PendingRequestStatusUpgrading
	default:
		return PendingRequestStatus(trimmed)
	}
}
