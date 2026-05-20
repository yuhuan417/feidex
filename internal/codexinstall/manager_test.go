package codexinstall

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
	commandPath := filepath.Join(binDir, "codex")
	if err := os.WriteFile(commandPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("WriteFile(codex) error = %v", err)
	}

	originalPath := os.Getenv("PATH")
	if err := os.Setenv("PATH", binDir+string(os.PathListSeparator)+originalPath); err != nil {
		t.Fatalf("Setenv(PATH) error = %v", err)
	}
	defer os.Setenv("PATH", originalPath)

	prevRunner := commandRunner
	commandRunner = func(_ context.Context, name string, args ...string) (string, string, error) {
		switch {
		case name == "codex" && len(args) == 1 && args[0] == "--version":
			return "WARNING: ignored\ncodex-cli 0.132.0", "", nil
		case name == "codex" && len(args) == 2 && args[0] == "update" && args[1] == "--help":
			return "Update Codex to the latest version", "", nil
		default:
			return "", "", errors.New("unexpected command")
		}
	}
	defer func() { commandRunner = prevRunner }()

	probe, err := New("codex").Probe(context.Background())
	if err != nil {
		t.Fatalf("Probe() error = %v", err)
	}
	if !probe.Supported {
		t.Fatalf("Probe().Supported = false, reason=%q", probe.Reason)
	}
	if probe.CurrentVersion != "0.132.0" {
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
	probe, err := New(filepath.Join(t.TempDir(), "missing-codex")).Probe(context.Background())
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
	prevLatestLookup := latestVersionLookup
	defer func() {
		commandRunner = prevRunner
		latestVersionLookup = prevLatestLookup
	}()

	var updates [][]string
	commandRunner = func(_ context.Context, name string, args ...string) (string, string, error) {
		switch {
		case name == "codex" && len(args) == 1 && args[0] == "--version":
			return "codex-cli 0.132.0", "", nil
		case name == "codex" && len(args) == 2 && args[0] == "update" && args[1] == "--help":
			return "Update Codex to the latest version", "", nil
		case name == "codex" && len(args) == 1 && args[0] == "update":
			updates = append(updates, append([]string(nil), args...))
			return "updated", "", nil
		default:
			return "", "", errors.New("unexpected command")
		}
	}
	latestVersionLookup = func(_ context.Context, packageName, userAgent string) (string, error) {
		if packageName != "@openai/codex" {
			t.Fatalf("latest package = %q, want @openai/codex", packageName)
		}
		if userAgent != "codex_cli_rs/0.132.0" {
			t.Fatalf("latest User-Agent = %q, want codex_cli_rs/0.132.0", userAgent)
		}
		return "0.133.0", nil
	}

	manager := New("codex")
	version, err := manager.LatestVersion(context.Background())
	if err != nil {
		t.Fatalf("LatestVersion() error = %v", err)
	}
	if version != "0.133.0" {
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
