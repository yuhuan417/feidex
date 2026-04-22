package app

import (
	"feidex/internal/feishu"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

var pendingCardActionHandlers = map[string]cardActionHandler{
	"user_input.answer": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeUserInputAnswer(action)
	},
	"user_input.toggle_multi": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeUserInputMultiToggle(action)
	},
	"approval.command.accept": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeApprovalAction(action, "approval.command.accept")
	},
	"approval.command.accept_session": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeApprovalAction(action, "approval.command.accept_session")
	},
	"approval.command.decline": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeApprovalAction(action, "approval.command.decline")
	},
	"approval.command.cancel": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeApprovalAction(action, "approval.command.cancel")
	},
	"approval.file.accept": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeApprovalAction(action, "approval.file.accept")
	},
	"approval.file.accept_session": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeApprovalAction(action, "approval.file.accept_session")
	},
	"approval.file.decline": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeApprovalAction(action, "approval.file.decline")
	},
	"approval.file.cancel": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeApprovalAction(action, "approval.file.cancel")
	},
	"approval.permissions.accept_turn": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeApprovalAction(action, "approval.permissions.accept_turn")
	},
	"approval.permissions.accept_session": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeApprovalAction(action, "approval.permissions.accept_session")
	},
	"pending_form.cancel": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completePendingFormCancel(action)
	},
	"review.base.select": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeReviewBaseSelect(action)
	},
	"review.commit.select": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeReviewCommitSelect(action)
	},
	"review.form.submit": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeReviewFormSubmit(action)
	},
	"elicitation_url.accept": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeElicitationURLAction(action, "elicitation_url.accept")
	},
	"elicitation_url.decline": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeElicitationURLAction(action, "elicitation_url.decline")
	},
	"elicitation_url.cancel": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeElicitationURLAction(action, "elicitation_url.cancel")
	},
}
