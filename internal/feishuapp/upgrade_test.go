package feishuapp

import (
	"encoding/json"
	appbackend "feidex/internal/adapter/feishu/backend"
	apppathpick "feidex/internal/adapter/feishu/pathpicker"
	appupgradecmd "feidex/internal/adapter/feishu/upgradecmd"
	"feidex/internal/application/backendmaintenance"
	"feidex/internal/daemon"
	domainbackend "feidex/internal/domain/backend"
	"feidex/internal/domain/conversation"
	catalog "feidex/internal/domain/modelconfig"
	"feidex/internal/release"
	appruntime "feidex/internal/runtime"

	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"feidex/internal/codexrpc"
	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/install"
	appstate "feidex/internal/state"
)

type fakeCodexInstallManager struct {
	probe              install.Probe
	probeErr           error
	latest             string
	latestErr          error
	installErrs        map[string]error
	installs           []string
	postInstallVersion string
}

func (f *fakeCodexInstallManager) Probe(context.Context) (install.Probe, error) {
	return f.probe, f.probeErr
}

func (f *fakeCodexInstallManager) LatestVersion(context.Context) (string, error) {
	return f.latest, f.latestErr
}

func (f *fakeCodexInstallManager) InstallVersion(_ context.Context, version string) error {
	f.installs = append(f.installs, version)
	if f.installErrs != nil {
		if err := f.installErrs[version]; err != nil {
			return err
		}
	}
	if f.postInstallVersion != "" {
		f.probe.CurrentVersion = f.postInstallVersion
	}
	return nil
}

func TestCommandCodexRendersStatusCard(t *testing.T) {
	a, ff, _ := newTestApp(t)
	manager := &fakeCodexInstallManager{
		probe: install.Probe{
			Command:        "codex",
			CommandPath:    "/usr/local/bin/codex",
			CurrentVersion: "1.0.0",
			UpdateCommand:  "update",
			Supported:      true,
		},
	}
	origManager := newCodexInstallManager
	newCodexInstallManager = func(string) codexInstallManager { return manager }
	defer func() { newCodexInstallManager = origManager }()

	msg := &feishu.InboundMessage{MessageID: "msg-1", ChatID: "chat-1", ChatType: "p2p", UserID: "user-1"}
	if err := a.bindings.BackendUpgrades.commandCodex(msg, nil); err != nil {
		t.Fatalf("commandCodex() error = %v", err)
	}
	replyCards := ff.replyCardsSnapshot()
	if len(replyCards) != 1 {
		t.Fatalf("replyCards = %d, want 1", len(replyCards))
	}
	body := cardMarkdownContent(t, replyCards[0])
	for _, want := range []string{"当前版本: `1.0.0`", "目标版本: `未检查`", "自升级命令: `codex update`", "状态: `等待检查`"} {
		if !strings.Contains(body, want) {
			t.Fatalf("status card body = %q, want %q", body, want)
		}
	}
}

func TestCommandCodexRendersUnsupportedReason(t *testing.T) {
	a, ff, _ := newTestApp(t)
	manager := &fakeCodexInstallManager{
		probe: install.Probe{
			Command:        "codex",
			CommandPath:    "/usr/local/bin/codex",
			CurrentVersion: "1.0.0",
			Supported:      false,
			Reason:         "当前 Codex CLI 不支持 `update` 自升级命令",
		},
	}
	origManager := newCodexInstallManager
	newCodexInstallManager = func(string) codexInstallManager { return manager }
	defer func() { newCodexInstallManager = origManager }()

	msg := &feishu.InboundMessage{MessageID: "msg-1", ChatID: "chat-1", ChatType: "p2p", UserID: "user-1"}
	if err := a.bindings.BackendUpgrades.commandCodex(msg, nil); err != nil {
		t.Fatalf("commandCodex() error = %v", err)
	}
	replyCards := ff.replyCardsSnapshot()
	if len(replyCards) != 1 {
		t.Fatalf("replyCards = %d, want 1", len(replyCards))
	}
	body := cardMarkdownContent(t, replyCards[0])
	for _, want := range []string{"状态: `不支持自动升级`", "原因: 当前 Codex CLI 不支持 `update` 自升级命令"} {
		if !strings.Contains(body, want) {
			t.Fatalf("status card body = %q, want %q", body, want)
		}
	}
}

func TestCommandCodexUpgradeCreatesPendingRequest(t *testing.T) {
	a, ff, _ := newTestApp(t)
	manager := &fakeCodexInstallManager{
		probe: install.Probe{
			Command:        "codex",
			CommandPath:    "/usr/local/bin/codex",
			CurrentVersion: "1.0.0",
			UpdateCommand:  "update",
			Supported:      true,
		},
		latest: "1.1.0",
	}
	origManager := newCodexInstallManager
	newCodexInstallManager = func(string) codexInstallManager { return manager }
	defer func() { newCodexInstallManager = origManager }()

	msg := &feishu.InboundMessage{MessageID: "msg-1", ChatID: "chat-1", ChatType: "p2p", UserID: "user-1"}
	if err := a.bindings.BackendUpgrades.commandCodex(msg, []string{"upgrade"}); err != nil {
		t.Fatalf("commandCodex(upgrade) error = %v", err)
	}
	replyCards := ff.replyCardsSnapshot()
	if len(replyCards) != 1 {
		t.Fatalf("replyCards = %d, want 1", len(replyCards))
	}
	body := cardMarkdownContent(t, replyCards[0])
	for _, want := range []string{"当前版本: `1.0.0`", "目标版本: `1.1.0`", "升级方式: `codex update`", "失败处理: 不自动回滚"} {
		if !strings.Contains(body, want) {
			t.Fatalf("confirm card body = %q, want %q", body, want)
		}
	}
	pending := a.State().PendingRequests()
	if len(pending) != 1 || pending[0] == nil || pending[0].Kind != codexUpgradePendingKind {
		t.Fatalf("pending requests = %+v", pending)
	}
	if pending[0].FeishuMsgID != "reply-card-id" {
		t.Fatalf("pending.FeishuMsgID = %q, want reply-card-id", pending[0].FeishuMsgID)
	}
}

