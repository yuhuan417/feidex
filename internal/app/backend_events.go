package app

import (
	"context"
	"encoding/json"
	"feidex/internal/adapter/backend/codex"
	"feidex/internal/adapter/feishu/approval"
	"feidex/internal/adapter/feishu/quietmode"
	"feidex/internal/adapter/feishu/turnitem"
	"feidex/internal/application"
	"feidex/internal/application/backendevents"
	"feidex/internal/codexrpc"
	"feidex/internal/config"
	"feidex/internal/domain/conversation"
	"feidex/internal/domain/submission"
	"feidex/internal/domain/turn"
)

func newBackendEventService(a *App) backendevents.Service {
	return backendevents.Service{Sink: backendEventSink{app: a}}
}

type backendEventSink struct{ app *App }

func (s backendEventSink) ItemStarted(ctx context.Context, threadID, turnID string, item turn.ProtocolItem) {
	a := s.app
	newRuntimeStateService(a).noteTurnItemStartedPayload(threadID, turnID, item)
	newCompactionService(a).NoteStandaloneCompactItemStarted(threadID, turnID, item.MergedRaw())
	if turnitem.NormalizeTurnItemType(item.Type) == "mcp_tool_call" {
		newTurnStreamService(a).updateInFlightTurnItemPayload(ctx, threadID, turnID, item.EffectiveID(""), item)
	}
}

func (s backendEventSink) ItemCompleted(ctx context.Context, threadID, turnID string, item turn.ProtocolItem) {
	a := s.app
	newTurnStreamService(a).completeTurnItemPayload(ctx, threadID, turnID, item.EffectiveID(""), item)
}

func (s backendEventSink) ItemProgress(ctx context.Context, threadID, turnID string, item turn.ProtocolItem) {
	a := s.app
	snapshot := newRuntimeStateService(a).updateInFlightTurnItemPayload(threadID, turnID, item.EffectiveID(""), item.MergedRaw())
	if quietmode.WorkingCardEnabled(feishuConfig(a)) {
		newTurnStreamService(a).updateInFlightTurnItemPayload(ctx, threadID, turnID, item.EffectiveID(""), snapshot)
	}
}

func (s backendEventSink) PlanUpdated(turnID, plan string) {
	newTurnStreamService(s.app).updatePendingPlan(turnID, plan)
}
func (s backendEventSink) TurnStarted(threadID, turnID string) {
	onTurnStartedNotification(s.app, threadID, turnID)
}
func (s backendEventSink) TurnCompleted(threadID, turnID, status string) {
	finishTurn(s.app, threadID, turnID, status)
}
func (s backendEventSink) RecordError(threadID, turnID, message string) {
	a := s.app
	newTurnStreamService(a).recordTurnError(threadID, turnID, message)
}
func (s backendEventSink) FailCompact(threadID, turnID, message string) bool {
	return newCompactionService(s.app).FailStandaloneCompactTurn(threadID, turnID, message)
}
func (s backendEventSink) FailSubmission(threadID, turnID string) {
	a := s.app
	newSubmissionQueueServiceFromApp(a).UpdateSubmissionByTurn(threadID, turnID, func(sub *submission.Submission) { sub.Status = submission.SubmissionStatusFailed.String() })
}
func (s backendEventSink) UsageUpdated(threadID, turnID string, usage turn.ThreadTokenUsage) {
	a := s.app
	onThreadTokenUsageUpdated(a, threadID, turnID, codex.ProtocolThreadUsage(usage))
}
func (s backendEventSink) GoalUpdated(threadID string, goal conversation.ThreadGoal) {
	a := s.app
	onThreadGoalUpdated(a, codexrpc.ThreadGoalUpdatedNotification{ThreadID: threadID, Goal: goal})
}
func (s backendEventSink) GoalCleared(threadID string) {
	a := s.app
	onThreadGoalCleared(a, codexrpc.ThreadGoalClearedNotification{ThreadID: threadID})
}
func (s backendEventSink) RequestResolved(id string) {
	a := s.app
	runtimeState := newRuntimeStateService(a)
	pending := runtimeState.resolveServerPendingRequest(id)
	runtimeState.resumeSubmissionAfterRequest(pending)
}
func (s backendEventSink) InteractionRequested(ctx context.Context, event application.BackendEvent) error {
	return deliverBackendInteraction(s.app, ctx, event)
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
		p := approval.PresentationForEvent(event, newRuntimeStateService(a).mergeApprovalPresentationWithTurnItem, cwd)
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
