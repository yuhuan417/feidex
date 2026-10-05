package feishuapp

func appendFeatureBindingsMenuCore(bindings map[string]featureBinding, inputs FeatureRegistryInputs) {
	bindings["menu.root"] = featureBinding{
		Commands:      map[string]featureCommandBinding{"menu": inputs.command("menu")},
		RenderActions: []string{"menu.root"},
		Render:        inputs.render("menu.root"),
		PortActions:   []string{"menu.root"},
	}
	bindings["menu.tools"] = featureBinding{
		RenderActions: []string{"menu.tools"},
		Render:        inputs.render("menu.tools"),
		PortActions:   []string{"menu.tools"},
	}
	bindings["menu.group.model"] = featureBinding{
		RenderActions: []string{"menu.group.model"},
		Render:        inputs.render("menu.group.model"),
		PortActions:   []string{"menu.group.model"},
	}
	bindings["menu.group.system"] = featureBinding{
		RenderActions: []string{"menu.group.system"},
		Render:        inputs.render("menu.group.system"),
		PortActions:   []string{"menu.group.system"},
	}
	bindings["menu.group.backend"] = featureBinding{
		Commands:      map[string]featureCommandBinding{"backend": inputs.command("backend")},
		RenderActions: []string{"menu.group.backend"},
		Render:        inputs.render("menu.group.backend"),
		PortActions:   []string{"menu.group.backend", "menu.backend", "menu.backend.switch", "menu.auto_retry", "backend.select", "auto_retry.set"},
	}
}
