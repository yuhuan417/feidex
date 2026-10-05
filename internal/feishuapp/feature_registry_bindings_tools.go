package feishuapp

func appendFeatureBindingsTools(bindings map[string]featureBinding, inputs FeatureRegistryInputs) {
	bindings["menu.review"] = featureBinding{
		Commands:      map[string]featureCommandBinding{"review": inputs.command("review")},
		RenderActions: []string{"menu.review"},
		Render:        inputs.render("menu.review"),
		PortActions:   []string{"menu.review", "menu.review.uncommitted", "menu.review.base", "menu.review.commit", "menu.review.custom"},
	}
	bindings["menu.quiet"] = featureBinding{
		Commands:    map[string]featureCommandBinding{"quiet": inputs.command("quiet")},
		PortActions: []string{"menu.quiet", "quiet.set"},
	}
	bindings["plan"] = featureBinding{
		Commands:    map[string]featureCommandBinding{"plan": inputs.command("plan")},
		PortActions: []string{"menu.plan"},
	}
	bindings["goal"] = featureBinding{
		Commands:    map[string]featureCommandBinding{"goal": inputs.command("goal")},
		PortActions: []string{"menu.goal", "goal.pause", "goal.resume", "goal.clear", "goal.edit", "goal.replace.confirm", "goal.replace.cancel", "goal.edit.submit"},
	}
	bindings["menu.compact"] = featureBinding{
		Commands:    map[string]featureCommandBinding{"compact": inputs.command("compact")},
		PortActions: []string{"menu.compact"},
	}
	bindings["menu.download"] = featureBinding{
		Commands:    map[string]featureCommandBinding{"download": inputs.command("download")},
		PortActions: []string{"menu.download"},
	}
	bindings["menu.history"] = featureBinding{
		Commands:    map[string]featureCommandBinding{"history": inputs.command("history")},
		PortActions: []string{"menu.history", "history.page", "history.detail", "history.detail.select"},
	}
	bindings["menu.skills"] = featureBinding{
		Commands:      map[string]featureCommandBinding{"skills": inputs.command("skills")},
		RenderActions: []string{"menu.skills"},
		Render:        inputs.render("menu.skills"),
		PortActions:   []string{"menu.skills", "skills.select", "skills.reload"},
	}
	bindings["menu.usage"] = featureBinding{
		Commands:    map[string]featureCommandBinding{"usage": inputs.command("usage")},
		PortActions: []string{"menu.usage"},
	}
}
