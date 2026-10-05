package feishuapp

func appendFeatureBindingsSystem(bindings map[string]featureBinding, inputs FeatureRegistryInputs) {
	bindings["menu.debug"] = featureBinding{
		Commands:      map[string]featureCommandBinding{"debug": inputs.command("debug")},
		RenderActions: []string{"menu.debug.logs"},
		Render:        inputs.render("menu.debug.logs"),
		PortActions:   []string{"menu.debug", "menu.debug.logs"},
	}
	bindings["menu.status"] = featureBinding{
		Commands:    map[string]featureCommandBinding{"status": inputs.command("status")},
		PortActions: []string{"menu.status"},
	}
	bindings["menu.help"] = featureBinding{
		Commands:    map[string]featureCommandBinding{"help": inputs.command("help")},
		PortActions: []string{"menu.help"},
	}
	bindings["menu.codex_upgrade"] = featureBinding{
		Commands:      map[string]featureCommandBinding{"codex": inputs.command("codex")},
		RenderActions: []string{"menu.codex_upgrade"},
		Render:        inputs.render("menu.codex_upgrade"),
		PortActions:   []string{"menu.codex_upgrade"},
	}
	bindings["menu.claude_upgrade"] = featureBinding{
		Commands:      map[string]featureCommandBinding{"claude": inputs.command("claude")},
		RenderActions: []string{"menu.claude_upgrade"},
		Render:        inputs.render("menu.claude_upgrade"),
		PortActions:   []string{"menu.claude_upgrade"},
	}
	bindings["menu.upgrade"] = featureBinding{
		Commands:    map[string]featureCommandBinding{"upgrade": inputs.command("upgrade")},
		PortActions: []string{"menu.upgrade"},
	}
}
