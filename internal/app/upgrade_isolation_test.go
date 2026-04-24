package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"feidex/internal/daemon"
	"feidex/internal/feishu"
	"feidex/internal/release"
	"feidex/internal/state"
)

func TestUpgradeCommandRemainsAvailableWithoutCodexOrSessionState(t *testing.T) {
	origRelease := newReleaseClient
	origManager := newDaemonManager
	origVersion := currentVersion
	origGOARCH := currentGOARCH
	defer func() {
		newReleaseClient = origRelease
		newDaemonManager = origManager
		currentVersion = origVersion
		currentGOARCH = origGOARCH
	}()

	a, ff, _ := newTestApp(t)
	a.codex = nil
	a.turnBindings = nil

	newReleaseClient = func() releaseClient {
		return &fakeReleaseClient{info: &release.ReleaseInfo{
			Version:        "v0.4.0",
			HTMLURL:        "https://example.test/releases/v0.4.0",
			BinaryName:     "feidex-linux-amd64",
			BinaryURL:      "https://github.com/example/feidex-linux-amd64",
			ExpectedSHA256: "abc123",
		}}
	}
	newDaemonManager = func(string) (daemon.Manager, error) {
		return &fakeDaemonManagerForApp{status: &daemon.Status{Installed: true, Running: true, PID: os.Getpid()}}, nil
	}
	currentVersion = func() string { return "v0.3.0" }
	currentGOARCH = func() string { return "amd64" }

	msg := &feishu.InboundMessage{MessageID: "m-upgrade-only", ChatID: "chat-1", ChatType: "p2p", UserID: "user-1"}
	if err := newAppUpgradeService(a).commandUpgrade(msg, nil); err != nil {
		t.Fatalf("commandUpgrade() with nil codex and no session state error = %v", err)
	}
	if len(ff.replyCards) != 1 {
		t.Fatalf("reply card count = %d, want 1", len(ff.replyCards))
	}
	body := cardMarkdownContent(t, ff.replyCards[0])
	if !strings.Contains(body, "升级确认") && !strings.Contains(body, "最新版本: `v0.4.0`") {
		t.Fatalf("upgrade card body = %q", body)
	}
	var pending *state.PendingRequest
	for _, req := range a.store.AllPendingRequests() {
		if req.Kind == "upgrade_release" {
			pending = req
			break
		}
	}
	if pending == nil {
		t.Fatal("expected upgrade pending request to be created without session state")
	}
}

func TestUpgradeLocalPathCommandRemainsAvailableWithoutCodexOrSessionState(t *testing.T) {
	origManager := newDaemonManager
	origVersion := currentVersion
	origGOARCH := currentGOARCH
	defer func() {
		newDaemonManager = origManager
		currentVersion = origVersion
		currentGOARCH = origGOARCH
	}()

	a, ff, _ := newTestApp(t)
	a.codex = nil
	a.turnBindings = nil

	newDaemonManager = func(string) (daemon.Manager, error) {
		return &fakeDaemonManagerForApp{status: &daemon.Status{Installed: true, Running: true, PID: os.Getpid()}}, nil
	}
	currentVersion = func() string { return "v0.3.0" }
	currentGOARCH = func() string { return "amd64" }

	localArtifact := filepath.Join(a.cfg.Workspaces[0].Cwd, "dist", "feidex linux amd64")
	if err := os.MkdirAll(filepath.Dir(localArtifact), 0o755); err != nil {
		t.Fatalf("MkdirAll(localArtifact) error = %v", err)
	}
	if err := os.WriteFile(localArtifact, []byte("local-binary"), 0o755); err != nil {
		t.Fatalf("WriteFile(localArtifact) error = %v", err)
	}

	msg := &feishu.InboundMessage{MessageID: "m-upgrade-local-path", ChatID: "chat-1", ChatType: "p2p", UserID: "user-1"}
	if err := newCommandService(a).handleCommand(msg, "/upgrade path dist/feidex linux amd64"); err != nil {
		t.Fatalf("handleCommand(/upgrade path ...) with nil codex and no session state error = %v", err)
	}
	if len(ff.replyCards) != 1 {
		t.Fatalf("reply card count = %d, want 1", len(ff.replyCards))
	}
	body := cardMarkdownContent(t, ff.replyCards[0])
	if !strings.Contains(body, "来源: 本地文件") {
		t.Fatalf("upgrade local path body = %q", body)
	}
	var pending *state.PendingRequest
	for _, req := range a.store.AllPendingRequests() {
		if req.Kind != "upgrade_release" || !strings.Contains(req.PayloadJSON, "\"source_path\":\"") {
			continue
		}
		pending = req
		break
	}
	if pending == nil {
		t.Fatal("expected local-path upgrade pending request to be created without session state")
	}
}

