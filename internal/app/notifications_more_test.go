package app

import (
	"encoding/json"
	"strings"
	"testing"

	"feidex/internal/codexrpc"
)

func TestApprovalRenderingHelpers(t *testing.T) {
	commandBody := renderCommandApprovalBody(map[string]any{
		"command": "pwd",
		"cwd":     "/tmp/work",
		"reason":  "needed",
	})
	if !strings.Contains(commandBody, "命令审批") || !strings.Contains(commandBody, "工作目录") || !strings.Contains(commandBody, "needed") {
		t.Fatalf("renderCommandApprovalBody() = %q", commandBody)
	}

	permissionsBody := renderPermissionsApprovalBody(map[string]any{
		"reason": "need access",
		"permissions": map[string]any{
			"mode":     "workspace-write",
			"scope":    "session",
			"sandbox":  map[string]any{"type": "workspace-write"},
			"network":  "allow",
			"writable_roots": []any{"/tmp/a", "/tmp/b"},
		},
	})
	if !strings.Contains(permissionsBody, "权限摘要") || !strings.Contains(permissionsBody, "network: 允许") {
		t.Fatalf("renderPermissionsApprovalBody() = %q", permissionsBody)
	}
	if values := summarizePermissions(map[string]any{"nested": map[string]any{"enabled": true}}); len(values) == 0 {
		t.Fatalf("summarizePermissions(flatten) = %+v, want fallback values", values)
	}
	if values := collectPermissionPaths(map[string]any{"roots": []any{"/tmp/b", "/tmp/a", "/tmp/a"}}); len(values) != 2 || values[0] != "/tmp/a" {
		t.Fatalf("collectPermissionPaths() = %+v, want sorted unique paths", values)
	}
	if got, ok := boolValue("deny"); !ok || got {
		t.Fatalf("boolValue(deny) = %v, %v, want false/true", got, ok)
	}
	if got := flattenPermissionScalars("root", map[string]any{"enabled": true, "value": float64(1)}, 0); len(got) != 2 {
		t.Fatalf("flattenPermissionScalars() = %+v, want scalar lines", got)
	}

	fileBody := renderFileApprovalBody(map[string]any{
		"changes": []any{
			map[string]any{"oldPath": "a.txt", "newPath": "b.txt", "kind": "rename"},
			"README.md",
		},
		"reason": "review",
	})
	if !strings.Contains(fileBody, "2 个文件") || !strings.Contains(fileBody, "a.txt -> b.txt (rename)") {
		t.Fatalf("renderFileApprovalBody() = %q", fileBody)
	}
	entries := collectFileApprovalEntries(map[string]any{
		"payload": map[string]any{
			"fileChanges": []any{map[string]any{"path": "main.go", "status": "modified"}},
		},
	})
	if len(entries) != 1 || entries[0].Path != "main.go" {
		t.Fatalf("collectFileApprovalEntries() = %+v", entries)
	}
	if got := parseApprovalFileEntry(map[string]any{"filePath": "go.mod", "changeType": "modified"}); got.Path != "go.mod" || got.Kind != "modified" {
		t.Fatalf("parseApprovalFileEntry() = %+v", got)
	}
	if got := truncatedApprovalRequestJSON(map[string]any{"threadId": "t", "value": "x"}); !strings.Contains(got, `"value": "x"`) || strings.Contains(got, "threadId") {
		t.Fatalf("truncatedApprovalRequestJSON() = %q", got)
	}
}

func TestSendApprovalAndUserInputCards(t *testing.T) {
	a, ff, fc := newTestApp(t)
	sub := seedActiveSubmission(t, a, "sess-1", "thread-1", "turn-1")

	a.sendApprovalCardWithPayload("command", json.RawMessage(`"req-1"`), "thread-1", "turn-1", "item-1", "need approve", map[string]any{"command": "pwd"})
	if len(ff.sendCards) != 1 {
		t.Fatalf("sendApprovalCardWithPayload() cards = %d, want 1", len(ff.sendCards))
	}
	pending := a.store.PendingByID("req-1")
	if pending == nil || pending.Kind != "command" || pending.Status != "pending" {
		t.Fatalf("approval pending = %+v, want stored command request", pending)
	}
	if got := a.store.GetSubmission(sub.ID); got.Status != "waiting_approval" {
		t.Fatalf("submission status = %q, want waiting_approval", got.Status)
	}

	a.sendPermissionsCardWithPayload(json.RawMessage(`"perm-1"`), "thread-1", "turn-1", "item-2", "need perms", map[string]any{"mode": "read"}, map[string]any{"permissions": map[string]any{"mode": "read"}})
	if pending := a.store.PendingByID("perm-1"); pending == nil || pending.Kind != "permissions" {
		t.Fatalf("permissions pending = %+v, want stored permissions request", pending)
	}

	a.sendUserInputCard(json.RawMessage(`"input-1"`), toolUserInputPayload{
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "item-3",
		Questions: []toolUserInputQuestion{
			{ID: "mode", Question: "Pick one", Options: []toolUserInputOption{{Label: "Fast"}, {Label: "Safe"}}},
		},
	})
	if pending := a.store.PendingByID("input-1"); pending == nil || pending.Kind != "tool_request_user_input" {
		t.Fatalf("user-input pending = %+v, want stored request", pending)
	}

	empty := &App{store: a.store, codex: fc, feishu: ff}
	empty.sendApprovalCardWithPayload("command", json.RawMessage(`"missing"`), "thread-x", "turn-x", "", "body", nil)
	empty.sendUserInputCard(json.RawMessage(`"missing-input"`), toolUserInputPayload{})
	empty.handleServerRequest(codexrpc.RequestEnvelope{ID: json.RawMessage(`"req"`), Method: "unknown"})
	if len(fc.replyErrors) < 3 {
		t.Fatalf("replyErrors = %+v, want errors for missing session/input/unknown method", fc.replyErrors)
	}
}
