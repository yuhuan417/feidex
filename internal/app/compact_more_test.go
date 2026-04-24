package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"feidex/internal/feishu"
	"feidex/internal/state"
)

type blockingClaudeCompactCore struct {
	*fakeClaudeCore
	started chan struct{}
	release chan struct{}
}

func (f *blockingClaudeCompactCore) StartTurn(_ context.Context, sessionKey, threadID, turnID, prompt string) error {
	if f.fakeClaudeCore == nil {
		f.fakeClaudeCore = &fakeClaudeCore{}
	}
	f.mu.Lock()
	f.startTurnCalls = append(f.startTurnCalls, fakeClaudeStartTurnCall{
		sessionKey: sessionKey,
		threadID:   threadID,
		turnID:     turnID,
		prompt:     prompt,
	})
	err := f.startTurnErr
	f.mu.Unlock()
	select {
	case f.started <- struct{}{}:
	default:
	}
	if f.release != nil {
		<-f.release
	}
	return err
}

func TestStandaloneCompactionLifecycle(t *testing.T) {
	a, ff, fc := newTestApp(t)
	sessionKey := "sess-compact"
	if err := a.store.UpsertSession(&state.Session{
		Key:            sessionKey,
		WorkspaceID:    a.cfg.Workspaces[0].ID,
		ActiveThreadID: "thread-1",
		ChatID:         "chat-1",
		ChatType:       "group",
		RootMessageID:  "root-1",
		Status:         "idle",
	}); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}

	fc.callHook = func(_ context.Context, method string, params any, _ any) error {
		if method != "thread/compact/start" {
			t.Fatalf("unexpected Call method %q", method)
		}
		payload, _ := params.(map[string]any)
		if got := strings.TrimSpace(stringValue(payload["threadId"])); got != "thread-1" {
			t.Fatalf("thread/compact/start threadId = %q", got)
		}
		return nil
	}

	sess, err := startThreadCompaction(a, sessionKey)
	if err != nil {
		t.Fatalf("startThreadCompaction() error = %v", err)
	}
	if sess == nil || sess.Status != sessionStatusCompacting {
		t.Fatalf("startThreadCompaction() = %+v", sess)
	}
	if !noteStandaloneCompactItemStarted(a, "thread-1", "turn-1", map[string]any{
		"id":   "item-compact",
		"type": "contextCompaction",
	}) {
		t.Fatal("noteStandaloneCompactItemStarted() should succeed")
	}
	if updated := a.store.GetSession(sessionKey); updated == nil || updated.ActiveTurnID != "turn-1" || updated.Status != sessionStatusCompacting {
		t.Fatalf("session after bind = %+v", updated)
	}
	if !completeStandaloneCompactItem(a, "thread-1", "turn-1", map[string]any{
		"id":     "item-compact",
		"type":   "contextCompaction",
		"status": "completed",
	}) {
		t.Fatal("completeStandaloneCompactItem() should succeed")
	}
	if updated := a.store.GetSession(sessionKey); updated == nil || updated.ActiveTurnID != "" || updated.Status != "idle" {
		t.Fatalf("session after finish = %+v", updated)
	}
	if len(ff.replyTexts) != 1 || ff.replyTexts[0] != "当前线程上下文已压缩完成。" {
		t.Fatalf("completeStandaloneCompactItem() notices = %#v", ff.replyTexts)
	}
}

