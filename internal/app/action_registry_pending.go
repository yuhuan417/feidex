package app

import (
	"feidex/internal/feishu"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

var pendingCardActionHandlers = map[string]cardActionHandler{
	"user_input.answer": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return s.app.completeUserInputAnswer(action)
	},
	"user_input.toggle_multi": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return s.app.completeUserInputMultiToggle(action)
	},
	"approval.command.accept": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return s.app.completeApprovalAction(action, "approval.command.accept")
	},
	"approval.command.accept_session": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return s.app.completeApprovalAction(action, "approval.command.accept_session")
	},
	"approval.command.decline": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return s.app.completeApprovalAction(action, "approval.command.decline")
	},
	"approval.command.cancel": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return s.app.completeApprovalAction(action, "approval.command.cancel")
	},
	"approval.file.accept": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return s.app.completeApprovalAction(action, "approval.file.accept")
	},
	"approval.file.accept_session": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return s.app.completeApprovalAction(action, "approval.file.accept_session")
	},
	"approval.file.decline": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return s.app.completeApprovalAction(action, "approval.file.decline")
	},
	"approval.file.cancel": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return s.app.completeApprovalAction(action, "approval.file.cancel")
	},
	"approval.permissions.accept_turn": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return s.app.completeApprovalAction(action, "approval.permissions.accept_turn")
	},
	"approval.permissions.accept_session": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return s.app.completeApprovalAction(action, "approval.permissions.accept_session")
	},
	"pending_form.cancel": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return s.app.completePendingFormCancel(action)
	},
	"review.base.select": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return newReviewFormService(s.app).completeReviewBaseSelect(action)
	},
	"review.commit.select": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return newReviewFormService(s.app).completeReviewCommitSelect(action)
	},
	"review.form.submit": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return newReviewFormService(s.app).completeReviewFormSubmit(action)
	},
	"elicitation_url.accept": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return s.app.completeElicitationURLAction(action, "elicitation_url.accept")
	},
	"elicitation_url.decline": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return s.app.completeElicitationURLAction(action, "elicitation_url.decline")
	},
	"elicitation_url.cancel": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return s.app.completeElicitationURLAction(action, "elicitation_url.cancel")
	},
}
