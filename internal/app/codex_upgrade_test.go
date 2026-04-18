package app

import (
	"context"
	"strings"
	"testing"

	"feidex/internal/codexinstall"
	"feidex/internal/codexrpc"
	"feidex/internal/config"
	"feidex/internal/feishu"
)

type fakeCodexInstallManager struct {
	probe       codexinstall.Probe
	probeErr    error
	latest      string
	latestErr   error
	installErrs map[string]error
	installs    []string
}

func (f *fakeCodexInstallManager) Probe(context.Context) (codexinstall.Probe, error) {
	return f.probe, f.probeErr
}

func (f *fakeCodexInstallManager) LatestVersion(context.Context) (string, error) {
	return f.latest, f.latestErr
}

func (f *fakeCodexInstallManager) InstallVersion(_ context.Context, version string) error {
	f.installs = append(f.installs, version)
	if f.installErrs != nil {
		return f.installErrs[version]
	}
	return nil
}

func TestCommandCodexRendersStatusCard(t *testing.T) {
	a, ff, _ := newTestApp(t)
	manager := &fakeCodexInstallManager{
		probe: codexinstall.Probe{
			Command:        "codex",
			CommandPath:    "/usr/local/bin/codex",
			NPMPath:        "/usr/bin/npm",
			CurrentVersion: "1.0.0",
			Supported:      true,
		},
	}
	origManager := newCodexInstallManager
	newCodexInstallManager = func(string) codexInstallManager { return manager }
	defer func() { newCodexInstallManager = origManager }()

	msg := &feishu.InboundMessage{MessageID: "msg-1", ChatID: "chat-1", ChatType: "p2p", UserID: "user-1"}
	if err := a.commandCodex(msg, nil); err != nil {
		t.Fatalf("commandCodex() error = %v", err)
	}
	if len(ff.replyCards) != 1 {
		t.Fatalf("replyCards = %d, want 1", len(ff.replyCards))
	}
	body := cardMarkdownContent(t, ff.replyCards[0])
	for _, want := range []string{"当前版本: `1.0.0`", "最新稳定版: `未检查`", "状态: `等待检查`"} {
		if !strings.Contains(body, want) {
			t.Fatalf("status card body = %q, want %q", body, want)
		}
	}
}

func TestCommandCodexUpgradeCreatesPendingRequest(t *testing.T) {
	a, ff, _ := newTestApp(t)
	manager := &fakeCodexInstallManager{
		probe: codexinstall.Probe{
			Command:        "codex",
			CommandPath:    "/usr/local/bin/codex",
			NPMPath:        "/usr/bin/npm",
			CurrentVersion: "1.0.0",
			Supported:      true,
		},
		latest: "1.1.0",
	}
	origManager := newCodexInstallManager
	newCodexInstallManager = func(string) codexInstallManager { return manager }
	defer func() { newCodexInstallManager = origManager }()

	msg := &feishu.InboundMessage{MessageID: "msg-1", ChatID: "chat-1", ChatType: "p2p", UserID: "user-1"}
	if err := a.commandCodex(msg, []string{"upgrade"}); err != nil {
		t.Fatalf("commandCodex(upgrade) error = %v", err)
	}
	if len(ff.replyCards) != 1 {
		t.Fatalf("replyCards = %d, want 1", len(ff.replyCards))
	}
	body := cardMarkdownContent(t, ff.replyCards[0])
	for _, want := range []string{"当前版本: `1.0.0`", "目标版本: `1.1.0`", "失败处理: 自动回滚到 `1.0.0`"} {
		if !strings.Contains(body, want) {
			t.Fatalf("confirm card body = %q, want %q", body, want)
		}
	}
	pending := a.appState().pendingRequests()
	if len(pending) != 1 || pending[0] == nil || pending[0].Kind != codexUpgradePendingKind {
		t.Fatalf("pending requests = %+v", pending)
	}
	if pending[0].FeishuMsgID != "reply-card-id" {
		t.Fatalf("pending.FeishuMsgID = %q, want reply-card-id", pending[0].FeishuMsgID)
	}
}

func TestCodexUpgradeBlocksCommandsAndInboundMessages(t *testing.T) {
	a, ff, _ := newTestApp(t)
	a.beginCodexUpgrade(codexUpgradeSnapshot{Phase: "preflight", Message: "running"})

	msg := &feishu.InboundMessage{MessageID: "status-1", ChatID: "chat-1", ChatType: "p2p", UserID: "user-1"}
	if err := a.handleCommand(msg, "/status"); err != nil {
		t.Fatalf("handleCommand(/status) error = %v", err)
	}
	if len(ff.replyCards) != 1 {
		t.Fatalf("expected /status to remain allowed, replyCards=%d", len(ff.replyCards))
	}
	if err := a.handleCommand(msg, "/quiet"); err == nil || !strings.Contains(err.Error(), "Codex 正在升级中") {
		t.Fatalf("handleCommand(/quiet) error = %v, want maintenance block", err)
	}

	router := newFeishuEventRouter(a)
	err := router.processMessage(&feishu.InboundMessage{MessageID: "m-1", ChatID: "chat-1", ChatType: "p2p", UserID: "user-1", Text: "hello"})
	if err == nil || !strings.Contains(err.Error(), "Codex 正在升级中") {
		t.Fatalf("processMessage(non-local) error = %v, want maintenance block", err)
	}
}