func TestStandaloneCompactionFailureBranches(t *testing.T) {
	if sessionHasActiveWork(nil) {
		t.Fatal("sessionHasActiveWork(nil) should be false")
	}
	if !sessionHasActiveWork(&state.Session{Status: sessionStatusCompacting}) {
		t.Fatal("sessionHasActiveWork(compacting) should be true")
	}
	if !sessionHasActiveWork(&state.Session{ActiveSubmissionID: "sub-1"}) {
		t.Fatal("sessionHasActiveWork(active submission) should be true")
	}
	if sessionHasActiveWork(&state.Session{Status: "idle"}) {
		t.Fatal("sessionHasActiveWork(idle) should be false")
	}
	if got := standaloneCompactResultText("unknown"); got != "" {
		t.Fatalf("standaloneCompactResultText(unknown) = %q", got)
	}

	a, ff, fc := newTestApp(t)
	if _, err := startThreadCompaction(a, "missing"); err == nil || !strings.Contains(err.Error(), "当前没有活动线程") {
		t.Fatalf("startThreadCompaction(missing) error = %v", err)
	}

	if err := a.store.UpsertSession(&state.Session{
		Key:                "sess-busy",
		WorkspaceID:        a.cfg.Workspaces[0].ID,
		ActiveThreadID:     "thread-busy",
		ActiveSubmissionID: "sub-1",
		Status:             "idle",
	}); err != nil {
		t.Fatalf("UpsertSession(sess-busy) error = %v", err)
	}
	if _, err := startThreadCompaction(a, "sess-busy"); err == nil || !strings.Contains(err.Error(), "当前任务仍在运行") {
		t.Fatalf("startThreadCompaction(busy) error = %v", err)
	}

	if err := a.store.UpsertSession(&state.Session{
		Key:            "sess-restore",
		WorkspaceID:    a.cfg.Workspaces[0].ID,
		ActiveThreadID: "thread-restore",
		ChatID:         "chat-restore",
		Status:         "waiting",
	}); err != nil {
		t.Fatalf("UpsertSession(sess-restore) error = %v", err)
	}
	fc.callErr = errors.New("compact boom")
	if _, err := startThreadCompaction(a, "sess-restore"); err == nil || !strings.Contains(err.Error(), "compact boom") {
		t.Fatalf("startThreadCompaction(restore) error = %v", err)
	}
	if updated := a.store.GetSession("sess-restore"); updated == nil || updated.Status != "waiting" {
		t.Fatalf("session after restore = %+v", updated)
	}

	if err := a.store.UpsertSession(&state.Session{
		Key:            "sess-fail",
		WorkspaceID:    a.cfg.Workspaces[0].ID,
		ActiveThreadID: "thread-fail",
		ActiveTurnID:   "turn-fail",
		ChatID:         "chat-fail",
		ChatType:       "group",
		RootMessageID:  "root-fail",
		Status:         sessionStatusCompacting,
	}); err != nil {
		t.Fatalf("UpsertSession(sess-fail) error = %v", err)
	}
	if !failStandaloneCompactTurn(a, "thread-fail", "", "boom") {
		t.Fatal("failStandaloneCompactTurn() should succeed")
	}
	if updated := a.store.GetSession("sess-fail"); updated == nil || updated.ActiveTurnID != "" || updated.Status != "idle" {
		t.Fatalf("session after fail = %+v", updated)
	}
	if len(ff.replyTexts) == 0 || !strings.Contains(ff.replyTexts[len(ff.replyTexts)-1], "boom") {
		t.Fatalf("failStandaloneCompactTurn() notices = %#v", ff.replyTexts)
	}

	if err := a.store.UpsertSession(&state.Session{
		Key:            "sess-complete",
		WorkspaceID:    a.cfg.Workspaces[0].ID,
		ActiveThreadID: "thread-complete",
		ActiveTurnID:   "turn-complete",
		ChatID:         "chat-complete",
		Status:         sessionStatusCompacting,
	}); err != nil {
		t.Fatalf("UpsertSession(sess-complete) error = %v", err)
	}
	if !completeStandaloneCompactTurn(a, "thread-complete", "") {
		t.Fatal("completeStandaloneCompactTurn() should succeed")
	}
	if updated := a.store.GetSession("sess-complete"); updated == nil || updated.ActiveTurnID != "" || updated.Status != "idle" {
		t.Fatalf("session after complete = %+v", updated)
	}
	if len(ff.sentTexts) == 0 || ff.sentTexts[len(ff.sentTexts)-1] != "当前线程上下文已压缩完成。" {
		t.Fatalf("completeStandaloneCompactTurn() sentTexts = %#v", ff.sentTexts)
	}

	sendStandaloneCompactResult(a, &state.Session{
		ChatID: "chat-interrupted",
	}, "interrupted")
	sendStandaloneCompactResult(a, &state.Session{
		ChatID: "chat-failed",
	}, "failed")
	if len(ff.sentTexts) < 3 || ff.sentTexts[len(ff.sentTexts)-2] != "当前线程上下文压缩已中断。" || ff.sentTexts[len(ff.sentTexts)-1] != "当前线程上下文压缩失败。" {
		t.Fatalf("sendStandaloneCompactResult() sentTexts = %#v", ff.sentTexts)
	}

	restoreStandaloneCompactSession(a, "sess-complete", "thread-other", "idle")
	restoreStandaloneCompactSession(a, "missing", "thread-missing", "idle")
}

