package app

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"feidex/internal/codexrpc"
	"feidex/internal/config"
)

func TestWorkspaceCodexPoolRoutesByWorkspaceAndRequest(t *testing.T) {
	root := t.TempDir()
	cfg := config.Default()
	cfg.Codex.Transport = "stdio"
	cfg.Workspaces = []config.Workspace{
		{ID: "ws1", Name: "WS1", Cwd: filepath.Join(root, "ws1")},
		{ID: "ws2", Name: "WS2", Cwd: filepath.Join(root, "ws2"), AppServerDir: filepath.Join(root, "ws2-app")},
	}

	clients := map[string]*fakeCodexClient{}
	factoryDirs := map[string]string{}
	pool := newWorkspaceCodexPool(cfg, func(c config.CodexConfig) codexClient {
		fc := &fakeCodexClient{}
		clients[c.AppServerDir] = fc
		factoryDirs[c.AppServerDir] = c.AppServerDir
		fc.callHook = func(_ context.Context, method string, params any, out any) error {
			switch method {
			case "thread/start":
				result := out.(*codexrpc.ThreadStartResult)
				if routeCWDFromParams(params) == cfg.Workspaces[0].Cwd {
					result.Thread.ID = "thread-ws1"
				} else {
					result.Thread.ID = "thread-ws2"
				}
			}
			return nil
		}
		return fc
	})

	if err := pool.Start(context.Background(), true); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	var ws1Result codexrpc.ThreadStartResult
	if err := pool.Call(withCodexWorkspace(context.Background(), "ws1"), "thread/start", map[string]any{
		"cwd": cfg.Workspaces[0].Cwd,
	}, &ws1Result); err != nil {
		t.Fatalf("Call(thread/start ws1) error = %v", err)
	}
	if ws1Result.Thread.ID != "thread-ws1" {
		t.Fatalf("ws1 thread/start = %+v", ws1Result)
	}

	var ws2Result codexrpc.ThreadStartResult
	if err := pool.Call(withCodexWorkspace(context.Background(), "ws2"), "thread/start", map[string]any{
		"cwd": cfg.Workspaces[1].Cwd,
	}, &ws2Result); err != nil {
		t.Fatalf("Call(thread/start ws2) error = %v", err)
	}
	if ws2Result.Thread.ID != "thread-ws2" {
		t.Fatalf("ws2 thread/start = %+v", ws2Result)
	}

	ws1Client := clients[cfg.Workspaces[0].Cwd]
	if ws1Client == nil || !ws1Client.started {
		t.Fatalf("ws1 client not started, clients=%+v", clients)
	}
	ws2Client := clients[cfg.Workspaces[1].AppServerDir]
	if ws2Client == nil || !ws2Client.started {
		t.Fatalf("ws2 client not started, clients=%+v", clients)
	}

	if err := pool.Call(context.Background(), "thread/read", map[string]any{
		"threadId": "thread-ws1",
	}, &codexrpc.ThreadReadResult{}); err != nil {
		t.Fatalf("Call(thread/read thread-ws1) error = %v", err)
	}
	if len(ws1Client.replies) != 0 || len(ws2Client.replies) != 0 {
		t.Fatalf("unexpected replies before request routing, ws1=%+v ws2=%+v", ws1Client.replies, ws2Client.replies)
	}

	ws2Client.onRequest(codexrpc.RequestEnvelope{
		ID:     json.RawMessage(`"req-ws2"`),
		Method: "item/commandExecution/requestApproval",
		Params: json.RawMessage(`{"threadId":"thread-ws2"}`),
	})
	if err := pool.Reply(json.RawMessage(`"req-ws2"`), map[string]any{"action": "approve"}); err != nil {
		t.Fatalf("Reply(req-ws2) error = %v", err)
	}
	if len(ws2Client.replies) != 1 {
		t.Fatalf("ws2 replies = %+v, want 1", ws2Client.replies)
	}
	if len(ws1Client.replies) != 0 {
		t.Fatalf("ws1 replies = %+v, want none", ws1Client.replies)
	}
	if got := factoryDirs[cfg.Workspaces[1].AppServerDir]; got != cfg.Workspaces[1].AppServerDir {
		t.Fatalf("factory dir record = %q, want %q", got, cfg.Workspaces[1].AppServerDir)
	}
}

