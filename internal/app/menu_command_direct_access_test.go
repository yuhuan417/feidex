package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"feidex/internal/codexrpc"
	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

func TestCommandThreadDirectSandboxAndPolicy(t *testing.T) {
	a, ff, _ := newTestApp(t)
	msg := &feishu.InboundMessage{MessageID: "m-1", ChatID: "chat-1", ChatType: "group", UserID: "user-1"}
	sessionKey := makeSessionKey(a, msg)
	if err := a.store.UpsertSession(&state.Session{
		Key:                     sessionKey,
		WorkspaceID:             a.cfg.Workspaces[0].ID,
		ActiveThreadID:          "thread-1",
		ActiveThreadWorkspaceID: a.cfg.Workspaces[0].ID,
	}); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}

	if err := newThreadService(a).CommandThread(msg, []string{"sandbox", "read-only"}); err != nil {
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

	if err := newThreadService(a).CommandThread(msg, []string{"policy", "never"}); err != nil {
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
	sessionKey := makeSessionKey(a, msg)
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

	if err := newThreadService(a).CommandThread(msg, []string{"resume", "thread-2"}); err != nil {
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
	sessionKey := makeSessionKey(a, msg)
	if err := a.store.UpsertSession(&state.Session{
		Key:         sessionKey,
		WorkspaceID: a.cfg.Workspaces[0].ID,
	}); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}

	if err := newWorkspaceService(a).commandWorkspace(msg, []string{"sandbox", "read-only"}); err != nil {
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

	if err := newWorkspaceService(a).commandWorkspace(msg, []string{"policy", "never"}); err != nil {
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

func TestCommandWorkspaceDeleteRemovesConfigOnly(t *testing.T) {
	a, ff, _ := newTestApp(t)
	altDir := filepath.Join(t.TempDir(), "alt-workspace")
	if err := os.MkdirAll(altDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(altDir) error = %v", err)
	}
	a.cfg.Workspaces = append(a.cfg.Workspaces, config.Workspace{ID: "alt", Name: "Alt", Cwd: altDir})

	msg := &feishu.InboundMessage{MessageID: "m-1", ChatID: "chat-1", ChatType: "group", UserID: "user-1"}
	sessionKey := makeSessionKey(a, msg)
	if err := a.store.UpsertSession(&state.Session{Key: sessionKey, WorkspaceID: "default"}); err != nil {
		t.Fatalf("UpsertSession(current) error = %v", err)
	}
	if err := a.store.UpsertSession(&state.Session{
		Key:                     "sess-other",
		WorkspaceID:             "alt",
		ActiveThreadID:          "thread-alt",
		ActiveThreadWorkspaceID: "alt",
	}); err != nil {
		t.Fatalf("UpsertSession(other) error = %v", err)
	}

	if err := newWorkspaceService(a).commandWorkspace(msg, []string{"delete", "alt"}); err != nil {
		t.Fatalf("commandWorkspace(delete alt) error = %v", err)
	}
	if ws := config.FindWorkspace(a.cfg, "alt"); ws != nil {
		t.Fatalf("workspace alt should be removed, got %+v", ws)
	}
	if _, err := os.Stat(altDir); err != nil {
		t.Fatalf("workspace directory should remain, stat error = %v", err)
	}
	if got := a.store.GetSession("sess-other"); got == nil || got.WorkspaceID != "default" || got.ActiveThreadID != "" || got.ActiveThreadWorkspaceID != "" {
		t.Fatalf("other session after delete = %+v", got)
	}
	if len(ff.replyTexts) != 1 || !strings.Contains(ff.replyTexts[0], "仅移除配置，未删除目录") {
		t.Fatalf("workspace delete replyTexts = %+v", ff.replyTexts)
	}
}

func TestCommandWorkspaceDeleteRejectsCurrentWorkspace(t *testing.T) {
	a, _, _ := newTestApp(t)
	altDir := filepath.Join(t.TempDir(), "alt-workspace")
	if err := os.MkdirAll(altDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(altDir) error = %v", err)
	}
	a.cfg.Workspaces = append(a.cfg.Workspaces, config.Workspace{ID: "alt", Name: "Alt", Cwd: altDir})

	msg := &feishu.InboundMessage{MessageID: "m-1", ChatID: "chat-1", ChatType: "group", UserID: "user-1"}
	sessionKey := makeSessionKey(a, msg)
	if err := a.store.UpsertSession(&state.Session{Key: sessionKey, WorkspaceID: "alt"}); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}

	err := newWorkspaceService(a).commandWorkspace(msg, []string{"delete", "alt"})
	if err == nil {
		t.Fatal("expected deleting current workspace to fail")
	}
	if !strings.Contains(err.Error(), "不能删除当前 workspace") {
		t.Fatalf("delete current workspace error = %v", err)
	}
	if ws := config.FindWorkspace(a.cfg, "alt"); ws == nil {
		t.Fatal("workspace alt should remain after rejected delete")
	}
}

func TestCommandModelDirectSetAndEffort(t *testing.T) {
	a, ff, fc := newTestApp(t)
	msg := &feishu.InboundMessage{MessageID: "m-1", ChatID: "chat-1", ChatType: "group", UserID: "user-1"}
	fc.callHook = func(_ context.Context, method string, _ any, out any) error {
		switch method {
		case "model/list":
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
		case "collaborationMode/list":
			*out.(*codexrpc.CollaborationModeListResponse) = codexrpc.CollaborationModeListResponse{
				Data: []codexrpc.CollaborationModeMask{
					{Name: "Plan", Mode: stringPtr("plan"), ReasoningEffort: stringPtr("medium")},
				},
			}
			return nil
		default:
			t.Fatalf("unexpected codex method: %s", method)
			return nil
		}
	}

	if err := newBackendConfigurationService(a).handleBackendModelCommand(msg, []string{"set", "gpt-5"}); err != nil {
		t.Fatalf("commandModel(set) error = %v", err)
	}
	if got := a.cfg.Codex.Model; got != "gpt-5" {
		t.Fatalf("global model = %q, want gpt-5", got)
	}
	if len(ff.replyCards) != 1 {
		t.Fatalf("reply card count after model set = %d, want 1", len(ff.replyCards))
	}

	if err := newBackendConfigurationService(a).handleBackendModelCommand(msg, []string{"effort", "high"}); err != nil {
		t.Fatalf("commandModel(effort) error = %v", err)
	}
	if got := a.cfg.Codex.ReasoningEffort; got != "high" {
		t.Fatalf("global reasoning effort = %q, want high", got)
	}
	if len(ff.replyCards) != 2 {
		t.Fatalf("reply card count after effort set = %d, want 2", len(ff.replyCards))
	}

	if err := newBackendConfigurationService(a).handleBackendModelCommand(msg, []string{"plan", "set", "gpt-5"}); err != nil {
		t.Fatalf("commandModel(plan set) error = %v", err)
	}
	if got := a.cfg.Codex.PlanModel; got != "gpt-5" {
		t.Fatalf("plan model = %q, want gpt-5", got)
	}
	if len(ff.replyCards) != 3 {
		t.Fatalf("reply card count after plan model set = %d, want 3", len(ff.replyCards))
	}

	if err := newBackendConfigurationService(a).handleBackendModelCommand(msg, []string{"plan", "effort", "high"}); err != nil {
		t.Fatalf("commandModel(plan effort) error = %v", err)
	}
	if got := a.cfg.Codex.PlanReasoningEffort; got != "high" {
		t.Fatalf("plan reasoning effort = %q, want high", got)
	}
	if len(ff.replyCards) != 4 {
		t.Fatalf("reply card count after plan effort set = %d, want 4", len(ff.replyCards))
	}

	if err := newBackendConfigurationService(a).handleBackendModelCommand(msg, []string{"set", "missing"}); err != nil {
		t.Fatalf("commandModel(set missing) error = %v", err)
	}
	if len(ff.replyTexts) != 1 {
		t.Fatalf("reply text count after missing model = %d, want 1", len(ff.replyTexts))
	}
	if got := ff.replyTexts[0]; !strings.Contains(got, "未找到 model: missing") {
		t.Fatalf("missing model reply = %q", got)
	}
}

func TestCommandModelDirectSetAndEffortForClaude(t *testing.T) {
	a, ff, _ := newTestApp(t)
	a.backend = backendClaude
	a.cfg.Feishu.Backend = backendClaude
	claude := &fakeClaudeCore{}
	a.claude = claude

	msg := &feishu.InboundMessage{MessageID: "m-1", ChatID: "chat-1", ChatType: "group", UserID: "user-1"}

	if err := newBackendConfigurationService(a).handleBackendModelCommand(msg, nil); err != nil {
		t.Fatalf("commandModel() error = %v", err)
	}
	if len(ff.replyCards) != 1 {
		t.Fatalf("reply card count after Claude /model = %d, want 1", len(ff.replyCards))
	}
	if selects := cardSelectStaticForTest(ff.replyCards[0]); len(selects) != 2 {
		t.Fatalf("Claude /model selects = %+v, want 2", selects)
	}

	if err := newBackendConfigurationService(a).handleBackendModelCommand(msg, []string{"set", "mimo-v2-pro"}); err != nil {
		t.Fatalf("commandModel(set) error = %v", err)
	}
	if got := a.cfg.Claude.Model; got != "mimo-v2-pro" {
		t.Fatalf("Claude model = %q, want mimo-v2-pro", got)
	}
	if len(claude.updatedConfigs) != 1 || claude.updatedConfigs[0].Model != "mimo-v2-pro" {
		t.Fatalf("updated Claude configs = %+v", claude.updatedConfigs)
	}

	if err := newModelConfigService(a).commandEffort(msg, []string{"max"}); err != nil {
		t.Fatalf("commandEffort(max) error = %v", err)
	}
	if got := a.cfg.Claude.Effort; got != "max" {
		t.Fatalf("Claude effort = %q, want max", got)
	}
	if len(claude.updatedConfigs) != 2 || claude.updatedConfigs[1].Effort != "max" {
		t.Fatalf("updated Claude configs after effort = %+v", claude.updatedConfigs)
	}

	if err := newBackendConfigurationService(a).handleBackendModelCommand(msg, []string{"set", "default"}); err != nil {
		t.Fatalf("commandModel(set default) error = %v", err)
	}
	if got := a.cfg.Claude.Model; got != "sonnet" {
		t.Fatalf("Claude default model = %q, want sonnet", got)
	}
}

func TestCommandModelOptionAddAndRemoveForClaude(t *testing.T) {
	a, _, _ := newTestApp(t)
	a.backend = backendClaude
	a.cfg.Feishu.Backend = backendClaude
	claude := &fakeClaudeCore{}
	a.claude = claude

	msg := &feishu.InboundMessage{MessageID: "m-1", ChatID: "chat-1", ChatType: "group", UserID: "user-1"}
	newRuntimeStateService(a).beginFrontendMessageTraffic()
	defer newRuntimeStateService(a).finishFrontendMessageTraffic()

	if err := newBackendConfigurationService(a).handleBackendModelCommand(msg, []string{"option", "add", "deepseek-v4-pro"}); err != nil {
		t.Fatalf("commandModel(option add) error = %v", err)
	}
	if got := a.cfg.Claude.ModelOptions; len(got) != 1 || got[0] != "deepseek-v4-pro" {
		t.Fatalf("Claude model options after add = %+v", got)
	}
	if len(claude.updatedConfigs) != 0 {
		t.Fatalf("updated Claude configs after option add = %+v, want none", claude.updatedConfigs)
	}

	if err := newBackendConfigurationService(a).handleBackendModelCommand(msg, []string{"option", "remove", "deepseek-v4-pro"}); err != nil {
		t.Fatalf("commandModel(option remove) error = %v", err)
	}
	if got := a.cfg.Claude.ModelOptions; len(got) != 0 {
		t.Fatalf("Claude model options after remove = %+v", got)
	}
}

func TestCommandModelDirectSetRawClaudeModelDuringMessageTraffic(t *testing.T) {
	a, _, _ := newTestApp(t)
	a.backend = backendClaude
	a.cfg.Feishu.Backend = backendClaude
	claude := &fakeClaudeCore{}
	a.claude = claude

	msg := &feishu.InboundMessage{MessageID: "m-1", ChatID: "chat-1", ChatType: "p2p", UserID: "user-1", Text: "/model set deepseek-v4-pro"}
	newRuntimeStateService(a).beginFrontendMessageTraffic()
	defer newRuntimeStateService(a).finishFrontendMessageTraffic()

	if err := handleCommand(a, msg, msg.Text); err != nil {
		t.Fatalf("handleCommand(/model set raw Claude model) error = %v", err)
	}
	if got := a.cfg.Claude.Model; got != "deepseek-v4-pro" {
		t.Fatalf("Claude model = %q, want deepseek-v4-pro", got)
	}
	if len(claude.updatedConfigs) != 1 || claude.updatedConfigs[0].Model != "deepseek-v4-pro" {
		t.Fatalf("updated Claude configs = %+v", claude.updatedConfigs)
	}
}

func TestCommandModelDirectSetClaudeModelRejectsConcurrentMessageTraffic(t *testing.T) {
	a, _, _ := newTestApp(t)
	a.backend = backendClaude
	a.cfg.Feishu.Backend = backendClaude
	claude := &fakeClaudeCore{}
	a.claude = claude

	msg := &feishu.InboundMessage{MessageID: "m-1", ChatID: "chat-1", ChatType: "group", UserID: "user-1", Text: "/model set deepseek-v4-pro"}
	rss := newRuntimeStateService(a)
	rss.beginFrontendMessageTraffic()
	defer rss.finishFrontendMessageTraffic()
	rss.beginFrontendMessageTraffic()
	defer rss.finishFrontendMessageTraffic()

	if err := handleCommand(a, msg, msg.Text); err != nil {
		t.Fatalf("handleCommand(/model set raw Claude model) error = %v", err)
	}
	if got := a.cfg.Claude.Model; got == "deepseek-v4-pro" {
		t.Fatalf("Claude model changed despite concurrent traffic: %q", got)
	}
	if len(claude.updatedConfigs) != 0 {
		t.Fatalf("updated Claude configs = %+v, want none", claude.updatedConfigs)
	}
}

func TestCommandHistoryDirectDetail(t *testing.T) {
	a, ff, fc := newTestApp(t)
	msg := &feishu.InboundMessage{MessageID: "m-1", ChatID: "chat-1", ChatType: "group", UserID: "user-1"}
	sessionKey := makeSessionKey(a, msg)
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

	if err := newHistoryService(a).CommandHistory(msg, []string{"detail", "1"}); err != nil {
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
