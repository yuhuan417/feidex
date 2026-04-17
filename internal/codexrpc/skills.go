package codexrpc

type SkillsListResult struct {
	Data []SkillsListEntry `json:"data"`
}

type SkillsListEntry struct {
	Cwd    string           `json:"cwd"`
	Skills []SkillMetadata  `json:"skills"`
	Errors []SkillErrorInfo `json:"errors"`
}

type SkillMetadata struct {
	Name             string          `json:"name"`
	Description      string          `json:"description"`
	ShortDescription string          `json:"shortDescription"`
	Interface        *SkillInterface `json:"interface"`
	Path             string          `json:"path"`
	Scope            string          `json:"scope"`
	Enabled          bool            `json:"enabled"`
}

type SkillInterface struct {
	DisplayName      string `json:"displayName"`
	ShortDescription string `json:"shortDescription"`
	DefaultPrompt    string `json:"defaultPrompt"`
}

type SkillErrorInfo struct {
	Path    string `json:"path"`
	Message string `json:"message"`
}
