//go:build integration

package codexrpc

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"feidex/internal/config"
)

type liveRequestRecorder struct {
	mu       sync.Mutex
	requests []RequestEnvelope
	ch       chan RequestEnvelope
}

func newLiveRequestRecorder() *liveRequestRecorder {
	return &liveRequestRecorder{ch: make(chan RequestEnvelope, 64)}
}

func (r *liveRequestRecorder) append(req RequestEnvelope) {
	copied := RequestEnvelope{
		ID:     append(json.RawMessage(nil), req.ID...),
		Method: req.Method,
		Params: append(json.RawMessage(nil), req.Params...),
	}
	r.mu.Lock()
	r.requests = append(r.requests, copied)
	r.mu.Unlock()
	r.ch <- copied
}

func (r *liveRequestRecorder) snapshot() []RequestEnvelope {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]RequestEnvelope, 0, len(r.requests))
	for _, req := range r.requests {
		out = append(out, RequestEnvelope{
			ID:     append(json.RawMessage(nil), req.ID...),
			Method: req.Method,
			Params: append(json.RawMessage(nil), req.Params...),
		})
	}
	return out
}

func TestLiveCodexTurnLifecycleCoreOnTinyRepo(t *testing.T) {
	requireLiveTokenTests(t)

	cfg, _ := liveCodexConfigFromEnv(t)
	repo := initTinyReviewRepo(t)

	client, notifications := startLiveClientWithRecorders(t, cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	startLiveClient(t, ctx, client)
	defer func() { _ = client.Close() }()

	threadID := startLiveThreadWithOptions(t, ctx, client, repo, "never", "read-only")
	start := notifications.len()
	turnID := startLiveTurn(t, ctx, client, threadID, repo, "never", map[string]any{"type": "readOnly"}, "Reply with exactly TURN_OK.")

	completedTurnID, status, lifecycle := waitForTurnCompletionOnThread(t, notifications, start, threadID, 90*time.Second)
	if status != "completed" {
		t.Fatalf("turn completed with status %q, want completed; notifications: %s", status, summarizeLiveNotifications(lifecycle))
	}
	_, startedTurnID, _ := findTurnNotification(lifecycle, "turn/started", threadID)
	agentCompletedIdx, agentTurnID, _ := findItemNotification(lifecycle, "item/completed", threadID, "agentMessage")
	turnCompletedIdx, completedNotifTurnID, _ := findTurnNotification(lifecycle, "turn/completed", threadID)

	if strings.TrimSpace(startedTurnID) == "" {
		t.Fatalf("missing turn/started id; notifications: %s", summarizeLiveNotifications(lifecycle))
	}
	if startedTurnID != turnID || completedNotifTurnID != turnID || completedTurnID != turnID {
		t.Fatalf("turn ids diverged: response=%s started=%s completed_notif=%s wait=%s", turnID, startedTurnID, completedNotifTurnID, completedTurnID)
	}
	if agentCompletedIdx < 0 || agentTurnID != turnID {
		t.Fatalf("missing item/completed(agentMessage) on response turn %s; notifications: %s", turnID, summarizeLiveNotifications(lifecycle))
	}
	if turnCompletedIdx < agentCompletedIdx {
		t.Fatalf("turn/completed arrived before final agent message: %s", summarizeLiveNotifications(lifecycle))
	}

	read := waitForThreadReadItemPredicate(t, ctx, client, threadID, 15*time.Second, func(item ThreadReadItem) bool {
		return strings.TrimSpace(item.Type) == "agentMessage" && threadReadItemContains(item, "TURN_OK")
	})
	if !threadReadContainsItemType(read, "agentMessage") {
		t.Fatalf("thread/read missing agentMessage: %+v", read.Thread.Turns)
	}
	if !threadReadContainsAgentText(read, "TURN_OK") {
		t.Fatalf("thread/read missing TURN_OK agent text: %+v", read.Thread.Turns)
	}
}

func TestLiveCodexSteerContinuationOnActiveTurn(t *testing.T) {
	requireLiveTokenTests(t)

	cfg, _ := liveCodexConfigFromEnv(t)
	repo := initTinyReviewRepo(t)

	client, notifications := startLiveClientWithRecorders(t, cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	startLiveClient(t, ctx, client)
	defer func() { _ = client.Close() }()

	threadID := startLiveThreadWithOptions(t, ctx, client, repo, "never", "read-only")
	start := notifications.len()
	turnID := startLiveTurn(t, ctx, client, threadID, repo, "never", map[string]any{"type": "readOnly"}, "Before answering, run the shell command `sleep 5`. After it finishes, reply with exactly ORIGINAL.")

	if _, commandTurnID, ok := waitForItemNotification(t, notifications, start, 45*time.Second, func(n liveNotification) (string, string, bool) {
		if n.Method != "item/started" {
			return "", "", false
		}
		var payload struct {
			ThreadID string         `json:"threadId"`
			TurnID   string         `json:"turnId"`
			Item     map[string]any `json:"item"`
		}
		if json.Unmarshal(n.Params, &payload) != nil {
			return "", "", false
		}
		if strings.TrimSpace(payload.ThreadID) != threadID || liveString(payload.Item["type"]) != "commandExecution" {
			return "", "", false
		}
		return strings.TrimSpace(payload.TurnID), liveString(payload.Item["type"]), true
	}); !ok {
		t.Fatalf("missing item/started(commandExecution) before steer")
	} else if commandTurnID != turnID {
		t.Fatalf("commandExecution turn id = %s, want response turn id %s", commandTurnID, turnID)
	}

	steerCtx, steerCancel := context.WithTimeout(ctx, 30*time.Second)
	defer steerCancel()
	if err := client.Call(steerCtx, "turn/steer", map[string]any{
		"threadId":       threadID,
		"expectedTurnId": turnID,
		"input": []map[string]any{
			{
				"type":          "text",
				"text":          "Ignore the previous final output instruction. After the sleep finishes, reply with exactly STEERED.",
				"text_elements": []any{},
			},
		},
	}, nil); err != nil {
		t.Fatalf("turn/steer error = %v", err)
	}

	completedTurnID, status, lifecycle := waitForTurnCompletionOnThread(t, notifications, start, threadID, 90*time.Second)
	if status != "completed" {
		t.Fatalf("steered turn completed with status %q, want completed; notifications: %s", status, summarizeLiveNotifications(lifecycle))
	}
	if completedTurnID != turnID {
		t.Fatalf("steered turn completed id = %s, want original turn id %s", completedTurnID, turnID)
	}

	read := waitForThreadReadItemPredicate(t, ctx, client, threadID, 15*time.Second, func(item ThreadReadItem) bool {
		return strings.TrimSpace(item.Type) == "agentMessage" && threadReadItemContains(item, "STEERED")
	})
	if !threadReadContainsAgentText(read, "STEERED") {
		t.Fatalf("thread/read missing STEERED final agent text: %+v", read.Thread.Turns)
	}
	if threadReadContainsAgentText(read, "ORIGINAL") {
		t.Fatalf("thread/read still contains ORIGINAL final text after steer: %+v", read.Thread.Turns)
	}
}

func TestLiveCodexCommandApprovalLifecycleOnTinyRepo(t *testing.T) {
	requireLiveTokenTests(t)

	cfg, _ := liveCodexConfigFromEnv(t)
	repo, commandPath, expectedOutput := initTinyCommandApprovalRepo(t)

	client, notifications, requests := startLiveClientWithNotificationsAndRequests(t, cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	startLiveClient(t, ctx, client)
	defer func() { _ = client.Close() }()

	threadID := startLiveThreadWithOptions(t, ctx, client, repo, "on-request", "read-only")
	start := notifications.len()
	turnID := startLiveTurn(t, ctx, client, threadID, repo, "on-request", map[string]any{"type": "readOnly"}, fmt.Sprintf("Use the built-in command approval request mechanism, not a normal assistant message, to request approval before running any command. Wait for approval. Then invoke exactly this shell command via command execution: `%s`. Do not simulate or invent the output. Do not edit files. If you cannot issue the built-in approval request, do not reply with a fallback question. After the command finishes, reply with exactly the stdout of that command and nothing else.", commandPath))

	req, ok := pollForSpecificRequest(requests, 60*time.Second, func(req RequestEnvelope) bool {
		return req.Method == "item/commandExecution/requestApproval"
	})
	if !ok {
		t.Fatalf(
			"timed out waiting for command approval request; seen requests: %v; notifications: %s; thread/read: %s",
			liveRequestMethods(requests.snapshot()),
			summarizeLiveNotifications(notifications.snapshotFrom(start)),
			summarizeThreadReadResult(bestEffortLiveThreadRead(ctx, client, threadID)),
		)
	}
	commandPayload := parseLiveApprovalPayload(t, req)
	if commandPayload.ThreadID != threadID || commandPayload.TurnID != turnID {
		t.Fatalf("command approval payload = %+v, want thread %s turn %s", commandPayload, threadID, turnID)
	}
	if !strings.Contains(commandPayload.Command, commandPath) {
		t.Fatalf("command approval command = %q, want %q", commandPayload.Command, commandPath)
	}
	if err := client.Reply(req.ID, map[string]any{"decision": "accept"}); err != nil {
		t.Fatalf("Reply(command approval) error = %v", err)
	}

	waitForResolvedRequestNotification(t, notifications, start, threadID, normalizedLiveRequestID(req.ID), 45*time.Second)
	completedTurnID, status, lifecycle := waitForTurnCompletionOnThread(t, notifications, start, threadID, 90*time.Second)
	if status != "completed" {
		t.Fatalf("command approval turn completed with status %q, want completed; notifications: %s", status, summarizeLiveNotifications(lifecycle))
	}
	if completedTurnID != turnID {
		t.Fatalf("command approval completed turn id = %s, want %s", completedTurnID, turnID)
	}
	if idx, itemTurnID, _ := findItemNotification(lifecycle, "item/completed", threadID, "commandExecution"); idx < 0 || itemTurnID != turnID {
		t.Fatalf("missing item/completed(commandExecution) on turn %s; notifications: %s", turnID, summarizeLiveNotifications(lifecycle))
	}

	read := waitForThreadReadItemPredicate(t, ctx, client, threadID, 15*time.Second, func(item ThreadReadItem) bool {
		return strings.TrimSpace(item.Type) == "commandExecution" && item.AggregatedOutput != nil && strings.TrimSpace(*item.AggregatedOutput) != ""
	})
	if !threadReadContainsItemType(read, "commandExecution") {
		t.Fatalf("thread/read missing commandExecution item: %+v", read.Thread.Turns)
	}
	if !threadReadContainsText(read, commandPath) {
		t.Fatalf("thread/read missing %q command trace: %+v", commandPath, read.Thread.Turns)
	}
	commandOutput := strings.TrimSpace(threadReadFirstCommandOutput(read))
	if commandOutput == "" {
		t.Fatalf("thread/read missing commandExecution output: %+v", read.Thread.Turns)
	}
	if commandOutput != expectedOutput {
		t.Fatalf("command output = %q, want %q", commandOutput, expectedOutput)
	}
	if !threadReadContainsAgentText(read, commandOutput) {
		t.Fatalf("thread/read final agent message does not echo command output %q: %+v", commandOutput, read.Thread.Turns)
	}
}

func TestLiveCodexFileApprovalLifecycleOnTinyRepo(t *testing.T) {
	requireLiveTokenTests(t)

	cfg, _ := liveCodexConfigFromEnv(t)
	repo := initTinyReviewRepo(t)

	client, notifications, requests := startLiveClientWithNotificationsAndRequests(t, cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	startLiveClient(t, ctx, client)
	defer func() { _ = client.Close() }()

	threadID := startLiveThreadWithOptions(t, ctx, client, repo, "untrusted", "workspace-write")
	start := notifications.len()
	turnID := startLiveTurn(t, ctx, client, threadID, repo, "untrusted", map[string]any{"type": "workspaceWrite"}, "You must edit calc.go so Add returns subtraction (`a - b`) instead of addition. Change only that file. If approval is required, request it and wait. After the edit finishes, reply with exactly FILE_OK.")

	fileReq := waitForSpecificRequest(t, requests, 90*time.Second, func(req RequestEnvelope) bool {
		switch req.Method {
		case "item/commandExecution/requestApproval":
			if err := client.Reply(req.ID, map[string]any{"decision": "accept"}); err != nil {
				t.Fatalf("Reply(unexpected command approval) error = %v", err)
			}
			return false
		case "item/fileChange/requestApproval":
			return true
		default:
			t.Fatalf("unexpected live server request before file approval: %s", req.Method)
			return false
		}
	})
	filePayload := parseLiveApprovalPayload(t, fileReq)
	if filePayload.ThreadID != threadID || filePayload.TurnID != turnID {
		t.Fatalf("file approval payload = %+v, want thread %s turn %s", filePayload, threadID, turnID)
	}
	if err := client.Reply(fileReq.ID, map[string]any{"decision": "accept"}); err != nil {
		t.Fatalf("Reply(file approval) error = %v", err)
	}

	waitForResolvedRequestNotification(t, notifications, start, threadID, normalizedLiveRequestID(fileReq.ID), 45*time.Second)
	completedTurnID, status, lifecycle := waitForTurnCompletionOnThread(t, notifications, start, threadID, 120*time.Second)
	if status != "completed" {
		t.Fatalf("file approval turn completed with status %q, want completed; notifications: %s", status, summarizeLiveNotifications(lifecycle))
	}
	if completedTurnID != turnID {
		t.Fatalf("file approval completed turn id = %s, want %s", completedTurnID, turnID)
	}
	if idx, itemTurnID, _ := findItemNotification(lifecycle, "item/completed", threadID, "fileChange"); idx < 0 || itemTurnID != turnID {
		t.Fatalf("missing item/completed(fileChange) on turn %s; notifications: %s", turnID, summarizeLiveNotifications(lifecycle))
	}

	contents := readTinyRepoFile(t, repo, "calc.go")
	if !strings.Contains(contents, "return a - b") {
		t.Fatalf("calc.go contents = %q, want subtraction edit", contents)
	}

	read := waitForThreadReadItemPredicate(t, ctx, client, threadID, 15*time.Second, func(item ThreadReadItem) bool {
		return strings.TrimSpace(item.Type) == "fileChange" && threadReadItemContains(item, "calc.go")
	})
	if !threadReadContainsItemType(read, "fileChange") {
		t.Fatalf("thread/read missing fileChange item: %+v", read.Thread.Turns)
	}
	if !threadReadContainsText(read, "calc.go") {
		t.Fatalf("thread/read missing calc.go file change trace: %+v", read.Thread.Turns)
	}
	if !threadReadContainsAgentText(read, "FILE_OK") {
		t.Fatalf("thread/read missing FILE_OK final agent text: %+v", read.Thread.Turns)
	}
}

type liveApprovalPayload struct {
	ThreadID string
	TurnID   string
	ItemID   string
	Command  string
	RawJSON  string
}

func startLiveClientWithRecorders(t *testing.T, cfg config.CodexConfig) (*Client, *liveNotificationRecorder) {
	t.Helper()
	client := New(cfg)
	notifications := &liveNotificationRecorder{}
	client.SetHandlers(func(method string, params json.RawMessage) {
		notifications.append(method, params)
	}, nil)
	return client, notifications
}

func startLiveClientWithNotificationsAndRequests(t *testing.T, cfg config.CodexConfig) (*Client, *liveNotificationRecorder, *liveRequestRecorder) {
	t.Helper()
	client := New(cfg)
	notifications := &liveNotificationRecorder{}
	requests := newLiveRequestRecorder()
	client.SetHandlers(func(method string, params json.RawMessage) {
		notifications.append(method, params)
	}, func(req RequestEnvelope) {
		requests.append(req)
	})
	return client, notifications, requests
}

func startLiveClient(t *testing.T, ctx context.Context, client *Client) {
	t.Helper()
	if err := client.Start(ctx, true); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
}

func startLiveThreadWithOptions(t *testing.T, parent context.Context, client *Client, cwd, approvalPolicy, sandbox string) string {
	t.Helper()

	ctx, cancel := context.WithTimeout(parent, 45*time.Second)
	defer cancel()

	var thread ThreadStartResult
	if err := client.Call(ctx, "thread/start", map[string]any{
		"cwd":                    cwd,
		"approvalPolicy":         approvalPolicy,
		"sandbox":                sandbox,
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

func startLiveTurn(t *testing.T, parent context.Context, client *Client, threadID, cwd string, approvalPolicy any, sandboxPolicy map[string]any, prompt string) string {
	t.Helper()

	ctx, cancel := context.WithTimeout(parent, 45*time.Second)
	defer cancel()

	var turn TurnStartResult
	if err := client.Call(ctx, "turn/start", map[string]any{
		"threadId":       threadID,
		"cwd":            cwd,
		"approvalPolicy": approvalPolicy,
		"sandboxPolicy":  sandboxPolicy,
		"input": []map[string]any{
			{
				"type":          "text",
				"text":          prompt,
				"text_elements": []any{},
			},
		},
	}, &turn); err != nil {
		t.Fatalf("turn/start error = %v", err)
	}
	turnID := strings.TrimSpace(turn.Turn.ID)
	if turnID == "" {
		t.Fatalf("turn/start result = %+v, want non-empty turn id", turn)
	}
	return turnID
}

func pollForSpecificRequest(requests *liveRequestRecorder, timeout time.Duration, match func(RequestEnvelope) bool) (RequestEnvelope, bool) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		select {
		case req := <-requests.ch:
			if match(req) {
				return req, true
			}
		case <-timer.C:
			return RequestEnvelope{}, false
		}
	}
}

func waitForSpecificRequest(t *testing.T, requests *liveRequestRecorder, timeout time.Duration, match func(RequestEnvelope) bool) RequestEnvelope {
	t.Helper()
	req, ok := pollForSpecificRequest(requests, timeout, match)
	if ok {
		return req
	}
	t.Fatalf("timed out waiting for live server request; seen requests: %v", liveRequestMethods(requests.snapshot()))
	return RequestEnvelope{}
}

func liveRequestMethods(requests []RequestEnvelope) []string {
	methods := make([]string, 0, len(requests))
	for _, req := range requests {
		methods = append(methods, req.Method)
	}
	return methods
}

func parseLiveApprovalPayload(t *testing.T, req RequestEnvelope) liveApprovalPayload {
	t.Helper()
	var payload struct {
		ThreadID string `json:"threadId"`
		TurnID   string `json:"turnId"`
		ItemID   string `json:"itemId"`
		Command  string `json:"command"`
	}
	if err := json.Unmarshal(req.Params, &payload); err != nil {
		t.Fatalf("json.Unmarshal(request params) error = %v", err)
	}
	return liveApprovalPayload{
		ThreadID: strings.TrimSpace(payload.ThreadID),
		TurnID:   strings.TrimSpace(payload.TurnID),
		ItemID:   strings.TrimSpace(payload.ItemID),
		Command:  strings.TrimSpace(payload.Command),
		RawJSON:  string(req.Params),
	}
}

func normalizedLiveRequestID(raw json.RawMessage) string {
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return strings.TrimSpace(text)
	}
	var number float64
	if err := json.Unmarshal(raw, &number); err == nil {
		return strings.TrimSpace(fmt.Sprintf("%.0f", number))
	}
	return strings.TrimSpace(string(raw))
}

func waitForResolvedRequestNotification(t *testing.T, notifications *liveNotificationRecorder, start int, threadID, requestID string, timeout time.Duration) {
	t.Helper()
	if strings.TrimSpace(requestID) == "" {
		t.Fatal("requestID is required")
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		for _, notif := range notifications.snapshotFrom(start) {
			if notif.Method != "serverRequest/resolved" {
				continue
			}
			var payload struct {
				ThreadID  string          `json:"threadId"`
				RequestID json.RawMessage `json:"requestId"`
			}
			if json.Unmarshal(notif.Params, &payload) != nil {
				continue
			}
			if strings.TrimSpace(payload.ThreadID) != threadID {
				continue
			}
			if normalizedLiveRequestID(payload.RequestID) == requestID {
				return
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for serverRequest/resolved for request %q on thread %q", requestID, threadID)
}

func waitForItemNotification(t *testing.T, notifications *liveNotificationRecorder, start int, timeout time.Duration, match func(liveNotification) (string, string, bool)) (int, string, bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		snapshot := notifications.snapshotFrom(start)
		for idx, notif := range snapshot {
			if turnID, itemType, ok := match(notif); ok {
				_ = itemType
				return start + idx, turnID, true
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	return -1, "", false
}

func waitForThreadReadItemPredicate(t *testing.T, parent context.Context, client *Client, threadID string, timeout time.Duration, predicate func(ThreadReadItem) bool) ThreadReadResult {
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
		if err == nil && threadReadHasItem(read, predicate) {
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
		t.Fatalf("thread/read error = %v", err)
	}
	return read
}

func bestEffortLiveThreadRead(parent context.Context, client *Client, threadID string) *ThreadReadResult {
	ctx, cancel := context.WithTimeout(parent, 20*time.Second)
	defer cancel()
	var read ThreadReadResult
	if err := client.Call(ctx, "thread/read", map[string]any{
		"threadId":     threadID,
		"includeTurns": true,
	}, &read); err != nil {
		return nil
	}
	return &read
}

func summarizeThreadReadResult(read *ThreadReadResult) string {
	if read == nil {
		return "(unavailable)"
	}
	parts := make([]string, 0)
	for _, turn := range read.Thread.Turns {
		turnID := strings.TrimSpace(turn.ID)
		for _, item := range turn.Items {
			itemType := strings.TrimSpace(item.Type)
			if itemType == "" {
				itemType = "unknown"
			}
			part := itemType
			if cmd := strings.TrimSpace(item.Command); cmd != "" {
				part += fmt.Sprintf("[cmd=%s]", cmd)
			}
			if snippet := liveThreadReadSnippet(item); snippet != "" {
				part += fmt.Sprintf("[text=%s]", snippet)
			}
			if turnID != "" {
				part = fmt.Sprintf("%s:%s", turnID, part)
			}
			parts = append(parts, part)
		}
	}
	if len(parts) == 0 {
		return "(no turns/items)"
	}
	return strings.Join(parts, " -> ")
}

func liveThreadReadSnippet(item ThreadReadItem) string {
	for _, candidate := range []string{
		strings.TrimSpace(item.Text),
		strings.TrimSpace(threadReadAggregatedOutput(item)),
		strings.TrimSpace(string(item.Content)),
	} {
		if candidate == "" {
			continue
		}
		candidate = strings.ReplaceAll(candidate, "\n", "\\n")
		if len(candidate) > 120 {
			return candidate[:120] + "..."
		}
		return candidate
	}
	return ""
}

func threadReadAggregatedOutput(item ThreadReadItem) string {
	if item.AggregatedOutput == nil {
		return ""
	}
	return *item.AggregatedOutput
}

func threadReadHasItem(read ThreadReadResult, predicate func(ThreadReadItem) bool) bool {
	for _, turn := range read.Thread.Turns {
		for _, item := range turn.Items {
			if predicate(item) {
				return true
			}
		}
	}
	return false
}

func threadReadContainsAgentText(read ThreadReadResult, needle string) bool {
	return threadReadHasItem(read, func(item ThreadReadItem) bool {
		return strings.TrimSpace(item.Type) == "agentMessage" && threadReadItemContains(item, needle)
	})
}

func threadReadContainsText(read ThreadReadResult, needle string) bool {
	return threadReadHasItem(read, func(item ThreadReadItem) bool {
		return threadReadItemContains(item, needle)
	})
}

func threadReadFirstCommandOutput(read ThreadReadResult) string {
	for _, turn := range read.Thread.Turns {
		for _, item := range turn.Items {
			if strings.TrimSpace(item.Type) != "commandExecution" || item.AggregatedOutput == nil {
				continue
			}
			if output := strings.TrimSpace(*item.AggregatedOutput); output != "" {
				return output
			}
		}
	}
	return ""
}

func threadReadItemContains(item ThreadReadItem, needle string) bool {
	needle = strings.TrimSpace(needle)
	if needle == "" {
		return false
	}
	if strings.Contains(item.Text, needle) {
		return true
	}
	if strings.Contains(item.Command, needle) {
		return true
	}
	if item.AggregatedOutput != nil && strings.Contains(*item.AggregatedOutput, needle) {
		return true
	}
	return strings.Contains(string(item.Content), needle)
}

func readTinyRepoFile(t *testing.T, dir, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", name, err)
	}
	return string(data)
}

func initTinyCommandApprovalRepo(t *testing.T) (string, string, string) {
	t.Helper()

	repo := t.TempDir()
	commandName := fmt.Sprintf("./zz_feidex_probe_%d.sh", time.Now().UnixNano())
	output := fmt.Sprintf("probe-ok-%d", time.Now().UnixNano())
	scriptPath := filepath.Join(repo, strings.TrimPrefix(commandName, "./"))
	writeIntegrationFile(t, scriptPath, "#!/bin/sh\nprintf '%s\\n' '"+output+"'\n")
	if err := os.Chmod(scriptPath, 0o755); err != nil {
		t.Fatalf("Chmod(%q) error = %v", scriptPath, err)
	}
	return repo, commandName, output
}
