package routing

type FrontendCardNotification struct {
	Kind        string `json:"kind,omitempty"`
	CollapseKey string `json:"collapse_key,omitempty"`
	Title       string `json:"title"`
	Color       string `json:"color,omitempty"`
	Body        string `json:"body"`
	CreatedAt   int64  `json:"created_at,omitempty"`
}
