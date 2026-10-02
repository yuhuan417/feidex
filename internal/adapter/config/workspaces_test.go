package config

import (
	"path/filepath"
	"sync"
	"testing"

	fileconfig "feidex/internal/config"
)

type workspaceSourceStub struct {
	cfg  *fileconfig.Config
	mu   sync.RWMutex
	path string
}

func (s *workspaceSourceStub) Config() *fileconfig.Config { return s.cfg }
func (s *workspaceSourceStub) ConfigMu() *sync.RWMutex    { return &s.mu }
func (s *workspaceSourceStub) ConfigPath() string         { return s.path }

func TestWorkspaceRepositoryPersistsAndUpdatesConfig(t *testing.T) {
	source := &workspaceSourceStub{
		cfg:  fileconfig.Default(),
		path: filepath.Join(t.TempDir(), "config.toml"),
	}
	source.cfg.Workspaces = []fileconfig.Workspace{{ID: "default", Cwd: t.TempDir()}}
	repo := NewWorkspaceRepository(source)
	created, err := repo.Create(fileconfig.Workspace{ID: "new", Name: "New", Cwd: t.TempDir()})
	if err != nil || created == nil || created.ID != "new" {
		t.Fatalf("Create() = %#v, %v", created, err)
	}
	if fileconfig.FindWorkspace(source.cfg, "new") == nil {
		t.Fatal("Create() did not update source config")
	}
	updated, err := repo.Update("new", func(ws *fileconfig.Workspace) { ws.Name = "Renamed" })
	if err != nil || updated == nil || updated.Name != "Renamed" {
		t.Fatalf("Update() = %#v, %v", updated, err)
	}
	fallback, err := repo.Delete("new")
	if err != nil || fallback != "default" {
		t.Fatalf("Delete() = %q, %v", fallback, err)
	}
	if fileconfig.FindWorkspace(source.cfg, "new") != nil {
		t.Fatal("Delete() did not update source config")
	}
}
