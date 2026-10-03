package app

import (
	"encoding/json"
	"feidex/internal/adapter/feishu/pendingforms"
	domainbackend "feidex/internal/domain/backend"
	domainsubmission "feidex/internal/domain/submission"

	appturnlifecycle "feidex/internal/application/turn"
	"feidex/internal/codexrpc"
	"feidex/internal/domain/conversation"
	"path/filepath"

	appapproval "feidex/internal/adapter/feishu/approval"

	applinkutil "feidex/internal/adapter/feishu/linkutil"
	"feidex/internal/adapter/feishu/turnitem"
	"feidex/internal/state"
	"strings"
	"testing"
)

func TestNormalizeCardMarkdownOnlyTrimsWhitespace(t *testing.T) {
	input := "\n\n```txt\nhello\n```\n\n"
	got := applinkutil.NormalizeCardMarkdown(input)
	want := "```txt\nhello\n```"
	if got != want {
		t.Fatalf("applinkutil.NormalizeCardMarkdown() should only trim, got: %q", got)
	}
}

func TestMarkdownCodeBlockWithLangUsesDynamicOuterFence(t *testing.T) {
	got := applinkutil.NormalizeCardMarkdown("命令:\n" + turnitem.MarkdownCodeBlockWithLang("bash", "pwd"))
	if !strings.Contains(got, "````bash\npwd\n````") {
		t.Fatalf("simple content should keep 4-backtick outer fence, got: %q", got)
	}
	got = turnitem.MarkdownCodeBlockWithLang("", "keep ```` inner")
	if !strings.HasPrefix(got, "`````\n") || !strings.HasSuffix(got, "\n`````") {
		t.Fatalf("outer fence should be inner max+1 when inner has 4 backticks, got: %q", got)
	}
}

func TestApprovalButtonsCoverFullDecisionSet(t *testing.T) {
	commandButtons := appapproval.Buttons("command", "req-1")
	if len(commandButtons) != 4 {
		t.Fatalf("expected 4 command approval buttons, got %d", len(commandButtons))
	}
	commandTexts := map[string]bool{}
	for _, btn := range commandButtons {
		commandTexts[btn.Text] = true
	}
	for _, want := range []string{
		"允许一次",
		"本会话允许",
		"拒绝",
		"拒绝并中断",
	} {
		if !commandTexts[want] {
			t.Fatalf("missing command approval button %q in %#v", want, commandButtons)
		}
	}

	fileButtons := appapproval.Buttons("file", "req-2")
	if len(fileButtons) != 4 {
		t.Fatalf("expected 4 file approval buttons, got %d", len(fileButtons))
	}
	fileTexts := map[string]bool{}
	for _, btn := range fileButtons {
		fileTexts[btn.Text] = true
	}
	for _, want := range []string{"允许一次", "本会话允许", "拒绝", "拒绝并中断"} {
		if !fileTexts[want] {
			t.Fatalf("missing file approval button %q in %#v", want, fileButtons)
		}
	}
}

func TestTurnCompletionTerminalTextAlwaysNotifyInterrupted(t *testing.T) {
	terminalText := appturnlifecycle.TurnCompletionTerminalText("interrupted", "")
	if terminalText != "任务已中断。" {
		t.Fatalf("unexpected interrupted terminal text: %q", terminalText)
	}
}

func TestTurnCompletionTerminalTextKeepsCompletedSilent(t *testing.T) {
	terminalText := appturnlifecycle.TurnCompletionTerminalText("completed", "")
	if terminalText != "" {
		t.Fatalf("expected no terminal notice for completed turn, got %q", terminalText)
	}
}

