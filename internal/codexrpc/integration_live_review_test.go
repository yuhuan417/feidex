//go:build integration

package codexrpc

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	liveTokenTestsEnv        = "FEIDEX_CODEX_RUN_TOKEN_TESTS"
	legacyExpensiveReviewEnv = "FEIDEX_CODEX_RUN_EXPENSIVE"
)

type liveNotification struct {
	Method string
	Params json.RawMessage
}

type liveNotificationRecorder struct {
	mu            sync.Mutex
	notifications []liveNotification
}

func (r *liveNotificationRecorder) append(method string, params json.RawMessage) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.notifications = append(r.notifications, liveNotification{
		Method: method,
		Params: append(json.RawMessage(nil), params...),
	})
}

func (r *liveNotificationRecorder) len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.notifications)
}

func (r *liveNotificationRecorder) snapshotFrom(start int) []liveNotification {
	r.mu.Lock()
	defer r.mu.Unlock()
	if start < 0 {
		start = 0
	}
	if start >= len(r.notifications) {
		return nil
	}
	out := make([]liveNotification, 0, len(r.notifications)-start)
	for _, notif := range r.notifications[start:] {
		out = append(out, liveNotification{
			Method: notif.Method,
			Params: append(json.RawMessage(nil), notif.Params...),
		})
	}
	return out
}

