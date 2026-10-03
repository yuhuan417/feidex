package backend

import "time"

type Key string

const (
	KeyCodex  Key = "codex"
	KeyClaude Key = "claude"
)

type InstallProbe struct {
	Command, CommandPath, RealCommandPath, PackagePath string
	CurrentVersion, UpdateCommand                      string
	Supported                                          bool
	Reason                                             string
}

type UpgradePayload struct {
	CurrentVersion string `json:"current_version"`
	TargetVersion  string `json:"target_version"`
	Command        string `json:"command"`
	CommandPath    string `json:"command_path"`
	UpdateCommand  string `json:"update_command"`
}

type UpgradeSnapshot struct {
	Running                                                       bool
	Phase, Result, Message                                        string
	CurrentVersion, PreviousVersion, TargetVersion, LatestVersion string
	StartedAt, UpdatedAt                                          time.Time
}

type RestartSnapshot struct {
	Running                                bool
	Phase, Result, Message, CurrentVersion string
	StartedAt, UpdatedAt                   time.Time
}
