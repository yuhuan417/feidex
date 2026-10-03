package config

import (
	"fmt"
	"strings"

	appstate "feidex/internal/adapter/storage/json/scoped"
	workspaceapp "feidex/internal/application/workspace"
	fileconfig "feidex/internal/config"
	"feidex/internal/domain/conversation"
	"feidex/internal/domain/identity"
	"feidex/internal/state"
)

// WorkspaceLifecycleRepository composes the config and JSON persistence ports.
// It contains only storage mechanics; lifecycle decisions live in application.
type WorkspaceLifecycleRepository struct {
	Source Source
	Scope  *appstate.Store
}

func (r WorkspaceLifecycleRepository) State() workspaceapp.LifecycleState {
	snapshot := r.Scope.StateStore().WorkspaceState()
	result := workspaceapp.LifecycleState{}
	for _, sess := range snapshot.Sessions {
		result.Sessions = append(result.Sessions, sess)
	}
	for _, binding := range snapshot.Bindings {
		result.Bindings = append(result.Bindings, binding)
	}
	for _, profile := range snapshot.Profiles {
		result.Profiles = append(result.Profiles, profile)
	}
	return result
}

func (r WorkspaceLifecycleRepository) Commit(change workspaceapp.LifecycleChange) error {
	expected := state.WorkspaceState{Sessions: map[string]*conversation.Session{}, Bindings: map[string]*state.AgentBinding{}, Profiles: map[string]*state.BotProfile{}}
	for _, sess := range change.Expected.Sessions {
		expected.Sessions[sess.Key] = sess
	}
	for _, binding := range change.Expected.Bindings {
		expected.Bindings[binding.ID] = binding
	}
	for _, profile := range change.Expected.Profiles {
		expected.Profiles[profile.ID] = profile
	}
	mutation := state.WorkspaceMutation{Expected: expected, Binding: change.Binding, Profiles: change.Profiles}
	for _, sess := range change.Sessions {
		cp := conversation.CloneSession(sess)
		if change.DeleteWorkspace == "" && !strings.Contains(cp.Key, ":workspace") {
			cp.Key = identity.CanonicalSessionKey(r.Scope.FrontendID(), cp.Key)
		}
		// Preserve foreign frontend identity during machine-wide deletion cleanup.
		// Locally switched sessions already have canonical keys from the entrypoint.
		if change.DeleteWorkspace == "" && r.Scope.Backend() != "" && !strings.HasSuffix(cp.Key, ":workspace") && !strings.Contains(cp.Key, ":workspace:") {
			conversation.StoreBackendThread(cp, r.Scope.Backend())
		}
		mutation.Sessions = append(mutation.Sessions, cp)
	}
	if change.DeleteWorkspace == "" {
		return r.Scope.StateStore().CommitWorkspace(mutation, nil)
	}
	r.Source.ConfigMu().Lock()
	defer r.Source.ConfigMu().Unlock()
	candidate := fileconfig.Clone(r.Source.Config())
	id := change.DeleteWorkspace
	if fileconfig.FindWorkspace(candidate, id) == nil {
		return fmt.Errorf("workspace %q not found", id)
	}
	if len(candidate.Workspaces) <= 1 {
		return fmt.Errorf("至少保留一个 workspace")
	}
	if fileconfig.FindWorkspace(candidate, change.FallbackWorkspace) == nil {
		return fmt.Errorf("工作区配置已变化，请重试")
	}
	next := make([]fileconfig.Workspace, 0, len(candidate.Workspaces)-1)
	for _, ws := range candidate.Workspaces {
		if ws.ID != id {
			next = append(next, ws)
		}
	}
	candidate.Workspaces = next
	repository := NewWorkspaceRepository(r.Source)
	err := r.Scope.StateStore().CommitWorkspace(mutation, func() error { return repository.normalizeAndSaveLocked(candidate) })
	if err != nil {
		return err
	}
	*r.Source.Config() = *candidate
	return nil
}
