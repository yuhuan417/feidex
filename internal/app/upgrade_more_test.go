package app

import (
	"errors"
	"os"
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
	newDaemonManager = func() (daemon.Manager, error) {
		return &fakeDaemonManagerForApp{status: &daemon.Status{Installed: true, Running: true, PID: os.Getpid()}}, nil
	}
	newReleaseClient = func() releaseClient {
		return &fakeReleaseClient{info: &release.ReleaseInfo{Version: "v9.9.9", BinaryURL: "https://download.test/bin", ExpectedSHA256: "abc"}}
	}
	card, err := a.renderUpgradeCard("sess-1", "user-1")
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
	card, err = a.renderUpgradeCardForVersion("sess-1", "user-1", "v1.0.0")
	if err != nil || card == nil {
		t.Fatalf("renderUpgradeCardForVersion() = %#v, %v", card, err)
	}

	newDaemonManager = func() (daemon.Manager, error) {
		return &fakeDaemonManagerForApp{status: &daemon.Status{Installed: false}}, nil
	}
	if _, err := a.renderUpgradeCard("sess-1", "user-1"); err == nil {
		t.Fatal("expected renderUpgradeCard() to reject uninstalled daemon")
	}

	newDaemonManager = func() (daemon.Manager, error) {
		return &fakeDaemonManagerForApp{status: &daemon.Status{Installed: true, Running: true, PID: os.Getpid()}}, nil
	}
	newReleaseClient = func() releaseClient {
		return &fakeReleaseClient{info: &release.ReleaseInfo{Version: "v10.0.0", BinaryURL: "https://download.test/bin", ExpectedSHA256: "abc"}}
	}
	if resp, err := a.completeUpgradeAction(&feishu.CardAction{UserID: "user-1", ActionValue: map[string]any{"request_id": "missing"}}, "upgrade.confirm"); err != nil || resp.Toast == nil || resp.Toast.Type != "warning" {
		t.Fatalf("completeUpgradeAction(missing) = %#v, %v", resp, err)
	}
	if err := a.store.UpsertPending(&state.PendingRequest{ID: "upgrade-bad", Kind: "upgrade_release", OwnerUserID: "other", Status: "pending"}); err != nil {
		t.Fatalf("UpsertPending(upgrade-bad) error = %v", err)
	}
	if resp, err := a.completeUpgradeAction(&feishu.CardAction{UserID: "user-1", ActionValue: map[string]any{"request_id": "upgrade-bad"}}, "upgrade.confirm"); err != nil || resp.Toast == nil || resp.Toast.Type != "warning" {
		t.Fatalf("completeUpgradeAction(wrong owner) = %#v, %v", resp, err)
	}
	if err := a.store.UpsertPending(&state.PendingRequest{ID: "upgrade-json", Kind: "upgrade_release", OwnerUserID: "user-1", Status: "pending", PayloadJSON: "{"}); err != nil {
		t.Fatalf("UpsertPending(upgrade-json) error = %v", err)
	}
	if resp, err := a.completeUpgradeAction(&feishu.CardAction{UserID: "user-1", ActionValue: map[string]any{"request_id": "upgrade-json"}}, "upgrade.confirm"); err != nil || resp.Toast == nil || resp.Toast.Type != "warning" {
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
	if resp, err := a.completeUpgradeAction(&feishu.CardAction{UserID: "user-1", ActionValue: map[string]any{"request_id": "upgrade-start"}}, "upgrade.confirm"); err != nil || resp.Toast == nil || resp.Toast.Type != "warning" {
		t.Fatalf("completeUpgradeAction(start fail) = %#v, %v", resp, err)
	}
}
