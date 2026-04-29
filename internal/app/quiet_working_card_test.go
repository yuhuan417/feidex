package app

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"feidex/internal/config"
)

func TestQuietModeAggregatesIntermediateItemsBetweenAgentMessages(t *testing.T) {
	a, ff, _ := newTestApp(t)
	a.cfg.Feishu.Quiet = config.QuietModeProgress
	workspace := a.cfg.Workspaces[0].Cwd
	sub := seedActiveSubmission(t, a, "sess-1", "thread-1", "turn-1")

	newTurnStreamService(a).noteTurnStarted("sess-1", sub)
	newTurnStreamService(a).completeTurnItem(context.Background(), "thread-1", "turn-1", "reason-1", map[string]any{
		"id":   "reason-1",
		"type": "reasoning",
	})
	if len(ff.replyCards) != 1 {
		t.Fatalf("reply card count after reasoning = %d, want 1", len(ff.replyCards))
	}
	if got := cardHeaderTitle(t, ff.replyCards[0]); !strings.Contains(got, quietWorkingCardTitle) {
		t.Fatalf("working card title = %q, want to contain %q", got, quietWorkingCardTitle)
	}
	if body := cardMarkdownContent(t, ff.replyCards[0]); !strings.Contains(body, "思考中...") {
		t.Fatalf("working card body after reasoning = %q", body)
	}

	newTurnStreamService(a).completeTurnItem(context.Background(), "thread-1", "turn-1", "cmd-1", map[string]any{
		"id":     "cmd-1",
		"type":   "commandExecution",
		"status": "completed",
		"cwd":    workspace,
		"commandActions": []any{
			map[string]any{
				"type": "read",
				"path": filepath.Join(workspace, "internal", "app", "quiet_mode.go"),
			},
			map[string]any{
				"type": "listFiles",
				"path": filepath.Join(workspace, "internal", "app"),
			},
		},
	})
	if len(ff.patchedCards) != 1 {
		t.Fatalf("patched card count after command = %d, want 1", len(ff.patchedCards))
	}
	patchedBody := cardMarkdownContent(t, ff.patchedCards[0])
	if strings.Contains(patchedBody, "思考中...") {
		t.Fatalf("patched working body should remove reasoning line: %q", patchedBody)
	}
	for _, want := range []string{"Read `quiet_mode.go`", "List `internal/app`"} {
		if !strings.Contains(patchedBody, want) {
			t.Fatalf("patched working body missing %q: %q", want, patchedBody)
		}
	}

	newTurnStreamService(a).completeTurnItem(context.Background(), "thread-1", "turn-1", "agent-1", map[string]any{
		"id":   "agent-1",
		"type": "agentMessage",
		"text": "first reply",
	})
	if len(ff.replyCards) != 2 {
		t.Fatalf("reply card count after first agent message = %d, want 2", len(ff.replyCards))
	}
	if body := cardMarkdownContent(t, ff.replyCards[1]); !strings.Contains(body, "first reply") {
		t.Fatalf("agent message body = %q", body)
	}

	newTurnStreamService(a).completeTurnItem(context.Background(), "thread-1", "turn-1", "web-1", map[string]any{
		"id":    "web-1",
		"type":  "webSearch",
		"query": "latest golang release",
		"action": map[string]any{
			"type": "search",
		},
	})
	if len(ff.replyCards) != 3 {
		t.Fatalf("reply card count after web search = %d, want 3", len(ff.replyCards))
	}
	if got := cardHeaderTitle(t, ff.replyCards[2]); !strings.Contains(got, quietWorkingCardTitle) {
		t.Fatalf("second working card title = %q, want to contain %q", got, quietWorkingCardTitle)
	}
	if body := cardMarkdownContent(t, ff.replyCards[2]); !strings.Contains(body, "Searching the web: `latest golang release`") {
		t.Fatalf("second working card body = %q", body)
	}

	newTurnStreamService(a).completeTurnItem(context.Background(), "thread-1", "turn-1", "file-1", map[string]any{
		"id":     "file-1",
		"type":   "fileChange",
		"status": "completed",
		"changes": []any{
			map[string]any{
				"path": filepath.Join(workspace, "internal", "app", "quiet_working_card.go"),
				"kind": map[string]any{"type": "update"},
			},
		},
	})
	if len(ff.patchedCards) != 2 {
		t.Fatalf("patched card count after file change = %d, want 2", len(ff.patchedCards))
	}
	if body := cardMarkdownContent(t, ff.patchedCards[1]); !strings.Contains(body, "Update `quiet_working_card.go`") {
		t.Fatalf("patched second working card body = %q", body)
	}

	newTurnStreamService(a).completeTurnItem(context.Background(), "thread-1", "turn-1", "file-2", map[string]any{
		"id":     "file-2",
		"type":   "fileChange",
		"status": "completed",
		"changes": []any{
			map[string]any{
				"path": filepath.Join(workspace, "internal", "app", "quiet_mode.go"),
				"kind": map[string]any{"type": "update"},
			},
		},
	})
	if len(ff.patchedCards) != 3 {
		t.Fatalf("patched card count after second file change = %d, want 3", len(ff.patchedCards))
	}
	if body := cardMarkdownContent(t, ff.patchedCards[2]); !strings.Contains(body, "Update `quiet_working_card.go` `quiet_mode.go`") {
		t.Fatalf("patched second working card body = %q", body)
	}

	newTurnStreamService(a).completeTurnItem(context.Background(), "thread-1", "turn-1", "agent-2", map[string]any{
		"id":   "agent-2",
		"type": "agentMessage",
		"text": "second reply",
	})
	if len(ff.replyCards) != 4 {
		t.Fatalf("reply card count after second agent message = %d, want 4", len(ff.replyCards))
	}
	if body := cardMarkdownContent(t, ff.replyCards[3]); !strings.Contains(body, "second reply") {
		t.Fatalf("second agent message body = %q", body)
	}
}

