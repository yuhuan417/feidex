package codexrpc
import "feidex/internal/domain/modelconfig"
type CollaborationModeListResponse = modelconfig.CollaborationModeListResponse
type CollaborationModeMask = modelconfig.CollaborationModeMask
type CollaborationMode struct {
	Mode     string                    `json:"mode"`
	Settings CollaborationModeSettings `json:"settings"`
}

type CollaborationModeSettings struct {
	DeveloperInstructions *string `json:"developer_instructions"`
	Model                 string  `json:"model"`
	ReasoningEffort       *string `json:"reasoning_effort"`
}