func TestTurnStartedNotificationRebindsPendingSubmission(t *testing.T) {
	store, err := state.Open(t.TempDir() + "/state.json")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	a := &App{store: store, registry: testRegistryWithTrackers(&appTrackers{turnStreams: newTurnStreamTracker()})}
	if err := a.store.UpsertSession(&conversation.Session{
		Key:                     "sess-1",
		WorkspaceID:             "default",
		ActiveThreadID:          "thread-1",
		ActiveThreadWorkspaceID: "default",
		ActiveSubmissionID:      "sub-1",
		Status:                  "turn_starting",
	}); err != nil {
		t.Fatalf("upsert session: %v", err)
	}
	_, err = a.store.CreateSubmission(&domainsubmission.Submission{
		ID:          "sub-1",
		SessionKey:  "sess-1",
		WorkspaceID: "default",
		Status:      "queued",
	})
	if err != nil {
		t.Fatalf("create submission: %v", err)
	}

	onTurnStartedNotification(a, "thread-1", "turn-1")

	sess := a.store.GetSession("sess-1")
	if sess == nil {
		t.Fatal("expected session")
	}
	if sess.ActiveTurnID != "turn-1" || sess.Status != "turn_in_progress" {
		t.Fatalf("unexpected rebound session: %#v", sess)
	}
	sub := a.store.GetSubmission("sub-1")
	if sub == nil {
		t.Fatal("expected submission")
	}
	if sub.TurnID != "turn-1" || sub.ThreadID != "thread-1" || sub.Status != "running" {
		t.Fatalf("unexpected rebound submission: %#v", sub)
	}
}

func TestFindSubmissionByTurnFallsBackToActiveSubmissionOnThread(t *testing.T) {
	store, err := state.Open(t.TempDir() + "/state.json")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	a := &App{store: store}
	if err := a.store.UpsertSession(&conversation.Session{
		Key:                "sess-1",
		WorkspaceID:        "default",
		ActiveThreadID:     "thread-1",
		ActiveSubmissionID: "sub-1",
		Status:             "turn_starting",
	}); err != nil {
		t.Fatalf("upsert session: %v", err)
	}
	_, err = a.store.CreateSubmission(&domainsubmission.Submission{
		ID:          "sub-1",
		SessionKey:  "sess-1",
		WorkspaceID: "default",
		Status:      "queued",
	})
	if err != nil {
		t.Fatalf("create submission: %v", err)
	}
	sessionKey, sub := newSubmissionQueueServiceFromApp(a).FindSubmissionByTurn("thread-1", "")
	if sessionKey != "sess-1" || sub == nil || sub.ID != "sub-1" {
		t.Fatalf("unexpected fallback result: session=%q sub=%#v", sessionKey, sub)
	}
}

// --- merged from notifications_more_test.go ---

