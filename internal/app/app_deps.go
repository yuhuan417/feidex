package app

import (
	appconvbackend "feidex/internal/app/convbackend"
	"feidex/internal/app/appstate"
	"feidex/internal/config"
	"feidex/internal/state"
)

// AppDeps is the central dependency interface for the app package. It exposes
// the subset of *App capabilities that domain packages need. *App naturally
// satisfies this interface via its accessor methods.
//
// Domain packages define narrow sub-interfaces (e.g. turn.Deps, backend.Deps)
// that declare only the methods they actually use. Go's structural typing
// guarantees *App satisfies all such sub-interfaces without explicit
// declarations.
//
// Cross-domain calls are glued through the app layer: domain packages never
// import each other. Instead, app/ constructs each domain service with *App
// (which satisfies the domain's Deps interface) and the domain calls back
// into app-level operations through that interface.
type AppDeps interface {
	// State access
	State() *appstate.Store
	Store() *state.Store
	Config() *config.Config
	ConfigPath() string
	FrontendID() string
	Backend() string

	// Feishu
	Feishu() FeishuClient

	// Runtime clients
	Claude() ClaudeCore
	Codex() CodexClient

	// Facades
	BackendRuntime() backendRuntimeFacade
	ConversationBackend() appconvbackend.ConversationBackendFacade

	// Trackers
	Trackers() *appTrackers

	// Self-reference for backward compatibility with BackendDeps consumers.
	App() *App
}
