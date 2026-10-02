package codexrpc

import (
	"encoding/json"
	"feidex/internal/domain/conversation"
)

type ThreadGoalStatus = conversation.ThreadGoalStatus

const (
	ThreadGoalStatusActive        = conversation.ThreadGoalStatusActive
	ThreadGoalStatusPaused        = conversation.ThreadGoalStatusPaused
	ThreadGoalStatusBlocked       = conversation.ThreadGoalStatusBlocked
	ThreadGoalStatusUsageLimited  = conversation.ThreadGoalStatusUsageLimited
	ThreadGoalStatusBudgetLimited = conversation.ThreadGoalStatusBudgetLimited
	ThreadGoalStatusComplete      = conversation.ThreadGoalStatusComplete
)

type ThreadGoal = conversation.ThreadGoal

type ThreadGoalSetParams struct {
	ThreadID    string            `json:"threadId"`
	Objective   *string           `json:"objective,omitempty"`
	Status      *ThreadGoalStatus `json:"status,omitempty"`
	TokenBudget *NullableInt64    `json:"tokenBudget,omitempty"`
}

// NullableInt64 represents an optional nullable JSON number. A nil
// *NullableInt64 omits the field; a non-nil value with Value == nil emits null.
type NullableInt64 struct {
	Value *int64
}

func NewNullableInt64(value *int64) *NullableInt64 {
	if value == nil {
		return &NullableInt64{}
	}
	copy := *value
	return &NullableInt64{Value: &copy}
}

func (n NullableInt64) MarshalJSON() ([]byte, error) {
	if n.Value == nil {
		return []byte("null"), nil
	}
	return json.Marshal(*n.Value)
}

type ThreadGoalSetResponse struct {
	Goal ThreadGoal `json:"goal"`
}

type ThreadGoalGetResponse struct {
	Goal *ThreadGoal `json:"goal"`
}

type ThreadGoalClearResponse struct {
	Cleared bool `json:"cleared"`
}

type ThreadGoalUpdatedNotification struct {
	ThreadID string     `json:"threadId"`
	TurnID   *string    `json:"turnId"`
	Goal     ThreadGoal `json:"goal"`
}

type ThreadGoalClearedNotification struct {
	ThreadID string `json:"threadId"`
}
