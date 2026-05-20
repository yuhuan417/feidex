package claudeinstall

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestManagerProbeDetectsSupportedSelfUpdateCommand(t *testing.T) {
	tempDir := t.TempDir()
	binDir := filepath.Join(tempDir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(bin) error = %v", err)
	}
	commandPath := filepath.Join(binDir, "claude")
	if err := os.WriteFile(commandPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("WriteFile(claude) error = %v", err)
	}

	originalPath := os.Getenv("PATH")
	if err := os.Setenv("PATH", binDir+string(os.PathListSeparator)+originalPath); err != nil {
		t.Fatalf("Setenv(PATH) error = %v", err)
	}
	defer os.Setenv("PATH", originalPath)

	prevRunner := commandRunner
	commandRunner = func(_ context.Context, name string, args ...string) (string, string, error) {
		switch {
		case name == "claude" && len(args) == 1 && args[0] == "--version":
			return "2.1.138 (Claude Code)", "", nil
		case name == "claude" && len(args) == 2 && args[0] == "help" && args[1] == "update":
			return "Check for updates and install if available", "", nil
		default:
			return "", "", errors.New("unexpected command")
		}
	}
	defer func() { commandRunner = prevRunner }()

	probe, err := New("claude").Probe(context.Background())
	if err != nil {
		t.Fatalf("Probe() error = %v", err)
	}
	if !probe.Supported {
		t.Fatalf("Probe().Supported = false, reason=%q", probe.Reason)
	}
	if probe.CurrentVersion != "2.1.138" {
		t.Fatalf("Probe().CurrentVersion = %q", probe.CurrentVersion)
	}
	if probe.UpdateCommand != "update" {
		t.Fatalf("Probe().UpdateCommand = %q, want update", probe.UpdateCommand)
	}
	if probe.CommandPath != commandPath {
		t.Fatalf("Probe().CommandPath = %q, want %q", probe.CommandPath, commandPath)
	}
}

func TestManagerProbeRejectsMissingCommand(t *testing.T) {
	probe, err := New(filepath.Join(t.TempDir(), "missing-claude")).Probe(context.Background())
	if err != nil {
		t.Fatalf("Probe(missing) error = %v", err)
	}
	if probe.Supported {
		t.Fatal("Probe(missing) should be unsupported")
	}
	if probe.Reason == "" {
		t.Fatal("Probe(missing) should include reason")
	}
}

func TestManagerLatestVersionAndInstallVersionUseSelfUpdate(t *testing.T) {
	prevRunner := commandRunner
	defer func() { commandRunner = prevRunner }()

	var updates [][]string
	commandRunner = func(_ context.Context, name string, args ...string) (string, string, error) {
		switch {
		case name == "claude" && len(args) == 2 && args[0] == "help" && args[1] == "update":
			return "Check for updates and install if available", "", nil
		case name == "claude" && len(args) == 1 && args[0] == "update":
			updates = append(updates, append([]string(nil), args...))
			return "updated", "", nil
		default:
			return "", "", errors.New("unexpected command")
		}
	}

	manager := New("claude")
	version, err := manager.LatestVersion(context.Background())
	if err != nil {
		t.Fatalf("LatestVersion() error = %v", err)
	}
	if version != selfUpdateTargetLatest {
		t.Fatalf("LatestVersion() = %q", version)
	}
	if err := manager.InstallVersion(context.Background(), selfUpdateTargetLatest); err != nil {
		t.Fatalf("InstallVersion() error = %v", err)
	}
	if len(updates) != 1 || updates[0][0] != "update" {
		t.Fatalf("InstallVersion() updates = %#v", updates)
	}
	if err := manager.InstallVersion(context.Background(), "1.2.3"); err == nil {
		t.Fatal("InstallVersion(specific version) should fail")
	}
}
