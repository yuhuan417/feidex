package feishuapp

import (
	"context"
	"errors"
	"testing"

	appstate "feidex/internal/adapter/storage/json/scoped"
	"feidex/internal/application"
	"feidex/internal/domain/conversation"
	"feidex/internal/domain/identity"
	frontendruntime "feidex/internal/runtime"
)

func TestCompactionPortsUseCurrentFrontendClient(t *testing.T) {
	owner := frontendruntime.NewFrontendOwner()
	deps := CompactionPorts(context.Background, &appstate.Store{}, owner, "frontend", false)
	if err := deps.Gateway.StartCompaction(context.Background(), "thread"); err == nil {
		t.Fatal("compaction succeeded without a Codex client")
	}
	firstErr, replacementErr := errors.New("first client"), errors.New("replacement client")
	owner.SetCodexClient(&fakeCodexClient{callErr: firstErr})
	if err := deps.Gateway.StartCompaction(context.Background(), "thread"); !errors.Is(err, firstErr) {
		t.Fatalf("initial compaction error = %v", err)
	}
	owner.SetCodexClient(&fakeCodexClient{callErr: replacementErr})
	if err := deps.Gateway.StartCompaction(context.Background(), "thread"); !errors.Is(err, replacementErr) {
		t.Fatalf("compaction after client replacement error = %v", err)
	}
	owner.SetCodexClient(nil)
	if err := deps.Gateway.StartCompaction(context.Background(), "thread"); err == nil {
		t.Fatal("compaction retained a detached client")
	}
}

func TestCompactionPortsNoticeUsesFrontendEffectRunner(t *testing.T) {
	owner := frontendruntime.NewFrontendOwner()
	deps := CompactionPorts(context.Background, &appstate.Store{}, owner, "frontend", true)
	var sent []application.SendMessage
	type contextKey struct{}
	ctx := context.WithValue(context.Background(), contextKey{}, "notice")
	owner.EffectRunner = &frontendruntime.EffectRunner{Send: func(gotCtx context.Context, effect application.SendMessage) error {
		if gotCtx.Value(contextKey{}) != "notice" {
			t.Fatal("notice lost its operation context")
		}
		sent = append(sent, effect)
		return nil
	}}
	deps.Notices(ctx, &conversation.Session{ChatID: "chat", RootMessageID: "root"}, "completed")
	want := application.SendMessage{Frontend: identity.FrontendID("frontend"), Chat: identity.ChatRef{ID: "chat"}, Text: "completed"}
	if len(sent) != 1 || sent[0] != want {
		t.Fatalf("notice effects = %+v, want %+v", sent, want)
	}
	deps.Notices(ctx, &conversation.Session{}, "missing chat")
	disabled := CompactionPorts(context.Background, &appstate.Store{}, owner, "frontend", false)
	disabled.Notices(ctx, &conversation.Session{ChatID: "chat"}, "disabled")
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	deps.Notices(canceled, &conversation.Session{ChatID: "chat"}, "canceled")
	if len(sent) != 1 {
		t.Fatalf("skipped notices sent effects: %+v", sent)
	}
}
