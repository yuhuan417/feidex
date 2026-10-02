package application

type InboundMessage struct {
	SessionKey             string
	MessageID              string
	ChatID                 string
	ChatType               string
	UserID                 string
	UserName               string
	ChatName               string
	Text                   string
	RootMessageID          string
	ParentMessageID        string
	ThreadID               string
	Attachments            []Attachment
	MergeForwardMessageIDs []string
	ExpandedMergeForward   bool
	MentionedOpenIDs       []string
	MentionedAny           bool
	MentionedSelf          bool
	// MentionAll marks "@所有人".
	//
	// Measured against a live tenant: the platform does NOT put it in
	// mentions[] — that array arrives empty — and leaves the placeholder
	// "@_all" in the plain text instead. So this cannot be derived from
	// MentionedAny or MentionedOpenIDs, both of which stay empty/false; it has
	// to be read off the text. Group routing treats it separately: every bot
	// answers it.
	MentionAll bool
	CreatedAt  int64
}

type Attachment struct {
	Kind            string
	ResourceKey     string
	SourceMessageID string
}
