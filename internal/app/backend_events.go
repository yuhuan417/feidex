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
	"feidex/internal/domain/interaction"
	"feidex/internal/domain/submission"
	"feidex/internal/domain/turn"
)

func newBackendEventService(a *App) backendevents.Service {
	return backendevents.Service{
		ItemStarted: func(ctx context.Context, threadID, turnID string, item turn.ProtocolItem) {
			newRuntimeStateService(a).noteTurnItemStartedPayload(threadID, turnID, item)
			newCompactionService(a).NoteStandaloneCompactItemStarted(threadID, turnID, item.MergedRaw())
			if turnitem.NormalizeTurnItemType(item.Type) == "mcp_tool_call" {
				newTurnStreamService(a).updateInFlightTurnItemPayload(ctx, threadID, turnID, item.EffectiveID(""), item)
			}
		},
		ItemCompleted: func(ctx context.Context, threadID, turnID string, item turn.ProtocolItem) {
			newTurnStreamService(a).completeTurnItemPayload(ctx, threadID, turnID, item.EffectiveID(""), item)
		},
		ItemProgress: func(ctx context.Context, threadID, turnID string, item turn.ProtocolItem) {
			snapshot := newRuntimeStateService(a).updateInFlightTurnItemPayload(threadID, turnID, item.EffectiveID(""), item.MergedRaw())
			if quietmode.WorkingCardEnabled(feishuConfig(a)) {
				newTurnStreamService(a).updateInFlightTurnItemPayload(ctx, threadID, turnID, item.EffectiveID(""), snapshot)
			}
		},
		PlanUpdated:   func(turnID, plan string) { newTurnStreamService(a).updatePendingPlan(turnID, plan) },
		TurnStarted:   func(threadID, turnID string) { onTurnStartedNotification(a, threadID, turnID) },
		TurnCompleted: func(threadID, turnID, status string) { finishTurn(a, threadID, turnID, status) },
		RecordError: func(threadID, turnID, message string) {
			newTurnStreamService(a).recordTurnError(threadID, turnID, message)
		},
		FailCompact: func(threadID, turnID, message string) bool {
			return newCompactionService(a).FailStandaloneCompactTurn(threadID, turnID, message)
		},
		FailSubmission: func(threadID, turnID string) {
			newSubmissionQueueServiceFromApp(a).UpdateSubmissionByTurn(threadID, turnID, func(sub *submission.Submission) { sub.Status = submission.SubmissionStatusFailed.String() })
		},
		UsageUpdated: func(threadID, turnID string, usage turn.ThreadTokenUsage) {
			onThreadTokenUsageUpdated(a, threadID, turnID, codex.ProtocolThreadUsage(usage))
		},
		GoalUpdated: func(threadID string, goal conversation.ThreadGoal) {
			onThreadGoalUpdated(a, codexrpc.ThreadGoalUpdatedNotification{ThreadID: threadID, Goal: goal})
		},
		GoalCleared: func(threadID string) {
			onThreadGoalCleared(a, codexrpc.ThreadGoalClearedNotification{ThreadID: threadID})
		},
		RequestResolved: func(id string) {
			pending := newRuntimeStateService(a).resolveServerPendingRequest(id)
			a.ServerRequestService().ResumeSubmissionAfterRequest(pending)
		},
		InteractionRequested: func(ctx context.Context, event application.BackendEvent) error {
			return deliverBackendInteraction(a, ctx, event)
		},
	}
}
func deliverBackendInteraction(a *App, _ context.Context, event application.BackendEvent) error {
	token := json.RawMessage(event.ResponseToken)
	switch event.Kind {
	case application.EventApprovalRequested:
		cwd := ""
		if _, sub := newSubmissionQueueServiceFromApp(a).FindSubmissionByTurn(event.ThreadID, event.TurnID); sub != nil {
			if ws := config.FindWorkspace(a.cfg, sub.WorkspaceID); ws != nil {
				cwd = ws.Cwd
			}
		}
		p := approval.PresentationForEvent(event, newRuntimeStateService(a).mergeApprovalPresentationWithTurnItem, cwd)
		a.ServerRequestService().SendApprovalCardPresentation(token, p)
	case application.EventUserInputRequested:
		p := event.Payload.(interaction.ToolUserInputPayload)
		if len(p.Questions) == 1 && len(p.Questions[0].Options) > 0 && len(p.Questions[0].Options) <= 3 && !p.Questions[0].MultiSelect && !p.Questions[0].IsOther {
			a.ServerRequestService().SendUserInputCard(token, p)
		} else {
			a.ServerRequestService().SendUserInputFormCard(token, p)
		}
	case application.EventElicitationURLRequested:
		a.ServerRequestService().SendElicitationURLCard(token, event.Payload.(interaction.ElicitationURLPayload))
	case application.EventElicitationFormRequested:
		a.ServerRequestService().SendElicitationFormCard(token, event.Payload.(interaction.ElicitationFormPayload))
	case application.EventRequestRejected:
		replyCodexError(a, token, event.Payload.(application.RequestRejected).Code, event.Message)
	}
	return nil
}