func TestApprovalRenderingHelpers(t *testing.T) {
	commandBody := appapproval.RenderCommandBody(map[string]any{
		"command":                "pwd",
		"cwd":                    "/tmp/work",
		"reason":                 "needed",
		"networkApprovalContext": map[string]any{"protocol": "https", "host": "api.example.com"},
	})
	if !strings.Contains(commandBody, "命令审批") ||
		!strings.Contains(commandBody, "工作目录") ||
		!strings.Contains(commandBody, "needed") ||
		!strings.Contains(commandBody, "https://api.example.com") {
		t.Fatalf("appapproval.RenderCommandBody() = %q", commandBody)
	}

	permissionsBody := appapproval.RenderPermissionsApprovalBody(map[string]any{
		"reason": "need access",
		"permissions": map[string]any{
			"scope": "session",
			"fileSystem": map[string]any{
				"read":  []any{"/tmp/a"},
				"write": []any{"/tmp/b"},
			},
			"network": map[string]any{
				"enabled": true,
			},
		},
	})
	if !strings.Contains(permissionsBody, "权限摘要") ||
		!strings.Contains(permissionsBody, "fileSystem") ||
		!strings.Contains(permissionsBody, "read: `/tmp/a`") ||
		!strings.Contains(permissionsBody, "write: `/tmp/b`") ||
		!strings.Contains(permissionsBody, "network") ||
		!strings.Contains(permissionsBody, "enabled: 允许") {
		t.Fatalf("appapproval.RenderPermissionsApprovalBody() = %q", permissionsBody)
	}
	if values := appapproval.SummarizePermissions(map[string]any{"nested": map[string]any{"enabled": true}}); len(values) == 0 {
		t.Fatalf("appapproval.SummarizePermissions(flatten) = %+v, want fallback values", values)
	}
	claudeWebFetchBody := appapproval.RenderPermissionsApprovalBody(map[string]any{
		"tool": "WebFetch",
		"tool_input": map[string]any{
			"url":    "https://docs.anthropic.com/en/docs/claude-code/sdk",
			"prompt": "read the sdk docs",
		},
		"permissions": map[string]any{
			"tool": "WebFetch",
		},
	})
	if !strings.Contains(claudeWebFetchBody, "工具请求") ||
		!strings.Contains(claudeWebFetchBody, "tool: `WebFetch`") ||
		!strings.Contains(claudeWebFetchBody, "url: `https://docs.anthropic.com/en/docs/claude-code/sdk`") ||
		!strings.Contains(claudeWebFetchBody, "prompt: `read the sdk docs`") {
		t.Fatalf("appapproval.RenderPermissionsApprovalBody(claude webfetch) = %q", claudeWebFetchBody)
	}
	claudeGrepBody := appapproval.RenderPermissionsApprovalBody(map[string]any{
		"tool": "Grep",
		"tool_input": map[string]any{
			"path":    "README.md",
			"pattern": "sdk",
			"glob":    "*.md",
		},
		"permissions": map[string]any{
			"tool": "Grep",
		},
	})
	if !strings.Contains(claudeGrepBody, "tool: `Grep`") ||
		!strings.Contains(claudeGrepBody, "path: `README.md`") ||
		!strings.Contains(claudeGrepBody, "pattern: `sdk`") ||
		!strings.Contains(claudeGrepBody, "glob: `*.md`") {
		t.Fatalf("appapproval.RenderPermissionsApprovalBody(claude grep) = %q", claudeGrepBody)
	}
	if values := appapproval.CollectPermissionPaths(map[string]any{"roots": []any{"/tmp/b", "/tmp/a", "/tmp/a"}}); len(values) != 2 || values[0] != "/tmp/a" {
		t.Fatalf("appapproval.CollectPermissionPaths() = %+v, want sorted unique paths", values)
	}
	if got := appapproval.FlattenPermissionScalars("root", map[string]any{"enabled": true, "value": float64(1)}, 0); len(got) != 2 {
		t.Fatalf("appapproval.FlattenPermissionScalars() = %+v, want scalar lines", got)
	}

	fileBody := appapproval.RenderFileBody(map[string]any{
		"changes": []any{
			map[string]any{"oldPath": "a.txt", "newPath": "b.txt", "kind": "rename"},
			"README.md",
		},
		"grantRoot": "/repo/tmp",
		"reason":    "review",
	})
	if !strings.Contains(fileBody, "变更摘要") ||
		!strings.Contains(fileBody, "文件数: 2") ||
		!strings.Contains(fileBody, "文件列表") ||
		!strings.Contains(fileBody, "`a.txt -> b.txt` · 重命名") ||
		!strings.Contains(fileBody, "授权根目录") ||
		!strings.Contains(fileBody, "/repo/tmp") {
		t.Fatalf("appapproval.RenderFileBody() = %q", fileBody)
	}
	workspace := t.TempDir()
	fileBody = appapproval.RenderFileBodyWithWorkspace(map[string]any{
		"changes": []any{
			map[string]any{"path": filepath.Join(workspace, "internal", "app", "main.go"), "kind": "modified"},
			map[string]any{"path": "/tmp/outside.go", "kind": "modified"},
		},
	}, workspace)
	if !strings.Contains(fileBody, "`internal/app/main.go` · 修改") || !strings.Contains(fileBody, "`/tmp/outside.go` · 修改") {
		t.Fatalf("appapproval.RenderFileBodyWithWorkspace() = %q", fileBody)
	}
	fileBody = appapproval.RenderFileBodyWithWorkspace(map[string]any{
		"changes": []map[string]any{
			{"path": filepath.Join(workspace, "static", "shared.css"), "kind": "Write"},
		},
	}, workspace)
	if strings.Contains(fileBody, `"changes"`) || !strings.Contains(fileBody, "`static/shared.css` · 写入") {
		t.Fatalf("appapproval.RenderFileBodyWithWorkspace(typed slice) = %q", fileBody)
	}
	entries := appapproval.CollectFileEntries(map[string]any{
		"payload": map[string]any{
			"fileChanges": []any{map[string]any{"path": "main.go", "status": "modified"}},
		},
	})
	if len(entries) != 1 || entries[0].Path != "main.go" {
		t.Fatalf("appapproval.CollectFileEntries() = %+v", entries)
	}
	entries = appapproval.CollectFileEntries(map[string]any{
		"changes": []map[string]any{{"path": "static/shared.css", "kind": "Write"}},
	})
	if len(entries) != 1 || entries[0].Path != "static/shared.css" || entries[0].Kind != "Write" {
		t.Fatalf("appapproval.CollectFileEntries(typed slice) = %+v", entries)
	}
	if got := appapproval.ParseFileEntry(map[string]any{"filePath": "go.mod", "changeType": "modified"}); got.Path != "go.mod" || got.Kind != "modified" {
		t.Fatalf("appapproval.ParseFileEntry() = %+v", got)
	}
	if got := appapproval.ParseFileEntryWithWorkspace(map[string]any{"path": filepath.Join(workspace, "go.mod"), "changeType": "modified"}, workspace); got.Path != "go.mod" || got.Kind != "modified" {
		t.Fatalf("appapproval.ParseFileEntryWithWorkspace() = %+v", got)
	}
	if got := appapproval.TruncatedRequestJSON(map[string]any{"threadId": "t", "value": "x"}); !strings.Contains(got, `"value": "x"`) || strings.Contains(got, "threadId") {
		t.Fatalf("appapproval.TruncatedRequestJSON() = %q", got)
	}
}

