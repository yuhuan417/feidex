package app

import (
	"context"
	"strings"
	"testing"
	"time"

	"feidex/internal/claudeinstall"
	"feidex/internal/feishu"
)

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
	if err := a.commandClaude(msg, nil); err != nil {
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
	if err := a.commandClaude(msg, []string{"upgrade"}); err != nil {
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
	pending := a.appState().pendingRequests()
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
	a.beginClaudeUpgrade(claudeUpgradeSnapshot{Phase: "preflight", Message: "running"})

	msg := &feishu.InboundMessage{MessageID: "status-1", ChatID: "chat-1", ChatType: "p2p", UserID: "user-1"}
	if err := a.handleCommand(msg, "/status"); err != nil {
		t.Fatalf("handleCommand(/status) error = %v", err)
	}
	replyCards := ff.replyCardsSnapshot()
	if len(replyCards) != 1 {
		t.Fatalf("expected /status to remain allowed, replyCards=%d", len(replyCards))
	}
	if err := a.handleCommand(msg, "/quiet"); err == nil || !strings.Contains(err.Error(), "Claude 正在维护中") {
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

	if !a.beginClaudeUpgrade(claudeUpgradeSnapshot{
		Phase:           "preflight",
		CurrentVersion:  "1.0.0",
		PreviousVersion: "1.0.0",
		TargetVersion:   "1.1.0",
	}) {
		t.Fatal("beginClaudeUpgrade() should succeed")
	}
	a.runClaudeUpgradeOperation("msg-1", "sess-1", claudeUpgradePendingPayload{
		CurrentVersion: "1.0.0",
		TargetVersion:  "1.1.0",
	})

	if got := manager.installs; len(got) != 1 || got[0] != "1.1.0" {
		t.Fatalf("install versions = %#v", got)
	}
	if !claude.closed {
		t.Fatal("expected live Claude runtime to be closed after successful promotion")
	}
	snapshot := a.claudeUpgradeState()
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

	if !a.beginClaudeUpgrade(claudeUpgradeSnapshot{
		Phase:           "preflight",
		CurrentVersion:  "1.0.0",
		PreviousVersion: "1.0.0",
		TargetVersion:   "1.1.0",
	}) {
		t.Fatal("beginClaudeUpgrade() should succeed")
	}
	a.runClaudeUpgradeOperation("msg-1", "sess-1", claudeUpgradePendingPayload{
		CurrentVersion: "1.0.0",
		TargetVersion:  "1.1.0",
	})

	if got := manager.installs; len(got) != 2 || got[0] != "1.1.0" || got[1] != "1.0.0" {
		t.Fatalf("install versions = %#v", got)
	}
	if claude.closed {
		t.Fatal("live Claude runtime should not be closed when upgrade rolls back before promotion")
	}
	snapshot := a.claudeUpgradeState()
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
	if err := a.commandClaude(msg, []string{"restart"}); err != nil {
		t.Fatalf("commandClaude(restart) error = %v", err)
	}
	replyCards := ff.replyCardsSnapshot()
	if len(replyCards) != 1 {
		t.Fatalf("replyCards = %d, want 1", len(replyCards))
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if !a.claudeRestartState().Running {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !claude.closed {
		t.Fatal("expected live runtime to be closed during restart")
	}
	snapshot := a.claudeRestartState()
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

	snapshot, err := a.beginClaudeRestartOperation()
	if err != nil {
		t.Fatalf("beginClaudeRestartOperation() error = %v", err)
	}
	if !snapshot.Running {
		t.Fatalf("beginClaudeRestartOperation() = %+v", snapshot)
	}
	a.runClaudeRestartOperation("msg-1", "sess-1")
	if claude.closed {
		t.Fatal("restart should keep old runtime alive when new runtime validation fails")
	}
	state := a.claudeRestartState()
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

	switched, err := a.refreshClaudeRuntimeAfterMaintenance(context.Background())
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
