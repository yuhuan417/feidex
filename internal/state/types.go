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

type PendingRequestStatus string

const (
	PendingRequestStatusPending    PendingRequestStatus = "pending"
	PendingRequestStatusReplied    PendingRequestStatus = "replied"
	PendingRequestStatusResolved   PendingRequestStatus = "resolved"
	PendingRequestStatusExpired    PendingRequestStatus = "expired"
	PendingRequestStatusProcessing PendingRequestStatus = "processing"
	PendingRequestStatusCancelling PendingRequestStatus = "cancelling"
	PendingRequestStatusLaunching  PendingRequestStatus = "launching"
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
	case PendingRequestStatusLaunching.String():
		return PendingRequestStatusLaunching
	default:
		return PendingRequestStatus(trimmed)
	}
}
