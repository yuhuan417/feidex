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

func TestEventRegistrationsAreWellFormed(t *testing.T) {
	seen := map[string]bool{}
	for _, reg := range eventRegistrations() {
		if reg.eventType == "" {
			t.Fatal("eventRegistrations() contains an empty event type")
		}
		if reg.register == nil {
			t.Fatalf("eventRegistrations() entry %q has no register function", reg.eventType)
		}
		if seen[reg.eventType] {
			t.Fatalf("eventRegistrations() contains duplicate event type %q", reg.eventType)
		}
		seen[reg.eventType] = true
	}
	if !seen["card.action.trigger"] {
		t.Fatal("eventRegistrations() must wire the card action callback")
	}
}