func TestQuietModeReusesReasoningOnlyWorkingCardForNextAgentMessage(t *testing.T) {
	a, ff, _ := newTestApp(t)
	a.cfg.Feishu.Quiet = config.QuietModeProgress
	sub := seedActiveSubmission(t, a, "sess-1", "thread-1", "turn-1")

	newTurnStreamService(a).noteTurnStarted("sess-1", sub)
	newTurnStreamService(a).completeTurnItem(context.Background(), "thread-1", "turn-1", "reason-1", map[string]any{
		"id":   "reason-1",
		"type": "reasoning",
	})
	if len(ff.replyCards) != 1 {
		t.Fatalf("reply card count after reasoning = %d, want 1", len(ff.replyCards))
	}

	a.cfg.Feishu.Quiet = config.QuietModeNormal

	newTurnStreamService(a).completeTurnItem(context.Background(), "thread-1", "turn-1", "agent-1", map[string]any{
		"id":   "agent-1",
		"type": "agentMessage",
		"text": "reply after reasoning",
	})
	if len(ff.patchedCards) != 1 {
		t.Fatalf("patched cards = %d, want 1 when reasoning-only card is reused", len(ff.patchedCards))
	}
	if len(ff.replyCards) != 1 {
		t.Fatalf("reply card count after agent message = %d, want 1 because the existing card should be reused", len(ff.replyCards))
	}
	if body := cardMarkdownContent(t, ff.patchedCards[0]); !strings.Contains(body, "reply after reasoning") {
		t.Fatalf("patched agent message body = %q", body)
	}
}

func TestFinishTurnReusesLingeringWorkingCardForFinalCard(t *testing.T) {
	a, ff, _ := newTestApp(t)
	a.cfg.Feishu.Quiet = config.QuietModeProgress
	workspace := a.cfg.Workspaces[0].Cwd
	sub := seedActiveSubmission(t, a, "sess-1", "thread-1", "turn-1")

	newTurnStreamService(a).noteTurnStarted("sess-1", sub)
	newTurnStreamService(a).completeTurnItem(context.Background(), "thread-1", "turn-1", "cmd-1", map[string]any{
		"id":     "cmd-1",
		"type":   "commandExecution",
		"status": "completed",
		"cwd":    workspace,
		"commandActions": []any{
			map[string]any{
				"type": "read",
				"path": filepath.Join(workspace, "internal", "app", "quiet_mode.go"),
			},
		},
	})
	if len(ff.replyCards) != 1 {
		t.Fatalf("reply card count after command = %d, want 1", len(ff.replyCards))
	}

	a.cfg.Feishu.Quiet = config.QuietModeNormal
	finishTurn(a, "thread-1", "turn-1", "completed")

	if len(ff.patchedCards) != 1 {
		t.Fatalf("patched card count after finishTurn = %d, want 1", len(ff.patchedCards))
	}
	if got := cardHeaderTitle(t, ff.patchedCards[0]); !strings.Contains(got, "最终答复") {
		t.Fatalf("patched card title = %q, want to contain 最终答复", got)
	}
	if body := cardMarkdownContent(t, ff.patchedCards[0]); !strings.Contains(body, `<at id=user-1></at>`) {
		t.Fatalf("patched final card body = %q, want attention mention", body)
	}
	if len(ff.replyCards) != 1 {
		t.Fatalf("reply card count after finishTurn = %d, want 1 because lingering card should be reused", len(ff.replyCards))
	}
}

func TestFinishTurnReusesLingeringWorkingCardForTerminalCard(t *testing.T) {
	a, ff, _ := newTestApp(t)
	a.cfg.Feishu.Quiet = config.QuietModeProgress
	workspace := a.cfg.Workspaces[0].Cwd
	sub := seedActiveSubmission(t, a, "sess-1", "thread-1", "turn-1")

	newTurnStreamService(a).noteTurnStarted("sess-1", sub)
	newTurnStreamService(a).completeTurnItem(context.Background(), "thread-1", "turn-1", "cmd-1", map[string]any{
		"id":     "cmd-1",
		"type":   "commandExecution",
		"status": "completed",
		"cwd":    workspace,
		"commandActions": []any{
			map[string]any{
				"type": "listFiles",
				"path": filepath.Join(workspace, "internal", "app"),
			},
		},
	})
	if len(ff.replyCards) != 1 {
		t.Fatalf("reply card count after command = %d, want 1", len(ff.replyCards))
	}

	a.cfg.Feishu.Quiet = config.QuietModeFinal
	finishTurn(a, "thread-1", "turn-1", "failed")

	if len(ff.patchedCards) != 1 {
		t.Fatalf("patched card count after failed finishTurn = %d, want 1", len(ff.patchedCards))
	}
	if got := cardHeaderTitle(t, ff.patchedCards[0]); !strings.Contains(got, "任务状态") {
		t.Fatalf("patched card title = %q, want to contain 任务状态", got)
	}
	if body := cardMarkdownContent(t, ff.patchedCards[0]); !strings.Contains(body, `<at id=user-1></at>`) || !strings.Contains(body, "任务失败。") {
		t.Fatalf("patched terminal card body = %q", body)
	}
}
