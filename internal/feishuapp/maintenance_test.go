package feishuapp

import (
	appmaintenance "feidex/internal/adapter/feishu/maintenance"
	"feidex/internal/domain/conversation"

	"os"
	"path/filepath"
	"testing"
	"time"

	"feidex/internal/adapter/feishu/attachments"
	"feidex/internal/adapter/feishu/quietmode"
	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

func TestQuietModeCardAndCommandValidation(t *testing.T) {
	cfg := config.Default()
	cfg.Workspaces[0].Cwd = t.TempDir()
	a := prepareTestApp(&App{cfg: cfg, cfgPath: filepath.Join(t.TempDir(), "config.toml"), feishu: feishu.New(cfg.Feishu)})

	card := renderQuietModeCard(a)
	title, preview, buttonCount := feishu.New(cfg.Feishu).SimpleStatusCard("tmp", "blue", "tmp", nil)["header"], cardElementsForTest(card), 0
	_ = title
	_ = preview
	if elems := cardElementsForTest(card); len(elems) != 5 {
		t.Fatalf("renderQuietModeCard() elements = %#v", elems)
	}
	if err := commandQuiet(a, &feishu.InboundMessage{}, []string{"bad"}); err == nil {
		t.Fatal("expected commandQuiet(invalid arg) to fail")
	}
	if quietmode.StatusText(config.QuietModeVerbose) != "verbose" || quietmode.StatusText(config.QuietModeFinal) != "final" || buttonCount != 0 {
		t.Fatal("quietmode.StatusText() returned unexpected values")
	}
}

func TestRuntimeMaintenanceHelpers(t *testing.T) {
	store, err := state.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if err := store.UpsertPending(&state.PendingRequest{ID: "pending", Status: "pending", ExpiresAt: time.Now().Add(time.Hour).Unix()}); err != nil {
		t.Fatalf("UpsertPending(pending) error = %v", err)
	}
	if err := store.UpsertPending(&state.PendingRequest{ID: "done", Status: "resolved"}); err != nil {
		t.Fatalf("UpsertPending(resolved) error = %v", err)
	}

	workspace := t.TempDir()
	attachmentsRoot := filepath.Join(workspace, attachments.AttachmentsDirName, "session")
	oldDir := filepath.Join(attachmentsRoot, "old")
	newDir := filepath.Join(attachmentsRoot, "new")
	for _, path := range []string{oldDir, newDir} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatalf("MkdirAll(%s) error = %v", path, err)
		}
	}
	oldTime := time.Now().Add(-appmaintenance.AttachmentRetention - time.Hour)
	if err := os.Chtimes(oldDir, oldTime, oldTime); err != nil {
		t.Fatalf("Chtimes(oldDir) error = %v", err)
	}

	cfg := config.Default()
	cfg.Workspaces[0].Cwd = workspace
	a := prepareTestApp(&App{cfg: cfg, store: store})
	if err := a.bindings.StartupState.ExpireInteractions(); err != nil {
		t.Fatal(err)
	}
	if got := a.store.PendingByID("pending"); got == nil || got.Status != "expired" {
		t.Fatalf("expirePendingRequestsOnStartup() = %+v, want expired request", got)
	}

	a.bindings.MaintenanceCommands.CleanupExpiredAttachments()
	if _, err := os.Stat(oldDir); !os.IsNotExist(err) {
		t.Fatalf("expected old attachment dir to be removed, stat err = %v", err)
	}
	if _, err := os.Stat(newDir); err != nil {
		t.Fatalf("expected new attachment dir to remain, stat err = %v", err)
	}

	if err := os.MkdirAll(oldDir, 0o755); err != nil {
		t.Fatalf("recreate oldDir error = %v", err)
	}
	if err := os.Chtimes(oldDir, oldTime, oldTime); err != nil {
		t.Fatalf("Chtimes(oldDir second) error = %v", err)
	}
	a.bindings.MaintenanceCommands.CleanupAttachmentDir(attachmentsRoot)
	if _, err := os.Stat(oldDir); !os.IsNotExist(err) {
		t.Fatalf("cleanupAttachmentDir() should remove old dir, stat err = %v", err)
	}
}

func TestMiscAppFunctions(t *testing.T) {
	logSessionState("test", "sess", nil)
	logSessionState("test", "sess", &conversation.Session{WorkspaceID: "ws", Queue: []string{"a"}})

	started := time.Now()
	app := prepareTestApp(&App{started: started})
	if !isStaleInboundMessage(app.started, &feishu.InboundMessage{CreatedAt: started.Add(-31 * time.Second).Unix()}) {
		t.Fatal("expected old inbound message to be stale")
	}
	if isStaleInboundMessage(app.started, &feishu.InboundMessage{CreatedAt: started.Unix()}) {
		t.Fatal("expected recent inbound message to be fresh")
	}
	if got := nonZero(0, 0, 7, 9); got != 7 {
		t.Fatalf("nonZero() = %d, want first non-zero", got)
	}
	if got := app.configView().makeSessionKey(&feishu.InboundMessage{ChatType: "group", ChatID: "chat", RootMessageID: "root", MessageID: "msg"}); got != "feishu:chat:chat" {
		t.Fatalf("makeSessionKey(group) = %q", got)
	}
	if got := app.configView().makeSessionKey(&feishu.InboundMessage{ChatType: "p2p", ChatID: "chat", UserID: "user"}); got != "feishu:chat:chat" {
		t.Fatalf("makeSessionKey(p2p) = %q", got)
	}
}

func TestReplyErrorWithEmptyInputReturnsEarly(t *testing.T) {
	if err := replyError(prepareTestApp(&App{}), nil, nil); err != nil {
		t.Fatalf("replyError(nil, nil) error = %v", err)
	}
}
