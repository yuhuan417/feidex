package interaction

type PendingRequest struct {
	FrontendID   string `json:"frontend_id,omitempty"`
	ID           string `json:"id"`
	RequestIDRaw string `json:"request_id_raw,omitempty"`
	Backend      string `json:"backend,omitempty"`
	Kind         string `json:"kind"`
	SessionKey   string `json:"session_key"`
	ThreadID     string `json:"thread_id"`
	TurnID       string `json:"turn_id"`
	ItemID       string `json:"item_id"`
	OwnerUserID  string `json:"owner_user_id"`
	FeishuMsgID  string `json:"feishu_msg_id"`
	PayloadJSON  string `json:"payload_json"`
	Status       string `json:"status"`
	CreatedAt    int64  `json:"created_at"`
	ExpiresAt    int64  `json:"expires_at"`
}
