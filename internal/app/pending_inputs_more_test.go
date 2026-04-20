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

type pendingInputFeishuStub struct {
	*fakeFeishuClient
	downloadPath string
	added        []string
	removed      []string
}

func (s *pendingInputFeishuStub) DownloadMessageResource(_ context.Context, _ string, attachment feishu.Attachment, _ string) (string, string, error) {
	return s.downloadPath, filepath.Base(s.downloadPath), nil
}

func (s *pendingInputFeishuStub) AddReaction(_ context.Context, messageID, emoji string) error {
	s.added = append(s.added, messageID+":"+emoji)
	return nil
}

func (s *pendingInputFeishuStub) RemoveReaction(_ context.Context, messageID, emoji string) error {
	s.removed = append(s.removed, messageID+":"+emoji)
	return nil
}

func TestPendingInputHelpersAndStageImages(t *testing.T) {
	store, err := state.Open(t.TempDir() + "/state.json")
	if err != nil {
		t.Fatalf("Open(store) error = %v", err)
	}
	cfg := config.Default()
	cfg.Workspaces[0].Cwd = t.TempDir()
	stub := &pendingInputFeishuStub{
		fakeFeishuClient: &fakeFeishuClient{},
		downloadPath:     filepath.Join(t.TempDir(), "image.png"),
	}
	if err := os.WriteFile(stub.downloadPath, []byte("png"), 0o644); err != nil {
		t.Fatalf("WriteFile(downloadPath) error = %v", err)
	}
	a := &App{cfg: cfg, store: store, feishu: stub}

	msg := &feishu.InboundMessage{
		MessageID:     "img-1",
		ChatID:        "chat-1",
		ChatType:      "group",
		UserID:        "user-1",
		RootMessageID: "root-1",
		Attachments:   []feishu.Attachment{{Kind: "image", ResourceKey: "img-key"}},
	}
	if !a.shouldStageInboundImages(msg) {
		t.Fatal("shouldStageInboundImages() should accept image-only message")
	}
	if a.shouldStageInboundImages(&feishu.InboundMessage{Text: "hello", Attachments: msg.Attachments}) {
		t.Fatal("shouldStageInboundImages() should reject text+image message")
	}

	if err := a.stageInboundImagesForSession(msg, "sess-1"); err != nil {
		t.Fatalf("stageInboundImagesForSession() error = %v", err)
	}
	sess := a.store.GetSession("sess-1")
	if sess == nil || len(sess.StagedImages) != 1 || sess.Status != "queued" {
		t.Fatalf("session after stage = %+v", sess)
	}
	if len(stub.added) == 0 || stub.added[0] != "img-1:"+queueReactionEmoji {
		t.Fatalf("added reactions = %+v, want queue reaction", stub.added)
	}

	images := []state.SessionStagedImage{
		{SourceMessageID: "img-1", RootMessageID: "root-1", Name: "a.png", LocalPath: "/tmp/a.png"},
		{SourceMessageID: "img-2", RootMessageID: "", Name: "b.png", LocalPath: "/tmp/b.png"},
	}
	if got := stagedImageAttachments(images); len(got) != 2 || got[0].Kind != "image" {
		t.Fatalf("stagedImageAttachments() = %+v", got)
	}
	if got := stagedImageSourceMessageIDs(images); len(got) != 2 {
		t.Fatalf("stagedImageSourceMessageIDs() = %+v", got)
	}
	if got := stagedImageRootMessageIDs(images); len(got) != 2 || got[1] != "img-2" {
		t.Fatalf("stagedImageRootMessageIDs() = %+v", got)
	}

	sub := &state.Submission{TriggerMessageID: "msg-1", SourceMessageIDs: []string{"img-1", "msg-1"}}
	if !submissionHasSourceMessage(sub, "img-1") {
		t.Fatal("submissionHasSourceMessage() should find source message")
	}
	if got := sourceMessageIDsForSubmission(sub); len(got) != 2 {
		t.Fatalf("sourceMessageIDsForSubmission() = %+v", got)
	}
	if got := uniqueStrings([]string{" a ", "a", "", "b"}); len(got) != 2 {
		t.Fatalf("uniqueStrings() = %+v", got)
	}
	if got := removeString([]string{"a", "b", "b"}, "b"); len(got) != 2 || got[1] != "b" {
		t.Fatalf("removeString() = %+v", got)
	}
}

