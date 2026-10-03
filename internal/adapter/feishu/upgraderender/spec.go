package upgraderender

import "feidex/internal/install"

// Spec holds everything that differs between the Claude and Codex upgrade
// surfaces. Every renderer in this package takes one, so the two backends
// share a single implementation.
type Spec struct {
	// Name is the capitalized backend name shown in card titles and bodies.
	Name string
	// InstallSource is the label shown for the detected installation.
	InstallSource string
	// DefaultCommand is the CLI command name used when the probe is empty.
	DefaultCommand string
	// SmokeTestLabel describes how an upgrade is verified.
	SmokeTestLabel string
	// MenuAction is the breadcrumb action used for the card body.
	MenuAction string
	// ActionPrefix is the card-action namespace, e.g. "claude_upgrade".
	ActionPrefix string
	// RestartAction is the card action that restarts the runtime in place.
	RestartAction string
}

// ClaudeSpec describes the Claude upgrade surface.
var ClaudeSpec = Spec{
	Name:           "Claude",
	InstallSource:  "Claude CLI",
	DefaultCommand: "claude",
	SmokeTestLabel: "start + init",
	MenuAction:     "menu.claude_upgrade",
	ActionPrefix:   "claude_upgrade",
	RestartAction:  "claude_restart.run",
}

// CodexSpec describes the Codex upgrade surface.
var CodexSpec = Spec{
	Name:           "Codex",
	InstallSource:  "Codex CLI",
	DefaultCommand: "codex",
	SmokeTestLabel: "initialize + model/list",
	MenuAction:     "menu.codex_upgrade",
	ActionPrefix:   "codex_upgrade",
	RestartAction:  "codex_restart.run",
}

// UpgradeView is the backend-agnostic snapshot a status card is rendered from.
type UpgradeView struct {
	Probe         install.Probe
	LatestVersion string
	LatestError   string
	BusyReason    string
	Snapshot      BackendUpgradeSnapshot
	Restart       BackendRestartSnapshot
}