func TestLiveCodexInlineReviewLifecycleOnTinyRepo(t *testing.T) {
	requireLiveTokenTests(t)

	cfg, _ := liveCodexConfigFromEnv(t)
	repo := initTinyReviewRepo(t)

	client := New(cfg)
	recorder := &liveNotificationRecorder{}
	client.SetHandlers(func(method string, params json.RawMessage) {
		recorder.append(method, params)
	}, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()
	if err := client.Start(ctx, true); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer func() { _ = client.Close() }()

	threadID := startLiveThread(t, ctx, client, repo)
	runPrimerTurn(t, ctx, client, recorder, threadID, repo)

	reviewStartIndex := recorder.len()
	reviewTurnID := startInlineReview(t, ctx, client, threadID)
	completedTurnID, status, notifications := waitForTurnCompletionOnThread(t, recorder, reviewStartIndex, threadID, 3*time.Minute)
	if status != "completed" {
		t.Fatalf("review turn completed with status %q, want completed; notifications: %s", status, summarizeLiveNotifications(notifications))
	}

	turnStartedIdx, startedTurnID, _ := findTurnNotification(notifications, "turn/started", threadID)
	enteredStartedIdx, enteredTurnID, _ := findItemNotification(notifications, "item/started", threadID, "enteredReviewMode")
	enteredCompletedIdx, enteredCompletedTurnID, _ := findItemNotification(notifications, "item/completed", threadID, "enteredReviewMode")
	exitedCompletedIdx, exitedTurnID, reviewText := findItemNotification(notifications, "item/completed", threadID, "exitedReviewMode")
	turnCompletedIdx, completedNotifTurnID, _ := findTurnNotification(notifications, "turn/completed", threadID)

	if turnStartedIdx < 0 {
		t.Fatalf("missing turn/started for inline review; notifications: %s", summarizeLiveNotifications(notifications))
	}
	if enteredStartedIdx < 0 {
		t.Fatalf("missing item/started(enteredReviewMode); notifications: %s", summarizeLiveNotifications(notifications))
	}
	if enteredCompletedIdx < 0 {
		t.Fatalf("missing item/completed(enteredReviewMode); notifications: %s", summarizeLiveNotifications(notifications))
	}
	if exitedCompletedIdx < 0 {
		t.Fatalf("missing item/completed(exitedReviewMode); notifications: %s", summarizeLiveNotifications(notifications))
	}
	if strings.TrimSpace(reviewText) == "" {
		t.Fatalf("exitedReviewMode.review is empty; notifications: %s", summarizeLiveNotifications(notifications))
	}
	if turnCompletedIdx < 0 {
		t.Fatalf("missing turn/completed for inline review; notifications: %s", summarizeLiveNotifications(notifications))
	}
	if enteredTurnID != "" && enteredCompletedTurnID != "" && enteredTurnID != enteredCompletedTurnID {
		t.Fatalf("enteredReviewMode turn ids differ between started/completed: started=%s completed=%s; notifications: %s", enteredTurnID, enteredCompletedTurnID, summarizeLiveNotifications(notifications))
	}
	if enteredTurnID != "" && strings.TrimSpace(exitedTurnID) != "" && enteredTurnID != exitedTurnID {
		t.Fatalf("review lifecycle turn ids differ: entered=%s exited=%s; notifications: %s", enteredTurnID, exitedTurnID, summarizeLiveNotifications(notifications))
	}
	if completedNotifTurnID != reviewTurnID {
		t.Fatalf("turn/completed id = %s, want review/start response id %s; notifications: %s", completedNotifTurnID, reviewTurnID, summarizeLiveNotifications(notifications))
	}
	if strings.TrimSpace(exitedTurnID) != "" && exitedTurnID != reviewTurnID {
		t.Fatalf("exitedReviewMode turn id = %s, want review/start response id %s; notifications: %s", exitedTurnID, reviewTurnID, summarizeLiveNotifications(notifications))
	}
	if !(enteredStartedIdx < enteredCompletedIdx && enteredCompletedIdx < exitedCompletedIdx && exitedCompletedIdx < turnCompletedIdx) {
		t.Fatalf("unexpected inline review lifecycle order: %s", summarizeLiveNotifications(notifications))
	}
	if turnStartedIdx >= 0 && turnStartedIdx >= turnCompletedIdx {
		t.Fatalf("turn/started arrived after turn/completed: %s", summarizeLiveNotifications(notifications))
	}

	t.Logf(
		"inline review turn ids: response=%s started=%s exited=%s completed=%s wait_result=%s",
		reviewTurnID,
		startedTurnID,
		exitedTurnID,
		completedNotifTurnID,
		completedTurnID,
	)

	read := waitForThreadReadReviewItems(t, ctx, client, threadID, 15*time.Second)
	if !threadReadContainsItemType(read, "enteredReviewMode") {
		t.Fatalf("thread/read missing enteredReviewMode: %+v", read.Thread.Turns)
	}
	if !threadReadContainsItemType(read, "exitedReviewMode") {
		t.Fatalf("thread/read missing exitedReviewMode: %+v", read.Thread.Turns)
	}
}

func requireLiveTokenTests(t *testing.T) {
	t.Helper()
	if !envEnabled(liveTokenTestsEnv) && !envEnabled(legacyExpensiveReviewEnv) {
		t.Skipf("set %s=1 to run token-consuming live Codex turn/review tests", liveTokenTestsEnv)
	}
}

func envEnabled(key string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(key))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func initTinyReviewRepo(t *testing.T) string {
	t.Helper()

	repo := t.TempDir()
	runGitIntegration(t, repo, "init")
	runGitIntegration(t, repo, "config", "user.email", "feidex-test@example.com")
	runGitIntegration(t, repo, "config", "user.name", "Feidex Test")

	writeIntegrationFile(t, filepath.Join(repo, "calc.go"), strings.TrimSpace(`
package calc

// Add returns the sum of a and b.
func Add(a, b int) int {
	return a + b
}
`)+"\n")
	runGitIntegration(t, repo, "add", "calc.go")
	runGitIntegration(t, repo, "commit", "-m", "initial tiny fixture")

	writeIntegrationFile(t, filepath.Join(repo, "calc.go"), strings.TrimSpace(`
package calc

// Add returns the sum of a and b.
func Add(a, b int) int {
	return a - b
}
`)+"\n")
	return repo
}

func writeIntegrationFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}
}

func runGitIntegration(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Feidex Test",
		"GIT_AUTHOR_EMAIL=feidex-test@example.com",
		"GIT_COMMITTER_NAME=Feidex Test",
		"GIT_COMMITTER_EMAIL=feidex-test@example.com",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, string(output))
	}
}

