package app

import (
	"context"
	"encoding/json"
	appreview "feidex/internal/app/review"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"feidex/internal/codexrpc"
	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

func TestCommandReviewUncommittedCallsReviewStart(t *testing.T) {
	a, ff, fc := newTestApp(t)
	repo := initReviewGitRepo(t, a.cfg.Workspaces[0].Cwd)
	writeFile(t, filepath.Join(repo, "main.go"), "package main\n\nfunc main() {}\n")

	msg := &feishu.InboundMessage{MessageID: "msg-review", ChatID: "chat-1", ChatType: "p2p", UserID: "user-1"}
	sessionKey := makeSessionKey(a, msg)
	mustUpsertReviewSession(t, a, sessionKey, msg.ChatID, msg.ChatType, msg.UserID, "thread-1")
	markSessionThreadLive(a, sessionKey, "thread-1")

	var gotMethod string
	var gotParams map[string]any
	fc.callHook = func(_ context.Context, method string, params any, out any) error {
		gotMethod = method
		gotParams = params.(map[string]any)
		if result, ok := out.(*codexrpc.ReviewStartResult); ok {
			result.ReviewThreadID = "thread-1"
			result.Turn.ID = "review-turn-1"
		}
		return nil
	}

	if err := newConversationWorkflowService(a).commandReview(msg, nil); err != nil {
		t.Fatalf("commandReview() error = %v", err)
	}
	if gotMethod != "review/start" {
		t.Fatalf("review method = %q, want review/start", gotMethod)
	}
	if got, _ := gotParams["threadId"].(string); got != "thread-1" {
		t.Fatalf("threadId = %q, want thread-1", got)
	}
	if got, _ := gotParams["delivery"].(string); got != "inline" {
		t.Fatalf("delivery = %q, want inline", got)
	}
	target, _ := gotParams["target"].(map[string]any)
	if got, _ := target["type"].(string); got != appreview.TargetUncommitted {
		t.Fatalf("target.type = %q, want %q", got, appreview.TargetUncommitted)
	}
	if len(ff.replyTexts) != 1 || !strings.Contains(ff.replyTexts[0], "未提交改动") {
		t.Fatalf("review confirmation = %#v, want uncommitted target text", ff.replyTexts)
	}
	sess := a.store.GetSession(sessionKey)
	if sess == nil || sess.ActiveTurnID != "review-turn-1" || sess.ActiveSubmissionID == "" {
		t.Fatalf("session after review start = %+v", sess)
	}
}

func TestCommandReviewWithoutActiveThreadUsesGenericThreadStart(t *testing.T) {
	a, ff, fc := newTestApp(t)
	repo := initReviewGitRepo(t, a.cfg.Workspaces[0].Cwd)
	writeFile(t, filepath.Join(repo, "main.go"), "package main\n\nfunc main() {}\n")

	msg := &feishu.InboundMessage{MessageID: "msg-review-new", ChatID: "chat-1", ChatType: "p2p", UserID: "user-1"}
	sessionKey := makeSessionKey(a, msg)
	if err := a.store.UpsertSession(&state.Session{
		Key:         sessionKey,
		WorkspaceID: a.cfg.Workspaces[0].ID,
		OwnerUserID: msg.UserID,
		ChatID:      msg.ChatID,
		ChatType:    msg.ChatType,
		Status:      "idle",
	}); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}

	var methods []string
	var reviewParams map[string]any
	fc.callHook = func(_ context.Context, method string, params any, out any) error {
		methods = append(methods, method)
		switch method {
		case "thread/start":
			if result, ok := out.(*codexrpc.ThreadStartResult); ok {
				result.Thread.ID = "thread-new"
			}
		case "review/start":
			reviewParams = params.(map[string]any)
			if result, ok := out.(*codexrpc.ReviewStartResult); ok {
				result.ReviewThreadID = "thread-new"
				result.Turn.ID = "review-turn-new"
			}
		}
		return nil
	}

	if err := newConversationWorkflowService(a).commandReview(msg, nil); err != nil {
		t.Fatalf("commandReview() error = %v", err)
	}
	if len(methods) < 2 || methods[0] != "thread/start" || methods[1] != "review/start" {
		t.Fatalf("methods = %#v, want thread/start then review/start", methods)
	}
	if got, _ := reviewParams["threadId"].(string); got != "thread-new" {
		t.Fatalf("review threadId = %q, want thread-new", got)
	}
	if len(ff.replyTexts) != 1 || !strings.Contains(ff.replyTexts[0], "已启动 review") {
		t.Fatalf("replyTexts = %#v, want neutral review confirmation", ff.replyTexts)
	}
	sess := a.store.GetSession(sessionKey)
	if sess == nil || sess.ActiveThreadID != "thread-new" || sess.ActiveTurnID != "review-turn-new" {
		t.Fatalf("session after review without thread = %+v", sess)
	}
}

