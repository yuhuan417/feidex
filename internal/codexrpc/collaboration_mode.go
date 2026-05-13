package codexrpc

type CollaborationModeListResponse struct {
	Data []CollaborationModeMask `json:"data"`
}

type CollaborationModeMask struct {
	Name            string  `json:"name"`
	Mode            *string `json:"mode"`
	Model           *string `json:"model"`
	ReasoningEffort *string `json:"reasoning_effort"`
}

type CollaborationMode struct {
	Mode     string                    `json:"mode"`
	Settings CollaborationModeSettings `json:"settings"`
}

type CollaborationModeSettings struct {
	DeveloperInstructions *string `json:"developer_instructions"`
	Model                 string  `json:"model"`
	ReasoningEffort       *string `json:"reasoning_effort"`
}
