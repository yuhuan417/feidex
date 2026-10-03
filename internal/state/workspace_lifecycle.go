package state

import (
	"errors"
	"fmt"
	"maps"
	"reflect"
	"time"

	"feidex/internal/domain/conversation"
)

// WorkspaceState is a detached snapshot used for optimistic lifecycle commits.
type WorkspaceState struct {
	Sessions map[string]*conversation.Session
	Bindings map[string]*AgentBinding
	Profiles map[string]*BotProfile
}

type WorkspaceMutation struct {
	Expected WorkspaceState
	Sessions []*conversation.Session
	Binding  *AgentBinding
	Profiles []*BotProfile
}

func (s *Store) workspaceStateLocked() WorkspaceState {
	result := WorkspaceState{Sessions: map[string]*conversation.Session{}, Bindings: map[string]*AgentBinding{}, Profiles: map[string]*BotProfile{}}
	for key, sess := range s.runtime.Sessions {
		result.Sessions[key] = cloneSession(sess)
	}
	for key, binding := range s.data.AgentBindings {
		result.Bindings[key] = cloneAgentBinding(binding)
	}
	for key, profile := range s.data.BotProfiles {
		result.Profiles[key] = cloneBotProfile(profile)
	}
	return result
}

func (s *Store) WorkspaceState() WorkspaceState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.workspaceStateLocked()
}

// CommitWorkspace persists all references with one atomic snapshot replacement.
// persistConfiguration is a storage-only second-file commit, executed under the
// same state lock. On failure, both in-memory references and the state file are
// restored before returning. No runtime or network work belongs in this hook.
// Writing state before deleting config makes a crash in the two-file window
// conservative: an unused definition may remain, but no removed target is used.
func (s *Store) CommitWorkspace(change WorkspaceMutation, persistConfiguration func() error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !reflect.DeepEqual(s.workspaceStateLocked(), change.Expected) {
		return fmt.Errorf("工作区状态已变化，请重试")
	}
	oldRuntime, oldStored := s.runtime.Sessions, s.data.Sessions
	oldBindings, oldProfiles := s.data.AgentBindings, s.data.BotProfiles
	s.runtime.Sessions = maps.Clone(oldRuntime)
	s.data.Sessions = maps.Clone(oldStored)
	s.data.AgentBindings = maps.Clone(oldBindings)
	s.data.BotProfiles = maps.Clone(oldProfiles)
	restore := func() {
		s.runtime.Sessions, s.data.Sessions = oldRuntime, oldStored
		s.data.AgentBindings, s.data.BotProfiles = oldBindings, oldProfiles
	}
	now := time.Now().Unix()
	for _, sess := range change.Sessions {
		cp := cloneSession(sess)
		normalizeSessionValues(cp)
		cp.UpdatedAt = now
		s.runtime.Sessions[cp.Key] = cp
		s.syncPersistentSessionLocked(cp)
	}
	if binding := cloneAgentBinding(change.Binding); binding != nil {
		if s.data.AgentBindings == nil {
			s.data.AgentBindings = map[string]*AgentBinding{}
		}
		binding.UpdatedAt = now
		s.data.AgentBindings[binding.ID] = binding
	}
	for _, profile := range change.Profiles {
		cp := cloneBotProfile(profile)
		if s.data.BotProfiles == nil {
			s.data.BotProfiles = map[string]*BotProfile{}
		}
		if cp.CreatedAt == 0 {
			cp.CreatedAt = now
		}
		cp.UpdatedAt = now
		s.data.BotProfiles[cp.ID] = cp
	}
	if err := s.saveLocked(); err != nil {
		restore()
		return err
	}
	if persistConfiguration != nil {
		if err := persistConfiguration(); err != nil {
			restore()
			if rollbackErr := s.saveLocked(); rollbackErr != nil {
				return errors.Join(err, fmt.Errorf("workspace state rollback failed; retry required: %w", rollbackErr))
			}
			return err
		}
	}
	return nil
}