func TestCodexUpgradeBlocksCommandsAndInboundMessages(t *testing.T) {
	a, ff, _ := newTestApp(t)
	a.bindings.Maintenance.BeginCodexUpgrade(appbackend.BackendUpgradeSnapshot{Phase: "preflight", Message: "running"})

	msg := &feishu.InboundMessage{MessageID: "status-1", ChatID: "chat-1", ChatType: "p2p", UserID: "user-1"}
	if err := HandleInboundCommand(a, msg, "/status"); err != nil {
		t.Fatalf("HandleInboundCommand(/status) error = %v", err)
	}
	replyCards := ff.replyCardsSnapshot()
	if len(replyCards) != 1 {
		t.Fatalf("expected /status to remain allowed, replyCards=%d", len(replyCards))
	}
	if err := HandleInboundCommand(a, msg, "/quiet"); err == nil || !strings.Contains(err.Error(), "Codex 正在维护中") {
		t.Fatalf("HandleInboundCommand(/quiet) error = %v, want maintenance block", err)
	}

	router := newFeishuEventRouterForTest(a)
	err := router.processMessage(&feishu.InboundMessage{MessageID: "m-1", ChatID: "chat-1", ChatType: "p2p", UserID: "user-1", Text: "hello"})
	if err == nil || !strings.Contains(err.Error(), "Codex 正在维护中") {
		t.Fatalf("processMessage(non-local) error = %v, want maintenance block", err)
	}
}

