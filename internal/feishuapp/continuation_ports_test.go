package feishuapp

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"

	appstate "feidex/internal/adapter/storage/json/scoped"
	appsubmission "feidex/internal/application/submission"
	"feidex/internal/config"
	"feidex/internal/domain/conversation"
	domainsubmission "feidex/internal/domain/submission"
	"feidex/internal/feishu"
	frontendruntime "feidex/internal/runtime"
	"feidex/internal/state"
)

func TestContinuationPortsReadCurrentBackendAndScopedConfiguration(t *testing.T) {
	cfg := config.Default()
	cfg.Feishu.Backend = "claude"
	cfg.Frontends = []config.FrontendConfig{{FeishuConfig: config.FeishuConfig{Backend: "codex"}}}
	mu := &sync.RWMutex{}
	owner := frontendruntime.NewFrontendOwner()
	persistent, err := state.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	store := appstate.NewScoped(persistent, "frontend", "codex")
	deps := ContinuationPorts(cfg, mu, context.Background, store, owner, &appsubmission.SubmissionQueueService{}, "frontend", 0, nil)
	if got := deps.Backend(); got != "codex" {
		t.Fatalf("configured backend = %q", got)
	}
	owner.SetBackend("claude")
	if got := deps.Backend(); got != "claude" {
		t.Fatalf("runtime backend = %q", got)
	}
	owner.SetBackend("")
	mu.Lock()
	cfg.Frontends[0].Backend = "claude"
	cfg.Workspaces = []config.Workspace{{ID: "updated", Cwd: "/updated"}}
	mu.Unlock()
	if deps.Backend() != "claude" || deps.DefaultWorkspaceID() != "updated" || deps.Workspace("updated").Cwd != "/updated" {
		t.Fatal("continuation retained the initial configuration")
	}
	key := deps.MakeSessionKey(&feishu.InboundMessage{ChatID: "chat"})
	if key != "feishu:frontend:frontend:chat:chat" {
		t.Fatalf("session key = %q", key)
	}
	if err := deps.SaveSession(&conversation.Session{Key: key, WorkspaceID: "updated"}); err != nil {
		t.Fatal(err)
	}
	if got := deps.GetSession(key); got == nil || got.WorkspaceID != "updated" {
		t.Fatalf("scoped session = %+v", got)
	}
	if err := deps.SaveSession(&conversation.Session{Key: "feishu:frontend:other:chat:chat"}); err == nil {
		t.Fatal("foreign frontend session accepted")
	}
	if err := deps.SaveMessageLink(&state.MessageLink{MessageID: "message", SessionKey: key}); err != nil {
		t.Fatal(err)
	}
	if got := deps.GetMessageLink("message"); got == nil || got.FrontendID != "frontend" {
		t.Fatalf("scoped message link = %+v", got)
	}
	other := appstate.NewScoped(persistent, "other", "codex")
	if other.MessageLink("message") != nil || other.Session(key) != nil {
		t.Fatal("continuation state leaked across frontends")
	}
}

func TestContinuationPortsSteerUsesCurrentClientAndTurn(t *testing.T) {
	owner := frontendruntime.NewFrontendOwner()
	deps := ContinuationPorts(config.Default(), &sync.RWMutex{}, context.Background, nil, owner, &appsubmission.SubmissionQueueService{}, "frontend", -1, nil)
	input := &domainsubmission.Submission{InputText: "follow up"}
	if err := deps.Steer(context.Background(), "thread", "turn", input); err == nil {
		t.Fatal("steer succeeded without a Codex client")
	}
	initialErr := errors.New("initial client")
	owner.SetCodexClient(&fakeCodexClient{callErr: initialErr})
	if err := deps.Steer(context.Background(), "thread", "turn", input); !errors.Is(err, initialErr) {
		t.Fatalf("initial steer error = %v", err)
	}
	type contextKey struct{}
	ctx := context.WithValue(context.Background(), contextKey{}, "steer")
	calls := 0
	owner.SetCodexClient(&fakeCodexClient{callHook: func(gotCtx context.Context, method string, params, _ any) error {
		calls++
		payload, ok := params.(map[string]any)
		if gotCtx.Value(contextKey{}) != "steer" || method != "turn/steer" || !ok || payload["threadId"] != "thread" || payload["expectedTurnId"] != "turn" {
			t.Fatalf("steer context/method/params = %v / %q / %+v", gotCtx, method, params)
		}
		return nil
	}})
	if err := deps.Steer(ctx, "thread", "turn", input); err != nil || calls != 1 {
		t.Fatalf("replacement steer error/calls = %v / %d", err, calls)
	}
	owner.SetCodexClient(nil)
	if err := deps.Steer(ctx, "thread", "turn", input); err == nil {
		t.Fatal("steer retained a detached client")
	}
}

func TestContinuationPortsResolveForwardedAttachments(t *testing.T) {
	cfg := config.Default()
	client := &downloadFeishuStub{downloadPath: filepath.Join(t.TempDir(), "forwarded.png")}
	deps := ContinuationPorts(cfg, &sync.RWMutex{}, context.Background, nil, frontendruntime.NewFrontendOwner(), &appsubmission.SubmissionQueueService{}, "frontend", -1, client)
	msg := &feishu.InboundMessage{MessageID: "root", Attachments: []feishu.Attachment{{Kind: "image", SourceMessageID: "forwarded"}, {Kind: "file"}}}
	resolved, err := deps.ResolveInboundAttachments(msg, cfg.Workspaces[0].ID, "session")
	if err != nil {
		t.Fatal(err)
	}
	if len(resolved) != 2 || resolved[0].LocalPath != client.downloadPath || resolved[0].Kind != "image" || resolved[1].Kind != "file" {
		t.Fatalf("resolved attachments = %+v", resolved)
	}
	if len(client.messageIDs) != 2 || client.messageIDs[0] != "forwarded" || client.messageIDs[1] != "root" {
		t.Fatalf("attachment source message IDs = %+v", client.messageIDs)
	}
}
