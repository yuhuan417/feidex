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

// MessageRouteInput contains the facts an inbound adapter has already
// resolved for a message. The application owns the ordering of these facts;
// Feishu handlers only perform the selected effect.
type MessageRouteInput struct {
	ExpandedMergeForward bool
	TextEmpty            bool
	HasAttachments       bool
	StartsCommand        bool
	LocalCommand         bool
	PendingServerText    bool
	PendingRootText      bool
	StageImages          bool
	ReplyLink            bool
}

type MessageRoute uint8

const (
	MessageRouteNoop MessageRoute = iota
	MessageRoutePendingServerText
	MessageRoutePendingRootText
	MessageRouteLocalCommand
	MessageRouteStageImages
	MessageRouteSteerOrQueue
)

// ClassifyMessageRoute centralizes the inbound message precedence rules.
// Pending answers and local commands must win over normal submission; image
// staging happens before empty-message filtering; a reply link is eligible
// for steer and otherwise falls through to the normal queue.
func ClassifyMessageRoute(input MessageRouteInput) MessageRoute {
	if !input.ExpandedMergeForward && !input.StartsCommand && !input.HasAttachments {
		if input.PendingServerText {
			return MessageRoutePendingServerText
		}
		if input.PendingRootText {
			return MessageRoutePendingRootText
		}
	}
	if !input.ExpandedMergeForward && input.StartsCommand && input.LocalCommand {
		return MessageRouteLocalCommand
	}
	if input.StageImages {
		return MessageRouteStageImages
	}
	if input.TextEmpty && !input.HasAttachments {
		return MessageRouteNoop
	}
	return MessageRouteSteerOrQueue
}
