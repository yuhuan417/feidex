package feishuapp

import (
	claudesupport "feidex/internal/adapter/feishu/claudesupport"
	appreviewcmd "feidex/internal/adapter/feishu/reviewcmd"
	"feidex/internal/adapter/feishu/serverrequest"
	"feidex/internal/feishu"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

func pendingCardActionHandlers() map[string]cardActionHandler {
	return map[string]cardActionHandler{
		"async_user_input.answer": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return completeAsyncUserInput(s.app, action, false)
		},
		"async_user_input.cancel": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return completeAsyncUserInput(s.app, action, true)
		},
		"pending_form.cancel": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return completePendingFormCancelDispatch(s.app, action)
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
	}
}

func pendingPortCardActionHandlers(requests *serverrequest.Service, claude *claudesupport.Service, review appreviewcmd.ReviewFormService) map[string]cardActionPortHandler {
	return map[string]cardActionPortHandler{
		"pending_form.plan_reject": func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return completePlanReject(claude, requests, action)
		},
		"pending_form.plan_approve": func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return completePlanApprove(claude, action)
		},
		"review.base.select": func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return review.CompleteReviewBaseSelect(action)
		},
		"review.commit.select": func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return review.CompleteReviewCommitSelect(action)
		},
		"review.form.submit": func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return review.CompleteReviewFormSubmit(action)
		},
	}
}
