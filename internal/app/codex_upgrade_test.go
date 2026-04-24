package app

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

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
	replyCards := ff.replyCardsSnapshot()
	if len(replyCards) != 1 {
		t.Fatalf("replyCards = %d, want 1", len(replyCards))
	}
	body := cardMarkdownContent(t, replyCards[0])
	for _, want := range []string{"当前版本: `1.0.0`", "最新稳定版: `未检查`", "状态: `等待检查`"} {
		if !strings.Contains(body, want) {
			t.Fatalf("status card body = %q, want %q", body, want)
		}
	}
}

func TestCommandCodexRendersUnsupportedReason(t *testing.T) {
	a, ff, _ := newTestApp(t)
	manager := &fakeCodexInstallManager{
		probe: codexinstall.Probe{
			Command:        "codex",
			CommandPath:    "/usr/local/bin/codex",
			NPMPath:        "/usr/bin/npm",
			CurrentVersion: "1.0.0",
			Supported:      false,
			Reason:         "当前 codex 所属安装目录与 npm global prefix 不一致",
		},
	}
	origManager := newCodexInstallManager
	newCodexInstallManager = func(string) codexInstallManager { return manager }
	defer func() { newCodexInstallManager = origManager }()

	msg := &feishu.InboundMessage{MessageID: "msg-1", ChatID: "chat-1", ChatType: "p2p", UserID: "user-1"}
	if err := a.commandCodex(msg, nil); err != nil {
		t.Fatalf("commandCodex() error = %v", err)
	}
	replyCards := ff.replyCardsSnapshot()
	if len(replyCards) != 1 {
		t.Fatalf("replyCards = %d, want 1", len(replyCards))
	}
	body := cardMarkdownContent(t, replyCards[0])
	for _, want := range []string{"状态: `不支持自动升级`", "原因: 当前 codex 所属安装目录与 npm global prefix 不一致"} {
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
	replyCards := ff.replyCardsSnapshot()
	if len(replyCards) != 1 {
		t.Fatalf("replyCards = %d, want 1", len(replyCards))
	}
	body := cardMarkdownContent(t, replyCards[0])
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
	replyCards := ff.replyCardsSnapshot()
	if len(replyCards) != 1 {
		t.Fatalf("expected /status to remain allowed, replyCards=%d", len(replyCards))
	}
	if err := a.handleCommand(msg, "/quiet"); err == nil || !strings.Contains(err.Error(), "Codex 正在维护中") {
		t.Fatalf("handleCommand(/quiet) error = %v, want maintenance block", err)
	}

	router := newFeishuEventRouter(a)
	err := router.processMessage(&feishu.InboundMessage{MessageID: "m-1", ChatID: "chat-1", ChatType: "p2p", UserID: "user-1", Text: "hello"})
	if err == nil || !strings.Contains(err.Error(), "Codex 正在维护中") {
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
	var promoted *fakeCodexClient
	newCodexInstallManager = func(string) codexInstallManager { return manager }
	newCodexClient = func(config config.CodexConfig) codexClient {
		promoted = &fakeCodexClient{
			callHook: func(_ context.Context, method string, _ any, out any) error {
				if method != "model/list" {
					t.Fatalf("unexpected smoke method: %s", method)
				}
				result := out.(*codexrpc.ModelListResult)
				result.Data = []codexrpc.ModelListEntry{{ID: "gpt-5.4"}}
				return nil
			},
		}
		return promoted
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
	_, liveClosed := fc.statusSnapshot()
	if !liveClosed {
		t.Fatal("expected live codex runtime to be closed after successful promotion")
	}
	promotedStarted, promotedClosed := false, false
	if promoted != nil {
		promotedStarted, promotedClosed = promoted.statusSnapshot()
	}
	if promoted == nil || !promotedStarted || promotedClosed {
		t.Fatalf("promoted runtime = %+v, want started open client", promoted)
	}
	current, ok := a.currentCodexClient().(*fakeCodexClient)
	if !ok || current != promoted {
		t.Fatalf("a.codex = %#v, want promoted runtime %#v", a.currentCodexClient(), promoted)
	}
	snapshot := a.codexUpgradeState()
	if snapshot.Running || snapshot.Result != "success" || snapshot.CurrentVersion != "1.1.0" {
		t.Fatalf("final snapshot = %+v", snapshot)
	}
	patchedCards := ff.patchedCardsSnapshot()
	if len(patchedCards) == 0 {
		t.Fatal("expected progress cards to be patched")
	}
	body := cardMarkdownContent(t, patchedCards[len(patchedCards)-1])
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
	var smoke *fakeCodexClient
	var rollbackSmoke *fakeCodexClient
	runCount := 0
	newCodexInstallManager = func(string) codexInstallManager { return manager }
	newCodexClient = func(config config.CodexConfig) codexClient {
		runCount++
		client := &fakeCodexClient{
			callHook: func(_ context.Context, method string, _ any, out any) error {
				if method != "model/list" {
					t.Fatalf("unexpected smoke method: %s", method)
				}
				if runCount == 1 {
					return errString("boom")
				}
				out.(*codexrpc.ModelListResult).Data = []codexrpc.ModelListEntry{{ID: "gpt-5.4"}}
				return nil
			},
		}
		if runCount == 1 {
			smoke = client
		} else {
			rollbackSmoke = client
		}
		return client
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
	_, liveClosed := fc.statusSnapshot()
	if liveClosed {
		t.Fatal("live codex runtime should not be closed when upgrade rolls back")
	}
	_, smokeClosed := false, false
	if smoke != nil {
		_, smokeClosed = smoke.statusSnapshot()
	}
	if smoke == nil || !smokeClosed {
		t.Fatalf("smoke runtime = %+v, want closed after failed validation", smoke)
	}
	_, rollbackClosed := false, false
	if rollbackSmoke != nil {
		_, rollbackClosed = rollbackSmoke.statusSnapshot()
	}
	if rollbackSmoke == nil || !rollbackClosed {
		t.Fatalf("rollback smoke runtime = %+v, want closed after rollback validation", rollbackSmoke)
	}
	current, ok := a.currentCodexClient().(*fakeCodexClient)
	if !ok || current != fc {
		t.Fatalf("a.codex = %#v, want original live runtime %#v", a.currentCodexClient(), fc)
	}
	snapshot := a.codexUpgradeState()
	if snapshot.Running || snapshot.Result != "rolled_back" || snapshot.CurrentVersion != "1.0.0" {
		t.Fatalf("final snapshot = %+v", snapshot)
	}
	patchedCards := ff.patchedCardsSnapshot()
	if len(patchedCards) == 0 {
		t.Fatal("expected rollback progress cards to be patched")
	}
	body := cardMarkdownContent(t, patchedCards[len(patchedCards)-1])
	if !strings.Contains(body, "结果: `rolled_back`") {
		t.Fatalf("rollback patched card body = %q", body)
	}
}

func TestCommandCodexRestartStartsRestartOperation(t *testing.T) {
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
	var promoted *fakeCodexClient
	newCodexInstallManager = func(string) codexInstallManager { return manager }
	newCodexClient = func(config config.CodexConfig) codexClient {
		promoted = &fakeCodexClient{
			callHook: func(_ context.Context, method string, _ any, out any) error {
				if method != "model/list" {
					t.Fatalf("unexpected smoke method: %s", method)
				}
				out.(*codexrpc.ModelListResult).Data = []codexrpc.ModelListEntry{{ID: "gpt-5.4"}}
				return nil
			},
		}
		return promoted
	}
	defer func() {
		newCodexInstallManager = origManager
		newCodexClient = origClient
	}()

	msg := &feishu.InboundMessage{MessageID: "msg-1", ChatID: "chat-1", ChatType: "p2p", UserID: "user-1"}
	if err := a.commandCodex(msg, []string{"restart"}); err != nil {
		t.Fatalf("commandCodex(restart) error = %v", err)
	}
	replyCards := ff.replyCardsSnapshot()
	if len(replyCards) != 1 {
		t.Fatalf("replyCards = %d, want 1", len(replyCards))
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if !a.codexRestartState().Running {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	_, liveClosed := fc.statusSnapshot()
	if !liveClosed {
		t.Fatal("expected live runtime to be closed during restart")
	}
	promotedStarted, promotedClosed := false, false
	if promoted != nil {
		promotedStarted, promotedClosed = promoted.statusSnapshot()
	}
	if promoted == nil || !promotedStarted || promotedClosed {
		t.Fatalf("promoted runtime = %+v, want started open client", promoted)
	}
	current, ok := a.currentCodexClient().(*fakeCodexClient)
	if !ok || current != promoted {
		t.Fatalf("a.codex = %#v, want promoted runtime %#v", a.currentCodexClient(), promoted)
	}
	snapshot := a.codexRestartState()
	if snapshot.Running || snapshot.Result != "success" {
		t.Fatalf("restart snapshot = %+v", snapshot)
	}
	patchedCards := ff.patchedCardsSnapshot()
	if len(patchedCards) == 0 {
		t.Fatal("expected restart progress card patches")
	}
	body := cardMarkdownContent(t, patchedCards[len(patchedCards)-1])
	if !strings.Contains(body, "结果: `success`") {
		t.Fatalf("restart final card body = %q", body)
	}
}

func TestRunCodexRestartOperationFailureKeepsOldRuntime(t *testing.T) {
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
	var smoke *fakeCodexClient
	newCodexInstallManager = func(string) codexInstallManager { return manager }
	newCodexClient = func(config config.CodexConfig) codexClient {
		smoke = &fakeCodexClient{
			callHook: func(_ context.Context, method string, _ any, out any) error {
				if method != "model/list" {
					t.Fatalf("unexpected smoke method: %s", method)
				}
				return errString("restart-boom")
			},
		}
		return smoke
	}
	defer func() {
		newCodexInstallManager = origManager
		newCodexClient = origClient
	}()

	snapshot, err := a.beginCodexRestartOperation()
	if err != nil {
		t.Fatalf("beginCodexRestartOperation() error = %v", err)
	}
	if !snapshot.Running {
		t.Fatalf("beginCodexRestartOperation() = %+v", snapshot)
	}
	a.runCodexRestartOperation("msg-1", "sess-1")
	_, liveClosed := fc.statusSnapshot()
	if liveClosed {
		t.Fatal("restart should keep old runtime alive when new runtime validation fails")
	}
	_, smokeClosed := false, false
	if smoke != nil {
		_, smokeClosed = smoke.statusSnapshot()
	}
	if smoke == nil || !smokeClosed {
		t.Fatalf("smoke runtime = %+v, want closed after failed restart validation", smoke)
	}
	current, ok := a.currentCodexClient().(*fakeCodexClient)
	if !ok || current != fc {
		t.Fatalf("a.codex = %#v, want original live runtime %#v", a.currentCodexClient(), fc)
	}
	state := a.codexRestartState()
	if state.Running || state.Result != "failed" {
		t.Fatalf("restart state = %+v", state)
	}
	patchedCards := ff.patchedCardsSnapshot()
	if len(patchedCards) == 0 {
		t.Fatal("expected restart failure patches")
	}
	body := cardMarkdownContent(t, patchedCards[len(patchedCards)-1])
	if !strings.Contains(body, "结果: `failed`") {
		t.Fatalf("restart failure card body = %q", body)
	}
}

func TestRunCodexRestartOperationRecoversFromExitedRuntime(t *testing.T) {
	a, ff, fc := newTestApp(t)
	fc.closeErr = os.ErrProcessDone
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
	var promoted *fakeCodexClient
	newCodexInstallManager = func(string) codexInstallManager { return manager }
	newCodexClient = func(config config.CodexConfig) codexClient {
		promoted = &fakeCodexClient{
			callHook: func(_ context.Context, method string, _ any, out any) error {
				if method != "model/list" {
					t.Fatalf("unexpected smoke method: %s", method)
				}
				out.(*codexrpc.ModelListResult).Data = []codexrpc.ModelListEntry{{ID: "gpt-5.4"}}
				return nil
			},
		}
		return promoted
	}
	defer func() {
		newCodexInstallManager = origManager
		newCodexClient = origClient
	}()

	snapshot, err := a.beginCodexRestartOperation()
	if err != nil {
		t.Fatalf("beginCodexRestartOperation() error = %v", err)
	}
	if !snapshot.Running {
		t.Fatalf("beginCodexRestartOperation() = %+v", snapshot)
	}
	a.runCodexRestartOperation("msg-1", "sess-1")

	_, liveClosed := fc.statusSnapshot()
	if !liveClosed {
		t.Fatal("restart should still attempt to close exited runtime")
	}
	promotedStarted, promotedClosed := false, false
	if promoted != nil {
		promotedStarted, promotedClosed = promoted.statusSnapshot()
	}
	if promoted == nil || !promotedStarted || promotedClosed {
		t.Fatalf("promoted runtime = %+v, want started open client", promoted)
	}
	current, ok := a.currentCodexClient().(*fakeCodexClient)
	if !ok || current != promoted {
		t.Fatalf("a.codex = %#v, want promoted runtime %#v", a.currentCodexClient(), promoted)
	}
	state := a.codexRestartState()
	if state.Running || state.Result != "success" {
		t.Fatalf("restart state = %+v", state)
	}
	patchedCards := ff.patchedCardsSnapshot()
	if len(patchedCards) == 0 {
		t.Fatal("expected restart success patches")
	}
}

func TestRefreshCodexRuntimeAfterMaintenanceOnClaudeBackendOnlySmokes(t *testing.T) {
	a, _, fc := newTestApp(t)
	a.backend = backendClaude
	a.cfg.Feishu.Backend = backendClaude

	origClient := newCodexClient
	var smoke *fakeCodexClient
	newCodexClient = func(config config.CodexConfig) codexClient {
		smoke = &fakeCodexClient{
			callHook: func(_ context.Context, method string, _ any, out any) error {
				if method != "model/list" {
					t.Fatalf("unexpected smoke method: %s", method)
				}
				out.(*codexrpc.ModelListResult).Data = []codexrpc.ModelListEntry{{ID: "gpt-5.4"}}
				return nil
			},
		}
		return smoke
	}
	defer func() { newCodexClient = origClient }()

	switched, err := a.refreshCodexRuntimeAfterMaintenance(context.Background())
	if err != nil {
		t.Fatalf("refreshCodexRuntimeAfterMaintenance() error = %v", err)
	}
	if switched {
		t.Fatal("refreshCodexRuntimeAfterMaintenance() switched runtime on Claude backend")
	}
	_, smokeClosed := false, false
	if smoke != nil {
		_, smokeClosed = smoke.statusSnapshot()
	}
	if smoke == nil || !smokeClosed {
		t.Fatalf("smoke runtime = %+v, want closed after smoke-only validation", smoke)
	}
	_, liveClosed := fc.statusSnapshot()
	if liveClosed {
		t.Fatal("existing codex runtime should not be touched on Claude backend")
	}
	current, ok := a.currentCodexClient().(*fakeCodexClient)
	if !ok || current != fc {
		t.Fatalf("a.codex = %#v, want original codex runtime %#v", a.currentCodexClient(), fc)
	}
}

func TestRefreshCodexRuntimeAfterMaintenanceIgnoresExitedOldRuntime(t *testing.T) {
	a, _, fc := newTestApp(t)
	fc.closeErr = os.ErrProcessDone

	origClient := newCodexClient
	var promoted *fakeCodexClient
	newCodexClient = func(config config.CodexConfig) codexClient {
		promoted = &fakeCodexClient{
			callHook: func(_ context.Context, method string, _ any, out any) error {
				if method != "model/list" {
					t.Fatalf("unexpected smoke method: %s", method)
				}
				out.(*codexrpc.ModelListResult).Data = []codexrpc.ModelListEntry{{ID: "gpt-5.4"}}
				return nil
			},
		}
		return promoted
	}
	defer func() { newCodexClient = origClient }()

	switched, err := a.refreshCodexRuntimeAfterMaintenance(context.Background())
	if err != nil {
		t.Fatalf("refreshCodexRuntimeAfterMaintenance() error = %v", err)
	}
	if !switched {
		t.Fatal("refreshCodexRuntimeAfterMaintenance() did not switch runtime")
	}
	_, liveClosed := fc.statusSnapshot()
	if !liveClosed {
		t.Fatal("old runtime should still receive close attempt")
	}
	promotedStarted, promotedClosed := false, false
	if promoted != nil {
		promotedStarted, promotedClosed = promoted.statusSnapshot()
	}
	if promoted == nil || !promotedStarted || promotedClosed {
		t.Fatalf("promoted runtime = %+v, want started open client", promoted)
	}
	current, ok := a.currentCodexClient().(*fakeCodexClient)
	if !ok || current != promoted {
		t.Fatalf("a.codex = %#v, want promoted runtime %#v", a.currentCodexClient(), promoted)
	}
}
