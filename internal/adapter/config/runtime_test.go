package config

import (
	"path/filepath"
	"testing"

	fileconfig "feidex/internal/config"
)

func TestRuntimeRepositoryWritesFrontendPreferences(t *testing.T) {
	source := &backendSourceStub{cfg: fileconfig.Default(), path: filepath.Join(t.TempDir(), "config.toml"), idx: 0}
	source.cfg.Frontends = []fileconfig.FrontendConfig{{ID: "frontend-a"}}
	repo := NewRuntimeRepository(source)
	if err := repo.SetQuietMode("final"); err != nil {
		t.Fatal(err)
	}
	if err := repo.SetAutoRetry(true); err != nil {
		t.Fatal(err)
	}
	if err := repo.SetLogLevel("debug"); err != nil {
		t.Fatal(err)
	}
	if source.cfg.Frontends[0].Quiet.String() != "final" || !source.cfg.Frontends[0].AutoRetry {
		t.Fatalf("frontend config = %+v", source.cfg.Frontends[0])
	}
	if source.cfg.Log.Level != "debug" {
		t.Fatalf("log level = %q, want debug", source.cfg.Log.Level)
	}
}