func TestCompleteMenuCompactCodexAcksImmediatelyAndPatchesAcceptedCard(t *testing.T) {
	a, ff, fc := newTestApp(t)
	sessionKey := "feishu:p2p:chat:user"
	if err := a.store.UpsertSession(&state.Session{
		Key:            sessionKey,
		WorkspaceID:    a.cfg.Workspaces[0].ID,
		ActiveThreadID: "thread-1",
		ChatID:         "chat",
		ChatType:       "p2p",
		OwnerUserID:    "user",
		Status:         "idle",
	}); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}

	started := make(chan struct{}, 1)
	release := make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	fc.callHook = func(_ context.Context, method string, params any, _ any) error {
		if method != "thread/compact/start" {
			t.Fatalf("unexpected Call method %q", method)
		}
		payload, _ := params.(map[string]any)
		if got := strings.TrimSpace(stringValue(payload["threadId"])); got != "thread-1" {
			t.Fatalf("thread/compact/start threadId = %q", got)
		}
		started <- struct{}{}
		<-release
		return nil
	}

	resp, err := newMenuActionService(a).completeMenuCompact(&feishu.CardAction{
		MessageID: "card-1",
		ChatID:    "chat",
		UserID:    "user",
		ActionValue: map[string]any{
			"session_key":   sessionKey,
			"parent_action": "menu.tools",
		},
	}, sessionKey)
	if err != nil {
		t.Fatalf("completeMenuCompact() error = %v", err)
	}
	if resp.Toast == nil || resp.Toast.Type != "info" || !strings.Contains(resp.Toast.Content, "正在请求压缩") {
		t.Fatalf("completeMenuCompact() toast = %#v", resp.Toast)
	}
	if resp.Card == nil {
		t.Fatal("completeMenuCompact() should return preparing card")
	}
	if body := cardMarkdownContent(t, resp.Card.Data.(map[string]any)); !strings.Contains(body, "正在请求当前线程上下文压缩") {
		t.Fatalf("preparing card body = %q", body)
	}

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("thread/compact/start was not started asynchronously")
	}
	if patchedCards := ff.patchedCardsSnapshot(); len(patchedCards) != 0 {
		t.Fatalf("patchedCards before compact finishes = %+v, want none", patchedCards)
	}

	close(release)
	deadline := time.Now().Add(2 * time.Second)
	for len(ff.patchedCardsSnapshot()) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	patchedCards := ff.patchedCardsSnapshot()
	if len(patchedCards) == 0 {
		t.Fatal("expected accepted compact card patch")
	}
	body := cardMarkdownContent(t, patchedCards[len(patchedCards)-1])
	if !strings.Contains(body, "已提交 `/compact`") {
		t.Fatalf("accepted patched card body = %q", body)
	}
	if !cardHasButtonText(patchedCards[len(patchedCards)-1], "返回常用工具") {
		t.Fatalf("accepted patched card missing return button: %#v", patchedCards[len(patchedCards)-1])
	}
	if sess := a.store.GetSession(sessionKey); sess == nil || sess.Status != sessionStatusCompacting {
		t.Fatalf("session after compact ack = %+v", sess)
	}
}

