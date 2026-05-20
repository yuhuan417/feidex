package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"feidex/internal/app/turnitem"
	"feidex/internal/state"
)

func performMCPHTTPRequest(t *testing.T, handler http.Handler, token, sessionKey, body string) *httptest.ResponseRecorder {
	t.Helper()
	target := "http://127.0.0.1" + feidexMCPPath
	if strings.TrimSpace(sessionKey) != "" {
		target += "?" + feidexMCPSessionKeyName + "=" + sessionKey
	}
	req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(body))
	req.RemoteAddr = "127.0.0.1:12345"
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func TestFeidexMCPToolsList(t *testing.T) {
	a, _, _ := newTestApp(t)
	svc, err := newFeidexMCPService(a)
	if err != nil {
		t.Fatalf("newFeidexMCPService() error = %v", err)
	}
	rec := performMCPHTTPRequest(t, svc.handler, svc.token, "", `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("tools/list code = %d body=%s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal tools/list: %v", err)
	}
	if len(payload.Result.Tools) != 3 {
		t.Fatalf("tools/list count = %d, want 3", len(payload.Result.Tools))
	}
}

func TestFeidexMCPSendsCodexFileAttachment(t *testing.T) {
	a, ff, _ := newTestApp(t)
	sub := seedActiveSubmission(t, a, "sess-1", "thread-1", "turn-1")
	path := filepath.Join(t.TempDir(), "report.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	newRuntimeStateService(a).noteTurnItemStartedPayload("thread-1", "turn-1", turnitem.NewProtocolItemWithID("item-1", map[string]any{
		"id":        "item-1",
		"type":      "mcpToolCall",
		"server":    feidexMCPServerID,
		"tool":      feidexSendIMFileToolName,
		"status":    "inProgress",
		"arguments": map[string]any{"path": path},
	}))
	svc, err := newFeidexMCPService(a)
	if err != nil {
		t.Fatalf("newFeidexMCPService() error = %v", err)
	}
	rec := performMCPHTTPRequest(t, svc.handler, svc.token, "", `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"feishu_send_im_file","arguments":{"path":"`+path+`"}}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("tools/call code = %d body=%s", rec.Code, rec.Body.String())
	}
	if got := ff.replyLocalAttachmentCalls; len(got) != 1 || got[0] != path {
		t.Fatalf("replyLocalAttachmentCalls = %v", got)
	}
	if sub.TriggerMessageID == "" {
		t.Fatal("expected seeded trigger message id")
	}
}

func TestFeidexMCPRequiresClaudeSessionKeyForDynamicMCPTools(t *testing.T) {
	a, ff, _ := newTestApp(t)
	sessionKey := "sess-claude"
	path := filepath.Join(t.TempDir(), "image.png")
	if err := os.WriteFile(path, []byte("png"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	seedActiveSubmission(t, a, sessionKey, "thread-2", "turn-2")
	newRuntimeStateService(a).noteTurnItemStartedPayload("thread-2", "turn-2", turnitem.NewProtocolItemWithID("item-2", map[string]any{
		"id":     "item-2",
		"type":   "dynamic_tool_call",
		"tool":   "mcp__feidex-send__feishu_send_im_image",
		"status": "in_progress",
		"input":  map[string]any{"path": path},
	}))
	svc, err := newFeidexMCPService(a)
	if err != nil {
		t.Fatalf("newFeidexMCPService() error = %v", err)
	}
	rec := performMCPHTTPRequest(t, svc.handler, svc.token, "", `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"feishu_send_im_image","arguments":{"path":"`+path+`"}}}`)
	if !strings.Contains(rec.Body.String(), `"isError":true`) {
		t.Fatalf("expected fail-closed error without session key, got body=%s", rec.Body.String())
	}
	rec = performMCPHTTPRequest(t, svc.handler, svc.token, sessionKey, `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"feishu_send_im_image","arguments":{"path":"`+path+`"}}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("tools/call code = %d body=%s", rec.Code, rec.Body.String())
	}
	if got := ff.replyLocalImageCalls; len(got) != 1 || got[0] != path {
		t.Fatalf("replyLocalImageCalls = %v", got)
	}
}

func TestFeidexMCPFailsClosedOnAmbiguousMatch(t *testing.T) {
	a, ff, _ := newTestApp(t)
	path := filepath.Join(t.TempDir(), "artifact.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	seedActiveSubmission(t, a, "sess-1", "thread-1", "turn-1")
	if err := a.store.UpsertSession(&state.Session{
		Key:                "sess-2",
		WorkspaceID:        a.cfg.Workspaces[0].ID,
		ActiveThreadID:     "thread-2",
		ActiveTurnID:       "turn-2",
		ActiveSubmissionID: "sub-2",
		OwnerUserID:        "user-1",
		ChatID:             "chat-1",
		ChatType:           "group",
		Status:             "running",
	}); err != nil {
		t.Fatalf("UpsertSession(second) error = %v", err)
	}
	if _, err := a.store.CreateSubmission(&state.Submission{
		ID:               "sub-2",
		SessionKey:       "sess-2",
		WorkspaceID:      a.cfg.Workspaces[0].ID,
		ThreadID:         "thread-2",
		TurnID:           "turn-2",
		UserID:           "user-1",
		ChatID:           "chat-1",
		TriggerMessageID: "trigger-2",
		Status:           "running",
	}); err != nil {
		t.Fatalf("CreateSubmission(second) error = %v", err)
	}

	for _, tc := range []struct {
		threadID string
		turnID   string
		itemID   string
	}{
		{threadID: "thread-1", turnID: "turn-1", itemID: "item-1"},
		{threadID: "thread-2", turnID: "turn-2", itemID: "item-2"},
	} {
		newRuntimeStateService(a).noteTurnItemStartedPayload(tc.threadID, tc.turnID, turnitem.NewProtocolItemWithID(tc.itemID, map[string]any{
			"id":        tc.itemID,
			"type":      "mcpToolCall",
			"server":    feidexMCPServerID,
			"tool":      feidexSendIMFileToolName,
			"status":    "inProgress",
			"arguments": map[string]any{"path": path},
		}))
	}

	svc, err := newFeidexMCPService(a)
	if err != nil {
		t.Fatalf("newFeidexMCPService() error = %v", err)
	}
	rec := performMCPHTTPRequest(t, svc.handler, svc.token, "", `{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"feishu_send_im_file","arguments":{"path":"`+path+`"}}}`)
	if !strings.Contains(rec.Body.String(), `"isError":true`) {
		t.Fatalf("expected ambiguity to fail closed, got body=%s", rec.Body.String())
	}
	if got := ff.replyLocalAttachmentCalls; len(got) != 0 {
		t.Fatalf("replyLocalAttachmentCalls = %v, want none", got)
	}
}