func TestRunCodexUpgradeOperationSuccess(t *testing.T) {
	a, ff, fc := newTestApp(t)
	manager := &fakeCodexInstallManager{
		probe: install.Probe{
			Command:        "codex",
			CommandPath:    "/usr/local/bin/codex",
			CurrentVersion: "1.0.0",
			UpdateCommand:  "update",
			Supported:      true,
		},
		postInstallVersion: "1.1.0",
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
				result := out.(*catalog.ModelListResult)
				result.Data = []catalog.ModelListEntry{{ID: "gpt-5.4"}}
				return nil
			},
		}
		return promoted
	}
	defer func() {
		newCodexInstallManager = origManager
		newCodexClient = origClient
	}()

	if !a.bindings.Maintenance.BeginCodexUpgrade(appbackend.BackendUpgradeSnapshot{
		Phase:           "preflight",
		CurrentVersion:  "1.0.0",
		PreviousVersion: "1.0.0",
		TargetVersion:   "1.1.0",
	}) {
		t.Fatal("beginCodexUpgrade() should succeed")
	}
	a.bindings.BackendMaintenance["codex"].Run(a.Context(), backendmaintenance.Operation{MessageID: "msg-1", SessionKey: "sess-1", Payload: appruntime.BackendUpgradePendingPayload{
		CurrentVersion: "1.0.0",
		TargetVersion:  "1.1.0",
		UpdateCommand:  "update",
	}})

	if got := manager.installs; len(got) != 1 || got[0] != "latest" {
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
	current, ok := a.runtimeView().currentCodexClient().(*fakeCodexClient)
	if !ok || current != promoted {
		t.Fatalf("a.codex = %#v, want promoted runtime %#v", a.runtimeView().currentCodexClient(), promoted)
	}
	snapshot := a.bindings.Maintenance.CodexUpgradeState()
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

func TestRunCodexUpgradeOperationFailsWithoutRollbackAfterSmokeFailure(t *testing.T) {
	a, ff, fc := newTestApp(t)
	manager := &fakeCodexInstallManager{
		probe: install.Probe{
			Command:        "codex",
			CommandPath:    "/usr/local/bin/codex",
			CurrentVersion: "1.0.0",
			UpdateCommand:  "update",
			Supported:      true,
		},
		postInstallVersion: "1.1.0",
	}
	origManager := newCodexInstallManager
	origClient := newCodexClient
	var smoke *fakeCodexClient
	newCodexInstallManager = func(string) codexInstallManager { return manager }
	newCodexClient = func(config config.CodexConfig) CodexClient {
		client := &fakeCodexClient{
			callHook: func(_ context.Context, method string, _ any, out any) error {
				if method != "model/list" {
					t.Fatalf("unexpected smoke method: %s", method)
				}
				return appbackend.ErrString("boom")
			},
		}
		smoke = client
		return client
	}
	defer func() {
		newCodexInstallManager = origManager
		newCodexClient = origClient
	}()

	if !a.bindings.Maintenance.BeginCodexUpgrade(appbackend.BackendUpgradeSnapshot{
		Phase:           "preflight",
		CurrentVersion:  "1.0.0",
		PreviousVersion: "1.0.0",
		TargetVersion:   "1.1.0",
	}) {
		t.Fatal("beginCodexUpgrade() should succeed")
	}
	a.bindings.BackendMaintenance["codex"].Run(a.Context(), backendmaintenance.Operation{MessageID: "msg-1", SessionKey: "sess-1", Payload: appruntime.BackendUpgradePendingPayload{
		CurrentVersion: "1.0.0",
		TargetVersion:  "1.1.0",
		UpdateCommand:  "update",
	}})

	if got := manager.installs; len(got) != 1 || got[0] != "latest" {
		t.Fatalf("install versions = %#v", got)
	}
	_, liveClosed := fc.statusSnapshot()
	if liveClosed {
		t.Fatal("live codex runtime should not be closed when self-upgrade validation fails")
	}
	_, smokeClosed := false, false
	if smoke != nil {
		_, smokeClosed = smoke.statusSnapshot()
	}
	if smoke == nil || !smokeClosed {
		t.Fatalf("smoke runtime = %+v, want closed after failed validation", smoke)
	}
	current, ok := a.runtimeView().currentCodexClient().(*fakeCodexClient)
	if !ok || current != fc {
		t.Fatalf("a.codex = %#v, want original live runtime %#v", a.runtimeView().currentCodexClient(), fc)
	}
	snapshot := a.bindings.Maintenance.CodexUpgradeState()
	if snapshot.Running || snapshot.Result != "failed" || snapshot.CurrentVersion != "1.0.0" {
		t.Fatalf("final snapshot = %+v", snapshot)
	}
	patchedCards := ff.patchedCardsSnapshot()
	if len(patchedCards) == 0 {
		t.Fatal("expected failure progress cards to be patched")
	}
	body := cardMarkdownContent(t, patchedCards[len(patchedCards)-1])
	if !strings.Contains(body, "结果: `failed`") {
		t.Fatalf("failed patched card body = %q", body)
	}
}

func TestCommandCodexRestartStartsRestartOperation(t *testing.T) {
	a, ff, fc := newTestApp(t)
	manager := &fakeCodexInstallManager{
		probe: install.Probe{
			Command:        "codex",
			CommandPath:    "/usr/local/bin/codex",
			CurrentVersion: "1.0.0",
			UpdateCommand:  "update",
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
				out.(*catalog.ModelListResult).Data = []catalog.ModelListEntry{{ID: "gpt-5.4"}}
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
	if err := a.bindings.BackendUpgrades.commandCodex(msg, []string{"restart"}); err != nil {
		t.Fatalf("commandCodex(restart) error = %v", err)
	}
	replyCards := ff.replyCardsSnapshot()
	if len(replyCards) != 1 {
		t.Fatalf("replyCards = %d, want 1", len(replyCards))
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !a.bindings.Maintenance.CodexRestartState().Running {
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
	current, ok := a.runtimeView().currentCodexClient().(*fakeCodexClient)
	if !ok || current != promoted {
		t.Fatalf("a.codex = %#v, want promoted runtime %#v", a.runtimeView().currentCodexClient(), promoted)
	}
	snapshot := a.bindings.Maintenance.CodexRestartState()
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
		probe: install.Probe{
			Command:        "codex",
			CommandPath:    "/usr/local/bin/codex",
			CurrentVersion: "1.0.0",
			UpdateCommand:  "update",
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
				return appbackend.ErrString("restart-boom")
			},
		}
		return smoke
	}
	defer func() {
		newCodexInstallManager = origManager
		newCodexClient = origClient
	}()

	snapshot, err := a.bindings.BackendMaintenance["codex"].BeginRestart()
	if err != nil {
		t.Fatalf("beginCodexRestartOperation() error = %v", err)
	}
	if !snapshot.Running {
		t.Fatalf("beginCodexRestartOperation() = %+v", snapshot)
	}
	a.bindings.BackendMaintenance["codex"].Run(a.Context(), backendmaintenance.Operation{MessageID: "msg-1", SessionKey: "sess-1", Restart: true})
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
	current, ok := a.runtimeView().currentCodexClient().(*fakeCodexClient)
	if !ok || current != fc {
		t.Fatalf("a.codex = %#v, want original live runtime %#v", a.runtimeView().currentCodexClient(), fc)
	}
	state := a.bindings.Maintenance.CodexRestartState()
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
		probe: install.Probe{
			Command:        "codex",
			CommandPath:    "/usr/local/bin/codex",
			CurrentVersion: "1.0.0",
			UpdateCommand:  "update",
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
				out.(*catalog.ModelListResult).Data = []catalog.ModelListEntry{{ID: "gpt-5.4"}}
				return nil
			},
		}
		return promoted
	}
	defer func() {
		newCodexInstallManager = origManager
		newCodexClient = origClient
	}()

	snapshot, err := a.bindings.BackendMaintenance["codex"].BeginRestart()
	if err != nil {
		t.Fatalf("beginCodexRestartOperation() error = %v", err)
	}
	if !snapshot.Running {
		t.Fatalf("beginCodexRestartOperation() = %+v", snapshot)
	}
	a.bindings.BackendMaintenance["codex"].Run(a.Context(), backendmaintenance.Operation{MessageID: "msg-1", SessionKey: "sess-1", Restart: true})

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
	current, ok := a.runtimeView().currentCodexClient().(*fakeCodexClient)
	if !ok || current != promoted {
		t.Fatalf("a.codex = %#v, want promoted runtime %#v", a.runtimeView().currentCodexClient(), promoted)
	}
	state := a.bindings.Maintenance.CodexRestartState()
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
	a.SetBackend(domainbackend.BackendClaude)
	a.cfg.Feishu.Backend = domainbackend.BackendClaude

	origClient := newCodexClient
	var smoke *fakeCodexClient
	newCodexClient = func(config config.CodexConfig) CodexClient {
		smoke = &fakeCodexClient{
			callHook: func(_ context.Context, method string, _ any, out any) error {
				if method != "model/list" {
					t.Fatalf("unexpected smoke method: %s", method)
				}
				out.(*catalog.ModelListResult).Data = []catalog.ModelListEntry{{ID: "gpt-5.4"}}
				return nil
			},
		}
		return smoke
	}
	defer func() { newCodexClient = origClient }()

	switched, err := a.bindings.BackendUpgrades.refreshCodexRuntimeAfterMaintenance(context.Background())
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
	current, ok := a.runtimeView().currentCodexClient().(*fakeCodexClient)
	if !ok || current != fc {
		t.Fatalf("a.codex = %#v, want original codex runtime %#v", a.runtimeView().currentCodexClient(), fc)
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
				out.(*catalog.ModelListResult).Data = []catalog.ModelListEntry{{ID: "gpt-5.4"}}
				return nil
			},
		}
		return promoted
	}
	defer func() { newCodexClient = origClient }()

	switched, err := a.bindings.BackendUpgrades.refreshCodexRuntimeAfterMaintenance(context.Background())
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
	current, ok := a.runtimeView().currentCodexClient().(*fakeCodexClient)
	if !ok || current != promoted {
		t.Fatalf("a.codex = %#v, want promoted runtime %#v", a.runtimeView().currentCodexClient(), promoted)
	}
}

