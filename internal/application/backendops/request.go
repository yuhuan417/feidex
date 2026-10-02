// Package backendops defines the semantic operations consumed by application services.
package backendops

import (
	"feidex/internal/domain/conversation"
	"feidex/internal/domain/review"
	"feidex/internal/domain/submission"
)

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
	Token   []byte
	Payload any
	Error   *ResponseError
}
type ResponseError struct {
	Code    int
	Message string
}
