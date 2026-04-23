package app

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"feidex/internal/codexrpc"
	"feidex/internal/config"
	"feidex/internal/state"
)

func waitForTestCondition(t *testing.T, label string, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", label)
}

func TestHandleCodexTransportErrorRecoversRuntimeAndResumesQueuedSubmission(t *testing.T) {
	a, _, fc := newTestApp(t)
	a.configureCodexClientRuntime(fc)

	sessionKey := "sess-transport-recovery"
	activeSub := seedActiveSubmission(t, a, sessionKey, "thread-dead", "turn-dead")
	if _, err := a.store.UpdateSession(sessionKey, func(sess *state.Session) {
		sess.ActiveThreadWorkspaceID = a.cfg.Workspaces[0].ID
	}); err != nil {
		t.Fatalf("UpdateSession(active) error = %v", err)
	}

	queuedID, err := a.store.CreateSubmission(&state.Submission{
		ID:               "sub-queued",
		SessionKey:       sessionKey,
		WorkspaceID:      a.cfg.Workspaces[0].ID,
		UserID:           "user-1",
		ChatID:           "chat-1",
		TriggerMessageID: "trigger-queued",
		InputText:        "next task",
		Status:           "queued",
	})
	if err != nil {
		t.Fatalf("CreateSubmission(queued) error = %v", err)
	}
	if err := a.appState().queueSubmission(sessionKey, queuedID); err != nil {
		t.Fatalf("queueSubmission() error = %v", err)
	}

	blockStart := make(chan struct{})
	var promotedCallsMu sync.Mutex
	promotedCalls := []string{}
	promoted := &fakeCodexClient{
		startHook: func(context.Context, bool) error {
			<-blockStart
			return nil
		},
		callHook: func(_ context.Context, method string, _ any, out any) error {
			promotedCallsMu.Lock()
			promotedCalls = append(promotedCalls, method)
			promotedCallsMu.Unlock()
			switch method {
			case "model/list":
				out.(*codexrpc.ModelListResult).Data = []codexrpc.ModelListEntry{{ID: "gpt-5.4"}}
				return nil
			case "thread/start":
				out.(*codexrpc.ThreadStartResult).Thread.ID = "thread-recovered"
				out.(*codexrpc.ThreadStartResult).Thread.Name = "Recovered"
				out.(*codexrpc.ThreadStartResult).Thread.Preview = "preview"
				return nil
			case "turn/start":
				out.(*codexrpc.TurnStartResult).Turn.ID = "turn-recovered"
				return nil
			default:
				t.Fatalf("unexpected promoted client method: %s", method)
				return nil
			}
		},
	}

	origNewCodex := newCodexClient
	newCodexClient = func(config.CodexConfig) codexClient { return promoted }
	defer func() { newCodexClient = origNewCodex }()

	if fc.onError == nil {
		t.Fatal("expected configured codex error handler")
	}
	fc.onError(errors.New("stdio EOF"))

	waitForTestCondition(t, "codex runtime recovery to start", func() bool {
		return a.codexRuntimeRecovering()
	})
	waitForTestCondition(t, "active submission failure cleanup", func() bool {
		return a.store.GetSubmission(activeSub.ID) == nil
	})

	sess := a.store.GetSession(sessionKey)
	if sess == nil {
		t.Fatal("expected session after transport failure")
	}
	if len(sess.Queue) != 1 || sess.Queue[0] != queuedID {
		t.Fatalf("queue during recovery = %+v, want [%s]", sess.Queue, queuedID)
	}

	close(blockStart)

	waitForTestCondition(t, "codex runtime recovery to finish", func() bool {
		current, ok := a.codex.(*fakeCodexClient)
		return ok && current == promoted && !a.codexRuntimeRecovering()
	})
	waitForTestCondition(t, "queued submission to start on recovered runtime", func() bool {
		sub := a.store.GetSubmission(queuedID)
		return sub != nil && sub.Status == "running" && sub.ThreadID == "thread-recovered" && sub.TurnID == "turn-recovered"
	})

	if !fc.closed {
		t.Fatal("failed codex client should be closed after recovery")
	}
	if !promoted.started || promoted.closed {
		t.Fatalf("promoted runtime = %+v, want started open client", promoted)
	}
	if !a.sessionHasLiveThread(sessionKey, "thread-recovered") {
		t.Fatal("expected recovered thread to be marked live")
	}

	promotedCallsMu.Lock()
	defer promotedCallsMu.Unlock()
	if len(promotedCalls) < 3 || promotedCalls[0] != "model/list" {
		t.Fatalf("promoted client calls = %+v, want model/list followed by submission startup", promotedCalls)
	}
}

func TestStartNextSubmissionDefersWhileCodexRuntimeRecovering(t *testing.T) {
	a, _, _ := newTestApp(t)
	a.codexRecovering = true

	sessionKey := "sess-recovering"
	if err := a.store.UpsertSession(&state.Session{
		Key:         sessionKey,
		WorkspaceID: a.cfg.Workspaces[0].ID,
		Status:      "queued",
	}); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}
	subID, err := a.store.CreateSubmission(&state.Submission{
		ID:               "sub-1",
		SessionKey:       sessionKey,
		WorkspaceID:      a.cfg.Workspaces[0].ID,
		UserID:           "user-1",
		ChatID:           "chat-1",
		TriggerMessageID: "trigger-1",
		InputText:        "hello",
		Status:           "queued",
	})
	if err != nil {
		t.Fatalf("CreateSubmission() error = %v", err)
	}
	if err := a.appState().queueSubmission(sessionKey, subID); err != nil {
		t.Fatalf("queueSubmission() error = %v", err)
	}

	if err := newSubmissionWorkflow(a).startNextSubmissionWithFailureNotice(sessionKey, true); err != nil {
		t.Fatalf("startNextSubmissionWithFailureNotice() error = %v", err)
	}

	sess := a.store.GetSession(sessionKey)
	if sess == nil {
		t.Fatal("expected session to remain")
	}
	if len(sess.Queue) != 1 || sess.Queue[0] != subID {
		t.Fatalf("queue after deferred start = %+v, want [%s]", sess.Queue, subID)
	}
	sub := a.store.GetSubmission(subID)
	if sub == nil || sub.Status != "queued" {
		t.Fatalf("submission after deferred start = %+v, want queued", sub)
	}
}