func TestRefreshCodexRuntimeAfterMaintenanceRecoversFrontendThreadBindings(t *testing.T) {
	a, _, fc := newTestApp(t)
	sessionKey := "sess-refresh-thread-recovery"
	workspaceID := a.cfg.Workspaces[0].ID
	if err := a.store.UpsertSession(&conversation.Session{
		Key:                     sessionKey,
		WorkspaceID:             workspaceID,
		ActiveThreadID:          "thread-old",
		ActiveThreadWorkspaceID: workspaceID,
		ActiveThreadName:        "Old",
		Status:                  conversation.SessionStatusIdle.String(),
	}); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}
	markSessionThreadLive(a, sessionKey, "thread-old")

	origClient := newCodexClient
	var promoted *fakeCodexClient
	var calls []string
	newCodexClient = func(config.CodexConfig) CodexClient {
		promoted = &fakeCodexClient{
			callHook: func(_ context.Context, method string, params any, out any) error {
				calls = append(calls, method)
				switch method {
				case "model/list":
					out.(*catalog.ModelListResult).Data = []catalog.ModelListEntry{{ID: "gpt-5.4"}}
					return nil
				case "thread/resume":
					paramMap := params.(map[string]any)
					if got := paramMap["threadId"]; got != "thread-old" {
						t.Fatalf("thread/resume threadId = %v, want thread-old", got)
					}
					return errors.New("thread not found")
				case "thread/start":
					result := out.(*codexrpc.ThreadStartResult)
					result.Thread.ID = "thread-new"
					result.Thread.Name = "Recovered"
					result.Thread.Preview = "preview"
					return nil
				default:
					t.Fatalf("unexpected promoted method: %s", method)
					return nil
				}
			},
		}
		return promoted
	}
	defer func() { newCodexClient = origClient }()

	switched, err := a.bindings.BackendUpgrades.refreshCodexRuntimeAfterMaintenance(context.Background())
	if err != nil {
		t.Fatalf("refreshCodexRuntimeAfterMaintenance() error = %v", err)
	}
	if !switched {
		t.Fatal("refreshCodexRuntimeAfterMaintenance() did not switch runtime")
	}
	_, liveClosed := fc.statusSnapshot()
	if !liveClosed {
		t.Fatal("old runtime should be closed")
	}
	current, ok := a.runtimeView().currentCodexClient().(*fakeCodexClient)
	if !ok || current != promoted {
		t.Fatalf("a.codex = %#v, want promoted runtime %#v", a.runtimeView().currentCodexClient(), promoted)
	}
	wantCalls := []string{"model/list", "thread/resume", "thread/start"}
	if strings.Join(calls, ",") != strings.Join(wantCalls, ",") {
		t.Fatalf("promoted calls = %+v, want %+v", calls, wantCalls)
	}
	sess := a.store.GetSession(sessionKey)
	if sess == nil || sess.ActiveThreadID != "thread-new" || sess.ActiveThreadWorkspaceID != workspaceID || sess.Status != conversation.SessionStatusIdle.String() {
		t.Fatalf("session after runtime refresh = %+v, want recovered thread-new idle", sess)
	}
	if sessionHasLiveThread(a, sessionKey, "thread-old") {
		t.Fatal("old thread should not remain marked live")
	}
	if !sessionHasLiveThread(a, sessionKey, "thread-new") {
		t.Fatal("recovered thread should be marked live")
	}
}

type fakeClaudeInstallManager struct {
	probe              install.Probe
	probeErr           error
	latest             string
	latestErr          error
	installErrs        map[string]error
	installs           []string
	postInstallVersion string
}

func (f *fakeClaudeInstallManager) Probe(context.Context) (install.Probe, error) {
	return f.probe, f.probeErr
}

func (f *fakeClaudeInstallManager) LatestVersion(context.Context) (string, error) {
	return f.latest, f.latestErr
}

func (f *fakeClaudeInstallManager) InstallVersion(_ context.Context, version string) error {
	f.installs = append(f.installs, version)
	if f.installErrs != nil {
		if err := f.installErrs[version]; err != nil {
			return err
		}
	}
	if f.postInstallVersion != "" {
		f.probe.CurrentVersion = f.postInstallVersion
	}
	return nil
}

func TestCommandClaudeRendersStatusCard(t *testing.T) {
	a, ff, _ := newTestApp(t)
	manager := &fakeClaudeInstallManager{
		probe: install.Probe{
			Command:        "claude",
			CommandPath:    "/usr/local/bin/claude",
			CurrentVersion: "1.0.0",
			UpdateCommand:  "update",
			Supported:      true,
		},
	}
	origManager := newClaudeInstallManager
	newClaudeInstallManager = func(string) claudeInstallManager { return manager }
	defer func() { newClaudeInstallManager = origManager }()

	msg := &feishu.InboundMessage{MessageID: "msg-1", ChatID: "chat-1", ChatType: "p2p", UserID: "user-1"}
	if err := a.bindings.BackendUpgrades.commandClaude(msg, nil); err != nil {
		t.Fatalf("commandClaude() error = %v", err)
	}
	replyCards := ff.replyCardsSnapshot()
	if len(replyCards) != 1 {
		t.Fatalf("replyCards = %d, want 1", len(replyCards))
	}
	body := cardMarkdownContent(t, replyCards[0])
	for _, want := range []string{"当前版本: `1.0.0`", "目标版本: `未检查`", "自升级命令: `claude update`", "状态: `等待检查`", "smoke test: `start + init`"} {
		if !strings.Contains(body, want) {
			t.Fatalf("status card body = %q, want %q", body, want)
		}
	}
}

