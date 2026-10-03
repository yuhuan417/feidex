package workspace

import (
	"context"
	"feidex/internal/domain/conversation"
	"feidex/internal/domain/routing"
	domain "feidex/internal/domain/workspace"
	"log/slog"
)

type EffectRuntime interface {
	ClearLive(string)
	Run(string, func()) bool
	Replay(context.Context, string) error
}
type ThreadBinder interface {
	EnsureWorkspaceThreadBinding(string, *conversation.Session, *domain.Workspace) (*conversation.ThreadBinding, error)
}
type EffectService struct {
	Lifecycle     *Lifecycle
	Runtime       EffectRuntime
	Conversations ThreadBinder
	Context       func() context.Context
}

func (s EffectService) Apply(effects LifecycleEffects) {
	for _, key := range effects.ClearLiveThreads {
		s.Runtime.ClearLive(key)
	}
	if effects.Binding != nil {
		s.Replay(effects.Binding)
	} else if effects.Session != nil && effects.Workspace != nil {
		s.Bind(effects.Session.Key, effects.Workspace.ID)
	}
}

func (s EffectService) Bind(key, workspaceID string) {
	s.Runtime.Run(key, func() {
		sess, ws, err := s.Lifecycle.BindingCandidate(key, workspaceID)
		if err == nil && sess != nil && ws != nil {
			_, err = s.Conversations.EnsureWorkspaceThreadBinding(key, sess, ws)
		}
		if err != nil {
			slog.Warn("workspace thread binding failed", "session_key", key, "error", err)
		}
	})
}

func (s EffectService) Replay(binding *routing.AgentBinding) {
	if binding == nil || len(binding.PendingMessages) == 0 {
		return
	}
	s.Runtime.Run(binding.ChatID, func() {
		if err := s.Runtime.Replay(s.Context(), binding.ID); err != nil {
			slog.Warn("binding pending replay failed", "binding_id", binding.ID, "error", err)
		}
	})
}