func TestSendApprovalAndUserInputCards(t *testing.T) {
	a, ff, fc := newTestApp(t)
	sub := seedActiveSubmission(t, a, "sess-1", "thread-1", "turn-1")

	a.ServerRequestService().SendApprovalCardWithPayload("command", json.RawMessage(`"req-1"`), "thread-1", "turn-1", "item-1", "need approve", map[string]any{"command": "pwd"})
	if len(ff.replyCards) != 1 {
		t.Fatalf("sendApprovalCardWithPayload() cards = %d, want 1", len(ff.replyCards))
	}
	pending := a.store.PendingByID("req-1")
	if pending == nil || pending.Kind != "command" || pending.Status != "pending" || pending.Backend != domainbackend.BackendCodex {
		t.Fatalf("approval pending = %+v, want stored command request", pending)
	}
	if got := a.store.GetSubmission(sub.ID); got.Status != "waiting_approval" {
		t.Fatalf("submission status = %q, want waiting_approval", got.Status)
	}

	a.ServerRequestService().SendPermissionsCardWithPayload(json.RawMessage(`"perm-1"`), "thread-1", "turn-1", "item-2", "need perms", map[string]any{"mode": "read"}, map[string]any{"permissions": map[string]any{"mode": "read"}})
	if pending := a.store.PendingByID("perm-1"); pending == nil || pending.Kind != "permissions" || pending.Backend != domainbackend.BackendCodex {
		t.Fatalf("permissions pending = %+v, want stored permissions request", pending)
	}

	a.ServerRequestService().SendUserInputCard(json.RawMessage(`"input-1"`), pendingforms.ToolUserInputPayload{
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "item-3",
		Questions: []pendingforms.ToolUserInputQuestion{
			{ID: "mode", Question: "Pick one", Options: []pendingforms.ToolUserInputOption{{Label: "Fast"}, {Label: "Safe"}}},
		},
	})
	if pending := a.store.PendingByID("input-1"); pending == nil || pending.Kind != "tool_request_user_input" || pending.Backend != domainbackend.BackendCodex {
		t.Fatalf("user-input pending = %+v, want stored request", pending)
	}

	empty := &App{store: a.store, feishu: ff, registry: testRegistryWithCodex(fc)}
	empty.ServerRequestService().SendApprovalCardWithPayload("command", json.RawMessage(`"missing"`), "thread-x", "turn-x", "", "body", nil)
	empty.ServerRequestService().SendUserInputCard(json.RawMessage(`"missing-input"`), pendingforms.ToolUserInputPayload{})
	handleServerRequest(empty, codexrpc.RequestEnvelope{ID: json.RawMessage(`"req"`), Method: "unknown"})
	if len(fc.replyErrors) < 3 {
		t.Fatalf("replyErrors = %+v, want errors for missing session/input/unknown method", fc.replyErrors)
	}
}