func TestRunCodexUpgradeOperationSuccess(t *testing.T) {
	a, ff, fc := newTestApp(t)
	manager := &fakeCodexInstallManager{
		probe: codexinstall.Probe{
			Command:        "codex",
			CommandPath:    "/usr/local/bin/codex",
			NPMPath:        "/usr/bin/npm",
			CurrentVersion: "1.0.0",
			Supported:      true,
		},
	}
	origManager := newCodexInstallManager
	origClient := newCodexClient
	newCodexInstallManager = func(string) codexInstallManager { return manager }
	newCodexClient = func(config config.CodexConfig) codexClient {
		return &fakeCodexClient{
			callHook: func(_ context.Context, method string, _ any, out any) error {
				if method != "model/list" {
					t.Fatalf("unexpected smoke method: %s", method)
				}
				result := out.(*codexrpc.ModelListResult)
				result.Data = []codexrpc.ModelListEntry{{ID: "gpt-5.4"}}
				return nil
			},
		}
	}
	defer func() {
		newCodexInstallManager = origManager
		newCodexClient = origClient
	}()

	if !a.beginCodexUpgrade(codexUpgradeSnapshot{
		Phase:           "preflight",
		CurrentVersion:  "1.0.0",
		PreviousVersion: "1.0.0",
		TargetVersion:   "1.1.0",
	}) {
		t.Fatal("beginCodexUpgrade() should succeed")
	}
	a.runCodexUpgradeOperation("msg-1", "sess-1", codexUpgradePendingPayload{
		CurrentVersion: "1.0.0",
		TargetVersion:  "1.1.0",
	})

	if got := manager.installs; len(got) != 1 || got[0] != "1.1.0" {
		t.Fatalf("install versions = %#v", got)
	}
	if !fc.closed {
		t.Fatal("expected live codex runtime to be closed after successful promotion")
	}
	snapshot := a.codexUpgradeState()
	if snapshot.Running || snapshot.Result != "success" || snapshot.CurrentVersion != "1.1.0" {
		t.Fatalf("final snapshot = %+v", snapshot)
	}
	if len(ff.patchedCards) == 0 {
		t.Fatal("expected progress cards to be patched")
	}
	body := cardMarkdownContent(t, ff.patchedCards[len(ff.patchedCards)-1])
	if !strings.Contains(body, "结果: `success`") {
		t.Fatalf("final patched card body = %q", body)
	}
}

func TestRunCodexUpgradeOperationRollbackAfterSmokeFailure(t *testing.T) {
	a, ff, fc := newTestApp(t)
	manager := &fakeCodexInstallManager{
		probe: codexinstall.Probe{
			Command:        "codex",
			CommandPath:    "/usr/local/bin/codex",
			NPMPath:        "/usr/bin/npm",
			CurrentVersion: "1.0.0",
			Supported:      true,
		},
	}
	origManager := newCodexInstallManager
	origClient := newCodexClient
	smokeRuns := 0
	newCodexInstallManager = func(string) codexInstallManager { return manager }
	newCodexClient = func(config config.CodexConfig) codexClient {
		smokeRuns++
		return &fakeCodexClient{
			callHook: func(_ context.Context, method string, _ any, out any) error {
				if method != "model/list" {
					t.Fatalf("unexpected smoke method: %s", method)
				}
				if smokeRuns == 1 {
					return errString("boom")
				}
				result := out.(*codexrpc.ModelListResult)
				result.Data = []codexrpc.ModelListEntry{{ID: "gpt-5.4"}}
				return nil
			},
		}
	}
	defer func() {
		newCodexInstallManager = origManager
		newCodexClient = origClient
	}()

	if !a.beginCodexUpgrade(codexUpgradeSnapshot{
		Phase:           "preflight",
		CurrentVersion:  "1.0.0",
		PreviousVersion: "1.0.0",
		TargetVersion:   "1.1.0",
	}) {
		t.Fatal("beginCodexUpgrade() should succeed")
	}
	a.runCodexUpgradeOperation("msg-1", "sess-1", codexUpgradePendingPayload{
		CurrentVersion: "1.0.0",
		TargetVersion:  "1.1.0",
	})

	if got := manager.installs; len(got) != 2 || got[0] != "1.1.0" || got[1] != "1.0.0" {
		t.Fatalf("install versions = %#v", got)
	}
	if fc.closed {
		t.Fatal("live codex runtime should not be closed when upgrade rolls back")
	}
	snapshot := a.codexUpgradeState()
	if snapshot.Running || snapshot.Result != "rolled_back" || snapshot.CurrentVersion != "1.0.0" {
		t.Fatalf("final snapshot = %+v", snapshot)
	}
	if len(ff.patchedCards) == 0 {
		t.Fatal("expected rollback progress cards to be patched")
	}
	body := cardMarkdownContent(t, ff.patchedCards[len(ff.patchedCards)-1])
	if !strings.Contains(body, "结果: `rolled_back`") {
		t.Fatalf("rollback patched card body = %q", body)
	}
}
