package workspace

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"feidex/internal/domain/conversation"
	"feidex/internal/domain/routing"
	domain "feidex/internal/domain/workspace"
)

type lifecycleRepositoryStub struct {
	state   LifecycleState
	failure error
	changes []LifecycleChange
}

func (r *lifecycleRepositoryStub) State() LifecycleState { return r.state }
func (r *lifecycleRepositoryStub) Commit(change LifecycleChange) error {
	r.changes = append(r.changes, change)
	return r.failure
}

func TestLifecycleSwitchAdmissionUsesLatestSession(t *testing.T) {
	for _, kind := range []string{"turn", "queue", "staged", "compacting", "starting"} {
		t.Run(kind, func(t *testing.T) {
			current := &conversation.Session{Key: "session", WorkspaceID: "default"}
			switch kind {
			case "turn":
				current.ActiveTurnID = "turn"
			case "queue":
				current.Queue = []string{"queued"}
			case "staged":
				current.StagedImages = []conversation.SessionStagedImage{{}}
			case "compacting":
				current.Status = conversation.SessionStatusCompacting.String()
			case "starting":
				current.Status = conversation.SessionStatusTurnStarting.String()
			}
			repo := &lifecycleRepositoryStub{state: LifecycleState{Sessions: []*conversation.Session{current}}}
			l := Lifecycle{Repository: repo, Configuration: ConfigurationService{Repository: &configurationRepositoryStub{workspaces: map[string]domain.Workspace{"alt": {ID: "alt"}}}}}
			if _, err := l.Switch(SwitchRequest{Session: &conversation.Session{Key: "session"}, WorkspaceID: "alt"}); err == nil {
				t.Fatal("stale idle snapshot bypassed admission")
			}
			if len(repo.changes) != 0 {
				t.Fatal("blocked switch committed state")
			}
		})
	}
}

func TestLifecycleDeleteGuardsCurrentMissingAndLastWorkspace(t *testing.T) {
	repo := &lifecycleRepositoryStub{state: LifecycleState{Sessions: []*conversation.Session{{Key: "session", WorkspaceID: "default"}}}}
	config := &configurationRepositoryStub{workspaces: map[string]domain.Workspace{"default": {ID: "default"}, "alt": {ID: "alt"}}}
	l := Lifecycle{Repository: repo, Configuration: ConfigurationService{Repository: config}}
	for _, id := range []string{"default", "missing", ""} {
		if _, err := l.Delete("session", id); err == nil {
			t.Fatalf("deletion accepted %q", id)
		}
	}
	delete(config.workspaces, "default")
	if _, err := l.Delete("session", "alt"); err == nil || !strings.Contains(err.Error(), "至少保留") {
		t.Fatalf("last workspace: %v", err)
	}
	if len(repo.changes) != 0 {
		t.Fatal("rejected deletion committed state")
	}
}

func TestCreateAndSwitchKeepsCreatedDefinitionOnCommitFailure(t *testing.T) {
	repo := &lifecycleRepositoryStub{failure: errors.New("disk failure")}
	config := &configurationRepositoryStub{workspaces: map[string]domain.Workspace{"default": {ID: "default"}}}
	l := Lifecycle{Repository: repo, Configuration: ConfigurationService{Repository: config}}
	effects, err := l.CreateAndSwitch(SwitchRequest{Session: &conversation.Session{Key: "session", WorkspaceID: "default"}}, domain.Workspace{ID: "new"})
	if err == nil || !reflect.DeepEqual(effects, LifecycleEffects{}) {
		t.Fatalf("effects = %+v, error = %v", effects, err)
	}
	if _, ok := config.workspaces["new"]; !ok {
		t.Fatal("created definition lost, clone cannot retry switch")
	}
}

func TestGroupBindingSelectionPreservesActiveConversationAndUnbindRequiresIdle(t *testing.T) {
	current := &conversation.Session{Key: "session", WorkspaceID: "default", ActiveThreadID: "old-thread", ActiveTurnID: "turn", Queue: []string{"queued"}}
	binding := &routing.AgentBinding{ID: "binding", FrontendID: "bot-a", WorkspaceID: "default", Status: "active"}
	repo := &lifecycleRepositoryStub{state: LifecycleState{Sessions: []*conversation.Session{current}, Bindings: []*routing.AgentBinding{binding}}}
	l := Lifecycle{Frontend: "bot-a", Repository: repo, Configuration: ConfigurationService{Repository: &configurationRepositoryStub{workspaces: map[string]domain.Workspace{"alt": {ID: "alt"}}}}}
	effects, err := l.Switch(SwitchRequest{Session: current, Binding: binding, WorkspaceID: "alt"})
	if err != nil {
		t.Fatal(err)
	}
	if len(effects.ClearLiveThreads) != 0 || effects.Session != nil || len(repo.changes[0].Sessions) != 0 || effects.Binding.WorkspaceID != "alt" {
		t.Fatalf("binding overwrote conversation: %+v", effects)
	}
	if _, err := l.Switch(SwitchRequest{Session: current, Binding: binding, Unbind: true}); err == nil {
		t.Fatal("active unbind accepted")
	}
}
