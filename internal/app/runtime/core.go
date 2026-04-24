package runtime

import (
	"strings"

	"feidex/internal/config"
)

const (
	BackendCodex  = config.RuntimeBackendCodex
	BackendClaude = config.RuntimeBackendClaude
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
