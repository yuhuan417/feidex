package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveFillsDefaultsAndCleansPaths(t *testing.T) {
	cfg := &Config{ConfigPath: filepath.Join("..", "config.toml")}

	if err := Resolve(cfg); err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	if cfg.BinaryPath == "" || !filepath.IsAbs(cfg.ConfigPath) || cfg.WorkDir == "" || cfg.EnvPATH == "" || cfg.HomeDir == "" {
		t.Fatalf("Resolve() did not fill defaults: %+v", cfg)
	}
	if !filepath.IsAbs(cfg.BinaryPath) {
		t.Fatalf("BinaryPath = %q, want absolute path", cfg.BinaryPath)
	}
}

func TestResolvePreservesExplicitValuesAndRejectsNil(t *testing.T) {
	if err := Resolve(nil); err == nil {
		t.Fatal("expected Resolve(nil) to fail")
	}

	cfg := &Config{
		BinaryPath: "./bin/../feidex",
		ConfigPath: "./config.toml",
		WorkDir:    "./work/..",
		EnvPATH:    "CUSTOM_PATH",
		HomeDir:    "./home/..",
	}
	if err := Resolve(cfg); err != nil {
		t.Fatalf("Resolve(explicit) error = %v", err)
	}

	if !strings.HasSuffix(cfg.BinaryPath, "feidex") {
		t.Fatalf("BinaryPath = %q, want cleaned executable path", cfg.BinaryPath)
	}
	if cfg.EnvPATH != "CUSTOM_PATH" {
		t.Fatalf("EnvPATH = %q, want explicit value preserved", cfg.EnvPATH)
	}
	if cfg.HomeDir != filepath.Clean("./home/..") && cfg.HomeDir != "." {
		t.Fatalf("HomeDir = %q, want cleaned explicit home dir", cfg.HomeDir)
	}
}

func TestDefaultServiceNameConstant(t *testing.T) {
	if DefaultServiceName != "feidex" {
		t.Fatalf("DefaultServiceName = %q, want feidex", DefaultServiceName)
	}
	if _, err := os.Executable(); err != nil {
		t.Fatalf("os.Executable() should work in tests: %v", err)
	}
}
