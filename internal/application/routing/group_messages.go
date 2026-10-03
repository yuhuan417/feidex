package routing

import (
	"feidex/internal/domain/conversation"
	domain "feidex/internal/domain/routing"
	"strings"
)

type GroupMessage struct {
	ChatID, RootMessageID, ParentMessageID, Text string
	MentionedOpenIDs                             []string
	MentionAll, MentionedSelf, MentionedAny      bool
}

type GroupMessages struct {
	Frontend string
	Primary  Service
	Links    interface {
		MessageLink(string) *conversation.MessageLink
	}
	SelfOpenID func() string
}

func (s GroupMessages) Facts(input GroupMessage) domain.GroupMessageInput {
	primary, _ := s.Primary.Lookup(s.Frontend, "group", input.ChatID)
	linked := false
	for _, id := range []string{input.RootMessageID, input.ParentMessageID} {
		if strings.TrimSpace(id) != "" && s.Links.MessageLink(id) != nil {
			linked = true
		}
	}
	return domain.GroupMessageInput{
		MentionAll: input.MentionAll, MentionedAny: input.MentionedAny || len(input.MentionedOpenIDs) > 0,
		MentionedSelf: domain.MentionsSelf(s.SelfOpenID(), input.MentionedOpenIDs, input.MentionedSelf),
		InReplyChain:  strings.TrimSpace(input.RootMessageID) != "" || strings.TrimSpace(input.ParentMessageID) != "",
		HasLocalLink:  linked, HasPrimaryState: primary != nil, IsPrimary: primary != nil && primary.Enabled,
	}
}

func (s GroupMessages) Deliver(input GroupMessage) bool {
	return domain.DeliverGroupMessage(domain.GroupDeliveryInput{GroupMessageInput: s.Facts(input), Text: input.Text, MentionedOpenIDs: input.MentionedOpenIDs})
}

func (s GroupMessages) Accept(input GroupMessage) bool {
	return domain.AcceptGroupMessage(s.Facts(input))
}
func (s GroupMessages) Probe(input GroupMessage) bool {
	return domain.ProbeGroupPrimary(s.Facts(input))
}
