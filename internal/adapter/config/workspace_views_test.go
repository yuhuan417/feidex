package config

import (
	"path/filepath"
	"sync"
	"testing"

	scoped "feidex/internal/adapter/storage/json/scoped"
	workspaceapp "feidex/internal/application/workspace"
	fileconfig "feidex/internal/config"
	"feidex/internal/domain/conversation"
	"feidex/internal/domain/routing"
	"feidex/internal/state"
)

func TestWorkspaceViewsRejectOtherFrontendAndKeepDetachedRevision(t *testing.T) {
	store, err := state.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	mu := &sync.RWMutex{}
	scope := scoped.NewScoped(store, "bot-a", "codex")
	scope.RevisionMutex = mu
	if err := scope.SaveSession(&conversation.Session{Key: "feishu:frontend:bot-a:chat:group", ChatType: "group", ChatID: "group"}); err != nil {
		t.Fatal(err)
	}
	if err := scope.SaveAgentBinding(&routing.AgentBinding{ID: "binding", FrontendID: "bot-a", ChatType: "group", ChatID: "group", WorkspaceID: "repo"}); err != nil {
		t.Fatal(err)
	}
	cfg := &fileconfig.Config{Workspaces: []fileconfig.Workspace{{ID: "repo", Cwd: "/repo"}}}
	repository := WorkspaceViewRepository{Config: cfg, Mutex: mu, Scopes: scope, Backend: func() string { return "codex" }}
	service := workspaceapp.ViewService{Frontend: "bot-a", Repository: repository}
	view := service.Snapshot("feishu:frontend:bot-a:chat:group")
	if !view.Group || view.CurrentID != "repo" {
		t.Fatalf("frontend view: %+v", view)
	}
	view.Current.Cwd = "changed"
	if cfg.Workspaces[0].Cwd != "/repo" {
		t.Fatal("view changed config")
	}
	foreign := service.Snapshot("feishu:frontend:bot-b:chat:group")
	if foreign.Current != nil || len(foreign.Workspaces) != 0 || foreign.PendingPreview != "" {
		t.Fatalf("foreign scope leaked: %+v", foreign)
	}
}
