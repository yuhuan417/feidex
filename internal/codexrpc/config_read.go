package codexrpc

type ConfigReadParams struct {
	IncludeLayers bool    `json:"includeLayers"`
	CWD           *string `json:"cwd,omitempty"`
}

type ConfigReadResponse struct {
	Config ConfigSnapshot `json:"config"`
}

type ConfigSnapshot struct {
	ModelAutoCompactTokenLimit *int64 `json:"model_auto_compact_token_limit"`
	ModelContextWindow         *int64 `json:"model_context_window"`
}
