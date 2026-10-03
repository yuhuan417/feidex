package goal

import (
	"context"
	"fmt"
	"strings"
	"time"

	"feidex/internal/application/backendops"
	"feidex/internal/domain/conversation"
)

const MaxObjectiveRunes = 4000

type Gateway interface {
	GetGoal(context.Context, string) (backendops.GoalLookup, error)
	SetGoal(context.Context, backendops.GoalUpdate) (backendops.GoalResult, error)
	ClearGoal(context.Context, string) (backendops.GoalCleared, error)
}

type Management struct {
	Gateway func() (Gateway, error)
	Tracker *Tracker
	Context func() context.Context
}

type ObjectiveResult struct {
	Goal              *conversation.ThreadGoal
	NeedsConfirmation bool
}

func ValidateObjective(objective string) error {
	if strings.TrimSpace(objective) == "" {
		return fmt.Errorf("goal objective must not be empty")
	}
	if count := len([]rune(strings.TrimSpace(objective))); count > MaxObjectiveRunes {
		return fmt.Errorf("goal objective is too long: %d characters. Limit: %d characters. Put longer instructions in a file and refer to that file in the goal", count, MaxObjectiveRunes)
	}
	return nil
}

func EditedStatus(status conversation.ThreadGoalStatus) conversation.ThreadGoalStatus {
	switch status {
	case conversation.ThreadGoalStatusActive, conversation.ThreadGoalStatusPaused, conversation.ThreadGoalStatusBlocked, conversation.ThreadGoalStatusUsageLimited:
		return status
	default:
		return conversation.ThreadGoalStatusActive
	}
}

// ProposeObjective preserves unfinished goals until a separate confirmation.
func (s Management) ProposeObjective(threadID, objective string) (ObjectiveResult, error) {
	if err := ValidateObjective(objective); err != nil {
		return ObjectiveResult{}, err
	}
	current, err := s.Get(threadID)
	if err != nil {
		return ObjectiveResult{}, err
	}
	if current != nil && current.Status != conversation.ThreadGoalStatusComplete {
		return ObjectiveResult{Goal: current, NeedsConfirmation: true}, nil
	}
	if current != nil {
		if _, err := s.Clear(threadID); err != nil {
			return ObjectiveResult{}, err
		}
	}
	goal, err := s.Set(threadID, objective, conversation.ThreadGoalStatusActive, nil)
	return ObjectiveResult{Goal: goal}, err
}

func (s Management) Replace(threadID, objective string) (*conversation.ThreadGoal, error) {
	if err := ValidateObjective(objective); err != nil {
		return nil, err
	}
	if _, err := s.Clear(threadID); err != nil {
		return nil, err
	}
	return s.Set(threadID, objective, conversation.ThreadGoalStatusActive, nil)
}

func (s Management) Get(threadID string) (*conversation.ThreadGoal, error) {
	gateway, err := s.Gateway()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(s.Context(), 20*time.Second)
	defer cancel()
	resp, err := gateway.GetGoal(ctx, threadID)
	if err != nil {
		return nil, err
	}
	if resp.Goal == nil {
		s.Tracker.ClearGoal(threadID)
	} else {
		s.Tracker.NoteGoal(*resp.Goal)
	}
	return resp.Goal, nil
}

func (s Management) Set(threadID, objective string, status conversation.ThreadGoalStatus, tokenBudget *int64) (*conversation.ThreadGoal, error) {
	if strings.TrimSpace(objective) != "" {
		if err := ValidateObjective(objective); err != nil {
			return nil, err
		}
	}
	gateway, err := s.Gateway()
	if err != nil {
		return nil, err
	}
	params := backendops.GoalUpdate{ThreadID: strings.TrimSpace(threadID)}
	if objective = strings.TrimSpace(objective); objective != "" {
		params.Objective = &objective
	}
	if status != "" {
		params.Status = &status
	}
	if tokenBudget != nil {
		value := *tokenBudget
		params.TokenBudget = &backendops.BudgetUpdate{Value: &value}
	}
	ctx, cancel := context.WithTimeout(s.Context(), 20*time.Second)
	defer cancel()
	resp, err := gateway.SetGoal(ctx, params)
	if err != nil {
		return nil, err
	}
	s.Tracker.NoteGoal(resp.Goal)
	return &resp.Goal, nil
}

func (s Management) Clear(threadID string) (bool, error) {
	gateway, err := s.Gateway()
	if err != nil {
		return false, err
	}
	ctx, cancel := context.WithTimeout(s.Context(), 20*time.Second)
	defer cancel()
	resp, err := gateway.ClearGoal(ctx, threadID)
	if err != nil {
		return false, err
	}
	s.Tracker.ClearGoal(threadID)
	return resp.Cleared, nil
}
