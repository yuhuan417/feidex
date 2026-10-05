package feishuapp

func appendFeatureBindingsBinding(bindings map[string]featureBinding, inputs FeatureRegistryInputs) {
	bindings["menu.current_bot"] = featureBinding{
		Commands:      map[string]featureCommandBinding{"primary": inputs.command("primary")},
		RenderActions: []string{"menu.current_bot"},
		Render:        inputs.render("menu.current_bot"),
		PortActions:   []string{"menu.current_bot"},
	}
	bindings["menu.current_workspace"] = featureBinding{
		RenderActions: []string{"menu.current_workspace"},
		Render:        inputs.render("menu.current_workspace"),
		PortActions:   []string{"menu.current_workspace", "current_workspace.choose", "current_workspace.use"},
	}
}
