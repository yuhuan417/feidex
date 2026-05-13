package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
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
	newRuntimeMaintenanceService(a).ExpirePendingRequestsOnStartup()
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
	newRuntimeMaintenanceService(a).CleanupSubmissionRuntimeState(&state.Submission{ID: subID, TurnID: "turn-1"})
	if a.store.GetSubmission(subID) != nil || a.store.PendingByID("req-turn") != nil || a.store.GetMessageLink("msg-1") != nil {
		t.Fatal("cleanupSubmissionRuntimeState() should remove runtime artifacts")
	}

	ff.setCleanupState(feishu.PreviewDriveCleanupResult{}, context.Canceled)
	newRuntimeMaintenanceService(a).RunDriveArtifactGC("test")
	ff.setCleanupState(feishu.PreviewDriveCleanupResult{DeletedFileCount: 1}, nil)
	newRuntimeMaintenanceService(a).RunDriveArtifactGC("test")

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
	newRuntimeMaintenanceService(a).CleanupAttachmentDir(root)
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatalf("cleanupAttachmentDir() should remove expired dir, stat err=%v", err)
	}
}

func TestRunDriveArtifactGCUsesExtendedTimeout(t *testing.T) {
	a, ff, _ := newTestApp(t)
	var (
		gotDeadline   time.Time
		gotDeadlineOK bool
	)
	ff.setCleanupHook(func(ctx context.Context, _ time.Time) (feishu.PreviewDriveCleanupResult, error) {
		gotDeadline, gotDeadlineOK = ctx.Deadline()
		return feishu.PreviewDriveCleanupResult{}, nil
	})

	newRuntimeMaintenanceService(a).RunDriveArtifactGC("test")
	if !gotDeadlineOK {
		t.Fatal("CleanupArtifactsBefore context should have a deadline")
	}
	if remaining := time.Until(gotDeadline); remaining < artifactGCTimeout-5*time.Second {
		t.Fatalf("artifact GC timeout remaining = %s, want close to %s", remaining, artifactGCTimeout)
	}
}

func TestRunDriveArtifactGCNotifiesPermissionIssueToKnownChats(t *testing.T) {
	a, ff, _ := newTestApp(t)
	a.frontendID = "default"
	a.feishu = wrapFeishuClient(ff)
	for _, sess := range []*state.Session{
		{Key: "feishu:frontend:default:p2p:chat-b:ou-user-1", ChatID: "chat-b"},
		{Key: "feishu:frontend:default:group:chat-a:root:om-root-1", ChatID: "chat-a"},
		{Key: "feishu:frontend:default:p2p:chat-a:ou-user-2", ChatID: "chat-a"},
		{Key: "feishu:frontend:other:p2p:chat-c:ou-user-3", ChatID: "chat-c"},
	} {
		if err := a.store.UpsertSession(sess); err != nil {
			t.Fatalf("UpsertSession(%q) error = %v", sess.Key, err)
		}
	}
	ff.cleanupErr = &permissionIssueTestError{
		err: errors.New("permission denied"),
		issue: &feishu.PermissionIssue{
			API:     "drive.file.list",
			Code:    99991663,
			Message: "no permission",
		},
	}

	newRuntimeMaintenanceService(a).RunDriveArtifactGC("test")
	if got, want := len(ff.sendCards), 2; got != want {
		t.Fatalf("permission diagnostic send cards = %d, want %d", got, want)
	}
	if got, want := ff.sendCardChatIDs, []string{"chat-a", "chat-b"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("permission diagnostic chat ids = %#v, want %#v", got, want)
	}
	for i, card := range ff.sendCards {
		body := cardMarkdownContent(t, card)
		if !strings.Contains(body, "drive.file.list") {
			t.Fatalf("permission diagnostic body[%d] = %q", i, body)
		}
	}

	newRuntimeMaintenanceService(a).RunDriveArtifactGC("test")
	if got, want := len(ff.sendCards), 2; got != want {
		t.Fatalf("deduplicated permission diagnostic send cards = %d, want %d", got, want)
	}
}

func TestRunDriveArtifactGCQueuesPermissionIssueWithoutKnownChatsUntilNextMessage(t *testing.T) {
	a, ff, _ := newTestApp(t)
	a.frontendID = "default"
	a.feishu = wrapFeishuClient(ff)
	ff.cleanupErr = &permissionIssueTestError{
		err: errors.New("permission denied"),
		issue: &feishu.PermissionIssue{
			API:     "drive.file.list",
			Code:    99991663,
			Message: "no permission",
		},
	}

	newRuntimeMaintenanceService(a).RunDriveArtifactGC("test")
	if got := len(ff.sendCards); got != 0 {
		t.Fatalf("permission diagnostic send cards before inbound = %d, want 0", got)
	}
	if got := a.State().FrontendCardNotifications(); len(got) != 1 || !strings.Contains(got[0].Body, "drive.file.list") {
		t.Fatalf("frontendCardNotifications() = %+v", got)
	}

	router := newFeishuEventRouter(a)
	if err := router.processMessage(&feishu.InboundMessage{
		MessageID: "m-1",
		ChatID:    "chat-next",
		ChatType:  "p2p",
		UserID:    "ou-user-next",
	}); err != nil {
		t.Fatalf("processMessage() error = %v", err)
	}
	if got, want := len(ff.sendCards), 1; got != want {
		t.Fatalf("permission diagnostic send cards after inbound = %d, want %d", got, want)
	}
	if got, want := ff.sendCardChatIDs, []string{"chat-next"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("permission diagnostic chat ids after inbound = %#v, want %#v", got, want)
	}
	if got := a.State().FrontendCardNotifications(); len(got) != 0 {
		t.Fatalf("frontendCardNotifications() after inbound = %+v, want empty", got)
	}
}

func TestRunDriveArtifactGCQueuesOnlyOneDeferredPermissionIssue(t *testing.T) {
	a, ff, _ := newTestApp(t)
	a.frontendID = "default"
	a.feishu = wrapFeishuClient(ff)

	ff.cleanupErr = &permissionIssueTestError{
		err: errors.New("permission denied 1"),
		issue: &feishu.PermissionIssue{
			API:     "drive.file.list",
			Code:    99991663,
			Message: "first no permission",
		},
	}
	newRuntimeMaintenanceService(a).RunDriveArtifactGC("test")

	ff.cleanupErr = &permissionIssueTestError{
		err: errors.New("permission denied 2"),
		issue: &feishu.PermissionIssue{
			API:     "drive.file.delete",
			Code:    99991663,
			Message: "second no permission",
		},
	}
	newRuntimeMaintenanceService(a).RunDriveArtifactGC("test")

	got := a.State().FrontendCardNotifications()
	if len(got) != 1 {
		t.Fatalf("frontendCardNotifications() len = %d, want 1", len(got))
	}
	if !strings.Contains(got[0].Body, "drive.file.delete") || strings.Contains(got[0].Body, "drive.file.list") {
		t.Fatalf("frontendCardNotifications() body = %q, want latest collapsed error", got[0].Body)
	}
}
