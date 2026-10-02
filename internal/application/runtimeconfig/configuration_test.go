package runtimeconfig

import "testing"

type repositoryStub struct {
	quiet string
	retry bool
}

func (r *repositoryStub) SetQuietMode(mode string) error  { r.quiet = mode; return nil }
func (r *repositoryStub) SetAutoRetry(enabled bool) error { r.retry = enabled; return nil }
func (r *repositoryStub) SetLogLevel(string) error        { return nil }

func TestServicePersistsRuntimePreferences(t *testing.T) {
	repo := &repositoryStub{}
	service := Service{Repository: repo}
	if err := service.SetQuietMode("final"); err != nil {
		t.Fatal(err)
	}
	if err := service.SetAutoRetry(true); err != nil {
		t.Fatal(err)
	}
	if repo.quiet != "final" || !repo.retry {
		t.Fatalf("repository = %+v", repo)
	}
}
