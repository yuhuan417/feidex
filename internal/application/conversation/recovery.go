package conversation

import (
	"context"
	"feidex/internal/domain/conversation"
	"feidex/internal/domain/workspace"
	"feidex/internal/textutil"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

type RecoveryGateway interface {
	Resume(context.Context, Request) (Thread, error)
	Start(context.Context, Request) (Thread, error)
}
type RecoveryEndpoint struct {
	Gateway    RecoveryGateway
	Current    func() bool
	LazyResume bool
}
type RecoveryWorkspaces interface {
	Get(string) *workspace.Workspace
	DefaultID() string
}
type RecoveryDependencies struct {
	Repository    StartupRepository
	Conversations *Service
	Workspaces    RecoveryWorkspaces
	Capture       func() (RecoveryEndpoint, error)
}

type Recovery RecoveryDependencies

func NewRecovery(deps RecoveryDependencies) Recovery { return Recovery(deps) }

func (s Recovery) Restore() error {
	for _, sess := range s.Repository.Sessions() {
		if sess == nil || strings.TrimSpace(sess.ActiveThreadID) == "" || conversation.NormalizeSessionStatus(sess.Status) != conversation.SessionStatusIdle || conversation.HasInFlightSubmission(sess) || len(sess.Queue) != 0 || len(sess.StagedImages) != 0 {
			continue
		}
		id := textutil.FirstNonEmpty(sess.ActiveThreadWorkspaceID, sess.WorkspaceID, s.Workspaces.DefaultID())
		ws := s.Workspaces.Get(id)
		if ws == nil {
			conversation.ClearThreadContext(sess)
			if err := s.Repository.SaveSession(sess); err != nil {
				return err
			}
			s.Conversations.Deps.Live.ClearSessionLiveThread(sess.Key)
			continue
		}
		if err := s.restore(sess, ws); err != nil {
			return err
		}
	}
	return nil
}

func (s Recovery) restore(sess *conversation.Session, ws *workspace.Workspace) error {
	endpoint, err := s.Capture()
	if err != nil {
		slog.Debug("startup thread recovery unavailable", "session_key", sess.Key, "error", err)
		return nil
	}
	if endpoint.LazyResume {
		s.Conversations.Deps.Live.MarkSessionThreadLive(sess.Key, sess.ActiveThreadID)
		return nil
	}
	r := s.Conversations.request(sess.Key, sess, ws)
	r.Selection = conversation.ThreadSelection{ThreadID: sess.ActiveThreadID, Name: sess.ActiveThreadName, Preview: sess.ActiveThreadPreview}
	ctx, cancel := context.WithTimeout(s.Conversations.context(), 30*time.Second)
	thread, err := endpoint.Gateway.Resume(ctx, r)
	cancel()
	if !endpoint.Current() {
		return nil
	}
	if err == nil {
		_, err = s.Conversations.bind(sess.Key, sess, ws, thread, true, false)
		return err
	}
	slog.Warn("startup thread resume failed; starting fresh thread", "session_key", sess.Key, "thread_id", sess.ActiveThreadID, "error", err)
	ctx, cancel = context.WithTimeout(s.Conversations.context(), 30*time.Second)
	thread, err = endpoint.Gateway.Start(ctx, r)
	cancel()
	if !endpoint.Current() {
		return nil
	}
	if err != nil {
		slog.Warn("startup thread start failed; clearing lineage", "session_key", sess.Key, "error", err)
		conversation.ClearThreadContext(sess)
		if saveErr := s.Repository.SaveSession(sess); saveErr != nil {
			return fmt.Errorf("save cleared lineage: %w", saveErr)
		}
		s.Conversations.Deps.Live.ClearSessionLiveThread(sess.Key)
		return nil
	}
	_, err = s.Conversations.bind(sess.Key, sess, ws, thread, false, false)
	return err
}
