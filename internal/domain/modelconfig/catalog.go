package modelconfig

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
