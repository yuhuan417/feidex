package feishuapp

import (
	"feidex/internal/adapter/feishu/debugviewcmd"
	"feidex/internal/adapter/feishu/planmode"
	appreviewcmd "feidex/internal/adapter/feishu/reviewcmd"
	"feidex/internal/feishu"
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
		PortActions: []string{"menu.review", "menu.review.uncommitted", "menu.review.base", "menu.review.commit", "menu.review.custom"},
	}
	bindings["menu.quiet"] = featureBinding{
		Commands: map[string]featureCommandBinding{
			"quiet": {
				Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
					return commandQuiet(a, msg, args)
				},
			},
		},
		PortActions: []string{"menu.quiet", "quiet.set"},
	}
	bindings["plan"] = featureBinding{
		Commands: map[string]featureCommandBinding{
			"plan": {
				Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
					return planmode.CommandPlan(newPlanModeAppAdapter(a), msg, args)
				},
			},
		},
		PortActions: []string{"menu.plan"},
	}
	bindings["goal"] = featureBinding{
		Commands: map[string]featureCommandBinding{
			"goal": {
				HandleRaw: func(a *App, msg *feishu.InboundMessage, raw string, args []string) error {
					return commandGoalRaw(a.bindings.GoalCommands, msg, raw, args)
				},
			},
		},
		PortActions: []string{"menu.goal", "goal.pause", "goal.resume", "goal.clear", "goal.edit", "goal.replace.confirm", "goal.replace.cancel", "goal.edit.submit"},
	}
	bindings["menu.compact"] = featureBinding{
		Commands: map[string]featureCommandBinding{
			"compact": {
				Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
					return commandCompact(a.bindings.BackendActions, a.bindings.Compaction, msg, args)
				},
			},
		},
		PortActions: []string{"menu.compact"},
	}
	bindings["menu.download"] = featureBinding{
		Commands: map[string]featureCommandBinding{
			"download": {
				Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
					return debugviewcmd.CommandDownload(DebugViewDependencies(a), msg, args)
				},
			},
		},
		PortActions: []string{"menu.download"},
	}
	bindings["menu.history"] = featureBinding{
		Commands: map[string]featureCommandBinding{
			"history": {
				Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
					return a.bindings.History.CommandHistory(msg, args)
				},
			},
		},
		PortActions: []string{"menu.history", "history.page", "history.detail", "history.detail.select"},
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
		PortActions: []string{"menu.skills", "skills.select", "skills.reload"},
	}
	bindings["menu.usage"] = featureBinding{
		Commands: map[string]featureCommandBinding{
			"usage": {
				Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
					return a.bindings.Usage.CommandUsage(msg, args)
				},
			},
		},
		PortActions: []string{"menu.usage"},
	}
}
