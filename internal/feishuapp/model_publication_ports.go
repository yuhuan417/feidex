package feishuapp

type modelDefaultsPublisher struct{ app *App }

func ModelDefaultsPublisher(a *App) interface{ PublishDefaults(string) } {
	return modelDefaultsPublisher{app: a}
}

func (p modelDefaultsPublisher) PublishDefaults(backend string) {
	if backend != "claude" {
		return
	}
	if core := p.app.runtimeView().currentClaudeCore(); core != nil {
		p.app.ConfigMu().RLock()
		cfg := p.app.Config().Claude
		p.app.ConfigMu().RUnlock()
		core.UpdateConfig(cfg)
	}
}