func TestCommandReviewBaseOpensBranchPicker(t *testing.T) {
	a, ff, _ := newTestApp(t)
	repo := initReviewGitRepo(t, a.cfg.Workspaces[0].Cwd)

	msg := &feishu.InboundMessage{MessageID: "msg-base", ChatID: "chat-1", ChatType: "p2p", UserID: "user-1"}
	sessionKey := makeSessionKey(a, msg)
	mustUpsertReviewSession(t, a, sessionKey, msg.ChatID, msg.ChatType, msg.UserID, "thread-1")
	markSessionThreadLive(a, sessionKey, "thread-1")

	if err := newConversationWorkflowService(a).commandReview(msg, []string{"base"}); err != nil {
		t.Fatalf("commandReview(base) error = %v", err)
	}
	if len(ff.replyCards) != 1 {
		t.Fatalf("replyCards = %d, want 1", len(ff.replyCards))
	}
	selects := cardSelectStaticForTest(ff.replyCards[0])
	if len(selects) != 1 {
		t.Fatalf("select count = %d, want 1", len(selects))
	}
	options, _ := selects[0]["options"].([]map[string]any)
	if len(options) < 2 {
		t.Fatalf("branch options = %#v, want at least current and main", options)
	}
	firstText, _ := options[0]["text"].(map[string]any)
	firstLabel, _ := firstText["content"].(string)
	if !strings.Contains(firstLabel, "[当前] feature") {
		t.Fatalf("first branch label = %q, want current feature first", firstLabel)
	}
	pending := singleReviewPendingRequest(t, a)
	payload := reviewPendingPayloadFromPending(pending)
	if pending.Kind != pendingKindReview || payload.Mode != reviewFormModeBase || payload.Branch != "feature" {
		t.Fatalf("pending review payload = %+v / %+v", pending, payload)
	}
	if got, err := gitOutputForTest(repo, "branch", "--show-current"); err != nil || strings.TrimSpace(got) != "feature" {
		t.Fatalf("current branch = %q, %v, want feature", got, err)
	}
}

func TestCommandReviewCommitOpensRecentCommitPicker(t *testing.T) {
	a, ff, _ := newTestApp(t)
	_, commits := initReviewGitRepoWithCommits(t, a.cfg.Workspaces[0].Cwd)

	msg := &feishu.InboundMessage{MessageID: "msg-commit", ChatID: "chat-1", ChatType: "p2p", UserID: "user-1"}
	sessionKey := makeSessionKey(a, msg)
	mustUpsertReviewSession(t, a, sessionKey, msg.ChatID, msg.ChatType, msg.UserID, "thread-1")
	markSessionThreadLive(a, sessionKey, "thread-1")

	if err := newConversationWorkflowService(a).commandReview(msg, []string{"commit"}); err != nil {
		t.Fatalf("commandReview(commit) error = %v", err)
	}
	if len(ff.replyCards) != 1 {
		t.Fatalf("replyCards = %d, want 1", len(ff.replyCards))
	}
	selects := cardSelectStaticForTest(ff.replyCards[0])
	if len(selects) != 1 {
		t.Fatalf("select count = %d, want 1", len(selects))
	}
	options, _ := selects[0]["options"].([]map[string]any)
	if len(options) < 2 {
		t.Fatalf("commit options = %#v, want recent commits", options)
	}
	if got, _ := options[0]["value"].(string); got != commits[1] {
		t.Fatalf("latest commit option = %q, want %q", got, commits[1])
	}
	pending := singleReviewPendingRequest(t, a)
	payload := reviewPendingPayloadFromPending(pending)
	if payload.Mode != reviewFormModeCommit || payload.CommitSHA != commits[1] {
		t.Fatalf("pending commit payload = %+v, want latest commit", payload)
	}
}

