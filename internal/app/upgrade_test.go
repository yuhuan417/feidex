package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"feidex/internal/claudeinstall"
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
	if err := newBackendUpgradeService(a).commandCodex(msg, nil); err != nil {
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
	if err := newBackendUpgradeService(a).commandCodex(msg, nil); err != nil {
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
	if err := newBackendUpgradeService(a).commandCodex(msg, []string{"upgrade"}); err != nil {
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
	pending := appState(a).pendingRequests()
	if len(pending) != 1 || pending[0] == nil || pending[0].Kind != codexUpgradePendingKind {
		t.Fatalf("pending requests = %+v", pending)
	}
	if pending[0].FeishuMsgID != "reply-card-id" {
		t.Fatalf("pending.FeishuMsgID = %q, want reply-card-id", pending[0].FeishuMsgID)
	}
}

func TestCodexUpgradeBlocksCommandsAndInboundMessages(t *testing.T) {
	a, ff, _ := newTestApp(t)
	newMaintenanceStateService(a).BeginCodexUpgrade(backendUpgradeSnapshot{Phase: "preflight", Message: "running"})

	msg := &feishu.InboundMessage{MessageID: "status-1", ChatID: "chat-1", ChatType: "p2p", UserID: "user-1"}
	if err := handleCommand(a, msg, "/status"); err != nil {
		t.Fatalf("handleCommand(/status) error = %v", err)
	}
	replyCards := ff.replyCardsSnapshot()
	if len(replyCards) != 1 {
		t.Fatalf("expected /status to remain allowed, replyCards=%d", len(replyCards))
	}
	if err := handleCommand(a, msg, "/quiet"); err == nil || !strings.Contains(err.Error(), "Codex 正在维护中") {
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
	newCodexClient = func(config config.CodexConfig) CodexClient {
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

	if !newMaintenanceStateService(a).BeginCodexUpgrade(backendUpgradeSnapshot{
		Phase:           "preflight",
		CurrentVersion:  "1.0.0",
		PreviousVersion: "1.0.0",
		TargetVersion:   "1.1.0",
	}) {
		t.Fatal("beginCodexUpgrade() should succeed")
	}
	newBackendUpgradeService(a).runCodexUpgradeOperation("msg-1", "sess-1", codexUpgradePendingPayload{
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
	current, ok := currentCodexClient(a).(*fakeCodexClient)
	if !ok || current != promoted {
		t.Fatalf("a.codex = %#v, want promoted runtime %#v", currentCodexClient(a), promoted)
	}
	snapshot := newMaintenanceStateService(a).CodexUpgradeState()
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
	newCodexClient = func(config config.CodexConfig) CodexClient {
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

	if !newMaintenanceStateService(a).BeginCodexUpgrade(backendUpgradeSnapshot{
		Phase:           "preflight",
		CurrentVersion:  "1.0.0",
		PreviousVersion: "1.0.0",
		TargetVersion:   "1.1.0",
	}) {
		t.Fatal("beginCodexUpgrade() should succeed")
	}
	newBackendUpgradeService(a).runCodexUpgradeOperation("msg-1", "sess-1", codexUpgradePendingPayload{
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
	current, ok := currentCodexClient(a).(*fakeCodexClient)
	if !ok || current != fc {
		t.Fatalf("a.codex = %#v, want original live runtime %#v", currentCodexClient(a), fc)
	}
	snapshot := newMaintenanceStateService(a).CodexUpgradeState()
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
	newCodexClient = func(config config.CodexConfig) CodexClient {
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
	if err := newBackendUpgradeService(a).commandCodex(msg, []string{"restart"}); err != nil {
		t.Fatalf("commandCodex(restart) error = %v", err)
	}
	replyCards := ff.replyCardsSnapshot()
	if len(replyCards) != 1 {
		t.Fatalf("replyCards = %d, want 1", len(replyCards))
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if !newMaintenanceStateService(a).CodexRestartState().Running {
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
	current, ok := currentCodexClient(a).(*fakeCodexClient)
	if !ok || current != promoted {
		t.Fatalf("a.codex = %#v, want promoted runtime %#v", currentCodexClient(a), promoted)
	}
	snapshot := newMaintenanceStateService(a).CodexRestartState()
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
	newCodexClient = func(config config.CodexConfig) CodexClient {
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

	snapshot, err := newBackendUpgradeService(a).beginCodexRestartOperation()
	if err != nil {
		t.Fatalf("beginCodexRestartOperation() error = %v", err)
	}
	if !snapshot.Running {
		t.Fatalf("beginCodexRestartOperation() = %+v", snapshot)
	}
	newBackendUpgradeService(a).runCodexRestartOperation("msg-1", "sess-1")
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
	current, ok := currentCodexClient(a).(*fakeCodexClient)
	if !ok || current != fc {
		t.Fatalf("a.codex = %#v, want original live runtime %#v", currentCodexClient(a), fc)
	}
	state := newMaintenanceStateService(a).CodexRestartState()
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
	newCodexClient = func(config config.CodexConfig) CodexClient {
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

	snapshot, err := newBackendUpgradeService(a).beginCodexRestartOperation()
	if err != nil {
		t.Fatalf("beginCodexRestartOperation() error = %v", err)
	}
	if !snapshot.Running {
		t.Fatalf("beginCodexRestartOperation() = %+v", snapshot)
	}
	newBackendUpgradeService(a).runCodexRestartOperation("msg-1", "sess-1")

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
	current, ok := currentCodexClient(a).(*fakeCodexClient)
	if !ok || current != promoted {
		t.Fatalf("a.codex = %#v, want promoted runtime %#v", currentCodexClient(a), promoted)
	}
	state := newMaintenanceStateService(a).CodexRestartState()
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
	newCodexClient = func(config config.CodexConfig) CodexClient {
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

	switched, err := newBackendUpgradeService(a).refreshCodexRuntimeAfterMaintenance(context.Background())
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
	current, ok := currentCodexClient(a).(*fakeCodexClient)
	if !ok || current != fc {
		t.Fatalf("a.codex = %#v, want original codex runtime %#v", currentCodexClient(a), fc)
	}
}

func TestRefreshCodexRuntimeAfterMaintenanceIgnoresExitedOldRuntime(t *testing.T) {
	a, _, fc := newTestApp(t)
	fc.closeErr = os.ErrProcessDone

	origClient := newCodexClient
	var promoted *fakeCodexClient
	newCodexClient = func(config config.CodexConfig) CodexClient {
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

	switched, err := newBackendUpgradeService(a).refreshCodexRuntimeAfterMaintenance(context.Background())
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
	current, ok := currentCodexClient(a).(*fakeCodexClient)
	if !ok || current != promoted {
		t.Fatalf("a.codex = %#v, want promoted runtime %#v", currentCodexClient(a), promoted)
	}
}

type fakeClaudeInstallManager struct {
	probe       claudeinstall.Probe
	probeErr    error
	latest      string
	latestErr   error
	installErrs map[string]error
	installs    []string
}

func (f *fakeClaudeInstallManager) Probe(context.Context) (claudeinstall.Probe, error) {
	return f.probe, f.probeErr
}

func (f *fakeClaudeInstallManager) LatestVersion(context.Context) (string, error) {
	return f.latest, f.latestErr
}

func (f *fakeClaudeInstallManager) InstallVersion(_ context.Context, version string) error {
	f.installs = append(f.installs, version)
	if f.installErrs != nil {
		return f.installErrs[version]
	}
	return nil
}

func TestCommandClaudeRendersStatusCard(t *testing.T) {
	a, ff, _ := newTestApp(t)
	manager := &fakeClaudeInstallManager{
		probe: claudeinstall.Probe{
			Command:        "claude",
			CommandPath:    "/usr/local/bin/claude",
			NPMPath:        "/usr/bin/npm",
			CurrentVersion: "1.0.0",
			Supported:      true,
		},
	}
	origManager := newClaudeInstallManager
	newClaudeInstallManager = func(string) claudeInstallManager { return manager }
	defer func() { newClaudeInstallManager = origManager }()

	msg := &feishu.InboundMessage{MessageID: "msg-1", ChatID: "chat-1", ChatType: "p2p", UserID: "user-1"}
	if err := newBackendUpgradeService(a).commandClaude(msg, nil); err != nil {
		t.Fatalf("commandClaude() error = %v", err)
	}
	replyCards := ff.replyCardsSnapshot()
	if len(replyCards) != 1 {
		t.Fatalf("replyCards = %d, want 1", len(replyCards))
	}
	body := cardMarkdownContent(t, replyCards[0])
	for _, want := range []string{"当前版本: `1.0.0`", "最新稳定版: `未检查`", "状态: `等待检查`", "smoke test: `start + init`"} {
		if !strings.Contains(body, want) {
			t.Fatalf("status card body = %q, want %q", body, want)
		}
	}
}

func TestCommandClaudeRendersUnsupportedReason(t *testing.T) {
	a, ff, _ := newTestApp(t)
	manager := &fakeClaudeInstallManager{
		probe: claudeinstall.Probe{
			Command:        "claude",
			CommandPath:    "/usr/local/bin/claude",
			NPMPath:        "/usr/bin/npm",
			CurrentVersion: "1.0.0",
			Supported:      false,
			Reason:         "当前 claude 所属安装目录与 npm global prefix 不一致",
		},
	}
	origManager := newClaudeInstallManager
	newClaudeInstallManager = func(string) claudeInstallManager { return manager }
	defer func() { newClaudeInstallManager = origManager }()

	msg := &feishu.InboundMessage{MessageID: "msg-1", ChatID: "chat-1", ChatType: "p2p", UserID: "user-1"}
	if err := newBackendUpgradeService(a).commandClaude(msg, nil); err != nil {
		t.Fatalf("commandClaude() error = %v", err)
	}
	replyCards := ff.replyCardsSnapshot()
	if len(replyCards) != 1 {
		t.Fatalf("replyCards = %d, want 1", len(replyCards))
	}
	body := cardMarkdownContent(t, replyCards[0])
	for _, want := range []string{"状态: `不支持自动升级`", "原因: 当前 claude 所属安装目录与 npm global prefix 不一致"} {
		if !strings.Contains(body, want) {
			t.Fatalf("status card body = %q, want %q", body, want)
		}
	}
}

func TestCommandClaudeUpgradeCreatesPendingRequest(t *testing.T) {
	a, ff, _ := newTestApp(t)
	manager := &fakeClaudeInstallManager{
		probe: claudeinstall.Probe{
			Command:        "claude",
			CommandPath:    "/usr/local/bin/claude",
			NPMPath:        "/usr/bin/npm",
			CurrentVersion: "1.0.0",
			Supported:      true,
		},
		latest: "1.1.0",
	}
	origManager := newClaudeInstallManager
	newClaudeInstallManager = func(string) claudeInstallManager { return manager }
	defer func() { newClaudeInstallManager = origManager }()

	msg := &feishu.InboundMessage{MessageID: "msg-1", ChatID: "chat-1", ChatType: "p2p", UserID: "user-1"}
	if err := newBackendUpgradeService(a).commandClaude(msg, []string{"upgrade"}); err != nil {
		t.Fatalf("commandClaude(upgrade) error = %v", err)
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
	pending := appState(a).pendingRequests()
	if len(pending) != 1 || pending[0] == nil || pending[0].Kind != claudeUpgradePendingKind {
		t.Fatalf("pending requests = %+v", pending)
	}
	if pending[0].FeishuMsgID != "reply-card-id" {
		t.Fatalf("pending.FeishuMsgID = %q, want reply-card-id", pending[0].FeishuMsgID)
	}
}

func TestClaudeUpgradeBlocksCommandsAndInboundMessages(t *testing.T) {
	a, ff, _ := newTestApp(t)
	a.backend = backendClaude
	a.claude = &fakeClaudeCore{}
	newMaintenanceStateService(a).BeginClaudeUpgrade(backendUpgradeSnapshot{Phase: "preflight", Message: "running"})

	msg := &feishu.InboundMessage{MessageID: "status-1", ChatID: "chat-1", ChatType: "p2p", UserID: "user-1"}
	if err := handleCommand(a, msg, "/status"); err != nil {
		t.Fatalf("handleCommand(/status) error = %v", err)
	}
	replyCards := ff.replyCardsSnapshot()
	if len(replyCards) != 1 {
		t.Fatalf("expected /status to remain allowed, replyCards=%d", len(replyCards))
	}
	if err := handleCommand(a, msg, "/quiet"); err == nil || !strings.Contains(err.Error(), "Claude 正在维护中") {
		t.Fatalf("handleCommand(/quiet) error = %v, want maintenance block", err)
	}

	router := newFeishuEventRouter(a)
	err := router.processMessage(&feishu.InboundMessage{MessageID: "m-1", ChatID: "chat-1", ChatType: "p2p", UserID: "user-1", Text: "hello"})
	if err == nil || !strings.Contains(err.Error(), "Claude 正在维护中") {
		t.Fatalf("processMessage(non-local) error = %v, want maintenance block", err)
	}
}

func TestRunClaudeUpgradeOperationSuccess(t *testing.T) {
	a, ff, _ := newTestApp(t)
	a.backend = backendClaude
	a.cfg.Feishu.Backend = backendClaude
	claude := &fakeClaudeCore{}
	a.claude = claude
	manager := &fakeClaudeInstallManager{
		probe: claudeinstall.Probe{
			Command:        "claude",
			CommandPath:    "/usr/local/bin/claude",
			NPMPath:        "/usr/bin/npm",
			CurrentVersion: "1.0.0",
			Supported:      true,
		},
	}
	origManager := newClaudeInstallManager
	origSmoke := runClaudeSmokeTest
	newClaudeInstallManager = func(string) claudeInstallManager { return manager }
	runClaudeSmokeTest = func(_ *App, _ context.Context) error { return nil }
	defer func() {
		newClaudeInstallManager = origManager
		runClaudeSmokeTest = origSmoke
	}()

	if !newMaintenanceStateService(a).BeginClaudeUpgrade(backendUpgradeSnapshot{
		Phase:           "preflight",
		CurrentVersion:  "1.0.0",
		PreviousVersion: "1.0.0",
		TargetVersion:   "1.1.0",
	}) {
		t.Fatal("beginClaudeUpgrade() should succeed")
	}
	newBackendUpgradeService(a).runClaudeUpgradeOperation("msg-1", "sess-1", claudeUpgradePendingPayload{
		CurrentVersion: "1.0.0",
		TargetVersion:  "1.1.0",
	})

	if got := manager.installs; len(got) != 1 || got[0] != "1.1.0" {
		t.Fatalf("install versions = %#v", got)
	}
	if !claude.closed {
		t.Fatal("expected live Claude runtime to be closed after successful promotion")
	}
	snapshot := newMaintenanceStateService(a).ClaudeUpgradeState()
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

func TestRunClaudeUpgradeOperationRollbackAfterSmokeFailure(t *testing.T) {
	a, ff, _ := newTestApp(t)
	a.backend = backendClaude
	a.cfg.Feishu.Backend = backendClaude
	claude := &fakeClaudeCore{}
	a.claude = claude
	manager := &fakeClaudeInstallManager{
		probe: claudeinstall.Probe{
			Command:        "claude",
			CommandPath:    "/usr/local/bin/claude",
			NPMPath:        "/usr/bin/npm",
			CurrentVersion: "1.0.0",
			Supported:      true,
		},
	}
	origManager := newClaudeInstallManager
	origSmoke := runClaudeSmokeTest
	smokeRuns := 0
	newClaudeInstallManager = func(string) claudeInstallManager { return manager }
	runClaudeSmokeTest = func(_ *App, _ context.Context) error {
		smokeRuns++
		if smokeRuns == 1 {
			return errString("boom")
		}
		return nil
	}
	defer func() {
		newClaudeInstallManager = origManager
		runClaudeSmokeTest = origSmoke
	}()

	if !newMaintenanceStateService(a).BeginClaudeUpgrade(backendUpgradeSnapshot{
		Phase:           "preflight",
		CurrentVersion:  "1.0.0",
		PreviousVersion: "1.0.0",
		TargetVersion:   "1.1.0",
	}) {
		t.Fatal("beginClaudeUpgrade() should succeed")
	}
	newBackendUpgradeService(a).runClaudeUpgradeOperation("msg-1", "sess-1", claudeUpgradePendingPayload{
		CurrentVersion: "1.0.0",
		TargetVersion:  "1.1.0",
	})

	if got := manager.installs; len(got) != 2 || got[0] != "1.1.0" || got[1] != "1.0.0" {
		t.Fatalf("install versions = %#v", got)
	}
	if claude.closed {
		t.Fatal("live Claude runtime should not be closed when upgrade rolls back before promotion")
	}
	snapshot := newMaintenanceStateService(a).ClaudeUpgradeState()
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

func TestCommandClaudeRestartStartsRestartOperation(t *testing.T) {
	a, ff, _ := newTestApp(t)
	a.backend = backendClaude
	a.cfg.Feishu.Backend = backendClaude
	claude := &fakeClaudeCore{}
	a.claude = claude
	manager := &fakeClaudeInstallManager{
		probe: claudeinstall.Probe{
			Command:        "claude",
			CommandPath:    "/usr/local/bin/claude",
			NPMPath:        "/usr/bin/npm",
			CurrentVersion: "1.0.0",
			Supported:      true,
		},
	}
	origManager := newClaudeInstallManager
	origSmoke := runClaudeSmokeTest
	newClaudeInstallManager = func(string) claudeInstallManager { return manager }
	runClaudeSmokeTest = func(_ *App, _ context.Context) error { return nil }
	defer func() {
		newClaudeInstallManager = origManager
		runClaudeSmokeTest = origSmoke
	}()

	msg := &feishu.InboundMessage{MessageID: "msg-1", ChatID: "chat-1", ChatType: "p2p", UserID: "user-1"}
	if err := newBackendUpgradeService(a).commandClaude(msg, []string{"restart"}); err != nil {
		t.Fatalf("commandClaude(restart) error = %v", err)
	}
	replyCards := ff.replyCardsSnapshot()
	if len(replyCards) != 1 {
		t.Fatalf("replyCards = %d, want 1", len(replyCards))
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if !newMaintenanceStateService(a).ClaudeRestartState().Running {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !claude.closed {
		t.Fatal("expected live runtime to be closed during restart")
	}
	snapshot := newMaintenanceStateService(a).ClaudeRestartState()
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

func TestRunClaudeRestartOperationFailureKeepsOldRuntime(t *testing.T) {
	a, ff, _ := newTestApp(t)
	a.backend = backendClaude
	a.cfg.Feishu.Backend = backendClaude
	claude := &fakeClaudeCore{}
	a.claude = claude
	manager := &fakeClaudeInstallManager{
		probe: claudeinstall.Probe{
			Command:        "claude",
			CommandPath:    "/usr/local/bin/claude",
			NPMPath:        "/usr/bin/npm",
			CurrentVersion: "1.0.0",
			Supported:      true,
		},
	}
	origManager := newClaudeInstallManager
	origSmoke := runClaudeSmokeTest
	newClaudeInstallManager = func(string) claudeInstallManager { return manager }
	runClaudeSmokeTest = func(_ *App, _ context.Context) error { return errString("restart-boom") }
	defer func() {
		newClaudeInstallManager = origManager
		runClaudeSmokeTest = origSmoke
	}()

	snapshot, err := newBackendUpgradeService(a).beginClaudeRestartOperation()
	if err != nil {
		t.Fatalf("beginClaudeRestartOperation() error = %v", err)
	}
	if !snapshot.Running {
		t.Fatalf("beginClaudeRestartOperation() = %+v", snapshot)
	}
	newBackendUpgradeService(a).runClaudeRestartOperation("msg-1", "sess-1")
	if claude.closed {
		t.Fatal("restart should keep old runtime alive when new runtime validation fails")
	}
	state := newMaintenanceStateService(a).ClaudeRestartState()
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

func TestRefreshClaudeRuntimeAfterMaintenanceOnlySmokesOnCodexBackend(t *testing.T) {
	a, _, _ := newTestApp(t)
	claude := &fakeClaudeCore{}
	a.claude = claude

	origSmoke := runClaudeSmokeTest
	runClaudeSmokeTest = func(_ *App, _ context.Context) error { return nil }
	defer func() { runClaudeSmokeTest = origSmoke }()

	switched, err := newBackendUpgradeService(a).refreshClaudeRuntimeAfterMaintenance(context.Background())
	if err != nil {
		t.Fatalf("refreshClaudeRuntimeAfterMaintenance() error = %v", err)
	}
	if switched {
		t.Fatal("refreshClaudeRuntimeAfterMaintenance() switched runtime on non-Claude backend")
	}
	if claude.closed {
		t.Fatal("existing Claude runtime should not be closed on non-Claude backend")
	}
}

func TestClaudeSmokeTestUsesInitializeForIdleStartup(t *testing.T) {
	a, _, _ := newTestApp(t)
	a.cfg.Claude.Command = writeFakeClaudeSmokeScript(t, `#!/bin/sh
while IFS= read -r line; do
  rid=$(printf '%s\n' "$line" | sed -n 's/.*"request_id":"\([^"]*\)".*/\1/p')
  case "$line" in
    *'"subtype":"initialize"'*)
      printf '{"type":"control_response","response":{"subtype":"success","request_id":"%s"}}\n' "$rid"
      while IFS= read -r _; do :; done
      exit 0
      ;;
  esac
done
`)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := newBackendUpgradeService(a).claudeSmokeTest(ctx); err != nil {
		t.Fatalf("claudeSmokeTest() error = %v", err)
	}
}

func TestClaudeSmokeTestFailsWhenSessionExitsAfterInitialize(t *testing.T) {
	a, _, _ := newTestApp(t)
	a.cfg.Claude.Command = writeFakeClaudeSmokeScript(t, `#!/bin/sh
while IFS= read -r line; do
  rid=$(printf '%s\n' "$line" | sed -n 's/.*"request_id":"\([^"]*\)".*/\1/p')
  case "$line" in
    *'"subtype":"initialize"'*)
      printf '{"type":"control_response","response":{"subtype":"success","request_id":"%s"}}\n' "$rid"
      exit 0
      ;;
  esac
done
`)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	err := newBackendUpgradeService(a).claudeSmokeTest(ctx)
	if err == nil || !strings.Contains(err.Error(), "exited after initialize") {
		t.Fatalf("claudeSmokeTest() error = %v, want exited after initialize", err)
	}
}

func writeFakeClaudeSmokeScript(t *testing.T, script string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fake-claude-smoke.sh")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile(fake smoke script) error = %v", err)
	}
	return path
}
