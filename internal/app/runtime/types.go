package runtime

import "time"

// ClaudeUpgradePendingPayload describes a pending Claude self-upgrade.
type ClaudeUpgradePendingPayload struct {
	CurrentVersion string `json:"current_version"`
	TargetVersion  string `json:"target_version"`
	Command        string `json:"command"`
	CommandPath    string `json:"command_path"`
	UpdateCommand  string `json:"update_command"`
}

// CodexUpgradePendingPayload describes a pending Codex self-upgrade.
type CodexUpgradePendingPayload struct {
	CurrentVersion string `json:"current_version"`
	TargetVersion  string `json:"target_version"`
	Command        string `json:"command"`
	CommandPath    string `json:"command_path"`
	UpdateCommand  string `json:"update_command"`
}

// ClaudeHistoryRecord represents a single entry in a Claude session transcript.
type ClaudeHistoryRecord struct {
	EntryID    string
	EntryType  string
	Timestamp  string
	PromptID   string
	MessageID  string
	ParentUUID string
	StopReason string
	Preview    string
	Details    []string
}

// ClaudeHistoryTurnSummary summarizes a Claude turn for history display.
type ClaudeHistoryTurnSummary struct {
	Ordinal   int
	TurnID    string
	Status    string
	Preview   string
	Records   []ClaudeHistoryRecord
	IsCurrent bool
}

// ClaudeSessionListMeta holds metadata for a Claude session list entry.
type ClaudeSessionListMeta struct {
	ID        string
	Cwd       string
	Title     string
	Preview   string
	UpdatedAt time.Time
}

// ClaudeModelOption represents a selectable Claude model choice.
type ClaudeModelOption struct {
	Value string
	Label string
}

// ClaudePermissionModeOption represents a selectable Claude permission mode.
type ClaudePermissionModeOption struct {
	Value string
	Label string
}

// HistoryTurnSummary summarizes a Codex thread turn for history display.
type HistoryTurnSummary struct {
	Ordinal      int
	TurnID       string
	Status       string
	Inputs       []string
	Outputs      []string
	ErrorText    string
	IsCurrent    bool
	InputPreview string
}