func TestCompleteReviewFormSubmitStartsCustomReview(t *testing.T) {
	a, _, fc := newTestApp(t)
	msg := &feishu.InboundMessage{MessageID: "msg-custom", ChatID: "chat-1", ChatType: "p2p", UserID: "user-1"}
	sessionKey := makeSessionKey(a, msg)
	mustUpsertReviewSession(t, a, sessionKey, msg.ChatID, msg.ChatType, msg.UserID, "thread-1")
	markSessionThreadLive(a, sessionKey, "thread-1")

	if err := newReviewFormService(a).beginReviewForm(msg, reviewFormModeCustom); err != nil {
		t.Fatalf("beginReviewForm(custom) error = %v", err)
	}
	pending := singleReviewPendingRequest(t, a)

	var gotParams map[string]any
	fc.callHook = func(_ context.Context, method string, params any, out any) error {
		if method != "review/start" {
			t.Fatalf("unexpected method: %s", method)
		}
		gotParams = params.(map[string]any)
		if result, ok := out.(*codexrpc.ReviewStartResult); ok {
			result.ReviewThreadID = "thread-1"
			result.Turn.ID = "review-turn-custom"
		}
		return nil
	}

	resp, err := newReviewFormService(a).completeReviewFormSubmit(&feishu.CardAction{
		ActionValue: map[string]any{"request_id": pending.ID},
		FormValue:   map[string]any{"instructions": "focus on tests and regressions"},
		UserID:      msg.UserID,
		ChatID:      msg.ChatID,
		MessageID:   pending.FeishuMsgID,
	})
	if err != nil {
		t.Fatalf("completeReviewFormSubmit() error = %v", err)
	}
	if resp == nil || resp.Toast == nil || resp.Toast.Type != "success" {
		t.Fatalf("review submit response = %#v, want success", resp)
	}
	target, _ := gotParams["target"].(map[string]any)
	if got, _ := target["type"].(string); got != appreview.TargetCustom {
		t.Fatalf("custom target.type = %q, want %q", got, appreview.TargetCustom)
	}
	if got, _ := target["instructions"].(string); got != "focus on tests and regressions" {
		t.Fatalf("custom instructions = %q", got)
	}
	refreshed := a.store.PendingByID(pending.ID)
	if refreshed == nil || refreshed.Status != "resolved" {
		t.Fatalf("pending after custom submit = %+v, want resolved", refreshed)
	}
}

func TestExitedReviewModeDeliversFinalInQuietFinal(t *testing.T) {
	a, ff, _ := newTestApp(t)
	a.cfg.Feishu.Quiet = config.QuietModeFinal
	seedActiveSubmission(t, a, "sess-1", "thread-1", "turn-1")

	newTurnStreamService(a).completeTurnItem(context.Background(), "thread-1", "turn-1", "review-1", map[string]any{
		"id":     "review-1",
		"type":   "exitedReviewMode",
		"review": "Looks solid overall...",
	})

	if len(ff.replyCards) == 0 {
		t.Fatal("expected quiet final review result to send a card")
	}
	body := cardMarkdownContent(t, ff.replyCards[len(ff.replyCards)-1])
	if !strings.Contains(body, "Looks solid overall") {
		t.Fatalf("review final body = %q, want exitedReviewMode.review", body)
	}
}

func TestReviewResultSuppressesTrailingAgentMessageAndKeepsFooterOnLastSplitCard(t *testing.T) {
	a, ff, _ := newTestApp(t)
	sub := seedActiveSubmission(t, a, "sess-1", "thread-1", "turn-1")
	ff.replyCardIDs = []string{"card-1", "card-2", "card-3", "card-4"}
	newRuntimeStateService(a).bindTurnSubmission("thread-1", "turn-1", "sess-1", sub.ID)
	newRuntimeStateService(a).markTurnStartedAt("turn-1", time.Now().Add(-3*time.Second))
	modelContextWindow := int64(1000)
	newRuntimeStateService(a).recordTurnTokenUsage("thread-1", "turn-1", codexrpc.ThreadTokenUsage{
		Last: codexrpc.TokenUsageBreakdown{
			InputTokens: 150,
		},
		ModelContextWindow: &modelContextWindow,
	})

	longReview := strings.Repeat("review-detail ", 1800)
	newTurnStreamService(a).completeTurnItem(context.Background(), "thread-1", "turn-1", "review-1", map[string]any{
		"id":     "review-1",
		"type":   "exitedReviewMode",
		"review": longReview,
	})
	beforeTrailingAgent := len(ff.replyCards)
	if beforeTrailingAgent < 2 {
		t.Fatalf("review split replyCards = %d, want payload-driven split", beforeTrailingAgent)
	}
	for i, card := range ff.replyCards[:beforeTrailingAgent-1] {
		if footer := cardFooterTextForTest(card); strings.TrimSpace(footer) != "" {
			t.Fatalf("review split card[%d] should not include footer lines: %q", i, footer)
		}
	}
	lastFooter := cardFooterTextForTest(ff.replyCards[beforeTrailingAgent-1])
	if !strings.Contains(lastFooter, "耗时") && !strings.Contains(lastFooter, "context left") {
		t.Fatalf("review split last card missing footer lines: %q", lastFooter)
	}

	newTurnStreamService(a).completeTurnItem(context.Background(), "thread-1", "turn-1", "agent-1", map[string]any{
		"id":   "agent-1",
		"type": "agentMessage",
		"text": "trailing review summary",
	})
	if len(ff.replyCards) != beforeTrailingAgent {
		t.Fatalf("trailing review agent_message should be suppressed, got %d -> %d cards", beforeTrailingAgent, len(ff.replyCards))
	}
}

