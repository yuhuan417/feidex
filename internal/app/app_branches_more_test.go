package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"feidex/internal/codexrpc"
	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

type appDownloadFeishuStub struct {
	*fakeFeishuClient
	downloadPath string
}

func (s *appDownloadFeishuStub) DownloadMessageResource(context.Context, string, feishu.Attachment, string) (string, string, error) {
	return s.downloadPath, filepath.Base(s.downloadPath), nil
}

func TestHandleFeishuMessageAdditionalBranches(t *testing.T) {
	cfg := config.Default()
	cfg.Workspaces[0].Cwd = t.TempDir()
	store, err := state.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("Open(store) error = %v", err)
	}
	downloadPath := filepath.Join(t.TempDir(), "image.png")
	if err := os.WriteFile(downloadPath, []byte("png"), 0o644); err != nil {
		t.Fatalf("WriteFile(download) error = %v", err)
	}
	ff := &appDownloadFeishuStub{fakeFeishuClient: &fakeFeishuClient{}, downloadPath: downloadPath}
	fc := &fakeCodexClient{}
	a := &App{
		cfg:           cfg,
		store:         store,
		feishu:        ff,
		codex:         fc,
		started:       time.Now(),
		deduper:       newInboundDeduper(),
		turnStreams:   map[string]*turnStream{},
		liveThreads:   map[string]string{},
		statusFlushCh: make(chan struct{}, 1),
	}

	a.handleFeishuMessage(&feishu.InboundMessage{MessageID: "stale", CreatedAt: a.started.Add(-time.Minute).Unix()})
	if a.store.GetSession("feishu:p2p::") != nil {
		t.Fatal("stale message should be ignored")
	}

	_ = a.deduper.Claim("dup")
	a.handleFeishuMessage(&feishu.InboundMessage{MessageID: "dup"})

	sessionKey := "feishu:p2p:chat:user"
	if err := a.store.UpsertSession(&state.Session{
		Key:            sessionKey,
		WorkspaceID:    "default",
		ChatID:         "chat",
		ChatType:       "p2p",
		OwnerUserID:    "user",
		ActiveThreadID: "thread-1",
		ActiveTurnID:   "turn-1",
		Status:         "turn_in_progress",
	}); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}
	if err := a.store.UpsertPending(&state.PendingRequest{
		ID:          "append-1",
		Kind:        "turn_append",
		SessionKey:  sessionKey,
		ThreadID:    "thread-1",
		TurnID:      "turn-1",
		OwnerUserID: "user",
		Status:      "pending",
	}); err != nil {
		t.Fatalf("UpsertPending() error = %v", err)
	}
	calledSteer := false
	fc.callHook = func(_ context.Context, method string, _ any, _ any) error {
		if method == "turn/steer" {
			calledSteer = true
		}
		return nil
	}
	a.handleFeishuMessage(&feishu.InboundMessage{MessageID: "pending", ChatID: "chat", ChatType: "p2p", UserID: "user", Text: "append"})
	if !calledSteer {
		t.Fatal("pending text response should steer turn")
	}

	ff.replyCards = nil
	a.handleFeishuMessage(&feishu.InboundMessage{MessageID: "cmd", ChatID: "chat", ChatType: "p2p", UserID: "user", Text: "/menu"})
	if len(ff.replyCards) == 0 {
		t.Fatal("local command should render reply card")
	}

	a.handleFeishuMessage(&feishu.InboundMessage{MessageID: "img", ChatID: "chat", ChatType: "p2p", UserID: "user", Attachments: []feishu.Attachment{{Kind: "image", ResourceKey: "img"}}})
	sess := a.store.GetSession(sessionKey)
	if sess == nil || len(sess.StagedImages) == 0 {
		t.Fatalf("image-only message should be staged: %+v", sess)
	}

	a.handleFeishuMessage(&feishu.InboundMessage{MessageID: "empty", ChatID: "chat", ChatType: "p2p", UserID: "user"})

	bad := &App{
		cfg:           &config.Config{},
		store:         store,
		feishu:        ff,
		codex:         fc,
		started:       time.Now(),
		turnStreams:   map[string]*turnStream{},
		liveThreads:   map[string]string{},
		statusFlushCh: make(chan struct{}, 1),
	}
	bad.handleFeishuMessage(&feishu.InboundMessage{
		MessageID:   "bad-attach",
		ChatID:      "chat",
		ChatType:    "p2p",
		UserID:      "user",
		Text:        "hello",
		Attachments: []feishu.Attachment{{Kind: "file", ResourceKey: "file"}},
	})
	if len(ff.replyTexts) == 0 && len(ff.sentTexts) == 0 {
		t.Fatal("attachment error should trigger replyError fallback")
	}
}