func TestPendingInputReactionWrappersAndDiscardSession(t *testing.T) {
	store, err := state.Open(t.TempDir() + "/state.json")
	if err != nil {
		t.Fatalf("Open(store) error = %v", err)
	}
	stub := &pendingInputFeishuStub{fakeFeishuClient: &fakeFeishuClient{}}
	a := &App{store: store, feishu: stub}

	if err := a.store.UpsertSession(&state.Session{
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
	if _, err := a.store.CreateSubmission(&state.Submission{
		ID:               "sub-1",
		SessionKey:       "sess-1",
		WorkspaceID:      "default",
		TriggerMessageID: "msg-1",
		SourceMessageIDs: []string{"src-1"},
		Status:           "queued",
	}); err != nil {
		t.Fatalf("CreateSubmission() error = %v", err)
	}

	sub := a.store.GetSubmission("sub-1")
	a.markSubmissionQueuedReactions(sub)
	a.markSubmissionRunningReactions(sub)
	a.clearSubmissionProcessingReactions(sub)
	if len(stub.added) == 0 || len(stub.removed) == 0 {
		t.Fatalf("reaction wrappers did not call feishu client: added=%+v removed=%+v", stub.added, stub.removed)
	}

	if discarded := a.discardSessionPendingInputs("sess-1"); discarded != 2 {
		t.Fatalf("discardSessionPendingInputs() = %d, want 2", discarded)
	}
	sess := a.store.GetSession("sess-1")
	if sess == nil || sess.Status != "idle" || len(sess.Queue) != 0 || len(sess.StagedImages) != 0 {
		t.Fatalf("session after discard = %+v", sess)
	}

	session := &state.Session{Status: "queued", StagedImages: []state.SessionStagedImage{{SourceMessageID: "img-2"}}}
	if !discardStagedImageByMessageID(session, "img-2") || session.Status != "idle" {
		t.Fatalf("discardStagedImageByMessageID() = %+v", session)
	}
}

func TestDiscardQueuedSubmissionFromSessionSnapshotPreservesCurrentSessionState(t *testing.T) {
	store, err := state.Open(t.TempDir() + "/state.json")
	if err != nil {
		t.Fatalf("Open(store) error = %v", err)
	}
	a := &App{store: store}

	if err := a.store.UpsertSession(&state.Session{
		Key:                "sess-1",
		WorkspaceID:        "default",
		ActiveThreadID:     "thread-1",
		ActiveTurnID:       "turn-running",
		ActiveSubmissionID: "sub-running",
		Status:             "queued",
		Queue:              []string{"sub-queued"},
		ActiveOperations: []state.SessionActiveOperation{
			{
				Kind:         sessionOpKindSubmission,
				SubmissionID: "sub-running",
				ThreadID:     "thread-1",
				TurnID:       "turn-running",
				StartedAt:    time.Now().Unix(),
			},
		},
	}); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}
	if _, err := a.store.CreateSubmission(&state.Submission{
		ID:               "sub-queued",
		SessionKey:       "sess-1",
		WorkspaceID:      "default",
		TriggerMessageID: "msg-queued",
		SourceMessageIDs: []string{"msg-queued"},
		Status:           "queued",
	}); err != nil {
		t.Fatalf("CreateSubmission() error = %v", err)
	}

	staleSnapshot := a.store.GetSession("sess-1")
	queuedSub := a.store.GetSubmission("sub-queued")
	if staleSnapshot == nil || queuedSub == nil {
		t.Fatalf("missing stale snapshot or queued submission: %+v / %+v", staleSnapshot, queuedSub)
	}

	if _, err := a.store.UpdateSession("sess-1", func(current *state.Session) {
		sessionResetActiveOperations(current)
		current.Status = "idle"
	}); err != nil {
		t.Fatalf("UpdateSession() error = %v", err)
	}

	if !a.discardQueuedSubmissionFromSessionSnapshot(staleSnapshot, "sub-queued", queuedSub) {
		t.Fatal("discardQueuedSubmissionFromSessionSnapshot() should discard queued submission")
	}

	sess := a.store.GetSession("sess-1")
	if sess == nil {
		t.Fatal("expected session")
	}
	if sessionHasInFlightSubmission(sess) {
		t.Fatalf("session should not restore in-flight state: %+v", sess)
	}
	if len(sess.Queue) != 0 || sess.Status != "idle" {
		t.Fatalf("session after discard = %+v, want idle with empty queue", sess)
	}
	if got := a.store.GetSubmission("sub-queued"); got != nil {
		t.Fatalf("queued submission after discard = %+v, want runtime cleanup", got)
	}
}
