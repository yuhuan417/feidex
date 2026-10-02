package conversation

type MessageLink struct {
	FrontendID   string `json:"frontend_id,omitempty"`
	Backend      string `json:"backend,omitempty"`
	MessageID    string `json:"message_id"`
	SessionKey   string `json:"session_key,omitempty"`
	SubmissionID string `json:"submission_id,omitempty"`
	ThreadID     string `json:"thread_id,omitempty"`
	TurnID       string `json:"turn_id,omitempty"`
}
