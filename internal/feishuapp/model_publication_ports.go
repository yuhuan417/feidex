package feishuapp

import (
	"sync"

	"feidex/internal/config"
	frontendruntime "feidex/internal/runtime"
)

type modelDefaultsPublisher struct {
	runtimeOwner *frontendruntime.FrontendOwner
	cfg          *config.Config
	configMu     *sync.RWMutex
}

func ModelDefaultsPublisher(owner *frontendruntime.FrontendOwner, cfg *config.Config, configMu *sync.RWMutex) interface{ PublishDefaults(string) } {
	return modelDefaultsPublisher{runtimeOwner: owner, cfg: cfg, configMu: configMu}
}

func (p modelDefaultsPublisher) PublishDefaults(backend string) {
	if backend != "claude" {
		return
	}
	if core := (runtimeView{owner: p.runtimeOwner}).currentClaudeCore(); core != nil {
		p.configMu.RLock()
		cfg := p.cfg.Claude
		p.configMu.RUnlock()
		core.UpdateConfig(cfg)
	}
}
