package feishuapp

import (
	claudesupport "feidex/internal/adapter/feishu/claudesupport"
	"feidex/internal/adapter/feishu/planmode"
	appreviewcmd "feidex/internal/adapter/feishu/reviewcmd"
	"feidex/internal/adapter/feishu/serverrequest"
	"feidex/internal/adapter/feishu/threadmenu"
	"feidex/internal/feishu"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

func pendingPlanModeExitPortCardActionHandlers(deps planmode.Dependencies) map[string]cardActionPortHandler {
	return map[string]cardActionPortHandler{
		codexPlanModeExitImplementCurrentAction: func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return planmode.CompleteCodexPlanModeExit(deps, action, codexPlanModeExitImplementCurrentAction)
		},
		codexPlanModeExitImplementFreshAction: func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return planmode.CompleteCodexPlanModeExit(deps, action, codexPlanModeExitImplementFreshAction)
		},
		codexPlanModeExitStayAction: func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return planmode.CompleteCodexPlanModeExit(deps, action, codexPlanModeExitStayAction)
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

func threadMenuPortCardActionHandlers(service *threadmenu.Service) map[string]cardActionPortHandler {
	return map[string]cardActionPortHandler{
		"menu.interrupt": func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return service.CompleteMenuInterrupt(action, actionSessionKey(action), actionStringValue(action, "turn_id"))
		},
		"menu.thread": func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return service.CompleteMenuThread(action, actionSessionKey(action))
		},
		"menu.new": func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return service.CompleteMenuNew(action, actionSessionKey(action))
		},
	}
}
