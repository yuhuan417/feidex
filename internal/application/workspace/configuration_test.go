package workspace

import (
	"testing"

	domain "feidex/internal/domain/workspace"
)

type configurationRepositoryStub struct {
	workspaces map[string]domain.Workspace
}

func (r *configurationRepositoryStub) List() []domain.Workspace {
	result := make([]domain.Workspace, 0, len(r.workspaces))
	for _, value := range r.workspaces {
		result = append(result, value)
	}
	return result
}

func (r *configurationRepositoryStub) Get(id string) (*domain.Workspace, error) {
	value, ok := r.workspaces[id]
	if !ok {
		return nil, nil
	}
	copy := value
	return &copy, nil
}

func (r *configurationRepositoryStub) Create(value domain.Workspace) (*domain.Workspace, error) {
	if _, exists := r.workspaces[value.ID]; exists {
		return nil, errWorkspaceExists
	}
	r.workspaces[value.ID] = value
	copy := value
	return &copy, nil
}

func (r *configurationRepositoryStub) Update(id string, mutate func(*domain.Workspace)) (*domain.Workspace, error) {
	value, ok := r.workspaces[id]
	if !ok {
		return nil, errWorkspaceMissing
	}
	mutate(&value)
	r.workspaces[id] = value
	copy := value
	return &copy, nil
}

func (r *configurationRepositoryStub) Delete(id string) (string, error) {
	if _, ok := r.workspaces[id]; !ok {
		return "", errWorkspaceMissing
	}
	delete(r.workspaces, id)
	for next := range r.workspaces {
		return next, nil
	}
	return "", errWorkspaceMissing
}

type workspaceTestError string

func (e workspaceTestError) Error() string { return string(e) }

var (
	errWorkspaceExists  = workspaceTestError("exists")
	errWorkspaceMissing = workspaceTestError("missing")
)

func TestConfigurationServiceDelegatesWorkspaceMutations(t *testing.T) {
	repo := &configurationRepositoryStub{workspaces: map[string]domain.Workspace{"default": {ID: "default", Name: "Default"}}}
	service := ConfigurationService{Repository: repo}
	created, err := service.Create(domain.Workspace{ID: "new", Name: "New"})
	if err != nil || created == nil || created.ID != "new" {
		t.Fatalf("Create() = %#v, %v", created, err)
	}
	updated, err := service.Update("new", func(value *domain.Workspace) { value.Name = "Renamed" })
	if err != nil || updated == nil || updated.Name != "Renamed" {
		t.Fatalf("Update() = %#v, %v", updated, err)
	}
	fallback, err := service.Delete("new")
	if err != nil || fallback != "default" {
		t.Fatalf("Delete() fallback = %q, %v", fallback, err)
	}
}

func TestConfigurationServiceValidatesInputs(t *testing.T) {
	service := ConfigurationService{Repository: &configurationRepositoryStub{workspaces: map[string]domain.Workspace{}}}
	if _, err := service.Create(domain.Workspace{}); err == nil {
		t.Fatal("Create() accepted empty id")
	}
	if _, err := service.Update("", func(*domain.Workspace) {}); err == nil {
		t.Fatal("Update() accepted empty id")
	}
	if _, err := service.Update("missing", nil); err == nil {
		t.Fatal("Update() accepted nil mutation")
	}
	if _, err := service.Delete(""); err == nil {
		t.Fatal("Delete() accepted empty id")
	}
}
