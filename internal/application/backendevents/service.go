// Package backendevents coordinates normalized backend events across owners.
package backendevents

import (
	"context"

	"feidex/internal/application"
	"feidex/internal/domain/conversation"
	"feidex/internal/domain/interaction"
	"feidex/internal/domain/turn"
)

type Lifecycle interface {
	BindPendingSubmissionTurn(string, string, bool) bool
	OnTurnStartedNotification(string, string)
	FinishTurn(string, string, string)
}

type Items interface {
	Start(string, string, turn.ProtocolItem)
	Progress(string, string, string, map[string]any) turn.ProtocolItem
}

type Presentation interface {
	CompleteTurnItem(context.Context, string, string, string, turn.ProtocolItem)
	UpdateInFlightTurnItem(context.Context, string, string, string, turn.ProtocolItem)
	UpdatePendingPlan(string, string)
	RecordTurnError(string, string, string)
	ShowStartedProgress(turn.ProtocolItem) bool
	ProgressEnabled() bool
}

type Compaction interface {
	NoteItemStarted(string, string, turn.ProtocolItem) bool
	FailStandaloneCompactTurn(string, string, string) bool
}

type Submissions interface{ RecordTurnFailure(string, string) error }
type Usage interface {
	RecordTurnTokenUsage(string, string, turn.ThreadTokenUsage)
}
type Goals interface {
	NoteGoal(conversation.ThreadGoal)
	ClearGoal(string)
}
type Interactions interface {
	ResolveAndResume(string) (*interaction.Request, error)
}
type InteractionPresenter interface {
	InteractionRequested(context.Context, application.BackendEvent) error
}

type Dependencies struct {
	Lifecycle            Lifecycle
	Items                Items
	Presentation         Presentation
	Compaction           Compaction
	Submissions          Submissions
	Usage                Usage
	Goals                Goals
	Interactions         Interactions
	InteractionPresenter InteractionPresenter
}

type Service struct{ Deps Dependencies }

func (s Service) Handle(ctx context.Context, event application.BackendEvent) (application.Result, error) {
	d := s.Deps
	switch event.Kind {
	case application.EventTurnStarted:
		if event.TurnID != "" && d.Lifecycle != nil {
			d.Lifecycle.OnTurnStartedNotification(event.ThreadID, event.TurnID)
		}
	case application.EventTurnCompleted:
		if event.Message != "" && d.Presentation != nil {
			d.Presentation.RecordTurnError(event.ThreadID, event.TurnID, event.Message)
		}
		if d.Lifecycle != nil {
			d.Lifecycle.FinishTurn(event.ThreadID, event.TurnID, event.Status)
		}
	case application.EventTurnError:
		if d.Compaction != nil && d.Compaction.FailStandaloneCompactTurn(event.ThreadID, event.TurnID, event.Message) {
			break
		}
		if d.Presentation != nil {
			d.Presentation.RecordTurnError(event.ThreadID, event.TurnID, event.Message)
		}
		if d.Submissions != nil {
			return application.Result{}, d.Submissions.RecordTurnFailure(event.ThreadID, event.TurnID)
		}
	case application.EventRequestResolved:
		if d.Interactions != nil {
			_, err := d.Interactions.ResolveAndResume(event.RequestID)
			return application.Result{}, err
		}
	case application.EventItemStarted:
		if event.Item == nil {
			break
		}
		item := *event.Item
		if d.Lifecycle != nil {
			d.Lifecycle.BindPendingSubmissionTurn(event.ThreadID, event.TurnID, true)
		}
		if d.Items != nil {
			d.Items.Start(event.ThreadID, event.TurnID, item)
		}
		if d.Compaction != nil {
			d.Compaction.NoteItemStarted(event.ThreadID, event.TurnID, item)
		}
		if d.Presentation != nil && d.Presentation.ShowStartedProgress(item) {
			d.Presentation.UpdateInFlightTurnItem(ctx, event.ThreadID, event.TurnID, item.EffectiveID(""), item)
		}
	case application.EventItemCompleted:
		if event.Item != nil && d.Presentation != nil {
			d.Presentation.CompleteTurnItem(ctx, event.ThreadID, event.TurnID, event.Item.EffectiveID(""), *event.Item)
		}
	case application.EventItemProgress:
		if event.Item == nil {
			break
		}
		item := *event.Item
		if d.Items != nil {
			item = d.Items.Progress(event.ThreadID, event.TurnID, item.EffectiveID(""), item.MergedRaw())
		}
		if d.Presentation != nil && d.Presentation.ProgressEnabled() {
			d.Presentation.UpdateInFlightTurnItem(ctx, event.ThreadID, event.TurnID, item.EffectiveID(""), item)
		}
	case application.EventPlanUpdated:
		if d.Presentation != nil {
			d.Presentation.UpdatePendingPlan(event.TurnID, event.Message)
		}
	case application.EventUsageUpdated:
		if event.Usage != nil && d.Usage != nil {
			d.Usage.RecordTurnTokenUsage(event.ThreadID, event.TurnID, *event.Usage)
		}
	case application.EventGoalUpdated:
		if event.Goal != nil && d.Goals != nil {
			d.Goals.NoteGoal(*event.Goal)
		}
	case application.EventGoalCleared:
		if d.Goals != nil {
			d.Goals.ClearGoal(event.ThreadID)
		}
	case application.EventApprovalRequested, application.EventUserInputRequested, application.EventElicitationURLRequested, application.EventElicitationFormRequested, application.EventRequestRejected:
		if d.InteractionPresenter != nil {
			return application.Result{}, d.InteractionPresenter.InteractionRequested(ctx, event)
		}
	}
	return application.Result{}, nil
}
