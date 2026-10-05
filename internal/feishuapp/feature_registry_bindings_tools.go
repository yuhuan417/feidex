package feishuapp

import (
	"feidex/internal/adapter/feishu/debugviewcmd"
	"feidex/internal/adapter/feishu/goalcmd"
	"feidex/internal/adapter/feishu/planmode"
	appreviewcmd "feidex/internal/adapter/feishu/reviewcmd"
	"feidex/internal/codexrpc"
	"feidex/internal/config"
	"feidex/internal/feishu"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

func appendFeatureBindingsTools(bindings map[string]featureBinding) {
	bindings["menu.review"] = featureBinding{
		Commands: map[string]featureCommandBinding{
			"review": {
				Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
					return appreviewcmd.CommandReview(ReviewCommandDependencies(a), msg, args)
				},
			},
		},
		RenderActions: []string{"menu.review"},
		Render: func(actionName string, a *App, sessionKey string) (map[string]any, bool) {
			if actionName != "menu.review" {
				return nil, false
			}
			return a.bindings.ReviewCommands.RenderReviewMenuCard(sessionKey), true
		},
		HandleAction: func(actionName string, s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			sessionKey := actionSessionKey(action)
			switch actionName {
			case "menu.review":
				return s.completeMenuReview(action, sessionKey)
			case "menu.review.uncommitted":
				return appreviewcmd.CompleteMenuReviewUncommitted(ReviewCommandDependencies(s.app), action, sessionKey)
			case "menu.review.base":
				return appreviewcmd.CompleteMenuReviewBase(ReviewCommandDependencies(s.app), action, sessionKey)
			case "menu.review.commit":
				return appreviewcmd.CompleteMenuReviewCommit(ReviewCommandDependencies(s.app), action, sessionKey)
			case "menu.review.custom":
				return completeMenuCommand(s.app, action, sessionKey, "/review custom", "menu.review")
			default:
				return nil, nil
			}
		},
	}
	bindings["menu.quiet"] = featureBinding{
		Commands: map[string]featureCommandBinding{
			"quiet": {
				Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
					return commandQuiet(a, msg, args)
				},
			},
		},
		HandleAction: func(actionName string, s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			switch actionName {
			case "menu.quiet":
				return s.completeMenuQuiet(action, actionSessionKey(action))
			case "quiet.set":
				return s.completeQuietSet(action, config.QuietMode(actionStringValue(action, "mode")))
			default:
				return nil, nil
			}
		},
	}
	bindings["plan"] = featureBinding{
		Commands: map[string]featureCommandBinding{
			"plan": {
				Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
					return planmode.CommandPlan(newPlanModeAppAdapter(a), msg, args)
				},
			},
		},
		HandleAction: func(actionName string, s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			if actionName != "menu.plan" {
				return nil, nil
			}
			return completeMenuPlanAsync(s.app, action, actionSessionKey(action))
		},
	}
	bindings["goal"] = featureBinding{
		Commands: map[string]featureCommandBinding{
			"goal": {
				HandleRaw: func(a *App, msg *feishu.InboundMessage, raw string, args []string) error {
					return commandGoalRaw(a.bindings.GoalCommands, msg, raw, args)
				},
			},
		},
		HandleAction: func(actionName string, s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			sessionKey := actionSessionKey(action)
			switch actionName {
			case "menu.goal":
				return completeMenuGoalAsync(s.app, action, sessionKey)
			case "goal.pause":
				return completeGoalRenderedActionAsync(s.app, action, sessionKey, "正在更新 goal", func(goalSvc goalcmd.Service) (*callback.CardActionTriggerResponse, error) {
					return goalSvc.CompleteGoalStatusAction(action, codexrpc.ThreadGoalStatusPaused)
				})
			case "goal.resume":
				return completeGoalRenderedActionAsync(s.app, action, sessionKey, "正在更新 goal", func(goalSvc goalcmd.Service) (*callback.CardActionTriggerResponse, error) {
					return goalSvc.CompleteGoalStatusAction(action, codexrpc.ThreadGoalStatusActive)
				})
			case "goal.clear":
				return completeGoalRenderedActionAsync(s.app, action, sessionKey, "正在清除 goal", func(goalSvc goalcmd.Service) (*callback.CardActionTriggerResponse, error) {
					return goalSvc.CompleteGoalClearAction(action)
				})
			case "goal.edit":
				return completeGoalRenderedActionAsync(s.app, action, sessionKey, "正在打开 goal 编辑", func(goalSvc goalcmd.Service) (*callback.CardActionTriggerResponse, error) {
					return goalSvc.CompleteGoalEditAction(action)
				})
			case "goal.replace.confirm":
				return completeGoalRenderedActionAsync(s.app, action, sessionKey, "正在替换 goal", func(goalSvc goalcmd.Service) (*callback.CardActionTriggerResponse, error) {
					return goalSvc.CompleteGoalReplaceConfirm(action)
				})
			case "goal.replace.cancel":
				return completeGoalRenderedActionAsync(s.app, action, sessionKey, "正在保留当前 goal", func(goalSvc goalcmd.Service) (*callback.CardActionTriggerResponse, error) {
					return goalSvc.CompleteGoalReplaceCancel(action)
				})
			case "goal.edit.submit":
				return completeGoalRenderedActionAsync(s.app, action, sessionKey, "正在保存 goal", func(goalSvc goalcmd.Service) (*callback.CardActionTriggerResponse, error) {
					return goalSvc.CompleteGoalEditSubmit(action)
				})
			default:
				return nil, nil
			}
		},
	}
	bindings["menu.compact"] = featureBinding{
		Commands: map[string]featureCommandBinding{
			"compact": {
				Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
					return commandCompact(a.bindings.BackendActions, a.bindings.Compaction, msg, args)
				},
			},
		},
		HandleAction: func(actionName string, s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			if actionName != "menu.compact" {
				return nil, nil
			}
			return s.completeMenuCompact(action, actionSessionKey(action))
		},
	}
	bindings["menu.download"] = featureBinding{
		Commands: map[string]featureCommandBinding{
			"download": {
				Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
					return debugviewcmd.CommandDownload(DebugViewDependencies(a), msg, args)
				},
			},
		},
		HandleAction: func(actionName string, s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			if actionName != "menu.download" {
				return nil, nil
			}
			return debugviewcmd.CompleteMenuDownload(DebugViewDependencies(s.app), action, actionSessionKey(action))
		},
	}
	bindings["menu.history"] = featureBinding{
		Commands: map[string]featureCommandBinding{
			"history": {
				Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
					return a.bindings.History.CommandHistory(msg, args)
				},
			},
		},
		HandleAction: func(actionName string, s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			sessionKey := actionSessionKey(action)
			switch actionName {
			case "menu.history":
				return s.completeMenuHistory(action, sessionKey)
			default:
				return nil, nil
			}
		},
	}
	bindings["menu.skills"] = featureBinding{
		Commands: map[string]featureCommandBinding{
			"skills": {
				Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
					return a.bindings.SkillCommands.CommandSkills(msg, args)
				},
			},
		},
		RenderActions: []string{"menu.skills"},
		Render: func(actionName string, a *App, sessionKey string) (map[string]any, bool) {
			if actionName != "menu.skills" {
				return nil, false
			}
			card, err := a.bindings.SkillCommands.RenderSkillsCard(sessionKey, false)
			if err != nil {
				return nil, false
			}
			return card, true
		},
		HandleAction: func(actionName string, s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			sessionKey := actionSessionKey(action)
			switch actionName {
			case "menu.skills":
				if !menuActionVisibleForBackend(actionName, s.app.configView().configuredBackend()) {
					return completeMenuCommand(s.app, action, sessionKey, "/skills", "menu.tools")
				}
				return s.app.bindings.SkillCommands.CompleteSkillsOpen(action, sessionKey)
			case "skills.select":
				return s.app.bindings.SkillCommands.CompleteSkillsSelect(action, sessionKey, action.Option)
			case "skills.reload":
				return s.app.bindings.SkillCommands.CompleteSkillsReload(action, sessionKey)
			default:
				return nil, nil
			}
		},
	}
	bindings["menu.usage"] = featureBinding{
		Commands: map[string]featureCommandBinding{
			"usage": {
				Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
					return a.bindings.Usage.CommandUsage(msg, args)
				},
			},
		},
		HandleAction: func(actionName string, s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			if actionName != "menu.usage" {
				return nil, nil
			}
			return s.completeMenuUsage(action, actionSessionKey(action))
		},
	}
}
