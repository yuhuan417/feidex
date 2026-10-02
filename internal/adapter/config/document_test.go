package config

import (
	"errors"
	"path/filepath"
	"testing"

	fileconfig "feidex/internal/config"
)

func testDocumentConfig() *fileconfig.Config {
	cfg := fileconfig.Default()
	cfg.Workspaces = []fileconfig.Workspace{{ID: "default", Cwd: "."}}
	return cfg
}

func TestDocumentRepositoryPublishesSuccessfulMutation(t *testing.T) {
	source := &backendSourceStub{cfg: testDocumentConfig(), path: filepath.Join(t.TempDir(), "config.toml")}
	repo := NewDocumentRepository(source)
	if err := repo.UpdateConfig(func(cfg *fileconfig.Config) error {
		cfg.Codex.Model = "updated"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if source.cfg.Codex.Model != "updated" {
		t.Fatalf("model = %q, want updated", source.cfg.Codex.Model)
	}
}

func TestDocumentRepositoryDoesNotPublishFailedMutation(t *testing.T) {
	source := &backendSourceStub{cfg: testDocumentConfig(), path: filepath.Join(t.TempDir(), "config.toml")}
	wantErr := errors.New("reject")
	if err := NewDocumentRepository(source).UpdateConfig(func(cfg *fileconfig.Config) error {
		cfg.Codex.Model = "bad"
		return wantErr
	}); !errors.Is(err, wantErr) {
		t.Fatalf("UpdateConfig() error = %v, want %v", err, wantErr)
	}
	if source.cfg.Codex.Model == "bad" {
		t.Fatal("failed mutation was published")
	}
}