func TestCompleteMenuCompactClaudeAcksImmediatelyAndPatchesAcceptedCard(t *testing.T) {
	a, ff, _ := newTestApp(t)
	a.backend = backendClaude
	a.cfg.Feishu.Backend = backendClaude
	a.codex = nil
	claude := &blockingClaudeCompactCore{
		fakeClaudeCore: &fakeClaudeCore{},
		started:        make(chan struct{}, 1),
		release:        make(chan struct{}),
	}
	defer func() {
		select {
		case <-claude.release:
		default:
			close(claude.release)
		}
	}()
	a.claude = claude

	sessionKey := "feishu:p2p:chat:user"
	if err := a.store.UpsertSession(&state.Session{
		Key:         sessionKey,
		WorkspaceID: a.cfg.Workspaces[0].ID,
		ChatID:      "chat",
		ChatType:    "p2p",
		OwnerUserID: "user",
		Status:      "idle",
	}); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}

	resp, err := newMenuActionService(a).completeMenuCompact(&feishu.CardAction{
		MessageID: "card-claude",
		ChatID:    "chat",
		UserID:    "user",
		ActionValue: map[string]any{
			"session_key":   sessionKey,
			"parent_action": "menu.tools",
		},
	}, sessionKey)
	if err != nil {
		t.Fatalf("completeMenuCompact() error = %v", err)
	}
	if resp.Toast == nil || resp.Toast.Type != "info" {
		t.Fatalf("completeMenuCompact() toast = %#v", resp.Toast)
	}

	select {
	case <-claude.started:
	case <-time.After(2 * time.Second):
		t.Fatal("Claude /compact was not started asynchronously")
	}
	if patchedCards := ff.patchedCardsSnapshot(); len(patchedCards) != 0 {
		t.Fatalf("patchedCards before Claude start returns = %+v, want none", patchedCards)
	}

	close(claude.release)
	deadline := time.Now().Add(2 * time.Second)
	for len(ff.patchedCardsSnapshot()) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	patchedCards := ff.patchedCardsSnapshot()
	if len(patchedCards) == 0 {
		t.Fatal("expected accepted compact card patch")
	}
	startTurnCalls := claude.startTurnCallsSnapshot()
	if len(startTurnCalls) != 1 || strings.TrimSpace(startTurnCalls[0].prompt) != "/compact" {
		t.Fatalf("Claude startTurn calls = %#v", startTurnCalls)
	}
	body := cardMarkdownContent(t, patchedCards[len(patchedCards)-1])
	if !strings.Contains(body, "已提交 `/compact`") {
		t.Fatalf("accepted patched card body = %q", body)
	}
	sess := a.store.GetSession(sessionKey)
	if sess == nil || strings.TrimSpace(sess.ActiveSubmissionID) == "" || strings.TrimSpace(sess.ActiveThreadID) == "" {
		t.Fatalf("session after Claude compact = %+v", sess)
	}
}

func TestCompleteMenuCompactPatchesFailureCardOnError(t *testing.T) {
	a, ff, _ := newTestApp(t)
	a.backend = backendCodex
	a.cfg.Feishu.Backend = backendCodex
	sessionKey := "feishu:p2p:chat:user"
	if err := a.store.UpsertSession(&state.Session{
		Key:         sessionKey,
		WorkspaceID: a.cfg.Workspaces[0].ID,
		ChatID:      "chat",
		ChatType:    "p2p",
		OwnerUserID: "user",
		Status:      "idle",
	}); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}

	resp, err := newMenuActionService(a).completeMenuCompact(&feishu.CardAction{
		MessageID: "card-fail",
		ChatID:    "chat",
		UserID:    "user",
		ActionValue: map[string]any{
			"session_key":   sessionKey,
			"parent_action": "menu.tools",
		},
	}, sessionKey)
	if err != nil {
		t.Fatalf("completeMenuCompact() error = %v", err)
	}
	if resp.Toast == nil || resp.Toast.Type != "info" {
		t.Fatalf("completeMenuCompact() toast = %#v", resp.Toast)
	}

	deadline := time.Now().Add(2 * time.Second)
	for len(ff.patchedCardsSnapshot()) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	patchedCards := ff.patchedCardsSnapshot()
	if len(patchedCards) == 0 {
		t.Fatal("expected failure compact card patch")
	}
	body := cardMarkdownContent(t, patchedCards[len(patchedCards)-1])
	if !strings.Contains(body, "请求 `/compact` 失败") || !strings.Contains(body, "当前没有活动线程") {
		t.Fatalf("failure patched card body = %q", body)
	}
	if !cardHasButtonText(patchedCards[len(patchedCards)-1], "重试") {
		t.Fatalf("failure patched card missing retry button: %#v", patchedCards[len(patchedCards)-1])
	}
}
