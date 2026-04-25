package app

import (
	appworkspace "feidex/internal/app/workspace"
	appworkspacecmd "feidex/internal/app/workspacecmd"
	"feidex/internal/config"
	"feidex/internal/codexrpc"
	"feidex/internal/state"
)

type workspaceThreadBinding = appworkspace.ThreadBinding

// workspaceThreadService wraps appworkspacecmd.ThreadService and provides
// lowercase method names for backward compatibility.
type workspaceThreadService struct {
	inner *appworkspacecmd.ThreadService
}

func newWorkspaceThreadService(app *App) workspaceThreadService {
	return workspaceThreadService{inner: newWorkspaceThreadServiceInner(app)}
}

func (s workspaceThreadService) listWorkspaceThreads(sessionKey string, ws *config.Workspace, includeAll bool) ([]codexrpc.ThreadListEntry, error) {
	return s.inner.ListWorkspaceThreads(sessionKey, ws, includeAll)
}

func (s workspaceThreadService) ensureWorkspaceThreadBinding(sessionKey string, sess *state.Session, ws *config.Workspace) (*workspaceThreadBinding, error) {
	binding, err := s.inner.EnsureWorkspaceThreadBinding(sessionKey, sess, ws)
	if err != nil {
		return nil, err
	}
	return &workspaceThreadBinding{
		ThreadID: binding.ThreadID,
		Name:     binding.Name,
		Preview:  binding.Preview,
		Resumed:  binding.Resumed,
	}, nil
}

// Exported wrapper for sub-package interface satisfaction.
func (s workspaceThreadService) StartWorkspaceThread(sessionKey string, sess *state.Session, ws *config.Workspace) (*workspaceThreadBinding, error) {
	return s.startWorkspaceThread(sessionKey, sess, ws)
}

func (s workspaceThreadService) startWorkspaceThread(sessionKey string, sess *state.Session, ws *config.Workspace) (*workspaceThreadBinding, error) {
	binding, err := s.inner.StartWorkspaceThread(sessionKey, sess, ws)
	if err != nil {
		return nil, err
	}
	return &workspaceThreadBinding{
		ThreadID: binding.ThreadID,
		Name:     binding.Name,
		Preview:  binding.Preview,
		Resumed:  binding.Resumed,
	}, nil
}
