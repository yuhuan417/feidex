package codexrpc

import (
	"feidex/internal/domain/conversation"
	"feidex/internal/domain/modelconfig"
)

type ThreadStartResult struct {
	Thread struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Preview string `json:"preview"`
	} `json:"thread"`
}

type TurnStartResult struct {
	Turn struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	} `json:"turn"`
}

type ReviewStartResult struct {
	ReviewThreadID string `json:"reviewThreadId"`
	Turn           struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	} `json:"turn"`
}

type ThreadListResult struct {
	Data []ThreadListEntry `json:"data"`
}

type ThreadListEntry = conversation.ThreadEntry

type ModelListResult = modelconfig.ModelListResult
type ModelListEntry = modelconfig.ModelListEntry
type ModelReasoningEffortEntry = modelconfig.ModelReasoningEffortEntry
