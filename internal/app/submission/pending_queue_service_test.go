package submission

import (
	"context"
	"testing"
	"time"

	"feidex/internal/feishu"
	"feidex/internal/state"
)

func TestPendingQueueServiceStageInboundImagesForSession(t *testing.T) {
	app := newPendingQueueTestApp(t)
	svc := NewPendingQueueService(app)

	msg := &feishu.InboundMessage{
		MessageID:     "img-1",
		ChatID:        "chat-1",
		ChatType:      "group",
		UserID:        "user-1",
		RootMessageID: "root-1",
		Attachments:   []feishu.Attachment{{Kind: "image", ResourceKey: "img-key"}},
	}
	if !svc.ShouldStageInboundImages(msg) {
		t.Fatal("ShouldStageInboundImages() should accept image-only message")
	}
	if svc.ShouldStageInboundImages(&feishu.InboundMessage{Text: "hello", Attachments: msg.Attachments}) {
		t.Fatal("ShouldStageInboundImages() should reject text+image message")
	}

	if err := svc.StageInboundImagesForSession(msg, "sess-1", func(_ *feishu.InboundMessage, _, _ string) ([]state.SubmissionAttachment, error) {
		return []state.SubmissionAttachment{{Kind: "image", Name: "image.png", LocalPath: "/tmp/image.png"}}, nil
	}); err != nil {
		t.Fatalf("StageInboundImagesForSession() error = %v", err)
	}

	sess := app.store.GetSession("sess-1")
	if sess == nil || len(sess.StagedImages) != 1 || sess.Status != "queued" {
		t.Fatalf("session after stage = %+v", sess)
	}
	if len(app.added) == 0 || app.added[0] != "img-1:"+QueueReactionEmoji {
		t.Fatalf("added reactions = %+v, want queue reaction", app.added)
	}
}

func TestPendingQueueServiceReactionWrappersAndDiscardSession(t *testing.T) {
	app := newPendingQueueTestApp(t)
	svc := NewPendingQueueService(app)

	if err := app.store.UpsertSession(&state.Session{
		Key:         "sess-1",
		WorkspaceID: "default",
		Status:      "queued",
		Queue:       []string{"sub-1"},
		StagedImages: []state.SessionStagedImage{
			{SourceMessageID: "img-1", CreatedAt: time.Now().Unix()},
		},
	}); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}
	if _, err := app.store.CreateSubmission(&state.Submission{
		ID:               "sub-1",
		SessionKey:       "sess-1",
		WorkspaceID:      "default",
		TriggerMessageID: "msg-1",
		SourceMessageIDs: []string{"src-1"},
		Status:           "queued",
	}); err != nil {
		t.Fatalf("CreateSubmission() error = %v", err)
	}

	sub := app.store.GetSubmission("sub-1")
	svc.MarkSubmissionQueuedReactions(sub)
	svc.MarkSubmissionRunningReactions(sub)
	svc.ClearSubmissionProcessingReactions(sub)
	if len(app.added) == 0 || len(app.removed) == 0 {
		t.Fatalf("reaction wrappers did not call client: added=%+v removed=%+v", app.added, app.removed)
	}

	if discarded := svc.DiscardSessionPendingInputs("sess-1"); discarded != 2 {
		t.Fatalf("DiscardSessionPendingInputs() = %d, want 2", discarded)
	}
	sess := app.store.GetSession("sess-1")
	if sess == nil || sess.Status != "idle" || len(sess.Queue) != 0 || len(sess.StagedImages) != 0 {
		t.Fatalf("session after discard = %+v", sess)
	}
}

func TestPendingQueueServiceDiscardPendingInputByMessageID(t *testing.T) {
	app := newPendingQueueTestApp(t)
	svc := NewPendingQueueService(app)
	sessionKey := "feishu:p2p:chat:user"

	if err := app.store.UpsertSession(&state.Session{
		Key:         sessionKey,
		WorkspaceID: "default",
		ChatID:      "chat",
		ChatType:    "p2p",
		OwnerUserID: "user",
		Status:      "queued",
		Queue:       []string{"sub-1"},
		StagedImages: []state.SessionStagedImage{
			{
				SourceMessageID: "img-staged",
				Name:            "image.png",
				LocalPath:       "/tmp/image.png",
				CreatedAt:       time.Now().Unix(),
			},
		},
	}); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}
	if _, err := app.store.CreateSubmission(&state.Submission{
		ID:               "sub-1",
		SessionKey:       sessionKey,
		WorkspaceID:      "default",
		TriggerMessageID: "msg-queued",
		SourceMessageIDs: []string{"msg-queued", "img-bound"},
		Status:           "queued",
	}); err != nil {
		t.Fatalf("CreateSubmission() error = %v", err)
	}

	if !svc.DiscardPendingInputByMessageID("img-staged") {
		t.Fatal("expected staged image discard to succeed")
	}
	sess := app.store.GetSession(sessionKey)
	if sess == nil || len(sess.StagedImages) != 0 {
		t.Fatalf("expected staged images to be removed, got %#v", sess)
	}

	if !svc.DiscardPendingInputByMessageID("msg-queued") {
		t.Fatal("expected queued submission discard to succeed")
	}
	sess = app.store.GetSession(sessionKey)
	if sess == nil {
		t.Fatal("expected session")
	}
	if len(sess.Queue) != 0 || sess.Status != "idle" {
		t.Fatalf("expected session to become idle, got %#v", sess)
	}
	if sub := app.store.GetSubmission("sub-1"); sub != nil {
		t.Fatalf("expected queued submission to be released from runtime store, got %#v", sub)
	}
}

