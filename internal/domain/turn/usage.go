package turn

type TokenUsageBreakdown struct {
	TotalTokens           int64 `json:"totalTokens"`
	InputTokens           int64 `json:"inputTokens"`
	CachedInputTokens     int64 `json:"cachedInputTokens"`
	OutputTokens          int64 `json:"outputTokens"`
	ReasoningOutputTokens int64 `json:"reasoningOutputTokens"`
}

type ThreadTokenUsage struct {
	Total              TokenUsageBreakdown `json:"total"`
	Last               TokenUsageBreakdown `json:"last"`
	ModelContextWindow *int64              `json:"modelContextWindow"`
}

// ClaudeThreadUsage is the backend-neutral usage snapshot emitted by the
// Claude adapter. Concrete CLI event types must not cross into presentation
// or application command packages.
type ClaudeThreadUsage struct {
	InputTokens, OutputTokens                                int
	CacheReadTokens, CacheCreationTokens                     int
	CumulativeInputTokens, CumulativeOutputTokens            int
	CumulativeCacheReadTokens, CumulativeCacheCreationTokens int
	HasCumulativeUsage                                       bool
	ContextWindow                                            int
	CostUSD                                                  float64
}
