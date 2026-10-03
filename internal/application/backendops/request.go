// Package backendops defines the semantic operations consumed by application services.
package backendops

import (
	"encoding/json"
	"feidex/internal/domain/conversation"
	"feidex/internal/domain/history"
	"feidex/internal/domain/review"
	"feidex/internal/domain/submission"
)

// ThreadStartConfig is the backend-neutral configuration captured for a new
// conversation. Codex adapters translate it to thread/start wire fields.
type ThreadStartConfig struct {
	Cwd                    string
	ApprovalPolicy         string
	SandboxMode            string
	ServiceName            string
	ExperimentalRawEvents  bool
	PersistExtendedHistory bool
	ServiceTier            string
	Model                  string
	AuxiliaryConfig        map[string]any
}

// ThreadForkRequest contains semantic fork inputs. Backend adapters own the
// protocol field names and optional-field encoding.
type ThreadForkRequest struct {
	ThreadID       string
	Cwd            string
	ApprovalPolicy string
	SandboxMode    string
	ServiceTier    string
	Model          string
	MultiAgentMode string
}

type StartTurnRequest struct {
	ThreadID                                                                     string
	Submission                                                                   *submission.Submission
	Cwd, ApprovalPolicy, SandboxMode, ServiceTier, Model, Effort, MultiAgentMode string
	Collaboration                                                                *conversation.SessionCollaborationMode
}
type TurnResult struct{ ID, Status string }
type ThreadTurns struct {
	ThreadID string
	Turns    []TurnResult
}

type HistoryTurn struct {
	Ordinal                             int
	ID, Status, ErrorText, InputPreview string
	Inputs, Outputs                     []string
	Records                             []history.Record
	IsCurrent                           bool
}

type ThreadHistory struct {
	ID, Name, Preview, Cwd string
	Turns                  []HistoryTurn
}
type ReviewRequest struct {
	ThreadID string
	Target   review.TargetSpec
}
type ReviewResult struct {
	ReviewThreadID string
	Turn           TurnResult
}

// BudgetUpdate distinguishes leave unchanged, clear, and set.
type BudgetUpdate struct{ Value *int64 }
type GoalUpdate struct {
	ThreadID    string
	Objective   *string
	Status      *conversation.ThreadGoalStatus
	TokenBudget *BudgetUpdate
}
type GoalResult struct{ Goal conversation.ThreadGoal }
type GoalLookup struct{ Goal *conversation.ThreadGoal }
type GoalCleared struct{ Cleared bool }

// ResponseToken preserves the backend's opaque numeric/string request identity.
type Response struct {
	Token []byte
	// Payload is opaque backend JSON. Encoding belongs at the adapter boundary;
	// application code must not pass arbitrary Go values through this port.
	Payload json.RawMessage
	Error   *ResponseError
}
type ResponseError struct {
	Code    int
	Message string
}
