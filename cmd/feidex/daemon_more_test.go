package main

import (
	"context"
	"errors"
	"testing"

	"feidex/internal/config"
	"feidex/internal/daemon"
)

func TestDaemonLifecycleErrorBranches(t *testing.T) {
	resetMainStubs()
	defer resetMainStubs()

	loadConfig = func(string) (*config.Config, error) { return minimalConfig(), nil }

	newDaemonManager = func(string) (daemon.Manager, error) { return nil, errors.New("boom") }
	if got := daemonStart(nil); got != 1 {
		t.Fatalf("daemonStart(manager error) = %d, want 1", got)
	}
	if got := daemonStop(nil); got != 1 {
		t.Fatalf("daemonStop(manager error) = %d, want 1", got)
	}
	if got := daemonRestart(nil); got != 1 {
		t.Fatalf("daemonRestart(manager error) = %d, want 1", got)
	}
	if got := daemonStatus(nil); got != 1 {
		t.Fatalf("daemonStatus(manager error) = %d, want 1", got)
	}

	newDaemonManager = func(string) (daemon.Manager, error) {
		return &fakeManager{statusResp: &daemon.Status{Installed: true}, startErr: errors.New("start failed")}, nil
	}
	if got := daemonStart(nil); got != 1 {
		t.Fatalf("daemonStart(start error) = %d, want 1", got)
	}
	newDaemonManager = func(string) (daemon.Manager, error) {
		return &fakeManager{statusResp: &daemon.Status{Installed: true}, stopErr: errors.New("stop failed")}, nil
	}
	if got := daemonStop(nil); got != 1 {
		t.Fatalf("daemonStop(stop error) = %d, want 1", got)
	}
	newDaemonManager = func(string) (daemon.Manager, error) {
		return &fakeManager{statusResp: &daemon.Status{Installed: true}, restartErr: errors.New("restart failed")}, nil
	}
	if got := daemonRestart(nil); got != 1 {
		t.Fatalf("daemonRestart(restart error) = %d, want 1", got)
	}
	newDaemonManager = func(string) (daemon.Manager, error) { return &fakeManager{statusErr: errors.New("status failed")}, nil }
	if got := daemonStatus(nil); got != 1 {
		t.Fatalf("daemonStatus(status error) = %d, want 1", got)
	}
}

func TestDaemonUpgradeRunnerError(t *testing.T) {
	resetMainStubs()
	defer resetMainStubs()

	runDaemonUpgrade = func(context.Context, daemon.UpgradeSpec) error { return errors.New("upgrade failed") }
	if got := daemonUpgradeRunner([]string{"--binary-path", "/tmp/feidex", "--version", "v0.2.0", "--download-url", "https://example.test/feidex", "--expected-sha256", "abc"}); got != 1 {
		t.Fatalf("daemonUpgradeRunner(error) = %d, want 1", got)
	}
}