func TestUpgradeConfirmationRemainsAvailableWithoutCodexOrSessionState(t *testing.T) {
	origUpgrade := startDaemonUpgrade
	defer func() { startDaemonUpgrade = origUpgrade }()

	a, _, _ := newTestApp(t)
	a.codex = nil
	a.turnBindings = nil

	if err := a.store.UpsertPending(&state.PendingRequest{
		ID:          "upgrade-isolated",
		Kind:        "upgrade_release",
		OwnerUserID: "user-1",
		Status:      "pending",
		PayloadJSON: mustJSON(upgradePendingPayload{
			TargetVersion:  "v0.4.0",
			BinaryPath:     "/tmp/feidex",
			DownloadURL:    "https://download.test/bin",
			ExpectedSHA256: "abc123",
		}),
	}); err != nil {
		t.Fatalf("UpsertPending() error = %v", err)
	}

	var started daemon.UpgradeSpec
	startDaemonUpgrade = func(spec daemon.UpgradeSpec) (string, error) {
		started = spec
		return "feidex-upgrade-isolated", nil
	}

	resp, err := newAppUpgradeService(a).completeUpgradeAction(&feishu.CardAction{
		UserID:      "user-1",
		ActionValue: map[string]any{"request_id": "upgrade-isolated"},
	}, "upgrade.confirm")
	if err != nil {
		t.Fatalf("completeUpgradeAction() with nil codex and no session state error = %v", err)
	}
	if started.Version != "v0.4.0" || started.BinaryPath != "/tmp/feidex" {
		t.Fatalf("started upgrade spec = %+v", started)
	}
	if resp == nil || resp.Toast == nil || resp.Toast.Type != "success" {
		t.Fatalf("completeUpgradeAction() = %#v, want success", resp)
	}
	if pending := a.store.PendingByID("upgrade-isolated"); pending == nil || pending.Status != "resolved" {
		t.Fatalf("upgrade pending = %+v, want resolved", pending)
	}

	if err := a.store.UpsertPending(&state.PendingRequest{
		ID:          "upgrade-isolated-local",
		Kind:        "upgrade_release",
		OwnerUserID: "user-1",
		Status:      "pending",
		PayloadJSON: mustJSON(upgradePendingPayload{
			TargetVersion:  "local-bin",
			BinaryPath:     "/tmp/feidex",
			SourcePath:     "/tmp/staged-local-bin",
			ExpectedSHA256: "abc123",
		}),
	}); err != nil {
		t.Fatalf("UpsertPending(local) error = %v", err)
	}

	resp, err = newAppUpgradeService(a).completeUpgradeAction(&feishu.CardAction{
		UserID:      "user-1",
		ActionValue: map[string]any{"request_id": "upgrade-isolated-local"},
	}, "upgrade.confirm")
	if err != nil {
		t.Fatalf("completeUpgradeAction(local) error = %v", err)
	}
	if started.SourcePath != "/tmp/staged-local-bin" || started.DownloadURL != "" {
		t.Fatalf("started local upgrade spec = %+v", started)
	}
	if resp == nil || resp.Toast == nil || resp.Toast.Type != "success" {
		t.Fatalf("completeUpgradeAction(local) = %#v, want success", resp)
	}
}
