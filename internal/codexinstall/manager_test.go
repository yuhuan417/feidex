package codexinstall

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestManagerProbeDetectsSupportedNPMInstall(t *testing.T) {
	tempDir := t.TempDir()
	binDir := filepath.Join(tempDir, "bin")
	rootDir := filepath.Join(tempDir, "lib", "node_modules")
	packageDir := filepath.Join(rootDir, filepath.FromSlash(packageName))
	commandRealPath := filepath.Join(packageDir, "bin", "codex.js")
	if err := os.MkdirAll(filepath.Dir(commandRealPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(command) error = %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(filepath.Join(packageDir, "package.json")), 0o755); err != nil {
		t.Fatalf("MkdirAll(package) error = %v", err)
	}
	if err := os.WriteFile(commandRealPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("WriteFile(command) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(packageDir, "package.json"), []byte(`{"name":"@openai/codex"}`), 0o644); err != nil {
		t.Fatalf("WriteFile(package.json) error = %v", err)
	}
	commandPath := filepath.Join(binDir, "codex")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(bin) error = %v", err)
	}
	if err := os.Symlink(commandRealPath, commandPath); err != nil {
		t.Fatalf("Symlink(codex) error = %v", err)
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
			return rootDir, "", nil
		case name == "npm" && len(args) == 5 && args[0] == "ls":
			return `{"dependencies":{"@openai/codex":{"version":"0.9.1"}}}`, "", nil
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
	if probe.CurrentVersion != "0.9.1" {
		t.Fatalf("Probe().CurrentVersion = %q", probe.CurrentVersion)
	}
	if probe.CommandPath != commandPath {
		t.Fatalf("Probe().CommandPath = %q, want %q", probe.CommandPath, commandPath)
	}
	if probe.NPMPath != npmPath {
		t.Fatalf("Probe().NPMPath = %q, want %q", probe.NPMPath, npmPath)
	}
}

func TestManagerProbeRejectsCustomCommand(t *testing.T) {
	probe, err := New("/tmp/codex").Probe(context.Background())
	if err != nil {
		t.Fatalf("Probe(custom) error = %v", err)
	}
	if probe.Supported {
		t.Fatal("Probe(custom) should be unsupported")
	}
	if probe.Reason == "" {
		t.Fatal("Probe(custom) should include reason")
	}
}

func TestManagerLatestVersionAndInstallVersion(t *testing.T) {
	prevRunner := commandRunner
	defer func() { commandRunner = prevRunner }()

	var installs [][]string
	commandRunner = func(_ context.Context, name string, args ...string) (string, string, error) {
		switch {
		case name == "npm" && len(args) == 4 && args[0] == "view":
			return `"1.2.3"`, "", nil
		case name == "npm" && len(args) == 3 && args[0] == "i":
			installs = append(installs, append([]string(nil), args...))
			return "ok", "", nil
		default:
			return "", "", errors.New("unexpected command")
		}
	}

	manager := New("codex")
	version, err := manager.LatestVersion(context.Background())
	if err != nil {
		t.Fatalf("LatestVersion() error = %v", err)
	}
	if version != "1.2.3" {
		t.Fatalf("LatestVersion() = %q", version)
	}
	if err := manager.InstallVersion(context.Background(), "1.2.3"); err != nil {
		t.Fatalf("InstallVersion() error = %v", err)
	}
	if len(installs) != 1 || installs[0][2] != "@openai/codex@1.2.3" {
		t.Fatalf("InstallVersion() installs = %#v", installs)
	}
}

func TestManagerProbeFallsBackToCommandPackageWhenNPMGlobalPrefixDiffers(t *testing.T) {
	tempDir := t.TempDir()
	binDir := filepath.Join(tempDir, "bin")
	commandPackageDir := filepath.Join(tempDir, "nvm", "lib", "node_modules", filepath.FromSlash(packageName))
	commandRealPath := filepath.Join(commandPackageDir, "bin", "codex.js")
	if err := os.MkdirAll(filepath.Dir(commandRealPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(command) error = %v", err)
	}
	if err := os.WriteFile(commandRealPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("WriteFile(command) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(commandPackageDir, "package.json"), []byte(`{"name":"@openai/codex","version":"0.120.0"}`), 0o644); err != nil {
		t.Fatalf("WriteFile(package.json) error = %v", err)
	}
	commandPath := filepath.Join(binDir, "codex")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(bin) error = %v", err)
	}
	if err := os.Symlink(commandRealPath, commandPath); err != nil {
		t.Fatalf("Symlink(codex) error = %v", err)
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

	probe, err := New("codex").Probe(context.Background())
	if err != nil {
		t.Fatalf("Probe() error = %v", err)
	}
	if probe.Supported {
		t.Fatalf("Probe().Supported = true, want false with mismatched prefix")
	}
	if probe.CurrentVersion != "0.120.0" {
		t.Fatalf("Probe().CurrentVersion = %q, want 0.120.0", probe.CurrentVersion)
	}
	if probe.PackagePath != filepath.Join(commandPackageDir, "package.json") {
		t.Fatalf("Probe().PackagePath = %q", probe.PackagePath)
	}
	if probe.Reason == "" || !strings.Contains(probe.Reason, "npm global prefix 不一致") {
		t.Fatalf("Probe().Reason = %q, want mismatch hint", probe.Reason)
	}
}
