package config

import (
	"sync"

	skillapp "feidex/internal/application/skill"
	fileconfig "feidex/internal/config"
	"feidex/internal/domain/conversation"
	"feidex/internal/domain/workspace"
)

type SkillSessions interface {
	Session(string) *conversation.Session
}

type SkillWorkspaceRepository struct {
	Config   *fileconfig.Config
	Mutex    *sync.RWMutex
	Sessions SkillSessions
}

func (r SkillWorkspaceRepository) SkillWorkspaceSource(key string) skillapp.WorkspaceSource {
	if r.Mutex != nil {
		r.Mutex.RLock()
		defer r.Mutex.RUnlock()
	}
	source := skillapp.WorkspaceSource{}
	if r.Config != nil {
		source.Workspaces = append([]workspace.Workspace(nil), r.Config.Workspaces...)
	}
	if key != "" && r.Sessions != nil {
		if sess := r.Sessions.Session(key); sess != nil {
			source.WorkspaceID = sess.WorkspaceID
		}
	}
	return source
}
