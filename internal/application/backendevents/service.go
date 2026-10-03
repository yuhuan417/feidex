// Package backendevents applies normalized backend events to their owners.
package backendevents

import (
	"context"
	"feidex/internal/application"
	"feidex/internal/domain/conversation"
	"feidex/internal/domain/turn"
)

// EventSink is the single consumer-owned port for normalized backend events.
// The event handler does not locate services through a host, parse protocol,
// render cards or mutate DTOs. A sink may be implemented by the runtime
// composition root, but the application package only sees these semantics.
type EventSink interface {
	ItemStarted(context.Context, string, string, turn.ProtocolItem)
	ItemCompleted(context.Context, string, string, turn.ProtocolItem)
	ItemProgress(context.Context, string, string, turn.ProtocolItem)
	PlanUpdated(string, string)
	TurnStarted(string, string)
	TurnCompleted(string, string, string)
	RecordError(string, string, string)
	FailCompact(string, string, string) bool
	FailSubmission(string, string)
	UsageUpdated(string, string, turn.ThreadTokenUsage)
	GoalUpdated(string, conversation.ThreadGoal)
	GoalCleared(string)
	RequestResolved(string)
	InteractionRequested(context.Context, application.BackendEvent) error
}

type Service struct{ Sink EventSink }

func (s Service) Handle(ctx context.Context, event application.BackendEvent) (application.Result, error) {
	switch event.Kind {
	case application.EventTurnStarted:
		if event.TurnID != "" && s.Sink != nil {
			s.Sink.TurnStarted(event.ThreadID, event.TurnID)
		}
	case application.EventTurnCompleted:
		if event.Message != "" && s.Sink != nil {
			s.Sink.RecordError(event.ThreadID, event.TurnID, event.Message)
		}
		if s.Sink != nil {
			s.Sink.TurnCompleted(event.ThreadID, event.TurnID, event.Status)
		}
	case application.EventTurnError:
		if s.Sink != nil && s.Sink.FailCompact(event.ThreadID, event.TurnID, event.Message) {
			return application.Result{}, nil
		}
		if s.Sink != nil {
			s.Sink.RecordError(event.ThreadID, event.TurnID, event.Message)
		}
		if s.Sink != nil {
			s.Sink.FailSubmission(event.ThreadID, event.TurnID)
		}
	case application.EventRequestResolved:
		if s.Sink != nil {
			s.Sink.RequestResolved(event.RequestID)
		}
	case application.EventItemStarted:
		if item, ok := event.Payload.(turn.ProtocolItem); ok && s.Sink != nil {
			s.Sink.ItemStarted(ctx, event.ThreadID, event.TurnID, item)
		}
	case application.EventItemCompleted:
		if item, ok := event.Payload.(turn.ProtocolItem); ok && s.Sink != nil {
			s.Sink.ItemCompleted(ctx, event.ThreadID, event.TurnID, item)
		}
	case application.EventItemProgress:
		if item, ok := event.Payload.(turn.ProtocolItem); ok && s.Sink != nil {
			s.Sink.ItemProgress(ctx, event.ThreadID, event.TurnID, item)
		}
	case application.EventPlanUpdated:
		if s.Sink != nil {
			s.Sink.PlanUpdated(event.TurnID, event.Message)
		}
	case application.EventUsageUpdated:
		if usage, ok := event.Payload.(turn.ThreadTokenUsage); ok && s.Sink != nil {
			s.Sink.UsageUpdated(event.ThreadID, event.TurnID, usage)
		}
	case application.EventGoalUpdated:
		if goal, ok := event.Payload.(conversation.ThreadGoal); ok && s.Sink != nil {
			s.Sink.GoalUpdated(event.ThreadID, goal)
		}
	case application.EventGoalCleared:
		if s.Sink != nil {
			s.Sink.GoalCleared(event.ThreadID)
		}
	case application.EventApprovalRequested, application.EventUserInputRequested, application.EventElicitationURLRequested, application.EventElicitationFormRequested, application.EventRequestRejected:
		if s.Sink != nil {
			return application.Result{}, s.Sink.InteractionRequested(ctx, event)
		}
	}
	return application.Result{}, nil
}
