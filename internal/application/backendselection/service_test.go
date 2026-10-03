package backendselection

import (
	"context"
	"errors"
	"testing"

	"feidex/internal/domain/conversation"
)

type switchRepository struct {
	backend  string
	sessions []*conversation.Session
	fail     bool
}

func (r *switchRepository) ConfiguredBackend() string         { return r.backend }
func (r *switchRepository) Sessions() []*conversation.Session { return r.sessions }
func (r *switchRepository) CommitBackend(target string, _, next []*conversation.Session) error {
	if r.fail {
		return errors.New("commit failed")
	}
	r.backend = target
	r.sessions = next
	return nil
}

type switchTransition struct{ active bool }

func (*switchTransition) LockSwitch()                      {}
func (*switchTransition) UnlockSwitch()                    {}
func (s *switchTransition) BeginBackendSwitchState(string) { s.active = true }
func (s *switchTransition) FinishBackendSwitchState()      { s.active = false }

type switchRuntime struct {
	repo                         *switchRepository
	installed, closed, recovered int
	committedBeforeInstall       bool
	blocked                      string
}

func (*switchRuntime) AvailableBackends() []AvailableBackend {
	return []AvailableBackend{{Kind: "codex"}, {Kind: "claude"}}
}
func (*switchRuntime) Ready(string) bool           { return true }
func (r *switchRuntime) IdleBlockedReason() string { return r.blocked }
func (r *switchRuntime) Prepare(context.Context, string) (*RuntimeHandle, error) {
	return &RuntimeHandle{Close: func() error { r.closed++; return nil }, Install: func() { r.installed++; r.committedBeforeInstall = r.repo.backend == "claude" }}, nil
}
func (r *switchRuntime) Snapshot() *RuntimeHandle {
	return &RuntimeHandle{Close: func() error { r.closed++; return nil }}
}
func (r *switchRuntime) Recover() { r.recovered++ }

func TestSwitchCommitFailureDoesNotInstallRuntime(t *testing.T) {
	repo := &switchRepository{backend: "codex", fail: true}
	runtime := &switchRuntime{repo: repo}
	transition := &switchTransition{}
	if err := (Service{Repository: repo, Runtime: runtime, Transition: transition}).Switch(context.Background(), "claude"); err == nil {
		t.Fatal("expected commit failure")
	}
	if runtime.installed != 0 || runtime.closed != 1 || runtime.recovered != 0 || transition.active {
		t.Fatalf("runtime changed on failure: %+v", runtime)
	}
}
func TestSwitchPreservesDetachedLineageAndCommitsBeforeInstall(t *testing.T) {
	original := &conversation.Session{Key: "session", ActiveThreadID: "codex-thread", WorkspaceID: "repo", BackendThreads: map[string]conversation.SessionBackendThread{"claude": {ThreadID: "claude-thread", WorkspaceID: "repo"}}}
	repo := &switchRepository{backend: "codex", sessions: []*conversation.Session{original}}
	runtime := &switchRuntime{repo: repo}
	transition := &switchTransition{}
	if err := (Service{Repository: repo, Runtime: runtime, Transition: transition}).Switch(context.Background(), "claude"); err != nil {
		t.Fatal(err)
	}
	if original.ActiveThreadID != "codex-thread" || repo.sessions[0].ActiveThreadID != "claude-thread" || repo.sessions[0].BackendThreads["codex"].ThreadID != "codex-thread" {
		t.Fatal("lineage mutated or lost")
	}
	if !runtime.committedBeforeInstall || runtime.installed != 1 || runtime.recovered != 1 || runtime.closed != 1 || transition.active {
		t.Fatalf("switch effects: %+v", runtime)
	}
}
