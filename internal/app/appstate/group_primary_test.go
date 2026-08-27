package appstate

import (
	"testing"

	"feidex/internal/app/appcore"
	"feidex/internal/state"
)

func TestGroupPrimaryUsesCanonicalKeyOnly(t *testing.T) {
	store := newTestStateStore(t)
	frontend := &Store{AppStateFacade: appcore.AppStateFacade{Store: store, FrontendID: "default"}}
	if err := store.UpsertGroupPrimary(&state.GroupPrimary{
		ID:             "primary_default_group_chat-1",
		ChatID:         "chat-1",
		ChatType:       "group",
		OwnerBotOpenID: "bot-a-open",
	}); err != nil {
		t.Fatalf("UpsertGroupPrimary(legacy) error = %v", err)
	}

	if got := frontend.GroupPrimary("group", "chat-1"); got != nil {
		t.Fatalf("GroupPrimary() returned legacy frontend-scoped record: %+v", got)
	}
}
