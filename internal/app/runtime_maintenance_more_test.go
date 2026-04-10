package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"feidex/internal/feishu"
	"feidex/internal/state"
)

func TestRuntimeMaintenanceAdditionalHelpers(t *testing.T) {
	a, ff, _ := newTestApp(t)
	if err := a.store.UpsertPending(&state.PendingRequest{ID: "req-1", Status: "pending", ExpiresAt: time.Now().Add(time.Hour).Unix()}); err != nil {
		t.Fatalf("UpsertPending() error = %v", err)
	}
	a.expirePendingRequestsOnStartup()
	if req := a.store.PendingByID("req-1"); req == nil || req.Status != "expired" {
		t.Fatalf("expirePendingRequestsOnStartup() = %+v", req)
	}

	subID, err := a.store.CreateSubmission(&state.Submission{ID: "sub-1", SessionKey: "sess-1", WorkspaceID: "default", TurnID: "turn-1"})
	if err != nil {
		t.Fatalf("CreateSubmission() error = %v", err)
	}
	if err := a.store.UpsertPending(&state.PendingRequest{ID: "req-turn", TurnID: "turn-1", Status: "pending"}); err != nil {
		t.Fatalf("UpsertPending(turn) error = %v", err)
	}
	if err := a.store.UpsertMessageLink(&state.MessageLink{MessageID: "msg-1", SubmissionID: subID, TurnID: "turn-1"}); err != nil {
		t.Fatalf("UpsertMessageLink() error = %v", err)
	}
	a.cleanupSubmissionRuntimeState(&state.Submission{ID: subID, TurnID: "turn-1"})
	if a.store.GetSubmission(subID) != nil || a.store.PendingByID("req-turn") != nil || a.store.GetMessageLink("msg-1") != nil {
		t.Fatal("cleanupSubmissionRuntimeState() should remove runtime artifacts")
	}

	a.startMarkdownPreviewGCLoop(context.Background())
	ff.cleanupErr = context.Canceled
	a.runMarkdownPreviewGC("test")
	ff.cleanupErr = nil
	ff.cleanupResult = feishu.PreviewDriveCleanupResult{DeletedFileCount: 1}
	ff.sharedCleanupErr = context.Canceled
	a.runMarkdownPreviewGC("test")
	ff.sharedCleanupErr = nil
	ff.sharedCleanupResult = feishu.PreviewDriveCleanupResult{DeletedFileCount: 1}
	a.runMarkdownPreviewGC("test")

	root := filepath.Join(a.cfg.Workspaces[0].Cwd, attachmentsDirName, "old")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	old := filepath.Join(root, "expired")
	if err := os.MkdirAll(old, 0o755); err != nil {
		t.Fatalf("MkdirAll(old) error = %v", err)
	}
	oldTime := time.Now().Add(-attachmentRetention - time.Hour)
	if err := os.Chtimes(old, oldTime, oldTime); err != nil {
		t.Fatalf("Chtimes() error = %v", err)
	}
	a.cleanupAttachmentDir(root)
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatalf("cleanupAttachmentDir() should remove expired dir, stat err=%v", err)
	}
}
