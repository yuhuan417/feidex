package modelconfig

type CollaborationModeListResponse struct {
	Data []CollaborationModeMask `json:"data"`
}

type CollaborationModeMask struct {
	Name            string  `json:"name"`
	Mode            *string `json:"mode"`
	Model           *string `json:"model"`
	ReasoningEffort *string `json:"reasoning_effort"`
}
