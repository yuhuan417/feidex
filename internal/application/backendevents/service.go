// Package backendevents applies normalized backend events to their owners.
package backendevents

import (
	"context"
	"feidex/internal/application"
	"feidex/internal/domain/conversation"
	"feidex/internal/domain/turn"
)

// Each port belongs to the consuming use case. The event handler does not
// locate services through a host, parse protocol, render cards or mutate DTOs.
type Service struct {
	ItemStarted          func(context.Context, string, string, turn.ProtocolItem)
	ItemCompleted        func(context.Context, string, string, turn.ProtocolItem)
	ItemProgress         func(context.Context, string, string, turn.ProtocolItem)
	PlanUpdated          func(string, string)
	TurnStarted          func(string, string)
	TurnCompleted        func(string, string, string)
	RecordError          func(string, string, string)
	FailCompact          func(string, string, string) bool
	FailSubmission       func(string, string)
	UsageUpdated         func(string, string, turn.ThreadTokenUsage)
	GoalUpdated          func(string, conversation.ThreadGoal)
	GoalCleared          func(string)
	RequestResolved      func(string)
	InteractionRequested func(context.Context, application.BackendEvent) error
}

func (s Service) Handle(ctx context.Context, event application.BackendEvent) (application.Result, error) {
	switch event.Kind {
	case application.EventTurnStarted:
		if event.TurnID != "" && s.TurnStarted != nil {
			s.TurnStarted(event.ThreadID, event.TurnID)
		}
	case application.EventTurnCompleted:
		if event.Message != "" && s.RecordError != nil {
			s.RecordError(event.ThreadID, event.TurnID, event.Message)
		}
		if s.TurnCompleted != nil {
			s.TurnCompleted(event.ThreadID, event.TurnID, event.Status)
		}
	case application.EventTurnError:
		if s.FailCompact != nil && s.FailCompact(event.ThreadID, event.TurnID, event.Message) {
			return application.Result{}, nil
		}
		if s.RecordError != nil {
			s.RecordError(event.ThreadID, event.TurnID, event.Message)
		}
		if s.FailSubmission != nil {
			s.FailSubmission(event.ThreadID, event.TurnID)
		}
	case application.EventRequestResolved:
		if s.RequestResolved != nil {
			s.RequestResolved(event.RequestID)
		}
	case application.EventItemStarted:
		if item, ok := event.Payload.(turn.ProtocolItem); ok && s.ItemStarted != nil {
			s.ItemStarted(ctx, event.ThreadID, event.TurnID, item)
		}
	case application.EventItemCompleted:
		if item, ok := event.Payload.(turn.ProtocolItem); ok && s.ItemCompleted != nil {
			s.ItemCompleted(ctx, event.ThreadID, event.TurnID, item)
		}
	case application.EventItemProgress:
		if item, ok := event.Payload.(turn.ProtocolItem); ok && s.ItemProgress != nil {
			s.ItemProgress(ctx, event.ThreadID, event.TurnID, item)
		}
	case application.EventPlanUpdated:
		if s.PlanUpdated != nil {
			s.PlanUpdated(event.TurnID, event.Message)
		}
	case application.EventUsageUpdated:
		if usage, ok := event.Payload.(turn.ThreadTokenUsage); ok && s.UsageUpdated != nil {
			s.UsageUpdated(event.ThreadID, event.TurnID, usage)
		}
	case application.EventGoalUpdated:
		if goal, ok := event.Payload.(conversation.ThreadGoal); ok && s.GoalUpdated != nil {
			s.GoalUpdated(event.ThreadID, goal)
		}
	case application.EventGoalCleared:
		if s.GoalCleared != nil {
			s.GoalCleared(event.ThreadID)
		}
	case application.EventApprovalRequested, application.EventUserInputRequested, application.EventElicitationURLRequested, application.EventElicitationFormRequested, application.EventRequestRejected:
		if s.InteractionRequested != nil {
			return application.Result{}, s.InteractionRequested(ctx, event)
		}
	}
	return application.Result{}, nil
}