func startLiveThread(t *testing.T, parent context.Context, client *Client, cwd string) string {
	t.Helper()

	ctx, cancel := context.WithTimeout(parent, 45*time.Second)
	defer cancel()

	var thread ThreadStartResult
	if err := client.Call(ctx, "thread/start", map[string]any{
		"cwd":                    cwd,
		"approvalPolicy":         "never",
		"sandbox":                "read-only",
		"serviceName":            "feidex-integration",
		"experimentalRawEvents":  false,
		"persistExtendedHistory": true,
	}, &thread); err != nil {
		t.Fatalf("thread/start error = %v", err)
	}
	threadID := strings.TrimSpace(thread.Thread.ID)
	if threadID == "" {
		t.Fatalf("thread/start result = %+v, want non-empty thread id", thread)
	}
	return threadID
}

func runPrimerTurn(t *testing.T, parent context.Context, client *Client, recorder *liveNotificationRecorder, threadID, cwd string) {
	t.Helper()

	start := recorder.len()
	ctx, cancel := context.WithTimeout(parent, 90*time.Second)
	defer cancel()

	var turn TurnStartResult
	if err := client.Call(ctx, "turn/start", map[string]any{
		"threadId":       threadID,
		"cwd":            cwd,
		"approvalPolicy": "never",
		"sandboxPolicy": map[string]any{
			"type": "readOnly",
		},
		"input": []map[string]any{
			{
				"type":          "text",
				"text":          "Reply with exactly OK.",
				"text_elements": []any{},
			},
		},
	}, &turn); err != nil {
		t.Fatalf("turn/start primer error = %v", err)
	}
	if strings.TrimSpace(turn.Turn.ID) == "" {
		t.Fatalf("primer turn/start result = %+v, want non-empty turn id", turn)
	}

	completedTurnID, status, notifications := waitForTurnCompletionOnThread(t, recorder, start, threadID, 90*time.Second)
	if status != "completed" {
		t.Fatalf("primer turn completed with status %q, want completed; notifications: %s", status, summarizeLiveNotifications(notifications))
	}
	if strings.TrimSpace(completedTurnID) == "" {
		t.Fatalf("primer turn completed without turn id; notifications: %s", summarizeLiveNotifications(notifications))
	}
}

func startInlineReview(t *testing.T, parent context.Context, client *Client, threadID string) string {
	t.Helper()

	ctx, cancel := context.WithTimeout(parent, 45*time.Second)
	defer cancel()

	var review ReviewStartResult
	if err := client.Call(ctx, "review/start", map[string]any{
		"threadId": threadID,
		"delivery": "inline",
		"target": map[string]any{
			"type": "uncommittedChanges",
		},
	}, &review); err != nil {
		t.Fatalf("review/start error = %v", err)
	}
	if strings.TrimSpace(review.ReviewThreadID) != threadID {
		t.Fatalf("review/start reviewThreadId = %q, want %q for inline review", review.ReviewThreadID, threadID)
	}
	turnID := strings.TrimSpace(review.Turn.ID)
	if turnID == "" {
		t.Fatalf("review/start result = %+v, want non-empty turn id", review)
	}
	return turnID
}

func waitForTurnCompletionOnThread(t *testing.T, recorder *liveNotificationRecorder, start int, threadID string, timeout time.Duration) (string, string, []liveNotification) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		notifications := recorder.snapshotFrom(start)
		if idx, turnID, status := findTurnNotification(notifications, "turn/completed", threadID); idx >= 0 {
			return turnID, status, notifications
		}
		time.Sleep(200 * time.Millisecond)
	}

	notifications := recorder.snapshotFrom(start)
	t.Fatalf("timed out waiting for turn/completed on thread %q; notifications: %s", threadID, summarizeLiveNotifications(notifications))
	return "", "", nil
}

