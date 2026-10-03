package config

import (
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	scoped "feidex/internal/adapter/storage/json/scoped"
	modelapp "feidex/internal/application/modelconfig"
	workspaceapp "feidex/internal/application/workspace"
	fileconfig "feidex/internal/config"
	"feidex/internal/domain/routing"
	"feidex/internal/state"
)

func TestWorkspaceSettingsCommitProfileAndConfigurationTogether(t *testing.T) {
	_, store, source, key := lifecycleFixture(t)
	scope := scoped.NewScoped(store, "bot-a", "codex")
	service := workspaceapp.SettingsService{Frontend: "bot-a", Repository: WorkspaceSettingsRepository{Source: source, Scope: scope}}
	if err := service.Set(key, "default", routing.Sandbox, "read-only"); err != nil {
		t.Fatal(err)
	}
	profile := store.GetBotProfile("bot-a")
	loaded, err := fileconfig.Load(source.path)
	if err != nil {
		t.Fatal(err)
	}
	if profile == nil || profile.SandboxMode != "read-only" || fileconfig.FindWorkspace(source.cfg, "default").SandboxMode != "read-only" || fileconfig.FindWorkspace(loaded, "default").SandboxMode != "read-only" {
		t.Fatal("incomplete workspace/profile commit")
	}
	if err := service.Set("feishu:frontend:bot-b:chat:private", "default", routing.Sandbox, "danger-full-access"); err == nil {
		t.Fatal("foreign frontend changed settings")
	}
}

func TestSettingsPersistenceFailureRollsBackBothDocuments(t *testing.T) {
	for _, owner := range []string{"workspace", "model"} {
		for _, failure := range []string{"state", "config"} {
			t.Run(owner+"/"+failure, func(t *testing.T) {
				_, store, source, key := lifecycleFixture(t)
				scope := scoped.NewScoped(store, "bot-a", "codex")
				statePath, configPath := filepath.Join(filepath.Dir(source.path), "state.json"), source.path
				beforeDisk, err := os.ReadFile(configPath)
				if err != nil {
					t.Fatal(err)
				}
				beforeState, beforeConfig := store.WorkspaceState(), fileconfig.Clone(source.cfg)
				if failure == "state" {
					if err := os.Mkdir(statePath+".tmp", 0700); err != nil {
						t.Fatal(err)
					}
				} else {
					source.path = t.TempDir()
				}
				if owner == "workspace" {
					err = (workspaceapp.SettingsService{Frontend: "bot-a", Repository: WorkspaceSettingsRepository{Source: source, Scope: scope}}).Set(key, "default", routing.Sandbox, "read-only")
				} else {
					err = (modelapp.DefaultsService{Frontend: "bot-a", Repository: ModelDefaultsRepository{Source: source, Scope: scope}}).Set(modelapp.DefaultsCommand{Backend: "codex", Values: map[routing.Setting]string{routing.Model: "desired"}})
				}
				if err == nil {
					t.Fatal("expected persistence failure")
				}
				if !reflect.DeepEqual(beforeState, store.WorkspaceState()) || !reflect.DeepEqual(beforeConfig, source.cfg) {
					t.Fatal("failed save published configuration/state")
				}
				if failure == "state" {
					if err := os.Remove(statePath + ".tmp"); err != nil {
						t.Fatal(err)
					}
				}
				reopened, err := state.Open(statePath)
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(beforeState, reopened.WorkspaceState()) {
					t.Fatal("state disk rollback lost")
				}
				afterDisk, err := os.ReadFile(configPath)
				if err != nil {
					t.Fatal(err)
				}
				if string(afterDisk) != string(beforeDisk) {
					t.Fatal("failed save changed configuration disk")
				}
			})
		}
	}
}

func TestExplicitDefaultEqualToGlobalStillUpdatesProfile(t *testing.T) {
	_, store, source, _ := lifecycleFixture(t)
	source.cfg.Codex.Model = "global"
	if err := store.UpsertBotProfile(&routing.BotProfile{ID: "profile-a", FrontendID: "bot-a", Model: "old-profile"}); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertBotProfile(&routing.BotProfile{ID: "profile-b", FrontendID: "bot-b", Model: "foreign"}); err != nil {
		t.Fatal(err)
	}
	scope := scoped.NewScoped(store, "bot-a", "codex")
	defaults := modelapp.DefaultsService{Frontend: "bot-a", Repository: ModelDefaultsRepository{Source: source, Scope: scope}}
	if err := defaults.Set(modelapp.DefaultsCommand{Backend: "codex", Values: map[routing.Setting]string{routing.Model: "global"}}); err != nil {
		t.Fatal(err)
	}
	if store.GetBotProfile("bot-a").Model != "global" || store.GetBotProfile("bot-b").Model != "foreign" {
		t.Fatal("explicit user intent or frontend isolation lost")
	}
}

func TestModelDefaultsAndProfileReadersShareOneRevision(t *testing.T) {
	_, store, source, _ := lifecycleFixture(t)
	source.path = ""
	scope := scoped.NewScoped(store, "bot-a", "codex")
	defaults := modelapp.DefaultsService{Frontend: "bot-a", Repository: ModelDefaultsRepository{Source: source, Scope: scope}}
	reader := ModelSourceRepository{Config: source.cfg, Mutex: source.ConfigMu(), Scopes: scope}
	set := func(value string) error {
		return defaults.Set(modelapp.DefaultsCommand{Backend: "codex", Values: map[routing.Setting]string{routing.Model: value}})
	}
	if err := set("a"); err != nil {
		t.Fatal(err)
	}
	var writers sync.WaitGroup
	writers.Add(1)
	go func() {
		defer writers.Done()
		for i := 0; i < 30; i++ {
			value := "a"
			if i%2 == 0 {
				value = "b"
			}
			if err := set(value); err != nil {
				t.Error(err)
				return
			}
		}
	}()
	for i := 0; i < 100; i++ {
		revision := reader.ModelSourceRevision(nil)
		if revision.Profile.Model != revision.Global.Model {
			t.Errorf("mixed config/profile revision: %+v", revision)
			break
		}
	}
	writers.Wait()
}
