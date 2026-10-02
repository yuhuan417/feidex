package codexrpc

import "feidex/internal/domain/conversation"

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

type ModelListResult struct {
	Data []ModelListEntry `json:"data"`
}

type ModelListEntry struct {
	ID                        string                      `json:"id"`
	Model                     string                      `json:"model"`
	DisplayName               string                      `json:"displayName"`
	Description               string                      `json:"description"`
	DefaultReasoningEffort    string                      `json:"defaultReasoningEffort"`
	SupportedReasoningEfforts []ModelReasoningEffortEntry `json:"supportedReasoningEfforts"`
	IsDefault                 bool                        `json:"isDefault"`
}

type ModelReasoningEffortEntry struct {
	ReasoningEffort string `json:"reasoningEffort"`
	Description     string `json:"description"`
}
