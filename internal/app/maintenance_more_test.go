package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

func TestQuietModeCardAndCommandValidation(t *testing.T) {
	cfg := config.Default()
	cfg.Workspaces[0].Cwd = t.TempDir()
	a := &App{cfg: cfg, cfgPath: filepath.Join(t.TempDir(), "config.toml"), feishu: feishu.New(cfg.Feishu)}

	card := a.renderQuietModeCard()
	title, preview, buttonCount := feishu.New(cfg.Feishu).SimpleStatusCard("tmp", "blue", "tmp", nil)["header"], card["elements"], 0
	_ = title
	_ = preview
	if elems, ok := card["elements"].([]map[string]any); !ok || len(elems) != 2 {
		t.Fatalf("renderQuietModeCard() elements = %#v", card["elements"])
	}
	if err := a.commandQuiet(&feishu.InboundMessage{}, []string{"bad"}); err == nil {
		t.Fatal("expected commandQuiet(invalid arg) to fail")
	}
	if quietModeStatusText(true) != "开启" || quietModeStatusText(false) != "关闭" || buttonCount != 0 {
		t.Fatal("quietModeStatusText() returned unexpected values")
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
	attachmentsRoot := filepath.Join(workspace, attachmentsDirName, "session")
	oldDir := filepath.Join(attachmentsRoot, "old")
	newDir := filepath.Join(attachmentsRoot, "new")
	for _, path := range []string{oldDir, newDir} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatalf("MkdirAll(%s) error = %v", path, err)
		}
	}
	oldTime := time.Now().Add(-attachmentRetention - time.Hour)
	if err := os.Chtimes(oldDir, oldTime, oldTime); err != nil {
		t.Fatalf("Chtimes(oldDir) error = %v", err)
	}

	cfg := config.Default()
	cfg.Workspaces[0].Cwd = workspace
	a := &App{cfg: cfg, store: store}
	a.expirePendingRequestsOnStartup()
	if got := a.store.PendingByID("pending"); got == nil || got.Status != "expired" {
		t.Fatalf("expirePendingRequestsOnStartup() = %+v, want expired request", got)
	}

	a.cleanupExpiredAttachments()
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
	a.cleanupAttachmentDir(attachmentsRoot)
	if _, err := os.Stat(oldDir); !os.IsNotExist(err) {
		t.Fatalf("cleanupAttachmentDir() should remove old dir, stat err = %v", err)
	}
}

func TestStatusRefreshHelpersAndMiscAppFunctions(t *testing.T) {
	a := &App{statusFlushCh: make(chan struct{}, 1)}
	a.scheduleStatusCardRefresh("sub-2")
	a.scheduleStatusCardRefresh("sub-1")
	a.scheduleStatusCardRefresh("")
	ids := a.takePendingStatusCardRefreshes()
	if len(ids) != 2 || ids[0] != "sub-1" || ids[1] != "sub-2" {
		t.Fatalf("takePendingStatusCardRefreshes() = %+v, want sorted ids", ids)
	}
	if got := a.takePendingStatusCardRefreshes(); got != nil {
		t.Fatalf("takePendingStatusCardRefreshes(second) = %+v, want nil", got)
	}
	if err := a.refreshStatusCardNow(""); err != nil {
		t.Fatalf("refreshStatusCardNow(empty) error = %v", err)
	}
	a.startStatusRefreshLoop(context.Background())

	logSessionState("test", "sess", nil)
	logSessionState("test", "sess", &state.Session{WorkspaceID: "ws", Queue: []string{"a"}})

	started := time.Now()
	app := &App{started: started}
	if !app.isStaleInboundMessage(&feishu.InboundMessage{CreatedAt: started.Add(-31 * time.Second).Unix()}) {
		t.Fatal("expected old inbound message to be stale")
	}
	if app.isStaleInboundMessage(&feishu.InboundMessage{CreatedAt: started.Unix()}) {
		t.Fatal("expected recent inbound message to be fresh")
	}
	if got := nonZero(0, 0, 7, 9); got != 7 {
		t.Fatalf("nonZero() = %d, want first non-zero", got)
	}
	if got := app.makeSessionKey(&feishu.InboundMessage{ChatType: "group", ChatID: "chat", RootMessageID: "root", MessageID: "msg"}); got != "feishu:group:chat:root:root" {
		t.Fatalf("makeSessionKey(group) = %q", got)
	}
	if got := app.makeSessionKey(&feishu.InboundMessage{ChatType: "p2p", ChatID: "chat", UserID: "user"}); got != "feishu:p2p:chat:user" {
		t.Fatalf("makeSessionKey(p2p) = %q", got)
	}
}

func TestReplyAndStartupHelpersReturnEarly(t *testing.T) {
	if err := (&App{}).replyError(nil, nil); err != nil {
		t.Fatalf("replyError(nil, nil) error = %v", err)
	}
	var a *App
	a.sendStartupReadyNotifications()
	if got := submissionStatusPlaceholder(""); got != "任务状态未知。" {
		t.Fatalf("submissionStatusPlaceholder() = %q, want unknown placeholder", got)
	}
}
