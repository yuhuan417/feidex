package app

import (
	"feidex/internal/config"
	"feidex/internal/state"
)

// Feishu returns the Feishu client. Sub-packages should define narrow
// interfaces for the methods they need rather than depending on this type.
func (a *App) Feishu() feishuClient {
	if a == nil {
		return nil
	}
	return a.feishu
}

// Config returns the application configuration.
func (a *App) Config() *config.Config {
	if a == nil {
		return nil
	}
	return a.cfg
}

// Store returns the state store.
func (a *App) Store() *state.Store {
	if a == nil {
		return nil
	}
	return a.store
}

// Backend returns the name of the currently active backend.
func (a *App) Backend() string {
	if a == nil {
		return ""
	}
	return a.backend
}

// Claude returns the Claude core client.
func (a *App) Claude() claudeCore {
	if a == nil {
		return nil
	}
	return a.claude
}

// Codex returns the Codex client.
func (a *App) Codex() codexClient {
	if a == nil {
		return nil
	}
	return a.codex
}
