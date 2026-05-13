package app

import (
	"feidex/internal/app/debugviewcmd"
	appreviewcmd "feidex/internal/app/reviewcmd"
	"feidex/internal/config"
	"feidex/internal/feishu"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

func appendFeatureBindingsTools(bindings map[string]featureBinding) {
	bindings["menu.review"] = featureBinding{
		Commands: map[string]featureCommandBinding{
			"review": {
				Match: matchReviewCommand,
				Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
					return appreviewcmd.CommandReview(newReviewAppAdapter(a), msg, args)
				},
				Backends: map[string]func(fields []string) bool{
					backendClaude: nil,
				},
			},
		},
		RenderActions: []string{"menu.review"},
		Render: func(actionName string, a *App, sessionKey string) (map[string]any, bool) {
			if actionName != "menu.review" {
				return nil, false
			}
			return newReviewFormServiceInner(a).RenderReviewMenuCard(sessionKey), true
		},
		HandleAction: func(actionName string, s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			sessionKey := actionSessionKey(action)
			switch actionName {
			case "menu.review":
				return newMenuActionService(s.app).completeMenuReview(action, sessionKey)
			case "menu.review.uncommitted":
				return appreviewcmd.CompleteMenuReviewUncommitted(newReviewAppAdapter(s.app), action, sessionKey)
			case "menu.review.base":
				return appreviewcmd.CompleteMenuReviewBase(newReviewAppAdapter(s.app), action, sessionKey)
			case "menu.review.commit":
				return appreviewcmd.CompleteMenuReviewCommit(newReviewAppAdapter(s.app), action, sessionKey)
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
				Match: func(fields []string) bool {
					return exactOrSingleArgCommand(fields, "config", "verbose", "progress", "normal", "final")
				},
				Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
					return commandQuiet(a, msg, args)
				},
			},
		},
		HandleAction: func(actionName string, s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			switch actionName {
			case "menu.quiet":
				return newMenuActionService(s.app).completeMenuQuiet(action, actionSessionKey(action))
			case "quiet.set":
				return newMenuActionService(s.app).completeQuietSet(action, config.QuietMode(actionStringValue(action, "mode")))
			default:
				return nil, nil
			}
		},
	}
	bindings["plan"] = featureBinding{
		Commands: map[string]featureCommandBinding{
			"plan": {
				Match: func(fields []string) bool {
					return exactOrSingleArgCommand(fields, "on", "off")
				},
				Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
					return commandPlan(a, msg, args)
				},
				Backends: map[string]func(fields []string) bool{
					backendClaude: nil,
				},
			},
		},
		HandleAction: func(actionName string, s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			if actionName != "menu.plan" {
				return nil, nil
			}
			return newMenuActionService(s.app).completeMenuPlan(action, actionSessionKey(action))
		},
	}
	bindings["menu.compact"] = featureBinding{
		Commands: map[string]featureCommandBinding{
			"compact": {
				Match: exactCommand,
				Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
					return commandCompact(a, msg, args)
				},
			},
		},
		HandleAction: func(actionName string, s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			if actionName != "menu.compact" {
				return nil, nil
			}
			return newMenuActionService(s.app).completeMenuCompact(action, actionSessionKey(action))
		},
	}
	bindings["menu.download"] = featureBinding{
		Commands: map[string]featureCommandBinding{
			"download": {
				Match: exactCommand,
				Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
					return debugviewcmd.CommandDownload(newDebugViewAppAdapter(a), msg, args)
				},
			},
		},
		HandleAction: func(actionName string, s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			if actionName != "menu.download" {
				return nil, nil
			}
			return debugviewcmd.CompleteMenuDownload(newDebugViewAppAdapter(s.app), action, actionSessionKey(action))
		},
	}
	bindings["menu.history"] = featureBinding{
		Commands: map[string]featureCommandBinding{
			"history": {
				Match: matchHistoryCommand,
				Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
					return newHistoryServiceInner(a).CommandHistory(msg, args)
				},
			},
		},
		HandleAction: func(actionName string, s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			sessionKey := actionSessionKey(action)
			switch actionName {
			case "menu.history":
				return newMenuActionService(s.app).completeMenuHistory(action, sessionKey)
			case "history.page":
				return newMenuActionService(s.app).completeHistoryPage(action, sessionKey, actionIntValue(action, "page"))
			case "history.detail":
				return newMenuActionService(s.app).completeHistoryDetail(action, sessionKey, actionIntValue(action, "index"))
			case "history.detail.select":
				errResp, index, ok := actionIndexOption(action, "未收到有效 turn 选项")
				if !ok {
					return errResp, nil
				}
				return newMenuActionService(s.app).completeHistoryDetail(action, sessionKey, index)
			default:
				return nil, nil
			}
		},
	}
	bindings["menu.skills"] = featureBinding{
		Commands: map[string]featureCommandBinding{
			"skills": {
				Match: matchSkillsCommand,
				Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
					return newSkillsService(a).CommandSkills(msg, args)
				},
				Backends: map[string]func(fields []string) bool{
					backendClaude: nil,
				},
			},
		},
		RenderActions: []string{"menu.skills"},
		Render: func(actionName string, a *App, sessionKey string) (map[string]any, bool) {
			if actionName != "menu.skills" {
				return nil, false
			}
			card, err := newSkillsService(a).RenderSkillsCard(sessionKey, false)
			if err != nil {
				return nil, false
			}
			return card, true
		},
		HandleAction: func(actionName string, s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			sessionKey := actionSessionKey(action)
			switch actionName {
			case "menu.skills":
				return newMenuActionService(s.app).completeMenuSkills(action, sessionKey)
			case "skills.select":
				return newSkillsService(s.app).CompleteSkillsSelect(action, sessionKey, action.Option)
			case "skills.reload":
				return newSkillsService(s.app).CompleteSkillsReload(action, sessionKey)
			default:
				return nil, nil
			}
		},
	}
	bindings["menu.usage"] = featureBinding{
		Commands: map[string]featureCommandBinding{
			"usage": {
				Match: exactCommand,
				Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
					return newUsageServiceInner(a).CommandUsage(msg, args)
				},
			},
		},
		HandleAction: func(actionName string, s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			if actionName != "menu.usage" {
				return nil, nil
			}
			return newMenuActionService(s.app).completeMenuUsage(action, actionSessionKey(action))
		},
	}
}