func TestCommandClaudeRendersUnsupportedReason(t *testing.T) {
	a, ff, _ := newTestApp(t)
	manager := &fakeClaudeInstallManager{
		probe: install.Probe{
			Command:        "claude",
			CommandPath:    "/usr/local/bin/claude",
			CurrentVersion: "1.0.0",
			Supported:      false,
			Reason:         "当前 Claude CLI 不支持 `update` 自升级命令",
		},
	}
	origManager := newClaudeInstallManager
	newClaudeInstallManager = func(string) claudeInstallManager { return manager }
	defer func() { newClaudeInstallManager = origManager }()

	msg := &feishu.InboundMessage{MessageID: "msg-1", ChatID: "chat-1", ChatType: "p2p", UserID: "user-1"}
	if err := a.bindings.BackendUpgrades.commandClaude(msg, nil); err != nil {
		t.Fatalf("commandClaude() error = %v", err)
	}
	replyCards := ff.replyCardsSnapshot()
	if len(replyCards) != 1 {
		t.Fatalf("replyCards = %d, want 1", len(replyCards))
	}
	body := cardMarkdownContent(t, replyCards[0])
	for _, want := range []string{"状态: `不支持自动升级`", "原因: 当前 Claude CLI 不支持 `update` 自升级命令"} {
		if !strings.Contains(body, want) {
			t.Fatalf("status card body = %q, want %q", body, want)
		}
	}
}

func TestCommandClaudeUpgradeCreatesPendingRequest(t *testing.T) {
	a, ff, _ := newTestApp(t)
	manager := &fakeClaudeInstallManager{
		probe: install.Probe{
			Command:        "claude",
			CommandPath:    "/usr/local/bin/claude",
			CurrentVersion: "1.0.0",
			UpdateCommand:  "update",
			Supported:      true,
		},
		latest: "2.1.139",
	}
	origManager := newClaudeInstallManager
	newClaudeInstallManager = func(string) claudeInstallManager { return manager }
	defer func() { newClaudeInstallManager = origManager }()

	msg := &feishu.InboundMessage{MessageID: "msg-1", ChatID: "chat-1", ChatType: "p2p", UserID: "user-1"}
	if err := a.bindings.BackendUpgrades.commandClaude(msg, []string{"upgrade"}); err != nil {
		t.Fatalf("commandClaude(upgrade) error = %v", err)
	}
	replyCards := ff.replyCardsSnapshot()
	if len(replyCards) != 1 {
		t.Fatalf("replyCards = %d, want 1", len(replyCards))
	}
	body := cardMarkdownContent(t, replyCards[0])
	for _, want := range []string{"当前版本: `1.0.0`", "目标版本: `2.1.139`", "升级方式: `claude update`", "失败处理: 不自动回滚"} {
		if !strings.Contains(body, want) {
			t.Fatalf("confirm card body = %q, want %q", body, want)
		}
	}
	pending := a.State().PendingRequests()
	if len(pending) != 1 || pending[0] == nil || pending[0].Kind != claudeUpgradePendingKind {
		t.Fatalf("pending requests = %+v", pending)
	}
	if pending[0].FeishuMsgID != "reply-card-id" {
		t.Fatalf("pending.FeishuMsgID = %q, want reply-card-id", pending[0].FeishuMsgID)
	}
}

func TestClaudeUpgradeBlocksCommandsAndInboundMessages(t *testing.T) {
	a, ff, _ := newTestApp(t)
	a.SetBackend(domainbackend.BackendClaude)
	a.runtimeView().setClaudeCore(&fakeClaudeCore{})
	a.bindings.Maintenance.BeginClaudeUpgrade(appbackend.BackendUpgradeSnapshot{Phase: "preflight", Message: "running"})

	msg := &feishu.InboundMessage{MessageID: "status-1", ChatID: "chat-1", ChatType: "p2p", UserID: "user-1"}
	if err := HandleInboundCommand(a, msg, "/status"); err != nil {
		t.Fatalf("HandleInboundCommand(/status) error = %v", err)
	}
	replyCards := ff.replyCardsSnapshot()
	if len(replyCards) != 1 {
		t.Fatalf("expected /status to remain allowed, replyCards=%d", len(replyCards))
	}
	if err := HandleInboundCommand(a, msg, "/quiet"); err == nil || !strings.Contains(err.Error(), "Claude 正在维护中") {
		t.Fatalf("HandleInboundCommand(/quiet) error = %v, want maintenance block", err)
	}

	router := newFeishuEventRouterForTest(a)
	err := router.processMessage(&feishu.InboundMessage{MessageID: "m-1", ChatID: "chat-1", ChatType: "p2p", UserID: "user-1", Text: "hello"})
	if err == nil || !strings.Contains(err.Error(), "Claude 正在维护中") {
		t.Fatalf("processMessage(non-local) error = %v, want maintenance block", err)
	}
}

