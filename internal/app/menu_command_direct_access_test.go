package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"feidex/internal/codexrpc"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

func TestCommandThreadDirectSandboxAndPolicy(t *testing.T) {
	a, ff, _ := newTestApp(t)
	msg := &feishu.InboundMessage{MessageID: "m-1", ChatID: "chat-1", ChatType: "group", UserID: "user-1"}
	sessionKey := a.makeSessionKey(msg)
	if err := a.store.UpsertSession(&state.Session{
		Key:                     sessionKey,
		WorkspaceID:             a.cfg.Workspaces[0].ID,
		ActiveThreadID:          "thread-1",
		ActiveThreadWorkspaceID: a.cfg.Workspaces[0].ID,
	}); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}

	if err := a.commandThread(msg, []string{"sandbox", "read-only"}); err != nil {
		t.Fatalf("commandThread(sandbox set) error = %v", err)
	}
	if got := a.store.GetSession(sessionKey); got == nil || got.ActiveThreadSandboxMode != "read-only" {
		t.Fatalf("thread sandbox session = %+v", got)
	}
	if len(ff.replyCards) != 1 {
		t.Fatalf("reply card count after sandbox = %d, want 1", len(ff.replyCards))
	}
	if body := cardMarkdownContent(t, ff.replyCards[0]); !strings.Contains(body, "当前值: `read-only`") {
		t.Fatalf("sandbox card body = %q", body)
	}

	if err := a.commandThread(msg, []string{"policy", "never"}); err != nil {
		t.Fatalf("commandThread(policy set) error = %v", err)
	}
	if got := a.store.GetSession(sessionKey); got == nil || got.ActiveThreadApprovalPolicy != "never" {
		t.Fatalf("thread policy session = %+v", got)
	}
	if len(ff.replyCards) != 2 {
		t.Fatalf("reply card count after policy = %d, want 2", len(ff.replyCards))
	}
	if body := cardMarkdownContent(t, ff.replyCards[1]); !strings.Contains(body, "当前值: `never`") {
		t.Fatalf("policy card body = %q", body)
	}
}

func TestCommandThreadDirectResume(t *testing.T) {
	a, ff, fc := newTestApp(t)
	msg := &feishu.InboundMessage{MessageID: "m-1", ChatID: "chat-1", ChatType: "group", UserID: "user-1"}
	sessionKey := a.makeSessionKey(msg)
	if err := a.store.UpsertSession(&state.Session{
		Key:                     sessionKey,
		WorkspaceID:             a.cfg.Workspaces[0].ID,
		ActiveThreadID:          "thread-1",
		ActiveThreadWorkspaceID: a.cfg.Workspaces[0].ID,
		OwnerUserID:             msg.UserID,
		ChatID:                  msg.ChatID,
		ChatType:                msg.ChatType,
		Status:                  "idle",
	}); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}

	fc.callHook = func(_ context.Context, method string, params any, out any) error {
		switch method {
		case "thread/resume":
			raw, _ := params.(map[string]any)
			if got, _ := raw["threadId"].(string); got != "thread-2" {
				t.Fatalf("thread/resume threadId = %q, want thread-2", got)
			}
			result := out.(*codexrpc.ThreadStartResult)
			result.Thread.ID = "thread-2"
			result.Thread.Name = "Resumed Thread"
			result.Thread.Preview = "resumed preview"
		case "thread/list":
			result := out.(*codexrpc.ThreadListResult)
			result.Data = []codexrpc.ThreadListEntry{{
				ID:        "thread-2",
				Name:      "Resumed Thread",
				Preview:   "resumed preview",
				Cwd:       a.cfg.Workspaces[0].Cwd,
				UpdatedAt: 1,
			}}
		default:
			t.Fatalf("unexpected codex method: %s", method)
		}
		return nil
	}

	if err := a.commandThread(msg, []string{"resume", "thread-2"}); err != nil {
		t.Fatalf("commandThread(resume) error = %v", err)
	}
	if got := a.store.GetSession(sessionKey); got == nil || got.ActiveThreadID != "thread-2" || got.ActiveThreadName != "Resumed Thread" {
		t.Fatalf("session after thread resume = %+v", got)
	}
	if len(ff.replyCards) != 1 {
		t.Fatalf("reply card count after resume = %d, want 1", len(ff.replyCards))
	}
	if body := cardMarkdownContent(t, ff.replyCards[0]); !strings.Contains(body, "当前 thread id: `thread-2`") {
		t.Fatalf("resume card body = %q", body)
	}
}

func TestCommandWorkspaceDirectSandboxAndPolicy(t *testing.T) {
	a, ff, _ := newTestApp(t)
	msg := &feishu.InboundMessage{MessageID: "m-1", ChatID: "chat-1", ChatType: "group", UserID: "user-1"}
	sessionKey := a.makeSessionKey(msg)
	if err := a.store.UpsertSession(&state.Session{
		Key:         sessionKey,
		WorkspaceID: a.cfg.Workspaces[0].ID,
	}); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}

	if err := a.commandWorkspace(msg, []string{"sandbox", "read-only"}); err != nil {
		t.Fatalf("commandWorkspace(sandbox set) error = %v", err)
	}
	if got := a.cfg.Workspaces[0].SandboxMode; got != "read-only" {
		t.Fatalf("workspace sandbox = %q, want read-only", got)
	}
	if len(ff.replyCards) != 1 {
		t.Fatalf("reply card count after sandbox = %d, want 1", len(ff.replyCards))
	}
	if body := cardMarkdownContent(t, ff.replyCards[0]); !strings.Contains(body, "当前值: `read-only`") {
		t.Fatalf("workspace sandbox card body = %q", body)
	}

	if err := a.commandWorkspace(msg, []string{"policy", "never"}); err != nil {
		t.Fatalf("commandWorkspace(policy set) error = %v", err)
	}
	if got := a.cfg.Workspaces[0].ApprovalPolicy; got != "never" {
		t.Fatalf("workspace policy = %q, want never", got)
	}
	if len(ff.replyCards) != 2 {
		t.Fatalf("reply card count after policy = %d, want 2", len(ff.replyCards))
	}
	if body := cardMarkdownContent(t, ff.replyCards[1]); !strings.Contains(body, "当前值: `never`") {
		t.Fatalf("workspace policy card body = %q", body)
	}
}