func TestWorkspaceCodexPoolCloseIdleClients(t *testing.T) {
	root := t.TempDir()
	cfg := config.Default()
	cfg.Codex.Transport = "stdio"
	cfg.Workspaces = []config.Workspace{
		{ID: "ws1", Cwd: filepath.Join(root, "ws1")},
		{ID: "ws2", Cwd: filepath.Join(root, "ws2")},
		{ID: "ws3", Cwd: filepath.Join(root, "ws3")},
	}

	clients := map[string]*fakeCodexClient{}
	pool := newWorkspaceCodexPool(cfg, func(c config.CodexConfig) codexClient {
		fc := &fakeCodexClient{}
		clients[c.AppServerDir] = fc
		fc.callHook = func(_ context.Context, method string, params any, out any) error {
			if method == "thread/start" {
				out.(*codexrpc.ThreadStartResult).Thread.ID = fmt.Sprintf("thread-%s", filepath.Base(routeCWDFromParams(params)))
			}
			return nil
		}
		return fc
	})
	if err := pool.Start(context.Background(), true); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	for _, ws := range cfg.Workspaces {
		if err := pool.Call(withCodexWorkspace(context.Background(), ws.ID), "thread/start", map[string]any{"cwd": ws.Cwd}, &codexrpc.ThreadStartResult{}); err != nil {
			t.Fatalf("Call(thread/start %s) error = %v", ws.ID, err)
		}
	}
	ws2Client := clients[cfg.Workspaces[1].Cwd]
	ws2Client.onRequest(codexrpc.RequestEnvelope{
		ID:     json.RawMessage(`"req-open"`),
		Method: "item/commandExecution/requestApproval",
		Params: json.RawMessage(`{"threadId":"thread-ws2"}`),
	})

	now := time.Now()
	pool.mu.Lock()
	for _, entry := range pool.clients {
		entry.lastUsedAt = now.Add(-2 * time.Hour)
	}
	pool.mu.Unlock()

	closed := pool.CloseIdleClients(now, time.Hour, map[string]struct{}{"ws1": {}})
	if closed != 1 {
		t.Fatalf("CloseIdleClients() closed %d clients, want 1", closed)
	}
	if clients[cfg.Workspaces[0].Cwd].closed {
		t.Fatal("ws1 busy client should not be closed")
	}
	if clients[cfg.Workspaces[1].Cwd].closed {
		t.Fatal("ws2 client with open request should not be closed")
	}
	if !clients[cfg.Workspaces[2].Cwd].closed {
		t.Fatal("ws3 idle client should be closed")
	}
	if got := pool.threadClients["thread-ws3"]; got != "ws3" {
		t.Fatalf("thread-ws3 route = %q, want ws3", got)
	}
}

func TestWorkspaceCodexPoolRebindsThreadAfterIdleRecycle(t *testing.T) {
	root := t.TempDir()
	cfg := config.Default()
	cfg.Codex.Transport = "stdio"
	cfg.Workspaces = []config.Workspace{{ID: "ws1", Cwd: filepath.Join(root, "ws1")}}

	var created []*fakeCodexClient
	var createdCalls [][]string
	pool := newWorkspaceCodexPool(cfg, func(c config.CodexConfig) codexClient {
		fc := &fakeCodexClient{}
		created = append(created, fc)
		callIndex := len(createdCalls)
		createdCalls = append(createdCalls, nil)
		fc.callHook = func(_ context.Context, method string, params any, out any) error {
			createdCalls[callIndex] = append(createdCalls[callIndex], method)
			switch method {
			case "thread/start":
				out.(*codexrpc.ThreadStartResult).Thread.ID = "thread-ws1"
			}
			return nil
		}
		return fc
	})
	if err := pool.Start(context.Background(), true); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := pool.Call(withCodexWorkspace(context.Background(), "ws1"), "thread/start", map[string]any{"cwd": cfg.Workspaces[0].Cwd}, &codexrpc.ThreadStartResult{}); err != nil {
		t.Fatalf("Call(thread/start) error = %v", err)
	}

	now := time.Now()
	pool.mu.Lock()
	pool.clients["ws1"].lastUsedAt = now.Add(-2 * time.Hour)
	pool.mu.Unlock()
	if closed := pool.CloseIdleClients(now, time.Hour, nil); closed != 1 {
		t.Fatalf("CloseIdleClients() closed %d clients, want 1", closed)
	}

	if err := pool.Call(context.Background(), "thread/read", map[string]any{"threadId": "thread-ws1"}, &codexrpc.ThreadReadResult{}); err != nil {
		t.Fatalf("Call(thread/read after recycle) error = %v", err)
	}
	if len(created) != 2 {
		t.Fatalf("created clients = %d, want 2", len(created))
	}
	if !created[1].started {
		t.Fatal("expected recycled client to be started")
	}
	if got := createdCalls[1]; len(got) < 2 || got[0] != "thread/resume" || got[1] != "thread/read" {
		t.Fatalf("recycled client calls = %+v, want thread/resume then thread/read", got)
	}
}

func TestWorkspaceCodexPoolCloseClearsRoutes(t *testing.T) {
	root := t.TempDir()
	cfg := config.Default()
	cfg.Codex.Transport = "stdio"
	cfg.Workspaces = []config.Workspace{{ID: "ws1", Cwd: filepath.Join(root, "ws1")}}

	client := &fakeCodexClient{}
	pool := newWorkspaceCodexPool(cfg, func(config.CodexConfig) codexClient { return client })
	if err := pool.Start(context.Background(), true); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	pool.recordThreadRoute("ws1", "thread-ws1")
	pool.recordRequest("ws1", codexrpc.RequestEnvelope{ID: json.RawMessage(`"req-1"`), Params: json.RawMessage(`{"threadId":"thread-ws1"}`)})
	if err := pool.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if len(pool.threadClients) != 0 || len(pool.requestClients) != 0 || len(pool.clients) != 0 {
		t.Fatalf("Close() should clear routes and clients, pool=%+v", pool)
	}
}
