package feishuapp

import (
	"context"
	"errors"
	configadapter "feidex/internal/adapter/config"
	modelconfigapp "feidex/internal/application/modelconfig"
	planapp "feidex/internal/application/plan"
	"feidex/internal/config"
	"feidex/internal/domain/conversation"
	frontendruntime "feidex/internal/runtime"
	"sync"
	"testing"
)

func TestPlanPortsReadUpdatedSettingsAndWorkspaces(t *testing.T) {
	cfg := config.Default()
	cfg.Codex.ExperimentalAPI = true
	cfg.Codex.Model = "initial-model"
	mu := &sync.RWMutex{}
	snapshots := modelconfigapp.SnapshotService{Repository: configadapter.ModelSourceRepository{Config: cfg, Mutex: mu}}
	source, _, workspaces := PlanPorts(cfg, mu, snapshots, frontendruntime.NewFrontendOwner())
	if got := source.Values(nil); !got.Experimental || got.Model != "initial-model" {
		t.Fatalf("initial settings = %+v", got)
	}

	mu.Lock()
	cfg.Codex.ExperimentalAPI = false
	cfg.Codex.Model, cfg.Codex.ReasoningEffort = "updated-model", "high"
	cfg.Codex.PlanModel, cfg.Codex.PlanReasoningEffort = "updated-plan-model", "medium"
	cfg.Workspaces = []config.Workspace{{ID: "updated-workspace", Cwd: "/updated"}}
	mu.Unlock()
	want := planapp.SettingsValues{Model: "updated-model", Effort: "high", PlanModel: "updated-plan-model", PlanEffort: "medium"}
	if got := source.Values(nil); got != want {
		t.Fatalf("updated settings = %+v, want %+v", got, want)
	}
	sess := &conversation.Session{ModelOverride: "session-model", PlanModelOverride: "session-plan-model", PlanReasoningEffortOverride: "low"}
	want.Model, want.PlanModel, want.PlanEffort = "session-model", "session-plan-model", "low"
	if got := source.Values(sess); got != want {
		t.Fatalf("session settings = %+v, want %+v", got, want)
	}
	if workspaces.DefaultID() != "updated-workspace" || workspaces.Get("updated-workspace").Cwd != "/updated" {
		t.Fatal("plan workspace source retained the initial configuration")
	}
}

func TestPlanPortsCatalogUsesCurrentFrontendClient(t *testing.T) {
	owner := frontendruntime.NewFrontendOwner()
	_, catalog, _ := PlanPorts(nil, nil, modelconfigapp.SnapshotService{}, owner)
	if _, err := catalog(); err == nil {
		t.Fatal("catalog succeeded without a Codex client")
	}
	firstErr, replacementErr := errors.New("first client"), errors.New("replacement client")
	owner.SetCodexClient(&fakeCodexClient{callErr: firstErr})
	first, err := catalog()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := first.ListModels(context.Background(), 1); !errors.Is(err, firstErr) {
		t.Fatalf("initial catalog error = %v", err)
	}
	owner.SetCodexClient(&fakeCodexClient{callErr: replacementErr})
	replacement, err := catalog()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := replacement.ListModels(context.Background(), 1); !errors.Is(err, replacementErr) {
		t.Fatalf("catalog after client replacement error = %v", err)
	}
	owner.SetCodexClient(nil)
	if _, err := catalog(); err == nil {
		t.Fatal("catalog retained a detached client")
	}
}
