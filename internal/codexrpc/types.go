package codexrpc

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

type ThreadListResult struct {
	Data []ThreadListEntry `json:"data"`
}

type ThreadListEntry struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Preview   string `json:"preview"`
	CreatedAt int64  `json:"createdAt"`
	UpdatedAt int64  `json:"updatedAt"`
	Source    string `json:"source"`
	Cwd       string `json:"cwd"`
}
