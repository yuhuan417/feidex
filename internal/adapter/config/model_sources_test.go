package config

import (
	"path/filepath"
	"sync"
	"testing"

	scoped "feidex/internal/adapter/storage/json/scoped"
	"feidex/internal/application/modelconfig"
	"feidex/internal/application/routing"
	fileconfig "feidex/internal/config"
	"feidex/internal/domain/conversation"
	"feidex/internal/domain/identity"
	domain "feidex/internal/domain/modelconfig"
	domainrouting "feidex/internal/domain/routing"
	"feidex/internal/state"
)

func TestModelSourceSnapshotPreservesFrontendScopeAndPlanBoundary(t *testing.T) {
	store, err := state.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	frontendA := scoped.NewScoped(store, "a", "codex")
	frontendB := scoped.NewScoped(store, "b", "codex")
	mu := &sync.RWMutex{}
	frontendA.RevisionMutex, frontendB.RevisionMutex = mu, mu
	for _, frontend := range []*scoped.Store{frontendA, frontendB} {
		service := routing.ConfigurationService{Repository: frontend, Frontend: identity.FrontendID(frontend.FrontendID())}
		if _, err := service.SetProfile("codex", domainrouting.Model, "profile"); err != nil {
			t.Fatal(err)
		}
	}
	if err := frontendA.SaveAgentBinding(&domainrouting.AgentBinding{ID: "binding-a", FrontendID: "a", ModelOverride: "group-a", SubagentModelOverride: "child-a"}); err != nil {
		t.Fatal(err)
	}
	if err := frontendB.SaveAgentBinding(&domainrouting.AgentBinding{ID: "binding-b", FrontendID: "b", ModelOverride: "group-b"}); err != nil {
		t.Fatal(err)
	}
	cfg := fileconfig.Default()
	cfg.Codex.Model, cfg.Codex.ReasoningEffort = "global", "medium"
	sess := &conversation.Session{BindingID: "binding-a", ActiveThreadCollaborationMode: &conversation.SessionCollaborationMode{Mode: "plan", Model: "active", PresetReasoningEffort: "high"}}
	service := modelconfig.SnapshotService{Repository: ModelSourceRepository{Config: cfg, Mutex: mu, Scopes: frontendA}}
	desired := service.Desired("codex", sess)
	if desired.Model != "group-a" || desired.PlanModel != "group-a" || desired.SubagentModel != "child-a" || desired.PlanEffort != "" || desired.CollaborationMode != "" {
		t.Fatalf("desired = %+v", desired)
	}
	turn := service.TurnSnapshot("codex", sess)
	if turn.Model != "group-a" || turn.CollaborationMode != "plan" || turn.PlanEffort != "high" {
		t.Fatalf("turn = %+v", turn)
	}
	sess.ModelOverride = "session"
	if service.Desired("codex", sess).Model != "session" {
		t.Fatal("session override lost")
	}
	if _, err := (routing.ConfigurationService{Repository: frontendA}).SetBinding(frontendA.AgentBinding("binding-a"), domainrouting.Model, "default"); err != nil {
		t.Fatal(err)
	}
	sess.ModelOverride = ""
	if service.Desired("codex", sess).Model != "profile" {
		t.Fatal("cleared override did not inherit profile")
	}
	other := modelconfig.SnapshotService{Repository: ModelSourceRepository{Config: cfg, Mutex: mu, Scopes: frontendB}}
	if other.Desired("codex", sess).Model != "profile" {
		t.Fatal("foreign binding was visible to frontend B")
	}
	sess.BindingID = "binding-b"
	if other.Desired("codex", sess).Model != "group-b" {
		t.Fatal("frontend B was changed")
	}
	sess.BindingID = ""
	if service.Desired("codex", sess).Model != "profile" {
		t.Fatal("unbound session inherited group configuration")
	}
}

func TestModelSourcesUseOneRevisionDuringConcurrentWrites(t *testing.T) {
	cfg := fileconfig.Default()
	mu := &sync.RWMutex{}
	cfg.Codex.Model, cfg.Codex.ReasoningEffort = "a", "a"
	service := modelconfig.SnapshotService{Repository: ModelSourceRepository{Config: cfg, Mutex: mu}}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 1000; i++ {
			mu.Lock()
			value := "a"
			if i%2 == 0 {
				value = "b"
			}
			cfg.Codex.Model, cfg.Codex.ReasoningEffort = value, value
			mu.Unlock()
		}
	}()
	for i := 0; i < 1000; i++ {
		snapshot := service.Desired(domain.BackendCodex, nil)
		if snapshot.Model != snapshot.Effort {
			t.Errorf("mixed revision: %+v", snapshot)
			break
		}
	}
	<-done
}
