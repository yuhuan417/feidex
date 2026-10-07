package feishuapp

func appendFeatureBindingsThreadWorkspace(bindings map[string]featureBinding, inputs FeatureRegistryInputs) {
	bindings["menu.interrupt"] = featureBinding{
		Commands:    map[string]featureCommandBinding{"interrupt": inputs.command("interrupt")},
		PortActions: []string{"menu.interrupt"},
	}
	bindings["menu.thread"] = featureBinding{
		Commands: map[string]featureCommandBinding{
			"fork":    inputs.command("fork"),
			"new":     inputs.command("new"),
			"thread":  inputs.command("thread"),
			"session": inputs.command("session"),
			"threads": inputs.command("threads"),
		},
		RenderActions: []string{"menu.thread"},
		Render:        inputs.render("menu.thread"),
		PortActions:   []string{"menu.thread", "thread.new.start", "thread.fork.start"},
	}
	bindings["menu.workspace"] = featureBinding{
		Commands:      map[string]featureCommandBinding{"workspace": inputs.command("workspace")},
		RenderActions: []string{"menu.workspace"},
		Render:        inputs.render("menu.workspace"),
		PortActions:   []string{"menu.workspace"},
	}
	bindings["menu.model"] = featureBinding{
		PortActions: []string{"menu.model", "menu.model_auxiliary", "model.config.set_model", "model.config.select_model", "model.config.add_option", "model.config.remove_option", "model.config.set_effort", "model.config.select_effort", "model.plan_config.set_model", "model.plan_config.select_model", "model.plan_config.set_effort", "model.plan_config.select_effort", "model.aux_config.select_review_model", "model.aux_config.select_plan_model", "model.aux_config.select_plan_effort", "model.aux_config.select_subagent_model", "model.aux_config.select_subagent_effort", "model.aux_config.select_small_model"},
		Commands: map[string]featureCommandBinding{
			"model":  inputs.command("model"),
			"effort": inputs.command("effort"),
		},
	}
	bindings["menu.fast"] = featureBinding{
		PortActions: []string{"menu.fast", "service_tier.set"},
		Commands:    map[string]featureCommandBinding{"fast": inputs.command("fast")},
	}
}
