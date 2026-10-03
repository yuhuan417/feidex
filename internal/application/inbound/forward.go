package inbound

import (
	"context"
	"fmt"
	"strings"
	"time"

	"feidex/internal/application"
)

type ForwardGateway interface {
	ResolveForward(context.Context, string, []string) (string, []application.Attachment, error)
}
type ForwardTasks interface{ Run(func()) bool }
type ForwardService struct {
	Gateway ForwardGateway
	Tasks   ForwardTasks
	Context func() context.Context
	Process func(*application.InboundMessage) error
	Queued  func([]string)
	Clear   func([]string)
	Failed  func(*application.InboundMessage, error)
}

func (s ForwardService) Start(msg *application.InboundMessage) {
	if msg == nil || len(msg.MergeForwardMessageIDs) == 0 {
		return
	}
	prepared := *msg
	prepared.Attachments = append([]application.Attachment(nil), msg.Attachments...)
	prepared.MergeForwardMessageIDs = append([]string(nil), msg.MergeForwardMessageIDs...)
	ids := []string{msg.MessageID}
	fail := func(err error) { s.Clear(ids); s.Failed(&prepared, err) }
	s.Queued(ids)
	if !s.Tasks.Run(func() {
		ctx, cancel := context.WithTimeout(s.Context(), 30*time.Second)
		defer cancel()
		text, attachments, err := s.Gateway.ResolveForward(ctx, prepared.MessageID, prepared.MergeForwardMessageIDs)
		if err != nil {
			fail(fmt.Errorf("合并转发预取失败: %w", err))
			return
		}
		prepared.Text, prepared.Attachments = text, attachments
		prepared.MergeForwardMessageIDs, prepared.ExpandedMergeForward = nil, true
		if strings.TrimSpace(text) == "" && len(attachments) == 0 {
			fail(fmt.Errorf("合并转发预取失败: empty expanded content"))
			return
		}
		if err := s.Process(&prepared); err != nil {
			fail(err)
		}
	}) {
		fail(fmt.Errorf("frontend is stopping"))
	}
}