func findTurnNotification(notifications []liveNotification, method, threadID string) (int, string, string) {
	for i, notif := range notifications {
		if notif.Method != method {
			continue
		}
		var payload struct {
			ThreadID string `json:"threadId"`
			Turn     struct {
				ID     string `json:"id"`
				Status string `json:"status"`
			} `json:"turn"`
		}
		if err := json.Unmarshal(notif.Params, &payload); err != nil {
			continue
		}
		if strings.TrimSpace(payload.ThreadID) != strings.TrimSpace(threadID) {
			continue
		}
		return i, strings.TrimSpace(payload.Turn.ID), strings.TrimSpace(payload.Turn.Status)
	}
	return -1, "", ""
}

func findItemNotification(notifications []liveNotification, method, threadID, itemType string) (int, string, string) {
	for i, notif := range notifications {
		if notif.Method != method {
			continue
		}
		var payload struct {
			ThreadID string         `json:"threadId"`
			TurnID   string         `json:"turnId"`
			Item     map[string]any `json:"item"`
		}
		if err := json.Unmarshal(notif.Params, &payload); err != nil {
			continue
		}
		if strings.TrimSpace(payload.ThreadID) != strings.TrimSpace(threadID) {
			continue
		}
		if strings.TrimSpace(liveString(payload.Item["type"])) != itemType {
			continue
		}
		return i, strings.TrimSpace(payload.TurnID), strings.TrimSpace(liveString(payload.Item["review"]))
	}
	return -1, "", ""
}

func liveString(value any) string {
	text, _ := value.(string)
	return strings.TrimSpace(text)
}

func summarizeLiveNotifications(notifications []liveNotification) string {
	if len(notifications) == 0 {
		return "(none)"
	}
	parts := make([]string, 0, len(notifications))
	for _, notif := range notifications {
		parts = append(parts, summarizeLiveNotification(notif))
	}
	return strings.Join(parts, " -> ")
}

func summarizeLiveNotification(notif liveNotification) string {
	switch notif.Method {
	case "turn/started", "turn/completed":
		var payload struct {
			Turn struct {
				ID     string `json:"id"`
				Status string `json:"status"`
			} `json:"turn"`
		}
		if json.Unmarshal(notif.Params, &payload) == nil {
			switch notif.Method {
			case "turn/started":
				return fmt.Sprintf("%s(%s)", notif.Method, strings.TrimSpace(payload.Turn.ID))
			default:
				return fmt.Sprintf("%s(%s,%s)", notif.Method, strings.TrimSpace(payload.Turn.ID), strings.TrimSpace(payload.Turn.Status))
			}
		}
	case "item/started", "item/completed":
		var payload struct {
			Item map[string]any `json:"item"`
		}
		if json.Unmarshal(notif.Params, &payload) == nil {
			itemType := liveString(payload.Item["type"])
			if itemType != "" {
				return fmt.Sprintf("%s(%s)", notif.Method, itemType)
			}
		}
	}
	return notif.Method
}

func waitForThreadReadReviewItems(t *testing.T, parent context.Context, client *Client, threadID string, timeout time.Duration) ThreadReadResult {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(parent, 20*time.Second)
		var read ThreadReadResult
		err := client.Call(ctx, "thread/read", map[string]any{
			"threadId":     threadID,
			"includeTurns": true,
		}, &read)
		cancel()
		if err == nil && threadReadContainsItemType(read, "enteredReviewMode") && threadReadContainsItemType(read, "exitedReviewMode") {
			return read
		}
		time.Sleep(500 * time.Millisecond)
	}

	ctx, cancel := context.WithTimeout(parent, 20*time.Second)
	defer cancel()
	var read ThreadReadResult
	if err := client.Call(ctx, "thread/read", map[string]any{
		"threadId":     threadID,
		"includeTurns": true,
	}, &read); err != nil {
		t.Fatalf("thread/read error after review = %v", err)
	}
	return read
}

func threadReadContainsItemType(read ThreadReadResult, itemType string) bool {
	for _, turn := range read.Thread.Turns {
		for _, item := range turn.Items {
			if strings.TrimSpace(item.Type) == itemType {
				return true
			}
		}
	}
	return false
}
