package routing

type GroupAnnouncementBlock struct {
	ID              string `json:"id"`
	FrontendID      string `json:"frontend_id"`
	ChatID          string `json:"chat_id"`
	ChatType        string `json:"chat_type"`
	BotOpenID       string `json:"bot_open_id,omitempty"`
	BlockID         string `json:"block_id,omitempty"`
	Marker          string `json:"marker,omitempty"`
	LastContentHash string `json:"last_content_hash,omitempty"`
	LastUpdatedAt   int64  `json:"last_updated_at,omitempty"`
	CreatedAt       int64  `json:"created_at"`
	UpdatedAt       int64  `json:"updated_at"`
	BotAbsent       bool   `json:"bot_absent,omitempty"`
}
