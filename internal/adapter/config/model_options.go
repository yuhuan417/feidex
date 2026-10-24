package config

import (
	"strings"

	fileconfig "feidex/internal/config"
)

// ModelOptionsRepository persists the candidate-model picker list for the
// active backend. Backend is resolved before the config lock is taken — the
// usual Backend closures read that same lock, so calling them inside the
// update mutation would self-deadlock — so a frontend that switched between
// Codex and Claude keeps writing to the list its cards show. A nil Backend
// function keeps the historical Claude-only behavior.
type ModelOptionsRepository struct {
	Source  Source
	Backend func() string
}

func (r ModelOptionsRepository) UpdateModelOptions(mutate func([]string) ([]string, error)) error {
	backend := r.activeBackend()
	return NewDocumentRepository(r.Source).UpdateConfig(func(cfg *fileconfig.Config) error {
		options, err := mutate(append([]string(nil), optionsForBackend(backend, cfg)...))
		if err == nil {
			setOptionsForBackend(backend, cfg, options)
		}
		return err
	})
}

func (r ModelOptionsRepository) activeBackend() string {
	if r.Backend == nil {
		return fileconfig.RuntimeBackendClaude
	}
	return strings.TrimSpace(r.Backend())
}

func optionsForBackend(backend string, cfg *fileconfig.Config) []string {
	if backend == fileconfig.RuntimeBackendCodex {
		return cfg.Codex.ModelOptions
	}
	return cfg.Claude.ModelOptions
}

func setOptionsForBackend(backend string, cfg *fileconfig.Config, options []string) {
	if backend == fileconfig.RuntimeBackendCodex {
		cfg.Codex.ModelOptions = options
		return
	}
	cfg.Claude.ModelOptions = options
}
