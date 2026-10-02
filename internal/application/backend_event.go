package application

import (
	"feidex/internal/domain/conversation"
	"feidex/internal/domain/interaction"
	"feidex/internal/domain/turn"
)

const (
	EventItemStarted              = "item_started"
	EventItemCompleted            = "item_completed"
	EventItemProgress             = "item_progress"
	EventPlanUpdated              = "plan_updated"
	EventUsageUpdated             = "usage_updated"
	EventGoalUpdated              = "goal_updated"
	EventGoalCleared              = "goal_cleared"
	EventApprovalRequested        = "approval_requested"
	EventUserInputRequested       = "user_input_requested"
	EventElicitationURLRequested  = "elicitation_url_requested"
	EventElicitationFormRequested = "elicitation_form_requested"
	EventRequestRejected          = "request_rejected"
)

type ApprovalRequested struct {
	Kind, ItemID string
	Request      map[string]any
}
type RequestRejected struct{ Code int }
type ItemEvent = turn.ProtocolItem
type UsageEvent = turn.ThreadTokenUsage
type GoalEvent = conversation.ThreadGoal
type UserInputEvent = interaction.ToolUserInputPayload
type ElicitationURLEvent = interaction.ElicitationURLPayload
type ElicitationFormEvent = interaction.ElicitationFormPayload
