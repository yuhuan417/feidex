package backend

import "strings"

const (
	BackendCodex  = "codex"
	BackendClaude = "claude"
)

type SessionInflightMode string

const (
	SessionInflightSingle     SessionInflightMode = "single"
	SessionInflightSerialized SessionInflightMode = "serialized"
	SessionInflightParallel   SessionInflightMode = "parallel"
)

func NormalizeBackend(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "":
		return ""
	case BackendClaude:
		return BackendClaude
	case BackendCodex:
		return BackendCodex
	default:
		return ""
	}
}

func SessionInflightModeForBackend(string) SessionInflightMode {
	return SessionInflightSingle
}

func SessionInflightAllowsAdditional(mode SessionInflightMode) bool {
	return mode == SessionInflightSerialized || mode == SessionInflightParallel
}
