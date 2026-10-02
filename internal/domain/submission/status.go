package submission

import "strings"

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
