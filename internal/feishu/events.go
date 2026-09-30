package feishu

import (
	"context"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"
	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

// eventRegistration pairs a Feishu event type with the handler that processes
// it. The table in eventRegistrations is the single source of truth for both
// the SDK dispatcher wiring and RequiredEventTypes: an event can never be
// subscribed (or listed as required) without a real handler, and a handler
// can never be registered without being declared here.
type eventRegistration struct {
	eventType string
	// callback marks registrations that are delivered through the card
	// callback channel rather than the event subscription list.
	callback bool
	register func(a *Adapter, d *dispatcher.EventDispatcher)
}

func eventRegistrations() []eventRegistration {
	return []eventRegistration{
		{
			eventType: "im.message.receive_v1",
			register: func(a *Adapter, d *dispatcher.EventDispatcher) {
				d.OnP2MessageReceiveV1(func(ctx context.Context, event *larkim.P2MessageReceiveV1) error {
					if a.onMessage != nil {
						if msg := a.convertMessage(event); msg != nil {
							go a.onMessage(msg)
						}
					}
					return nil
				})
			},
		},
		{
			eventType: "im.message.recalled_v1",
			register: func(a *Adapter, d *dispatcher.EventDispatcher) {
				d.OnP2MessageRecalledV1(func(ctx context.Context, event *larkim.P2MessageRecalledV1) error {
					if a.onRecall != nil {
						if recall := a.convertMessageRecall(event); recall != nil {
							go a.onRecall(recall)
						}
					}
					return nil
				})
			},
		},
		{
			eventType: "im.message.reaction.created_v1",
			register: func(a *Adapter, d *dispatcher.EventDispatcher) {
				d.OnP2MessageReactionCreatedV1(func(ctx context.Context, event *larkim.P2MessageReactionCreatedV1) error {
					if a.onReaction != nil {
						if reaction := a.convertMessageReaction(event); reaction != nil {
							go a.onReaction(reaction)
						}
					}
					return nil
				})
			},
		},
		{
			eventType: "card.action.trigger",
			callback:  true,
			register: func(a *Adapter, d *dispatcher.EventDispatcher) {
				d.OnP2CardActionTrigger(func(ctx context.Context, event *callback.CardActionTriggerEvent) (*callback.CardActionTriggerResponse, error) {
					return a.handleCardActionEvent(ctx, event)
				})
			},
		},
		{
			eventType: "im.chat.member.bot.added_v1",
			register: func(a *Adapter, d *dispatcher.EventDispatcher) {
				d.OnP2ChatMemberBotAddedV1(func(ctx context.Context, event *larkim.P2ChatMemberBotAddedV1) error {
					if a.onBotAdded == nil || event == nil || event.Event == nil || event.Event.ChatId == nil {
						return nil
					}
					chatName := ""
					if event.Event.Name != nil {
						chatName = *event.Event.Name
					}
					go a.onBotAdded(&BotGroupEvent{ChatID: *event.Event.ChatId, ChatName: chatName})
					return nil
				})
			},
		},
	}
}

// RequiredEventTypes returns the Feishu event types this binary subscribes
// to, derived from the same table that wires the handlers. It is the
// authoritative subscription list used by the startup app-config healer.
// Card callbacks are configured through the callback channel and are not
// part of the event subscription list.
func RequiredEventTypes() []string {
	registrations := eventRegistrations()
	out := make([]string, 0, len(registrations))
	for _, reg := range registrations {
		if reg.callback {
			continue
		}
		out = append(out, reg.eventType)
	}
	return out
}

// buildEventDispatcher wires every registered event handler onto a fresh
// dispatcher.
func (a *Adapter) buildEventDispatcher() *dispatcher.EventDispatcher {
	d := dispatcher.NewEventDispatcher("", "")
	for _, reg := range eventRegistrations() {
		reg.register(a, d)
	}
	return d
}
