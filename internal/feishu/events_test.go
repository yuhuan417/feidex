package feishu

import (
	"reflect"
	"testing"
)

func TestRequiredEventTypesMatchRegistrations(t *testing.T) {
	want := []string{
		"im.message.receive_v1",
		"im.message.recalled_v1",
		"im.message.reaction.created_v1",
		"im.chat.member.bot.added_v1",
	}
	got := RequiredEventTypes()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("RequiredEventTypes() = %v, want %v", got, want)
	}
}

func TestRequiredEventTypesExcludeCallbacks(t *testing.T) {
	for _, eventType := range RequiredEventTypes() {
		if eventType == "card.action.trigger" {
			t.Fatalf("RequiredEventTypes() must not list card callbacks: %v", RequiredEventTypes())
		}
	}
}

// Every entry must be handled exactly once: either this project registers it
// (register set, viaChannel clear) or the SDK channel package does (viaChannel
// set, register nil). Both set would panic at wiring time — the dispatcher
// rejects a duplicate registration for the same event type — and neither set
// would subscribe an event nobody handles.
func TestEventRegistrationsAreWellFormed(t *testing.T) {
	seen := map[string]bool{}
	for _, reg := range eventRegistrations() {
		if reg.eventType == "" {
			t.Fatal("eventRegistrations() contains an empty event type")
		}
		if reg.viaChannel && reg.register != nil {
			t.Fatalf("entry %q is marked viaChannel but also registers; channel already registers it and the dispatcher panics on duplicates", reg.eventType)
		}
		if !reg.viaChannel && reg.register == nil {
			t.Fatalf("eventRegistrations() entry %q has no register function and is not marked viaChannel", reg.eventType)
		}
		if seen[reg.eventType] {
			t.Fatalf("eventRegistrations() contains duplicate event type %q", reg.eventType)
		}
		seen[reg.eventType] = true
	}
	if !seen["card.action.trigger"] {
		t.Fatal("eventRegistrations() must wire the card action callback")
	}
	// Card callbacks are delivered through the callback channel, not the event
	// subscription list, so they must never be delegated to channel: that would
	// drop the callback response (toast) for 98% of card paths.
	for _, reg := range eventRegistrations() {
		if reg.eventType == "card.action.trigger" && reg.viaChannel {
			t.Fatal("card.action.trigger must be registered by this project, not by channel")
		}
	}
}

// The events channel handles must still be declared here: they remain
// subscribed on the platform, so RequiredEventTypes has to keep listing them.
func TestChannelHandledEventsStaySubscribed(t *testing.T) {
	required := map[string]bool{}
	for _, eventType := range RequiredEventTypes() {
		required[eventType] = true
	}
	for _, reg := range eventRegistrations() {
		if reg.viaChannel && !required[reg.eventType] {
			t.Fatalf("viaChannel event %q dropped from the subscription list; the platform would unsubscribe it", reg.eventType)
		}
	}
}
