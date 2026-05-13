package app

import (
	"feidex/internal/feishu"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

var pendingCardActionHandlers = map[string]cardActionHandler{
	"user_input.answer": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return s.app.ServerRequestService().CompleteUserInputAnswer(action)
	},
	"user_input.toggle_multi": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return s.app.ServerRequestService().CompleteUserInputMultiToggle(action)
	},
	"approval.command.accept": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return s.app.ServerRequestService().CompleteApprovalAction(action, "approval.command.accept")
	},
	"approval.command.accept_session": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return s.app.ServerRequestService().CompleteApprovalAction(action, "approval.command.accept_session")
	},
	"approval.command.decline": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return s.app.ServerRequestService().CompleteApprovalAction(action, "approval.command.decline")
	},
	"approval.command.cancel": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return s.app.ServerRequestService().CompleteApprovalAction(action, "approval.command.cancel")
	},
	"approval.file.accept": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return s.app.ServerRequestService().CompleteApprovalAction(action, "approval.file.accept")
	},
	"approval.file.accept_session": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return s.app.ServerRequestService().CompleteApprovalAction(action, "approval.file.accept_session")
	},
	"approval.file.decline": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return s.app.ServerRequestService().CompleteApprovalAction(action, "approval.file.decline")
	},
	"approval.file.cancel": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return s.app.ServerRequestService().CompleteApprovalAction(action, "approval.file.cancel")
	},
	"approval.permissions.accept_turn": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return s.app.ServerRequestService().CompleteApprovalAction(action, "approval.permissions.accept_turn")
	},
	"approval.permissions.accept_session": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return s.app.ServerRequestService().CompleteApprovalAction(action, "approval.permissions.accept_session")
	},
	"pending_form.cancel": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return completePendingFormCancelDispatch(s.app, action)
	},
	"pending_form.plan_approve": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return completePlanApprove(s.app, action)
	},
	"pending_form.plan_reject": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return completePlanReject(s.app, action)
	},
	codexPlanModeExitImplementCurrentAction: func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return completeCodexPlanModeExit(s.app, action, codexPlanModeExitImplementCurrentAction)
	},
	codexPlanModeExitImplementFreshAction: func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return completeCodexPlanModeExit(s.app, action, codexPlanModeExitImplementFreshAction)
	},
	codexPlanModeExitStayAction: func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return completeCodexPlanModeExit(s.app, action, codexPlanModeExitStayAction)
	},
	"review.base.select": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return newReviewFormServiceInner(s.app).CompleteReviewBaseSelect(action)
	},
	"review.commit.select": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return newReviewFormServiceInner(s.app).CompleteReviewCommitSelect(action)
	},
	"review.form.submit": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return newReviewFormServiceInner(s.app).CompleteReviewFormSubmit(action)
	},
	"elicitation_url.accept": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return s.app.ServerRequestService().CompleteElicitationURLAction(action, "elicitation_url.accept")
	},
	"elicitation_url.decline": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return s.app.ServerRequestService().CompleteElicitationURLAction(action, "elicitation_url.decline")
	},
	"elicitation_url.cancel": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return s.app.ServerRequestService().CompleteElicitationURLAction(action, "elicitation_url.cancel")
	},
}
