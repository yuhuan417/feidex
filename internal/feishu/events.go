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
	// viaChannel marks events whose dispatcher handler is registered by the
	// SDK channel package instead of by this table (see channel_runtime.go).
	// They MUST stay in this table: they are still subscribed on the platform,
	// so RequiredEventTypes has to keep listing them. What they must not do is
	// register here — the dispatcher panics on a duplicate registration for the
	// same event type, and channel registers these lazily when the
	// corresponding On* method is called.
	//
	// Handler ownership still lives in this table's spirit: the invariant that
	// "an event is never subscribed without a real handler" holds, the handler
	// just lives in channel_runtime.go rather than in a register func.
	viaChannel bool
	register   func(a *Adapter, d *dispatcher.EventDispatcher)
}

func eventRegistrations() []eventRegistration {
	return []eventRegistration{
		{
			eventType:  "im.message.receive_v1",
			viaChannel: true,
		},
		{
			eventType: "im.message.recalled_v1",
			// channel does not handle message recall, so this one stays ours.
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
			eventType:  "im.message.reaction.created_v1",
			viaChannel: true,
		},
		{
			eventType: "card.action.trigger",
			callback:  true,
			// Card callbacks stay ours: channel's OnCardAction discards the
			// callback response, and 98% of this project's card paths return a
			// toast through it. See docs/oapi-sdk-v3.12.0-upgrade-plan.md 3.2.
			register: func(a *Adapter, d *dispatcher.EventDispatcher) {
				d.OnP2CardActionTrigger(func(ctx context.Context, event *callback.CardActionTriggerEvent) (*callback.CardActionTriggerResponse, error) {
					return a.handleCardActionEvent(ctx, event)
				})
			},
		},
		{
			eventType:  "im.chat.member.bot.added_v1",
			viaChannel: true,
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

// buildEventDispatcher wires every event handler this project owns onto a
// fresh dispatcher. Events marked viaChannel are skipped: channel registers
// those itself, and a second registration would panic.
func (a *Adapter) buildEventDispatcher() *dispatcher.EventDispatcher {
	d := dispatcher.NewEventDispatcher("", "")
	for _, reg := range eventRegistrations() {
		if reg.viaChannel {
			continue
		}
		reg.register(a, d)
	}
	return d
}
