package config

import (
	"path/filepath"
	"sync"
	"testing"

	fileconfig "feidex/internal/config"
)

type backendSourceStub struct {
	cfg  *fileconfig.Config
	mu   sync.RWMutex
	path string
	idx  int
}

func (s *backendSourceStub) Config() *fileconfig.Config { return s.cfg }
func (s *backendSourceStub) ConfigMu() *sync.RWMutex    { return &s.mu }
func (s *backendSourceStub) ConfigPath() string         { return s.path }
func (s *backendSourceStub) FrontendConfigIndex() int   { return s.idx }

func TestBackendRepositoryWritesFrontendConfig(t *testing.T) {
	source := &backendSourceStub{cfg: fileconfig.Default(), path: filepath.Join(t.TempDir(), "config.toml"), idx: 0}
	source.cfg.Frontends = []fileconfig.FrontendConfig{{ID: "frontend-a"}}
	if err := NewBackendRepository(source).SetBackend("claude"); err != nil {
		t.Fatalf("SetBackend() error = %v", err)
	}
	if source.cfg.Frontends[0].Backend != "claude" || source.cfg.Feishu.Backend != "" {
		t.Fatalf("config = %+v, want frontend backend only", source.cfg)
	}
}
