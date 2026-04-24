package app

import (
	"testing"
	"time"

	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

func TestEnqueueSubmissionBindsStagedImagesToNextText(t *testing.T) {
	store, err := state.Open(t.TempDir() + "/state.json")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	cfg := config.Default()
	cfg.Workspaces[0].Cwd = t.TempDir()
	a := &App{cfg: cfg, store: store}

	sessionKey := "feishu:p2p:chat:user"
	if err := a.store.UpsertSession(&state.Session{
		Key:                sessionKey,
		WorkspaceID:        "default",
		ChatID:             "chat",
		ChatType:           "p2p",
		OwnerUserID:        "user",
		ActiveSubmissionID: "sub-running",
		Status:             "queued",
		StagedImages: []state.SessionStagedImage{
			{
				SourceMessageID: "img-1",
				Name:            "image.png",
				LocalPath:       "/tmp/image.png",
				CreatedAt:       time.Now().Unix(),
			},
		},
	}); err != nil {
		t.Fatalf("upsert session: %v", err)
	}

	if err := a.enqueueSubmission(&feishu.InboundMessage{
		MessageID: "msg-2",
		ChatID:    "chat",
		ChatType:  "p2p",
		UserID:    "user",
		Text:      "describe this image",
	}); err != nil {
		t.Fatalf("enqueueSubmission: %v", err)
	}

	sess := a.store.GetSession(sessionKey)
	if sess == nil {
		t.Fatal("expected session")
	}
	if len(sess.StagedImages) != 0 {
		t.Fatalf("expected staged images to be consumed, got %#v", sess.StagedImages)
	}
	if len(sess.Queue) != 1 {
		t.Fatalf("expected one queued submission, got %#v", sess.Queue)
	}

	sub := a.store.GetSubmission(sess.Queue[0])
	if sub == nil {
		t.Fatal("expected queued submission")
	}
	if sub.TriggerMessageID != "msg-2" {
		t.Fatalf("unexpected trigger message id: %#v", sub)
	}
	if len(sub.Attachments) != 1 || sub.Attachments[0].LocalPath != "/tmp/image.png" {
		t.Fatalf("expected staged image attachment to be bound, got %#v", sub.Attachments)
	}
	if len(sub.SourceMessageIDs) != 2 || sub.SourceMessageIDs[0] != "msg-2" || sub.SourceMessageIDs[1] != "img-1" {
		t.Fatalf("unexpected source message ids: %#v", sub.SourceMessageIDs)
	}
}

func TestDiscardPendingInputByMessageIDCancelsStagedAndQueuedInputs(t *testing.T) {
	store, err := state.Open(t.TempDir() + "/state.json")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	a := &App{store: store}

	sessionKey := "feishu:p2p:chat:user"
	if err := a.store.UpsertSession(&state.Session{
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
		t.Fatalf("upsert session: %v", err)
	}
	if _, err := a.store.CreateSubmission(&state.Submission{
		ID:               "sub-1",
		SessionKey:       sessionKey,
		WorkspaceID:      "default",
		TriggerMessageID: "msg-queued",
		SourceMessageIDs: []string{"msg-queued", "img-bound"},
		Status:           "queued",
	}); err != nil {
		t.Fatalf("create submission: %v", err)
	}

	if !newPendingQueueService(a).discardPendingInputByMessageID("img-staged") {
		t.Fatal("expected staged image discard to succeed")
	}
	sess := a.store.GetSession(sessionKey)
	if sess == nil || len(sess.StagedImages) != 0 {
		t.Fatalf("expected staged images to be removed, got %#v", sess)
	}

	if !newPendingQueueService(a).discardPendingInputByMessageID("msg-queued") {
		t.Fatal("expected queued submission discard to succeed")
	}
	sess = a.store.GetSession(sessionKey)
	if sess == nil {
		t.Fatal("expected session")
	}
	if len(sess.Queue) != 0 {
		t.Fatalf("expected queue to be cleared, got %#v", sess.Queue)
	}
	if sess.Status != "idle" {
		t.Fatalf("expected session to become idle, got %#v", sess)
	}
	sub := a.store.GetSubmission("sub-1")
	if sub != nil {
		t.Fatalf("expected queued submission to be released from runtime store, got %#v", sub)
	}
}