func TestDiscardQueuedSubmissionFromSessionSnapshotPreservesCurrentSessionState(t *testing.T) {
	app := newPendingQueueTestApp(t)
	svc := NewPendingQueueService(app)

	if err := app.store.UpsertSession(&state.Session{
		Key:                "sess-1",
		WorkspaceID:        "default",
		ActiveThreadID:     "thread-1",
		ActiveTurnID:       "turn-running",
		ActiveSubmissionID: "sub-running",
		Status:             "queued",
		Queue:              []string{"sub-queued"},
		ActiveOperations: []state.SessionActiveOperation{
			{
				Kind:         "submission",
				SubmissionID: "sub-running",
				ThreadID:     "thread-1",
				TurnID:       "turn-running",
				StartedAt:    time.Now().Unix(),
			},
		},
	}); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}
	if _, err := app.store.CreateSubmission(&state.Submission{
		ID:               "sub-queued",
		SessionKey:       "sess-1",
		WorkspaceID:      "default",
		TriggerMessageID: "msg-queued",
		SourceMessageIDs: []string{"msg-queued"},
		Status:           "queued",
	}); err != nil {
		t.Fatalf("CreateSubmission() error = %v", err)
	}

	staleSnapshot := app.store.GetSession("sess-1")
	queuedSub := app.store.GetSubmission("sub-queued")
	if staleSnapshot == nil || queuedSub == nil {
		t.Fatalf("missing stale snapshot or queued submission: %+v / %+v", staleSnapshot, queuedSub)
	}

	if _, err := app.store.UpdateSession("sess-1", func(current *state.Session) {
		current.ActiveOperations = nil
		current.ActiveTurnID = ""
		current.ActiveSubmissionID = ""
		current.Status = "idle"
	}); err != nil {
		t.Fatalf("UpdateSession() error = %v", err)
	}

	if !svc.DiscardQueuedSubmissionFromSessionSnapshot(staleSnapshot, "sub-queued", queuedSub) {
		t.Fatal("DiscardQueuedSubmissionFromSessionSnapshot() should discard queued submission")
	}

	sess := app.store.GetSession("sess-1")
	if sess == nil {
		t.Fatal("expected session")
	}
	if sess.ActiveSubmissionID != "" || len(sess.ActiveOperations) != 0 {
		t.Fatalf("session should not restore stale in-flight submission state: %+v", sess)
	}
	if len(sess.Queue) != 0 || sess.Status != "idle" {
		t.Fatalf("session after discard = %+v, want idle with empty queue", sess)
	}
	if got := app.store.GetSubmission("sub-queued"); got != nil {
		t.Fatalf("queued submission after discard = %+v, want runtime cleanup", got)
	}
}

type pendingQueueTestApp struct {
	store *state.Store

	added   []string
	removed []string
}

func newPendingQueueTestApp(t *testing.T) *pendingQueueTestApp {
	t.Helper()
	store, err := state.Open(t.TempDir() + "/state.json")
	if err != nil {
		t.Fatalf("Open(store) error = %v", err)
	}
	return &pendingQueueTestApp{store: store}
}

func (a *pendingQueueTestApp) PendingQueueAppState() PendingQueueAppStateProvider {
	return pendingQueueTestState{store: a.store}
}

func (a *pendingQueueTestApp) PendingQueueRuntimeMaintenance() PendingQueueRuntimeMaintenanceProvider {
	return pendingQueueTestRuntimeMaintenance{store: a.store}
}

func (a *pendingQueueTestApp) PendingQueueDefaultWorkspaceID() string {
	return "default"
}

func (a *pendingQueueTestApp) PendingQueueAddReaction(_ context.Context, messageID, emoji string) error {
	a.added = append(a.added, messageID+":"+emoji)
	return nil
}

func (a *pendingQueueTestApp) PendingQueueRemoveReaction(_ context.Context, messageID, emoji string) error {
	a.removed = append(a.removed, messageID+":"+emoji)
	return nil
}

func (a *pendingQueueTestApp) PendingQueueLogSessionState(_, _ string, _ *state.Session) {}

type pendingQueueTestState struct {
	store *state.Store
}

func (s pendingQueueTestState) Session(key string) *state.Session {
	return s.store.GetSession(key)
}

func (s pendingQueueTestState) Sessions() []*state.Session {
	return s.store.AllSessions()
}

func (s pendingQueueTestState) Submission(id string) *state.Submission {
	return s.store.GetSubmission(id)
}

func (s pendingQueueTestState) SaveSession(sess *state.Session) error {
	return s.store.UpsertSession(sess)
}

func (s pendingQueueTestState) UpdateSession(key string, mutate func(*state.Session)) (*state.Session, error) {
	return s.store.UpdateSession(key, mutate)
}

func (s pendingQueueTestState) UpdateSubmission(id string, mutate func(*state.Submission)) error {
	return s.store.UpdateSubmission(id, mutate)
}

type pendingQueueTestRuntimeMaintenance struct {
	store *state.Store
}

func (m pendingQueueTestRuntimeMaintenance) CleanupSubmissionRuntimeState(sub *state.Submission) {
	if sub == nil {
		return
	}
	m.store.DeleteSubmission(sub.ID)
}
