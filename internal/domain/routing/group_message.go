package routing

import "strings"

type GroupDeliveryInput struct {
	GroupMessageInput
	Text             string
	MentionedOpenIDs []string
}

func DeliverGroupMessage(input GroupDeliveryInput) bool {
	_, assignment := ParseGroupPrimaryAssignment(input.Text, input.MentionedOpenIDs)
	if !input.MentionAll && (ParsePrimaryOnCommand(input.Text) || ParseEmptyBotMention(input.Text)) {
		return assignment
	}
	return AcceptGroupMessage(input.GroupMessageInput) || assignment || ProbeGroupPrimary(input.GroupMessageInput)
}

func MentionsSelf(self string, mentioned []string, fallback bool) bool {
	self = strings.TrimSpace(self)
	if self == "" || len(mentioned) == 0 {
		return fallback
	}
	for _, id := range mentioned {
		if strings.TrimSpace(id) == self {
			return true
		}
	}
	return false
}

// GroupMessageInput contains the transport-independent facts needed to
// decide whether a group message belongs to this frontend. The caller resolves
// frontend-scoped state such as primary assignment and local message links.
type GroupMessageInput struct {
	MentionAll      bool
	MentionedSelf   bool
	MentionedAny    bool
	InReplyChain    bool
	HasLocalLink    bool
	IsPrimary       bool
	HasPrimaryState bool
}

// AcceptGroupMessage applies the group delivery policy after transport and
// storage details have been resolved by the adapter/application boundary.
func AcceptGroupMessage(input GroupMessageInput) bool {
	if input.MentionAll {
		return true
	}
	if input.MentionedSelf {
		return true
	}
	// An explicit mention of another person or bot must not fall through to the
	// local primary frontend.
	if input.MentionedAny {
		return false
	}
	if input.InReplyChain {
		// A reply is owned only when this frontend has a local message link for
		// the root or parent message.
		return input.HasLocalLink
	}
	return input.IsPrimary
}

// ProbeGroupPrimary reports whether an uninitialized group should trigger a
// primary-state lookup/initialization attempt.
func ProbeGroupPrimary(input GroupMessageInput) bool {
	if input.HasPrimaryState {
		return false
	}
	if input.MentionedSelf {
		return true
	}
	if input.MentionedAny {
		return false
	}
	return !input.InReplyChain
}
