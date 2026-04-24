package claudeinstall

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestManagerProbeFallsBackToCommandPackageWhenNPMGlobalPrefixDiffers(t *testing.T) {
	tempDir := t.TempDir()
	binDir := filepath.Join(tempDir, "bin")
	commandPackageDir := filepath.Join(tempDir, "nvm", "lib", "node_modules", filepath.FromSlash(packageName))
	commandRealPath := filepath.Join(commandPackageDir, "bin", "claude.js")
	if err := os.MkdirAll(filepath.Dir(commandRealPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(command) error = %v", err)
	}
	if err := os.WriteFile(commandRealPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("WriteFile(command) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(commandPackageDir, "package.json"), []byte(`{"name":"@anthropic-ai/claude-code","version":"1.2.3"}`), 0o644); err != nil {
		t.Fatalf("WriteFile(package.json) error = %v", err)
	}
	commandPath := filepath.Join(binDir, "claude")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(bin) error = %v", err)
	}
	if err := os.Symlink(commandRealPath, commandPath); err != nil {
		t.Fatalf("Symlink(claude) error = %v", err)
	}
	npmPath := filepath.Join(binDir, "npm")
	if err := os.WriteFile(npmPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("WriteFile(npm) error = %v", err)
	}

	originalPath := os.Getenv("PATH")
	if err := os.Setenv("PATH", binDir+string(os.PathListSeparator)+originalPath); err != nil {
		t.Fatalf("Setenv(PATH) error = %v", err)
	}
	defer os.Setenv("PATH", originalPath)

	prevRunner := commandRunner
	commandRunner = func(_ context.Context, name string, args ...string) (string, string, error) {
		switch {
		case name == "npm" && len(args) == 2 && args[0] == "root" && args[1] == "-g":
			return filepath.Join(tempDir, "other", "lib", "node_modules"), "", nil
		default:
			return "", "", errors.New("unexpected command")
		}
	}
	defer func() { commandRunner = prevRunner }()

	probe, err := New("claude").Probe(context.Background())
	if err != nil {
		t.Fatalf("Probe() error = %v", err)
	}
	if probe.Supported {
		t.Fatalf("Probe().Supported = true, want false with mismatched prefix")
	}
	if probe.CurrentVersion != "1.2.3" {
		t.Fatalf("Probe().CurrentVersion = %q, want 1.2.3", probe.CurrentVersion)
	}
	if probe.PackagePath != filepath.Join(commandPackageDir, "package.json") {
		t.Fatalf("Probe().PackagePath = %q", probe.PackagePath)
	}
	if probe.Reason == "" || !strings.Contains(probe.Reason, "npm global prefix 不一致") {
		t.Fatalf("Probe().Reason = %q, want mismatch hint", probe.Reason)
	}
}
