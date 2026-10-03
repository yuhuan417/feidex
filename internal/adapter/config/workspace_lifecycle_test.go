package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	scoped "feidex/internal/adapter/storage/json/scoped"
	workspaceapp "feidex/internal/application/workspace"
	fileconfig "feidex/internal/config"
	"feidex/internal/domain/conversation"
	"feidex/internal/domain/identity"
	"feidex/internal/domain/routing"
	"feidex/internal/state"
)

func lifecycleFixture(t *testing.T) (workspaceapp.Lifecycle, *state.Store, *workspaceSourceStub, string) {
	t.Helper()
	dir := t.TempDir()
	source := &workspaceSourceStub{cfg: fileconfig.Default(), path: filepath.Join(dir, "config.toml")}
	source.cfg.Workspaces = []fileconfig.Workspace{{ID: "default", Cwd: dir}, {ID: "alt", Cwd: dir}}
	if err := fileconfig.Save(source.path, source.cfg); err != nil {
		t.Fatal(err)
	}
	store, err := state.Open(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	scope := scoped.NewScoped(store, "bot-a", "codex", false)
	key := "feishu:frontend:bot-a:chat:private"
	if err := scope.SaveSession(&conversation.Session{Key: key, ChatID: "private", ChatType: "p2p", OwnerUserID: "user", WorkspaceID: "default", ActiveThreadID: "old-thread", ActiveThreadWorkspaceID: "default", RecentWorkspaceIDs: []string{"default"}}); err != nil {
		t.Fatal(err)
	}
	lifecycle := workspaceapp.Lifecycle{
		Frontend:      identity.FrontendID("bot-a"),
		Configuration: workspaceapp.ConfigurationService{Repository: NewWorkspaceRepository(source)},
		Selection:     workspaceapp.SelectionService{Frontend: "bot-a", DefaultWorkspace: "default", Repository: scoped.WorkspaceSelections{Store: scope}},
		Repository:    WorkspaceLifecycleRepository{Source: source, Scope: scope},
	}
	return lifecycle, store, source, key
}

func TestWorkspaceSwitchCommitsSelectionSessionAndProfileTogether(t *testing.T) {
	lifecycle, store, _, key := lifecycleFixture(t)
	old := store.GetSession(key)
	effects, err := lifecycle.Switch(workspaceapp.SwitchRequest{Session: old, WorkspaceID: "alt"})
	if err != nil {
		t.Fatal(err)
	}
	selected := store.GetSession(workspaceapp.SelectionKey("bot-a", "p2p", "private", "user"))
	session, profile := store.GetSession(key), store.GetBotProfile("bot-a")
	if session.WorkspaceID != "alt" || session.ActiveThreadID != "" || selected == nil || selected.WorkspaceID != "alt" || profile == nil || profile.WorkspaceID != "alt" {
		t.Fatalf("incomplete switch: %v, %v, %v", session, selected, profile)
	}
	if !reflect.DeepEqual(effects.ClearLiveThreads, []string{key}) || effects.Workspace == nil {
		t.Fatalf("effects = %+v", effects)
	}
	if old.ActiveThreadID != "old-thread" || old.WorkspaceID != "default" || old.RecentWorkspaceIDs[0] != "default" {
		t.Fatalf("input snapshot mutated: %+v", old)
	}
}

func TestWorkspaceLifecycleStateWriteFailurePublishesNothing(t *testing.T) {
	for _, operation := range []string{"switch", "delete", "unbind"} {
		t.Run(operation, func(t *testing.T) {
			lifecycle, store, source, key := lifecycleFixture(t)
			group := &routing.AgentBinding{ID: "binding", FrontendID: "bot-a", ChatType: "group", ChatID: "group", WorkspaceID: "default", Status: "active"}
			if err := store.UpsertAgentBinding(group); err != nil {
				t.Fatal(err)
			}
			before := store.WorkspaceState()
			// WriteFile fails deterministically, including when tests run as root.
			statePath := filepath.Join(filepath.Dir(source.path), "state.json")
			if err := os.Mkdir(statePath+".tmp", 0700); err != nil {
				t.Fatal(err)
			}
			var effects workspaceapp.LifecycleEffects
			var err error
			switch operation {
			case "switch":
				effects, err = lifecycle.Switch(workspaceapp.SwitchRequest{Session: store.GetSession(key), WorkspaceID: "alt"})
			case "delete":
				effects, err = lifecycle.Delete(key, "alt")
			case "unbind":
				effects, err = lifecycle.Switch(workspaceapp.SwitchRequest{Session: store.GetSession(key), Binding: group, Unbind: true})
			}
			if err == nil {
				t.Fatal("expected disk failure")
			}
			if !reflect.DeepEqual(effects, workspaceapp.LifecycleEffects{}) {
				t.Fatalf("failed operation published effects: %+v", effects)
			}
			if !reflect.DeepEqual(before, store.WorkspaceState()) {
				t.Fatal("failed write changed memory")
			}
			if fileconfig.FindWorkspace(source.cfg, "alt") == nil {
				t.Fatal("failed state write deleted config")
			}
			if err := os.Remove(statePath + ".tmp"); err != nil {
				t.Fatal(err)
			}
			reopened, err := state.Open(statePath)
			if err != nil {
				t.Fatal(err)
			}
			if reopened.GetSession(key).WorkspaceID != "default" {
				t.Fatal("failed write changed disk")
			}
		})
	}
}

func TestWorkspaceDeletionRollsBackReferencesWhenConfigSaveFails(t *testing.T) {
	lifecycle, store, source, key := lifecycleFixture(t)
	foreignKey := "feishu:frontend:bot-b:chat:foreign"
	if err := store.UpsertSession(&conversation.Session{Key: foreignKey, WorkspaceID: "alt", ActiveThreadID: "foreign-thread", ActiveThreadWorkspaceID: "alt"}); err != nil {
		t.Fatal(err)
	}
	before := store.WorkspaceState()
	statePath := filepath.Join(filepath.Dir(source.path), "state.json")
	source.path = t.TempDir()
	effects, err := lifecycle.Delete(key, "alt")
	if err == nil {
		t.Fatal("expected config persistence failure")
	}
	if !reflect.DeepEqual(effects, workspaceapp.LifecycleEffects{}) || !reflect.DeepEqual(before, store.WorkspaceState()) {
		t.Fatal("config failure changed state or published effects")
	}
	if fileconfig.FindWorkspace(source.cfg, "alt") == nil {
		t.Fatal("failed config save changed memory")
	}
	reopened, err := state.Open(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.GetSession(foreignKey).ActiveThreadID != "foreign-thread" || reopened.GetSession(foreignKey).WorkspaceID != "alt" {
		t.Fatal("state rollback did not survive reopen")
	}
}

func TestWorkspaceDeletionRejectsForeignBindingAndPendingWork(t *testing.T) {
	for _, kind := range []string{"binding", "turn", "queue", "staged", "compaction", "lineage"} {
		t.Run(kind, func(t *testing.T) {
			lifecycle, store, _, key := lifecycleFixture(t)
			foreign := &conversation.Session{Key: "feishu:frontend:bot-b:chat:foreign", WorkspaceID: "alt"}
			switch kind {
			case "binding":
				if err := store.UpsertAgentBinding(&routing.AgentBinding{ID: "foreign-binding", FrontendID: "bot-b", ChatType: "group", ChatID: "group", WorkspaceID: "alt"}); err != nil {
					t.Fatal(err)
				}
			case "turn":
				foreign.ActiveTurnID = "turn"
			case "queue":
				foreign.Queue = []string{"queued-input"}
			case "staged":
				foreign.StagedImages = []conversation.SessionStagedImage{{}}
			case "compaction":
				foreign.Status = conversation.SessionStatusCompacting.String()
			case "lineage":
				foreign.WorkspaceID = "default"
				foreign.BackendThreads = map[string]conversation.SessionBackendThread{"claude": {WorkspaceID: "alt", ThreadID: "stored"}}
				foreign.Queue = []string{"queued"}
			}
			if err := store.UpsertSession(foreign); err != nil {
				t.Fatal(err)
			}
			if _, err := lifecycle.Delete(key, "alt"); err == nil {
				t.Fatal("foreign reference did not block deletion")
			}
		})
	}
}

func TestWorkspaceDeletionCleansForeignIdleReferencesWithoutLosingOtherThread(t *testing.T) {
	lifecycle, store, source, key := lifecycleFixture(t)
	foreignKey := "feishu:frontend:bot-b:chat:foreign"
	foreign := &conversation.Session{Key: foreignKey, WorkspaceID: "default", ActiveThreadID: "keep", ActiveThreadWorkspaceID: "default", BackendThreads: map[string]conversation.SessionBackendThread{"claude": {WorkspaceID: "alt", ThreadID: "remove"}, "codex": {WorkspaceID: "default", ThreadID: "keep"}}}
	if err := store.UpsertSession(foreign); err != nil {
		t.Fatal(err)
	}
	effects, err := lifecycle.Delete(key, "alt")
	if err != nil {
		t.Fatal(err)
	}
	actual := store.GetSession(foreignKey)
	if fileconfig.FindWorkspace(source.cfg, "alt") != nil || actual.ActiveThreadID != "keep" || len(actual.BackendThreads) != 1 || actual.BackendThreads["codex"].ThreadID != "keep" {
		t.Fatalf("incorrect cleanup: %+v", actual)
	}
	if len(effects.ClearLiveThreads) != 1 {
		t.Fatalf("cleanup effects = %+v", effects)
	}
}

func TestWorkspaceCommitRejectsConcurrentTurnWithoutOverwritingIt(t *testing.T) {
	lifecycle, store, source, key := lifecycleFixture(t)
	repository := lifecycle.Repository.(WorkspaceLifecycleRepository)
	expected := repository.State()
	candidate := conversation.CloneSession(store.GetSession(key))
	conversation.SwitchSessionWorkspace(candidate, "alt")
	if _, err := store.UpdateSession(key, func(sess *conversation.Session) { sess.ActiveTurnID = "new-turn" }); err != nil {
		t.Fatal(err)
	}
	if err := repository.Commit(workspaceapp.LifecycleChange{Expected: expected, Sessions: []*conversation.Session{candidate}}); err == nil {
		t.Fatal("stale lifecycle commit accepted")
	}
	if sess := store.GetSession(key); sess.ActiveTurnID != "new-turn" || sess.WorkspaceID != "default" {
		t.Fatalf("concurrent turn overwritten: %+v", sess)
	}
	if fileconfig.FindWorkspace(source.cfg, "alt") == nil {
		t.Fatal("config changed")
	}
}