func TestCommandModelDirectSetAndEffort(t *testing.T) {
	a, ff, fc := newTestApp(t)
	msg := &feishu.InboundMessage{MessageID: "m-1", ChatID: "chat-1", ChatType: "group", UserID: "user-1"}
	fc.callHook = func(_ context.Context, method string, _ any, out any) error {
		if method != "model/list" {
			t.Fatalf("unexpected codex method: %s", method)
		}
		*out.(*codexrpc.ModelListResult) = codexrpc.ModelListResult{
			Data: []codexrpc.ModelListEntry{
				{
					ID:                     "gpt-5",
					Model:                  "gpt-5",
					DisplayName:            "GPT-5",
					DefaultReasoningEffort: "medium",
					SupportedReasoningEfforts: []codexrpc.ModelReasoningEffortEntry{
						{ReasoningEffort: "low"},
						{ReasoningEffort: "medium"},
						{ReasoningEffort: "high"},
					},
					IsDefault: true,
				},
			},
		}
		return nil
	}

	if err := a.commandModel(msg, []string{"set", "gpt-5"}); err != nil {
		t.Fatalf("commandModel(set) error = %v", err)
	}
	if got := a.cfg.Codex.Model; got != "gpt-5" {
		t.Fatalf("global model = %q, want gpt-5", got)
	}
	if len(ff.replyCards) != 1 {
		t.Fatalf("reply card count after model set = %d, want 1", len(ff.replyCards))
	}

	if err := a.commandModel(msg, []string{"effort", "high"}); err != nil {
		t.Fatalf("commandModel(effort) error = %v", err)
	}
	if got := a.cfg.Codex.ReasoningEffort; got != "high" {
		t.Fatalf("global reasoning effort = %q, want high", got)
	}
	if len(ff.replyCards) != 2 {
		t.Fatalf("reply card count after effort set = %d, want 2", len(ff.replyCards))
	}

	if err := a.commandModel(msg, []string{"set", "missing"}); err != nil {
		t.Fatalf("commandModel(set missing) error = %v", err)
	}
	if len(ff.replyTexts) != 1 {
		t.Fatalf("reply text count after missing model = %d, want 1", len(ff.replyTexts))
	}
	if got := ff.replyTexts[0]; !strings.Contains(got, "未找到 model: missing") {
		t.Fatalf("missing model reply = %q", got)
	}
}

func TestCommandHistoryDirectDetail(t *testing.T) {
	a, ff, fc := newTestApp(t)
	msg := &feishu.InboundMessage{MessageID: "m-1", ChatID: "chat-1", ChatType: "group", UserID: "user-1"}
	sessionKey := a.makeSessionKey(msg)
	if err := a.store.UpsertSession(&state.Session{
		Key:            sessionKey,
		WorkspaceID:    a.cfg.Workspaces[0].ID,
		ActiveThreadID: "thread-1",
	}); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}

	fc.callHook = func(_ context.Context, method string, _ any, out any) error {
		if method != "thread/read" {
			t.Fatalf("unexpected codex method: %s", method)
		}
		result := out.(*codexrpc.ThreadReadResult)
		result.Thread.ID = "thread-1"
		result.Thread.Turns = []codexrpc.ThreadReadTurn{
			{
				ID:     "turn-1",
				Status: "completed",
				Items: []codexrpc.ThreadReadItem{
					{Type: "userMessage", ID: "item-u1", Content: json.RawMessage(`[{"type":"text","text":"first input","text_elements":[]}]`)},
					{Type: "agentMessage", ID: "item-a1", Text: "first answer"},
				},
			},
			{
				ID:     "turn-2",
				Status: "completed",
				Items: []codexrpc.ThreadReadItem{
					{Type: "userMessage", ID: "item-u2", Content: json.RawMessage(`[{"type":"text","text":"second input","text_elements":[]}]`)},
					{Type: "agentMessage", ID: "item-a2", Text: "second answer"},
				},
			},
		}
		return nil
	}

	if err := a.commandHistory(msg, []string{"detail", "1"}); err != nil {
		t.Fatalf("commandHistory(detail) error = %v", err)
	}
	if len(ff.replyCards) != 1 {
		t.Fatalf("reply card count after history detail = %d, want 1", len(ff.replyCards))
	}
	body := cardMarkdownContent(t, ff.replyCards[0])
	if !strings.Contains(body, "Turn #1") || !strings.Contains(body, "first input") {
		t.Fatalf("history detail body = %q", body)
	}
	if strings.Contains(body, "Turn #2") {
		t.Fatalf("history detail should resolve by turn number, got %q", body)
	}
}