func TestRunClaudeUpgradeOperationSuccess(t *testing.T) {
	a, ff, _ := newTestApp(t)
	a.SetBackend(domainbackend.BackendClaude)
	a.cfg.Feishu.Backend = domainbackend.BackendClaude
	claude := &fakeClaudeCore{}
	a.runtimeView().setClaudeCore(claude)
	manager := &fakeClaudeInstallManager{
		probe: install.Probe{
			Command:        "claude",
			CommandPath:    "/usr/local/bin/claude",
			CurrentVersion: "1.0.0",
			UpdateCommand:  "update",
			Supported:      true,
		},
		postInstallVersion: "1.1.0",
	}
	origManager := newClaudeInstallManager
	origSmoke := a.bindings.ClaudeMaintenance.Smoke
	newClaudeInstallManager = func(string) claudeInstallManager { return manager }
	a.bindings.ClaudeMaintenance.Smoke = func(_ context.Context) error { return nil }
	defer func() {
		newClaudeInstallManager = origManager
		a.bindings.ClaudeMaintenance.Smoke = origSmoke
	}()

	if !a.bindings.Maintenance.BeginClaudeUpgrade(appbackend.BackendUpgradeSnapshot{
		Phase:           "preflight",
		CurrentVersion:  "1.0.0",
		PreviousVersion: "1.0.0",
		TargetVersion:   "2.1.139",
	}) {
		t.Fatal("beginClaudeUpgrade() should succeed")
	}
	a.bindings.BackendMaintenance["claude"].Run(a.Context(), backendmaintenance.Operation{MessageID: "msg-1", SessionKey: "sess-1", Payload: appruntime.BackendUpgradePendingPayload{
		CurrentVersion: "1.0.0",
		TargetVersion:  "2.1.139",
		UpdateCommand:  "update",
	}})

	if got := manager.installs; len(got) != 1 || got[0] != "latest" {
		t.Fatalf("install versions = %#v", got)
	}
	if !claude.closed {
		t.Fatal("expected live Claude runtime to be closed after successful promotion")
	}
	snapshot := a.bindings.Maintenance.ClaudeUpgradeState()
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

func TestRunClaudeUpgradeOperationFailsWithoutRollbackAfterSmokeFailure(t *testing.T) {
	a, ff, _ := newTestApp(t)
	a.SetBackend(domainbackend.BackendClaude)
	a.cfg.Feishu.Backend = domainbackend.BackendClaude
	claude := &fakeClaudeCore{}
	a.runtimeView().setClaudeCore(claude)
	manager := &fakeClaudeInstallManager{
		probe: install.Probe{
			Command:        "claude",
			CommandPath:    "/usr/local/bin/claude",
			CurrentVersion: "1.0.0",
			UpdateCommand:  "update",
			Supported:      true,
		},
		postInstallVersion: "1.1.0",
	}
	origManager := newClaudeInstallManager
	origSmoke := a.bindings.ClaudeMaintenance.Smoke
	newClaudeInstallManager = func(string) claudeInstallManager { return manager }
	a.bindings.ClaudeMaintenance.Smoke = func(_ context.Context) error {
		return appbackend.ErrString("boom")
	}
	defer func() {
		newClaudeInstallManager = origManager
		a.bindings.ClaudeMaintenance.Smoke = origSmoke
	}()

	if !a.bindings.Maintenance.BeginClaudeUpgrade(appbackend.BackendUpgradeSnapshot{
		Phase:           "preflight",
		CurrentVersion:  "1.0.0",
		PreviousVersion: "1.0.0",
		TargetVersion:   "2.1.139",
	}) {
		t.Fatal("beginClaudeUpgrade() should succeed")
	}
	a.bindings.BackendMaintenance["claude"].Run(a.Context(), backendmaintenance.Operation{MessageID: "msg-1", SessionKey: "sess-1", Payload: appruntime.BackendUpgradePendingPayload{
		CurrentVersion: "1.0.0",
		TargetVersion:  "2.1.139",
		UpdateCommand:  "update",
	}})

	if got := manager.installs; len(got) != 1 || got[0] != "latest" {
		t.Fatalf("install versions = %#v", got)
	}
	if claude.closed {
		t.Fatal("live Claude runtime should not be closed when self-upgrade validation fails")
	}
	snapshot := a.bindings.Maintenance.ClaudeUpgradeState()
	if snapshot.Running || snapshot.Result != "failed" || snapshot.CurrentVersion != "1.0.0" {
		t.Fatalf("final snapshot = %+v", snapshot)
	}
	patchedCards := ff.patchedCardsSnapshot()
	if len(patchedCards) == 0 {
		t.Fatal("expected failure progress cards to be patched")
	}
	body := cardMarkdownContent(t, patchedCards[len(patchedCards)-1])
	if !strings.Contains(body, "结果: `failed`") {
		t.Fatalf("failed patched card body = %q", body)
	}
}

func TestCommandClaudeRestartStartsRestartOperation(t *testing.T) {
	a, ff, _ := newTestApp(t)
	a.SetBackend(domainbackend.BackendClaude)
	a.cfg.Feishu.Backend = domainbackend.BackendClaude
	claude := &fakeClaudeCore{}
	a.runtimeView().setClaudeCore(claude)
	manager := &fakeClaudeInstallManager{
		probe: install.Probe{
			Command:        "claude",
			CommandPath:    "/usr/local/bin/claude",
			CurrentVersion: "1.0.0",
			UpdateCommand:  "update",
			Supported:      true,
		},
	}
	origManager := newClaudeInstallManager
	origSmoke := a.bindings.ClaudeMaintenance.Smoke
	newClaudeInstallManager = func(string) claudeInstallManager { return manager }
	a.bindings.ClaudeMaintenance.Smoke = func(_ context.Context) error { return nil }
	defer func() {
		newClaudeInstallManager = origManager
		a.bindings.ClaudeMaintenance.Smoke = origSmoke
	}()

	msg := &feishu.InboundMessage{MessageID: "msg-1", ChatID: "chat-1", ChatType: "p2p", UserID: "user-1"}
	if err := a.bindings.BackendUpgrades.commandClaude(msg, []string{"restart"}); err != nil {
		t.Fatalf("commandClaude(restart) error = %v", err)
	}
	replyCards := ff.replyCardsSnapshot()
	if len(replyCards) != 1 {
		t.Fatalf("replyCards = %d, want 1", len(replyCards))
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !a.bindings.Maintenance.ClaudeRestartState().Running {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !claude.closed {
		t.Fatal("expected live runtime to be closed during restart")
	}
	snapshot := a.bindings.Maintenance.ClaudeRestartState()
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
	a.SetBackend(domainbackend.BackendClaude)
	a.cfg.Feishu.Backend = domainbackend.BackendClaude
	claude := &fakeClaudeCore{}
	a.runtimeView().setClaudeCore(claude)
	manager := &fakeClaudeInstallManager{
		probe: install.Probe{
			Command:        "claude",
			CommandPath:    "/usr/local/bin/claude",
			CurrentVersion: "1.0.0",
			UpdateCommand:  "update",
			Supported:      true,
		},
	}
	origManager := newClaudeInstallManager
	origSmoke := a.bindings.ClaudeMaintenance.Smoke
	newClaudeInstallManager = func(string) claudeInstallManager { return manager }
	a.bindings.ClaudeMaintenance.Smoke = func(_ context.Context) error { return appbackend.ErrString("restart-boom") }
	defer func() {
		newClaudeInstallManager = origManager
		a.bindings.ClaudeMaintenance.Smoke = origSmoke
	}()

	snapshot, err := a.bindings.BackendMaintenance["claude"].BeginRestart()
	if err != nil {
		t.Fatalf("beginClaudeRestartOperation() error = %v", err)
	}
	if !snapshot.Running {
		t.Fatalf("beginClaudeRestartOperation() = %+v", snapshot)
	}
	a.bindings.BackendMaintenance["claude"].Run(a.Context(), backendmaintenance.Operation{MessageID: "msg-1", SessionKey: "sess-1", Restart: true})
	if claude.closed {
		t.Fatal("restart should keep old runtime alive when new runtime validation fails")
	}
	state := a.bindings.Maintenance.ClaudeRestartState()
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
	a.runtimeView().setClaudeCore(claude)

	origSmoke := a.bindings.ClaudeMaintenance.Smoke
	a.bindings.ClaudeMaintenance.Smoke = func(_ context.Context) error { return nil }
	defer func() { a.bindings.ClaudeMaintenance.Smoke = origSmoke }()

	switched, err := a.bindings.ClaudeMaintenance.Refresh(context.Background())
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
	if err := a.bindings.ClaudeMaintenance.Smoke(ctx); err != nil {
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
      sleep 0.05
      exit 0
      ;;
  esac
done
`)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	err := a.bindings.ClaudeMaintenance.Smoke(ctx)
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

// --- merged from upgrade_more_test.go ---

func TestUpgradeBranches(t *testing.T) {
	origManager := newDaemonManager
	origRelease := newReleaseClient
	origVersion := currentVersion
	origGOOS := currentGOOS
	origGOARCH := currentGOARCH
	origStart := startDaemonUpgrade
	defer func() {
		newDaemonManager = origManager
		newReleaseClient = origRelease
		currentVersion = origVersion
		currentGOOS = origGOOS
		currentGOARCH = origGOARCH
		startDaemonUpgrade = origStart
	}()

	a, _, _ := newTestApp(t)
	currentVersion = func() string { return "v9.9.9" }
	currentGOOS = func() string { return "linux" }
	currentGOARCH = func() string { return "amd64" }
	newDaemonManager = func(string) (daemon.Manager, error) {
		return &fakeDaemonManagerForApp{status: &daemon.Status{Installed: true, Running: true, PID: os.Getpid()}}, nil
	}
	newReleaseClient = func() releaseClient {
		return &fakeReleaseClient{info: &release.ReleaseInfo{Version: "v9.9.9", BinaryURL: "https://download.test/bin", ExpectedSHA256: "abc"}}
	}
	card, err := a.bindings.Upgrades.RenderUpgradeCardForTarget("sess-1", "user-1", "", false)
	if err != nil || card == nil {
		t.Fatalf("renderUpgradeCard(latest) = %#v, %v", card, err)
	}

	newReleaseClient = func() releaseClient {
		return &fakeReleaseClient{
			latestErr: errors.New("latest should not be called"),
			versionInfo: map[string]*release.ReleaseInfo{
				"v1.0.0": {Version: "v1.0.0", BinaryURL: "https://download.test/bin", ExpectedSHA256: "abc"},
			},
		}
	}
	card, err = a.bindings.Upgrades.RenderUpgradeCardForVersion("sess-1", "user-1", "v1.0.0")
	if err != nil || card == nil {
		t.Fatalf("renderUpgradeCardForVersion() = %#v, %v", card, err)
	}

	newDaemonManager = func(string) (daemon.Manager, error) {
		return &fakeDaemonManagerForApp{status: &daemon.Status{Installed: false}}, nil
	}
	if _, err := a.bindings.Upgrades.RenderUpgradeCardForTarget("sess-1", "user-1", "", false); err == nil {
		t.Fatal("expected renderUpgradeCard() to reject uninstalled daemon")
	}

	newDaemonManager = func(string) (daemon.Manager, error) {
		return &fakeDaemonManagerForApp{status: &daemon.Status{Installed: true, Running: true, PID: os.Getpid()}}, nil
	}
	newReleaseClient = func() releaseClient {
		return &fakeReleaseClient{info: &release.ReleaseInfo{Version: "v10.0.0", BinaryURL: "https://download.test/bin", ExpectedSHA256: "abc"}}
	}
	if resp, err := a.bindings.Upgrades.CompleteUpgradeAction(&feishu.CardAction{UserID: "user-1", ActionValue: map[string]any{"request_id": "missing"}}, "upgrade.confirm"); err != nil || resp.Toast == nil || resp.Toast.Type != "warning" {
		t.Fatalf("completeUpgradeAction(missing) = %#v, %v", resp, err)
	}
	if err := a.store.UpsertPending(&appstate.PendingRequest{ID: "upgrade-bad", Kind: "upgrade_release", OwnerUserID: "other", Status: "pending"}); err != nil {
		t.Fatalf("UpsertPending(upgrade-bad) error = %v", err)
	}
	if resp, err := a.bindings.Upgrades.CompleteUpgradeAction(&feishu.CardAction{UserID: "user-1", ActionValue: map[string]any{"request_id": "upgrade-bad"}}, "upgrade.confirm"); err != nil || resp.Toast == nil || resp.Toast.Type != "warning" {
		t.Fatalf("completeUpgradeAction(wrong owner) = %#v, %v", resp, err)
	}
	if err := a.store.UpsertPending(&appstate.PendingRequest{ID: "upgrade-json", Kind: "upgrade_release", OwnerUserID: "user-1", Status: "pending", PayloadJSON: "{"}); err != nil {
		t.Fatalf("UpsertPending(upgrade-json) error = %v", err)
	}
	if resp, err := a.bindings.Upgrades.CompleteUpgradeAction(&feishu.CardAction{UserID: "user-1", ActionValue: map[string]any{"request_id": "upgrade-json"}}, "upgrade.confirm"); err != nil || resp.Toast == nil || resp.Toast.Type != "warning" {
		t.Fatalf("completeUpgradeAction(bad json) = %#v, %v", resp, err)
	}

	if err := a.store.UpsertPending(&appstate.PendingRequest{
		ID:          "upgrade-start",
		Kind:        "upgrade_release",
		OwnerUserID: "user-1",
		SessionKey:  "sess-1",
		Status:      "pending",
		PayloadJSON: mustJSON(appupgradecmd.UpgradePendingPayload{TargetVersion: "v10.0.0", BinaryPath: "/tmp/feidex", DownloadURL: "https://download.test/bin", ExpectedSHA256: "abc"}),
	}); err != nil {
		t.Fatalf("UpsertPending(upgrade-start) error = %v", err)
	}
	startDaemonUpgrade = func(daemon.UpgradeSpec) (string, error) { return "", errors.New("boom") }
	if resp, err := a.bindings.Upgrades.CompleteUpgradeAction(&feishu.CardAction{UserID: "user-1", ActionValue: map[string]any{"request_id": "upgrade-start"}}, "upgrade.confirm"); err != nil || resp.Toast == nil || resp.Toast.Type != "warning" {
		t.Fatalf("completeUpgradeAction(start fail) = %#v, %v", resp, err)
	}

	localArtifact := filepath.Join(a.cfg.Workspaces[0].Cwd, "dist", "feidex-linux-amd64")
	if err := os.MkdirAll(filepath.Dir(localArtifact), 0o755); err != nil {
		t.Fatalf("MkdirAll(localArtifact) error = %v", err)
	}
	if err := os.WriteFile(localArtifact, []byte("local-binary"), 0o755); err != nil {
		t.Fatalf("WriteFile(localArtifact) error = %v", err)
	}
	resp, err := a.bindings.Upgrades.CompleteUpgradeLocalPick(&feishu.CardAction{
		UserID:      "user-1",
		MessageID:   "msg-1",
		ActionValue: map[string]any{"session_key": "sess-1"},
	})
	if err != nil || resp == nil || resp.Card == nil {
		t.Fatalf("completeUpgradeLocalPick() = %#v, %v", resp, err)
	}
	var picker *appstate.PendingRequest
	for _, req := range a.store.AllPendingRequests() {
		if req.Kind == appupgradecmd.UpgradeLocalBinaryPendingKind {
			picker = req
			break
		}
	}
	if picker == nil {
		t.Fatal("expected local upgrade picker pending request")
	}
	resp, err = completePathPickerAction(a, &feishu.CardAction{
		UserID:      "user-1",
		MessageID:   "msg-1",
		ActionValue: map[string]any{"request_id": picker.ID},
		Option:      apppathpick.EncodeOption(apppathpick.Entry{Name: filepath.Base(localArtifact), Path: localArtifact, IsDir: false}),
	}, "path_picker.dropdown")
	if err != nil || resp == nil || resp.Card == nil {
		t.Fatalf("local picker dropdown = %#v, %v", resp, err)
	}
	resp, err = completePathPickerAction(a, &feishu.CardAction{
		UserID:      "user-1",
		MessageID:   "msg-1",
		ActionValue: map[string]any{"request_id": picker.ID},
	}, "path_picker.confirm")
	if err != nil || resp == nil || resp.Card == nil {
		t.Fatalf("local picker confirm = %#v, %v", resp, err)
	}
	foundLocal := false
	for _, req := range a.store.AllPendingRequests() {
		if req.Kind != "upgrade_release" || req.ID == "upgrade-start" {
			continue
		}
		var payload appupgradecmd.UpgradePendingPayload
		if err := json.Unmarshal([]byte(req.PayloadJSON), &payload); err != nil {
			continue
		}
		if payload.SourcePath == "" {
			continue
		}
		if payload.DownloadURL != "" || payload.ExpectedSHA256 == "" {
			t.Fatalf("unexpected local upgrade payload = %+v", payload)
		}
		if _, err := os.Stat(payload.SourcePath); err != nil {
			t.Fatalf("staged local artifact stat error = %v", err)
		}
		foundLocal = true
		break
	}
	if !foundLocal {
		t.Fatal("expected staged local upgrade pending request")
	}
}

func TestUpgradeCommandReturnsCheckOnlyCardOnDarwin(t *testing.T) {
	origRelease := newReleaseClient
	origManager := newDaemonManager
	origVersion := currentVersion
	origGOOS := currentGOOS
	origGOARCH := currentGOARCH
	defer func() {
		newReleaseClient = origRelease
		newDaemonManager = origManager
		currentVersion = origVersion
		currentGOOS = origGOOS
		currentGOARCH = origGOARCH
	}()

	a, ff, _ := newTestApp(t)
	daemonCalled := false
	newDaemonManager = func(string) (daemon.Manager, error) {
		daemonCalled = true
		return &fakeDaemonManagerForApp{status: &daemon.Status{Installed: true, Running: true, PID: os.Getpid()}}, nil
	}
	newReleaseClient = func() releaseClient {
		return &fakeReleaseClient{info: &release.ReleaseInfo{
			Version:        "v0.2.0",
			HTMLURL:        "https://example.test/releases/v0.2.0",
			BinaryURL:      "https://github.com/example/feidex-darwin-arm64",
			ExpectedSHA256: "abc123",
		}}
	}
	currentVersion = func() string { return "v0.1.0" }
	currentGOOS = func() string { return "darwin" }
	currentGOARCH = func() string { return "arm64" }

	msg := &feishu.InboundMessage{MessageID: "m-darwin", ChatID: "chat-1", ChatType: "p2p", UserID: "user-1"}
	if err := a.bindings.Upgrades.CommandUpgrade(msg, nil); err != nil {
		t.Fatalf("commandUpgrade() error = %v", err)
	}
	if daemonCalled {
		t.Fatal("expected darwin upgrade check to skip daemon validation")
	}
	if len(ff.replyCards) != 1 {
		t.Fatalf("reply card count = %d, want 1", len(ff.replyCards))
	}
	body := cardMarkdownContent(t, ff.replyCards[0])
	if !strings.Contains(body, "当前平台仅支持 release 检查，不支持自动升级。") {
		t.Fatalf("darwin upgrade card body = %q", body)
	}
	if !strings.Contains(body, "目标平台: `darwin/arm64`") || !strings.Contains(body, "目标包: `feidex-darwin-arm64`") {
		t.Fatalf("darwin upgrade card body = %q", body)
	}
	if pending := a.store.PendingByID("upgrade-1"); pending != nil {
		t.Fatalf("expected no upgrade pending request, got %+v", pending)
	}
}
