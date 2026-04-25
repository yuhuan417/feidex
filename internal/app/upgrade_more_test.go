package app

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"feidex/internal/daemon"
	"feidex/internal/feishu"
	"feidex/internal/release"
	"feidex/internal/state"
)

func TestUpgradeBranches(t *testing.T) {
	origManager := newDaemonManager
	origRelease := newReleaseClient
	origVersion := currentVersion
	origGOARCH := currentGOARCH
	origStart := startDaemonUpgrade
	defer func() {
		newDaemonManager = origManager
		newReleaseClient = origRelease
		currentVersion = origVersion
		currentGOARCH = origGOARCH
		startDaemonUpgrade = origStart
	}()

	a, _, _ := newTestApp(t)
	currentVersion = func() string { return "v9.9.9" }
	currentGOARCH = func() string { return "amd64" }
	newDaemonManager = func(string) (daemon.Manager, error) {
		return &fakeDaemonManagerForApp{status: &daemon.Status{Installed: true, Running: true, PID: os.Getpid()}}, nil
	}
	newReleaseClient = func() releaseClient {
		return &fakeReleaseClient{info: &release.ReleaseInfo{Version: "v9.9.9", BinaryURL: "https://download.test/bin", ExpectedSHA256: "abc"}}
	}
	card, err := newAppUpgradeService(a).renderUpgradeCardForTarget("sess-1", "user-1", "", false)
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
	card, err = newAppUpgradeService(a).renderUpgradeCardForVersion("sess-1", "user-1", "v1.0.0")
	if err != nil || card == nil {
		t.Fatalf("renderUpgradeCardForVersion() = %#v, %v", card, err)
	}

	newDaemonManager = func(string) (daemon.Manager, error) {
		return &fakeDaemonManagerForApp{status: &daemon.Status{Installed: false}}, nil
	}
	if _, err := newAppUpgradeService(a).renderUpgradeCardForTarget("sess-1", "user-1", "", false); err == nil {
		t.Fatal("expected renderUpgradeCard() to reject uninstalled daemon")
	}

	newDaemonManager = func(string) (daemon.Manager, error) {
		return &fakeDaemonManagerForApp{status: &daemon.Status{Installed: true, Running: true, PID: os.Getpid()}}, nil
	}
	newReleaseClient = func() releaseClient {
		return &fakeReleaseClient{info: &release.ReleaseInfo{Version: "v10.0.0", BinaryURL: "https://download.test/bin", ExpectedSHA256: "abc"}}
	}
	if resp, err := newAppUpgradeService(a).completeUpgradeAction(&feishu.CardAction{UserID: "user-1", ActionValue: map[string]any{"request_id": "missing"}}, "upgrade.confirm"); err != nil || resp.Toast == nil || resp.Toast.Type != "warning" {
		t.Fatalf("completeUpgradeAction(missing) = %#v, %v", resp, err)
	}
	if err := a.store.UpsertPending(&state.PendingRequest{ID: "upgrade-bad", Kind: "upgrade_release", OwnerUserID: "other", Status: "pending"}); err != nil {
		t.Fatalf("UpsertPending(upgrade-bad) error = %v", err)
	}
	if resp, err := newAppUpgradeService(a).completeUpgradeAction(&feishu.CardAction{UserID: "user-1", ActionValue: map[string]any{"request_id": "upgrade-bad"}}, "upgrade.confirm"); err != nil || resp.Toast == nil || resp.Toast.Type != "warning" {
		t.Fatalf("completeUpgradeAction(wrong owner) = %#v, %v", resp, err)
	}
	if err := a.store.UpsertPending(&state.PendingRequest{ID: "upgrade-json", Kind: "upgrade_release", OwnerUserID: "user-1", Status: "pending", PayloadJSON: "{"}); err != nil {
		t.Fatalf("UpsertPending(upgrade-json) error = %v", err)
	}
	if resp, err := newAppUpgradeService(a).completeUpgradeAction(&feishu.CardAction{UserID: "user-1", ActionValue: map[string]any{"request_id": "upgrade-json"}}, "upgrade.confirm"); err != nil || resp.Toast == nil || resp.Toast.Type != "warning" {
		t.Fatalf("completeUpgradeAction(bad json) = %#v, %v", resp, err)
	}

	if err := a.store.UpsertPending(&state.PendingRequest{
		ID:          "upgrade-start",
		Kind:        "upgrade_release",
		OwnerUserID: "user-1",
		SessionKey:  "sess-1",
		Status:      "pending",
		PayloadJSON: mustJSON(upgradePendingPayload{TargetVersion: "v10.0.0", BinaryPath: "/tmp/feidex", DownloadURL: "https://download.test/bin", ExpectedSHA256: "abc"}),
	}); err != nil {
		t.Fatalf("UpsertPending(upgrade-start) error = %v", err)
	}
	startDaemonUpgrade = func(daemon.UpgradeSpec) (string, error) { return "", errors.New("boom") }
	if resp, err := newAppUpgradeService(a).completeUpgradeAction(&feishu.CardAction{UserID: "user-1", ActionValue: map[string]any{"request_id": "upgrade-start"}}, "upgrade.confirm"); err != nil || resp.Toast == nil || resp.Toast.Type != "warning" {
		t.Fatalf("completeUpgradeAction(start fail) = %#v, %v", resp, err)
	}

	localArtifact := filepath.Join(a.cfg.Workspaces[0].Cwd, "dist", "feidex-linux-amd64")
	if err := os.MkdirAll(filepath.Dir(localArtifact), 0o755); err != nil {
		t.Fatalf("MkdirAll(localArtifact) error = %v", err)
	}
	if err := os.WriteFile(localArtifact, []byte("local-binary"), 0o755); err != nil {
		t.Fatalf("WriteFile(localArtifact) error = %v", err)
	}
	resp, err := newAppUpgradeService(a).completeUpgradeLocalPick(&feishu.CardAction{
		UserID:      "user-1",
		MessageID:   "msg-1",
		ActionValue: map[string]any{"session_key": "sess-1"},
	})
	if err != nil || resp == nil || resp.Card == nil {
		t.Fatalf("completeUpgradeLocalPick() = %#v, %v", resp, err)
	}
	var picker *state.PendingRequest
	for _, req := range a.store.AllPendingRequests() {
		if req.Kind == upgradeLocalBinaryPendingKind {
			picker = req
			break
		}
	}
	if picker == nil {
		t.Fatal("expected local upgrade picker pending request")
	}
	resp, err = newWorkspaceService(a).completePathPickerAction(&feishu.CardAction{
		UserID:      "user-1",
		MessageID:   "msg-1",
		ActionValue: map[string]any{"request_id": picker.ID},
		Option:      encodePathPickerOption(pathPickerEntry{Name: filepath.Base(localArtifact), Path: localArtifact, IsDir: false}),
	}, "path_picker.dropdown")
	if err != nil || resp == nil || resp.Card == nil {
		t.Fatalf("local picker dropdown = %#v, %v", resp, err)
	}
	resp, err = newWorkspaceService(a).completePathPickerAction(&feishu.CardAction{
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
		var payload upgradePendingPayload
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
