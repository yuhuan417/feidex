package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestClaudeRuntimeWarmInitializeReturnsBeforeReadyAndLateBindsSessionID(t *testing.T) {
	a, _, _ := newTestApp(t)
	a.cfg.Feishu.Backend = backendClaude
	a.codex = nil
	a.cfg.Claude.Command = writeFakeClaudeWarmupCLI(t)

	runtime := newClaudeRuntime(a, a.cfg.Claude).(*claudeRuntime)
	a.claude = runtime
	defer runtime.Close()

	ws := &a.cfg.Workspaces[0]
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	startedAt := time.Now()
	threadID, err := runtime.EnsureSession(ctx, "sess-warm", ws, "", "")
	if err != nil {
		t.Fatalf("EnsureSession() error = %v", err)
	}
	if threadID != "" {
		t.Fatalf("EnsureSession() threadID = %q, want empty before first turn", threadID)
	}
	if elapsed := time.Since(startedAt); elapsed > time.Second {
		t.Fatalf("EnsureSession() took %v, want warm initialize to return quickly", elapsed)
	}

	state, err := runtime.sessionState("sess-warm")
	if err != nil {
		t.Fatalf("sessionState() error = %v", err)
	}
	state.mu.Lock()
	if got := strings.TrimSpace(state.sessionID); got != "" {
		state.mu.Unlock()
		t.Fatalf("sessionID before first turn = %q, want empty", got)
	}
	state.mu.Unlock()

	turnCtx, turnCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer turnCancel()
	if err := runtime.StartTurn(turnCtx, "sess-warm", "", "turn-1", "hello"); err != nil {
		t.Fatalf("StartTurn() error = %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		state.mu.Lock()
		got := strings.TrimSpace(state.sessionID)
		state.mu.Unlock()
		if got == "claude-session-ready" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	state.mu.Lock()
	got := strings.TrimSpace(state.sessionID)
	state.mu.Unlock()
	t.Fatalf("sessionID after first turn = %q, want claude-session-ready", got)
}

func TestClaudeRuntimeWarmForkReturnsBeforeReadyAndLateBindsSessionID(t *testing.T) {
	a, _, _ := newTestApp(t)
	a.cfg.Feishu.Backend = backendClaude
	a.codex = nil
	a.cfg.Claude.Command = writeFakeClaudeWarmupCLI(t)

	runtime := newClaudeRuntime(a, a.cfg.Claude).(*claudeRuntime)
	a.claude = runtime
	defer runtime.Close()

	ws := &a.cfg.Workspaces[0]
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	startedAt := time.Now()
	threadID, err := runtime.ForkSession(ctx, "sess-fork", ws, "claude-parent", "")
	if err != nil {
		t.Fatalf("ForkSession() error = %v", err)
	}
	if threadID != "" {
		t.Fatalf("ForkSession() threadID = %q, want empty before first turn", threadID)
	}
	if elapsed := time.Since(startedAt); elapsed > time.Second {
		t.Fatalf("ForkSession() took %v, want warm initialize to return quickly", elapsed)
	}

	state, err := runtime.sessionState("sess-fork")
	if err != nil {
		t.Fatalf("sessionState() error = %v", err)
	}
	state.mu.Lock()
	if got := strings.TrimSpace(state.sessionID); got != "" {
		state.mu.Unlock()
		t.Fatalf("sessionID before first turn = %q, want empty for pending fork", got)
	}
	state.mu.Unlock()

	ensureCtx, ensureCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer ensureCancel()
	ensuredID, err := runtime.EnsureSession(ensureCtx, "sess-fork", ws, "", "")
	if err != nil {
		t.Fatalf("EnsureSession() on pending fork error = %v", err)
	}
	if ensuredID != "" {
		t.Fatalf("EnsureSession() on pending fork = %q, want empty before first turn", ensuredID)
	}

	turnCtx, turnCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer turnCancel()
	if err := runtime.StartTurn(turnCtx, "sess-fork", "", "turn-1", "hello"); err != nil {
		t.Fatalf("StartTurn() error = %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		state.mu.Lock()
		got := strings.TrimSpace(state.sessionID)
		state.mu.Unlock()
		if got == "claude-session-ready" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	state.mu.Lock()
	got := strings.TrimSpace(state.sessionID)
	state.mu.Unlock()
	t.Fatalf("sessionID after first turn = %q, want claude-session-ready", got)
}

func writeFakeClaudeWarmupCLI(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "fake-claude-warmup.sh")
	script := `#!/usr/bin/env bash
set -euo pipefail

while IFS= read -r line; do
  if [[ "$line" == *'"type":"control_request"'* && "$line" == *'"subtype":"initialize"'* ]]; then
    req_id="$(printf '%s' "$line" | sed -n 's/.*"request_id":"\([^"]*\)".*/\1/p')"
    printf '{"type":"control_response","response":{"subtype":"success","request_id":"%s","response":{"pid":123}}}\n' "$req_id"
    continue
  fi

  if [[ "$line" == *'"type":"user"'* ]]; then
    printf '{"type":"system","subtype":"init","cwd":"%s","session_id":"claude-session-ready","tools":["Read"],"model":"claude-test","permissionMode":"default"}\n' "$PWD"
    printf '{"type":"stream_event","event":{"type":"message_start"}}\n'
    printf '{"type":"assistant","message":{"id":"msg-1","role":"assistant","content":[{"type":"text","text":"ok"}]}}\n'
    printf '{"type":"result","is_error":false,"duration_ms":1,"result":"ok","total_cost_usd":0,"usage":{"input_tokens":1,"cache_creation_input_tokens":0,"cache_read_input_tokens":0,"output_tokens":1},"modelUsage":{}}\n'
  fi
done
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile(%s) error = %v", path, err)
	}
	return path
}
