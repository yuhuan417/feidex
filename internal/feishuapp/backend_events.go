package feishuapp

import (
	"context"
	"encoding/json"
	"feidex/internal/adapter/feishu/approval"
	"feidex/internal/application"
	"feidex/internal/application/backendevents"
	"feidex/internal/config"
)

func BackendEventPorts(a *App) backendevents.Dependencies {
	return backendevents.Dependencies{
		Lifecycle:            a.bindings.Turns,
		Items:                a.bindings.TurnItems,
		Presentation:         a.bindings.TurnPresentation,
		Compaction:           a.bindings.Compaction,
		Submissions:          a.bindings.SubmissionStatus,
		Usage:                a.runtimeOwner.TurnBindings,
		Goals:                goalTrackerForApp(a),
		Interactions:         a.bindings.Interactions,
		InteractionPresenter: backendInteractionPresenter{app: a},
	}
}

type backendInteractionPresenter struct{ app *App }

func (p backendInteractionPresenter) InteractionRequested(ctx context.Context, event application.BackendEvent) error {
	return deliverBackendInteraction(p.app, ctx, event)
}
func deliverBackendInteraction(a *App, _ context.Context, event application.BackendEvent) error {
	token := json.RawMessage(event.ResponseToken)
	switch event.Kind {
	case application.EventApprovalRequested:
		cwd := ""
		if _, sub := findSubmissionByTurn(a, event.ThreadID, event.TurnID); sub != nil {
			if ws := config.FindWorkspace(a.cfg, sub.WorkspaceID); ws != nil {
				cwd = ws.Cwd
			}
		}
		p := approval.PresentationForEvent(event, a.bindings.ItemContext.MergePresentation, cwd)
		a.ServerRequestService().SendApprovalCardPresentation(token, p)
	case application.EventUserInputRequested:
		if event.UserInput == nil {
			return nil
		}
		p := *event.UserInput
		if len(p.Questions) == 1 && len(p.Questions[0].Options) > 0 && len(p.Questions[0].Options) <= 3 && !p.Questions[0].MultiSelect && !p.Questions[0].IsOther {
			a.ServerRequestService().SendUserInputCard(token, p)
		} else {
			a.ServerRequestService().SendUserInputFormCard(token, p)
		}
	case application.EventElicitationURLRequested:
		if event.ElicitationURL == nil {
			return nil
		}
		a.ServerRequestService().SendElicitationURLCard(token, *event.ElicitationURL)
	case application.EventElicitationFormRequested:
		if event.ElicitationForm == nil {
			return nil
		}
		a.ServerRequestService().SendElicitationFormCard(token, *event.ElicitationForm)
	case application.EventRequestRejected:
		if event.Rejected == nil {
			return nil
		}
		replyCodexError(a, token, event.Rejected.Code, event.Message)
	}
	return nil
}