func TestReviewTurnStartedNotificationDoesNotOverrideResponseTurnID(t *testing.T) {
	a, _, fc := newTestApp(t)
	repo := initReviewGitRepo(t, a.cfg.Workspaces[0].Cwd)
	writeFile(t, filepath.Join(repo, "main.go"), "package main\n\nfunc main() { println(\"changed\") }\n")

	msg := &feishu.InboundMessage{MessageID: "msg-review", ChatID: "chat-1", ChatType: "p2p", UserID: "user-1"}
	sessionKey := makeSessionKey(a, msg)
	mustUpsertReviewSession(t, a, sessionKey, msg.ChatID, msg.ChatType, msg.UserID, "thread-1")
	markSessionThreadLive(a, sessionKey, "thread-1")

	fc.callHook = func(_ context.Context, method string, params any, out any) error {
		if method == "review/start" {
			if result, ok := out.(*codexrpc.ReviewStartResult); ok {
				result.ReviewThreadID = "thread-1"
				result.Turn.ID = "review-turn-a"
			}
		}
		return nil
	}

	if err := newConversationWorkflowService(a).commandReview(msg, nil); err != nil {
		t.Fatalf("commandReview() error = %v", err)
	}
	handleNotification(a, "turn/started", json.RawMessage(`{"threadId":"thread-1","turn":{"id":"persisted-turn-b"}}`))

	sess := a.store.GetSession(sessionKey)
	if sess == nil || sess.ActiveTurnID != "review-turn-a" {
		t.Fatalf("session after turn/started = %+v, want review response turn id", sess)
	}
}

func initReviewGitRepo(t *testing.T, dir string) string {
	t.Helper()
	repo, _ := initReviewGitRepoWithCommits(t, dir)
	return repo
}

func initReviewGitRepoWithCommits(t *testing.T, dir string) (string, []string) {
	t.Helper()
	runGitForTest(t, dir, "init")
	runGitForTest(t, dir, "config", "user.email", "test@example.com")
	runGitForTest(t, dir, "config", "user.name", "Test User")

	writeFile(t, filepath.Join(dir, "main.go"), "package main\n\nfunc main() { println(\"main\") }\n")
	runGitForTest(t, dir, "add", "main.go")
	runGitForTest(t, dir, "commit", "-m", "initial")
	runGitForTest(t, dir, "branch", "-M", "main")
	first := strings.TrimSpace(mustGitOutput(t, dir, "rev-parse", "HEAD"))

	runGitForTest(t, dir, "checkout", "-b", "feature")
	writeFile(t, filepath.Join(dir, "main.go"), "package main\n\nfunc main() { println(\"feature\") }\n")
	runGitForTest(t, dir, "commit", "-am", "feature change")
	second := strings.TrimSpace(mustGitOutput(t, dir, "rev-parse", "HEAD"))

	return dir, []string{first, second}
}

func mustUpsertReviewSession(t *testing.T, a *App, sessionKey, chatID, chatType, userID, threadID string) {
	t.Helper()
	if err := a.store.UpsertSession(&state.Session{
		Key:                     sessionKey,
		WorkspaceID:             a.cfg.Workspaces[0].ID,
		ActiveThreadID:          threadID,
		ActiveThreadWorkspaceID: a.cfg.Workspaces[0].ID,
		OwnerUserID:             userID,
		ChatID:                  chatID,
		ChatType:                chatType,
		Status:                  "idle",
	}); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}
}

func singleReviewPendingRequest(t *testing.T, a *App) *state.PendingRequest {
	t.Helper()
	var found *state.PendingRequest
	for _, pending := range a.store.AllPendingRequests() {
		if pending != nil && pending.Kind == pendingKindReview {
			if found != nil {
				t.Fatalf("multiple review pending requests: %+v %+v", found, pending)
			}
			found = pending
		}
	}
	if found == nil {
		t.Fatal("expected one review pending request")
	}
	return found
}

func runGitForTest(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Test User",
		"GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=Test User",
		"GIT_COMMITTER_EMAIL=test@example.com",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, string(output))
	}
}

func mustGitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	output, err := gitOutputForTest(dir, args...)
	if err != nil {
		t.Fatalf("git %s failed: %v", strings.Join(args, " "), err)
	}
	return output
}

func gitOutputForTest(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Test User",
		"GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=Test User",
		"GIT_COMMITTER_EMAIL=test@example.com",
	)
	output, err := cmd.CombinedOutput()
	return string(output), err
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}
}
