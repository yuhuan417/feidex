package feishuapp

import (
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
		"pending_form.plan_approve": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return completePlanApprove(s.app.bindings.ClaudeSupport, action)
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
			return s.app.bindings.ReviewCommands.CompleteReviewBaseSelect(action)
		},
		"review.commit.select": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return s.app.bindings.ReviewCommands.CompleteReviewCommitSelect(action)
		},
		"review.form.submit": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return s.app.bindings.ReviewCommands.CompleteReviewFormSubmit(action)
		},
	}

}