func TestStartNextSubmissionAdditionalBranches(t *testing.T) {
	a, _, fc := newTestApp(t)

	if err := a.startNextSubmission("missing"); err != nil {
		t.Fatalf("startNextSubmission(missing) error = %v", err)
	}
	if got := (&App{cfg: &config.Config{}}).defaultWorkspaceID(); got != "default" {
		t.Fatalf("defaultWorkspaceID() = %q, want default", got)
	}
	if got := nonZero(0, 0); got != 0 {
		t.Fatalf("nonZero(all zero) = %d, want 0", got)
	}
	a.handleBotMenu(nil)

	sessionKey := "sess-err"
	if err := a.store.UpsertSession(&state.Session{
		Key:         sessionKey,
		WorkspaceID: "missing",
		ChatID:      "chat-1",
		ChatType:    "group",
		Status:      "idle",
		Queue:       []string{"sub-missing"},
	}); err != nil {
		t.Fatalf("UpsertSession(missing) error = %v", err)
	}
	if _, err := a.store.CreateSubmission(&state.Submission{ID: "sub-missing", SessionKey: sessionKey, WorkspaceID: "missing", Status: "queued"}); err != nil {
		t.Fatalf("CreateSubmission(sub-missing) error = %v", err)
	}
	if err := a.startNextSubmission(sessionKey); err == nil {
		t.Fatal("expected missing workspace to fail")
	}

	sessionKey = "sess-resume"
	if err := a.store.UpsertSession(&state.Session{
		Key:                     sessionKey,
		WorkspaceID:             "default",
		ChatID:                  "chat-1",
		ChatType:                "group",
		Status:                  "idle",
		ActiveThreadID:          "thread-old",
		ActiveThreadWorkspaceID: "default",
		Queue:                   []string{"sub-1"},
	}); err != nil {
		t.Fatalf("UpsertSession(resume) error = %v", err)
	}
	if _, err := a.store.CreateSubmission(&state.Submission{ID: "sub-1", SessionKey: sessionKey, WorkspaceID: "default", InputText: "hello", TriggerMessageID: "m-1", Status: "queued"}); err != nil {
		t.Fatalf("CreateSubmission(sub-1) error = %v", err)
	}
	fc.callHook = func(_ context.Context, method string, _ any, out any) error {
		switch method {
		case "thread/resume":
			return errors.New("resume failed")
		case "thread/start":
			result := out.(*codexrpc.ThreadStartResult)
			result.Thread.ID = "thread-new"
			return nil
		case "turn/start":
			result := out.(*codexrpc.TurnStartResult)
			result.Turn.ID = "turn-new"
			return nil
		default:
			return nil
		}
	}
	if err := a.startNextSubmission(sessionKey); err != nil {
		t.Fatalf("startNextSubmission(resume fallback) error = %v", err)
	}
	if sess := a.store.GetSession(sessionKey); sess == nil || sess.ActiveThreadID != "thread-new" || sess.ActiveTurnID != "turn-new" {
		t.Fatalf("session after resume fallback = %+v", sess)
	}

	sessionKey = "sess-timeout"
	if err := a.store.UpsertSession(&state.Session{
		Key:         sessionKey,
		WorkspaceID: "default",
		ChatID:      "chat-1",
		ChatType:    "group",
		Status:      "idle",
		Queue:       []string{"sub-2"},
	}); err != nil {
		t.Fatalf("UpsertSession(timeout) error = %v", err)
	}
	if _, err := a.store.CreateSubmission(&state.Submission{ID: "sub-2", SessionKey: sessionKey, WorkspaceID: "default", InputText: "hello", TriggerMessageID: "m-2", Status: "queued"}); err != nil {
		t.Fatalf("CreateSubmission(sub-2) error = %v", err)
	}
	fc.callHook = func(_ context.Context, method string, _ any, out any) error {
		switch method {
		case "thread/start":
			result := out.(*codexrpc.ThreadStartResult)
			result.Thread.ID = "thread-timeout"
			return nil
		case "turn/start":
			return context.DeadlineExceeded
		default:
			return nil
		}
	}
	if err := a.startNextSubmission(sessionKey); err != nil {
		t.Fatalf("startNextSubmission(timeout) error = %v", err)
	}
	if sess := a.store.GetSession(sessionKey); sess == nil || sess.ActiveSubmissionID != "sub-2" || sess.Status != "turn_starting" {
		t.Fatalf("session after timeout = %+v", sess)
	}
}
