package codex

import (
	"feidex/internal/codexrpc"
	domainturn "feidex/internal/domain/turn"
)

func ThreadUsage(usage codexrpc.ThreadTokenUsage) domainturn.ThreadTokenUsage {
	return domainturn.ThreadTokenUsage{Total: domainturn.TokenUsageBreakdown(usage.Total), Last: domainturn.TokenUsageBreakdown(usage.Last), ModelContextWindow: cloneContextWindow(usage.ModelContextWindow)}
}

// ProtocolThreadUsage serves the remaining transport-facing callers during
// migration. The runtime itself stores only neutral domain usage values.
func ProtocolThreadUsage(usage domainturn.ThreadTokenUsage) codexrpc.ThreadTokenUsage {
	return codexrpc.ThreadTokenUsage{Total: codexrpc.TokenUsageBreakdown(usage.Total), Last: codexrpc.TokenUsageBreakdown(usage.Last), ModelContextWindow: cloneContextWindow(usage.ModelContextWindow)}
}

func cloneContextWindow(src *int64) *int64 {
	if src == nil {
		return nil
	}
	value := *src
	return &value
}
