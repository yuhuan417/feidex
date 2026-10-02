package backendconfig

import "testing"

type repositoryStub struct{ value string }

func (r *repositoryStub) SetBackend(value string) error {
	r.value = value
	return nil
}

func TestServiceNormalizesAndPersistsBackend(t *testing.T) {
	repo := &repositoryStub{}
	if err := (Service{Repository: repo}).SetBackend("  CODEX "); err != nil {
		t.Fatalf("SetBackend() error = %v", err)
	}
	if repo.value != "codex" {
		t.Fatalf("backend = %q, want codex", repo.value)
	}
}

func TestServiceRejectsUnknownBackend(t *testing.T) {
	if err := (Service{Repository: &repositoryStub{}}).SetBackend("unknown"); err == nil {
		t.Fatal("SetBackend() accepted unknown backend")
	}
}
