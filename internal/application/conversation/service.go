// Package conversation owns conversation binding, selection and lineage.
package conversation

import (
	"context"
	domain "feidex/internal/domain/conversation"
	"feidex/internal/domain/modelconfig"
	"feidex/internal/domain/workspace"
	"feidex/internal/textutil"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// Thread is a protocol-independent result. Applied is only present when the
// backend confirms initialization settings; selection never invents an applied value.
type Thread struct {
	ID, Name, Preview string
	Applied           *modelconfig.Snapshot
}

type Request struct {
	SessionKey        string
	Session           *domain.Session
	Workspace         *workspace.Workspace
	Model             string
	SelectionExplicit bool
	Selection         domain.ThreadSelection
}

// Gateway performs external conversation operations, never changes local state.
type Gateway interface {
	List(context.Context, *workspace.Workspace, bool) ([]domain.ThreadEntry, error)
	Start(context.Context, Request) (Thread, error)
	Resume(context.Context, Request) (Thread, error)
	Fork(context.Context, Request) (Thread, error)
	Interrupt(context.Context, *domain.Session) error
	Steer(context.Context, *domain.Session, string) error
}

type Repository interface {
	SaveSession(*domain.Session) error
	Session(string) *domain.Session
}
type LiveThreads interface {
	MarkSessionThreadLive(string, string)
	ClearSessionLiveThread(string)
}

type Service struct {
	Context      context.Context
	Backend      string
	Gateway      Gateway
	Repository   Repository
	Live         LiveThreads
	ResolveModel func(*domain.Session, *workspace.Workspace) string
}

func (s *Service) context() context.Context {
	if s.Context != nil {
		return s.Context
	}
	return context.Background()
}
func (s *Service) request(key string, sess *domain.Session, ws *workspace.Workspace) Request {
	r := Request{SessionKey: key, Session: sess, Workspace: ws}
	if s.ResolveModel != nil {
		r.Model = strings.TrimSpace(s.ResolveModel(sess, ws))
	}
	return r
}
func (s *Service) validate(sess *domain.Session, ws *workspace.Workspace) error {
	if sess == nil {
		return fmt.Errorf("session not initialized")
	}
	if ws == nil {
		return fmt.Errorf("workspace not found")
	}
	if s.Gateway == nil {
		return fmt.Errorf("%s backend not initialized", s.Backend)
	}
	return nil
}

func (s *Service) ListWorkspaceThreads(_ string, ws *workspace.Workspace, all bool) ([]domain.ThreadEntry, error) {
	if ws == nil {
		return nil, fmt.Errorf("workspace not found")
	}
	if s.Gateway == nil {
		return nil, fmt.Errorf("%s backend not initialized", s.Backend)
	}
	ctx, cancel := context.WithTimeout(s.context(), 20*time.Second)
	defer cancel()
	return s.Gateway.List(ctx, ws, all)
}

func (s *Service) bind(key string, sess *domain.Session, ws *workspace.Workspace, t Thread, resumed, clear bool) (*domain.ThreadBinding, error) {
	original := sess
	sess = domain.CloneSession(sess)
	if strings.TrimSpace(t.ID) == "" {
		return nil, fmt.Errorf("thread operation returned empty thread id")
	}
	if clear {
		domain.ClearThreadContext(sess)
	}
	domain.SetThreadContext(sess, ws.ID, t.ID, t.Name, t.Preview)
	if t.Applied != nil {
		sess.AppliedModelConfig = *t.Applied
		sess.ModelConfigError = ""
	}
	domain.ResetActiveOperations(sess)
	sess.Status = domain.SessionStatusIdle.String()
	if err := s.Repository.SaveSession(sess); err != nil {
		return nil, err
	}
	*original = *sess
	if s.Live != nil {
		s.Live.MarkSessionThreadLive(key, t.ID)
	}
	return &domain.ThreadBinding{ThreadID: t.ID, Name: sess.ActiveThreadName, Preview: sess.ActiveThreadPreview, Resumed: resumed}, nil
}

func (s *Service) StartWorkspaceThread(key string, sess *domain.Session, ws *workspace.Workspace) (*domain.ThreadBinding, error) {
	if err := s.validate(sess, ws); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(s.context(), 30*time.Second)
	defer cancel()
	t, err := s.Gateway.Start(ctx, s.request(key, sess, ws))
	if err != nil {
		return nil, err
	}
	return s.bind(key, sess, ws, t, false, true)
}

func (s *Service) ResumeSelectedThread(key string, sess *domain.Session, ws *workspace.Workspace, sel domain.ThreadSelection) (*domain.ThreadBinding, error) {
	if err := s.validate(sess, ws); err != nil {
		return nil, err
	}
	if strings.TrimSpace(sel.ThreadID) == "" {
		return nil, fmt.Errorf("missing thread id")
	}
	if strings.TrimSpace(sel.Cwd) != "" && !domain.SameWorkspaceCWD(sel.Cwd, ws.Cwd) {
		return nil, domain.NewWarning("该会话不属于当前工作区，请先切换 workspace")
	}
	r := s.request(key, sess, ws)
	r.Selection = sel
	r.SelectionExplicit = true
	ctx, cancel := context.WithTimeout(s.context(), 30*time.Second)
	defer cancel()
	t, err := s.Gateway.Resume(ctx, r)
	if err != nil {
		return nil, err
	}
	original := sess
	sess = domain.CloneSession(sess)
	// Explicit selection clears per-thread permission and collaboration overrides.
	// Codex's multi-agent and service-tier settings retain their existing scope.
	if s.Backend == "codex" {
		sess.ActiveThreadApprovalPolicy = ""
		sess.ActiveThreadSandboxMode = ""
		sess.ActiveClaudePermissionMode = ""
		sess.ActiveThreadCollaborationMode = nil
	}
	binding, err := s.bind(key, sess, ws, t, true, s.Backend == "claude")
	if err == nil {
		*original = *sess
	}
	return binding, err
}

func (s *Service) EnsureWorkspaceThreadBinding(key string, sess *domain.Session, ws *workspace.Workspace) (*domain.ThreadBinding, error) {
	if err := s.validate(sess, ws); err != nil {
		return nil, err
	}
	if s.Backend == "claude" {
		if strings.TrimSpace(sess.ActiveThreadWorkspaceID) == strings.TrimSpace(ws.ID) && strings.TrimSpace(sess.ActiveThreadID) != "" {
			r := s.request(key, sess, ws)
			r.Selection.ThreadID = sess.ActiveThreadID
			ctx, cancel := context.WithTimeout(s.context(), 30*time.Second)
			t, err := s.Gateway.Resume(ctx, r)
			cancel()
			if err == nil {
				t.Name = textutil.FirstNonEmpty(sess.ActiveThreadName, "Claude")
				t.Preview = textutil.FirstNonEmpty(sess.ActiveThreadPreview, ws.Name)
				return s.bind(key, sess, ws, t, true, false)
			}
			slog.Warn("Claude workspace session resume failed; starting fresh session", "session_key", key, "error", err)
		}
	} else {
		items, err := s.ListWorkspaceThreads(key, ws, false)
		if err != nil {
			slog.Warn("workspace thread list failed; falling back to new thread", "session_key", key, "error", err)
		}
		if len(items) > 0 {
			domain.SortThreadsByUpdated(items)
			r := s.request(key, sess, ws)
			e := items[0]
			r.Selection = domain.ThreadSelection{ThreadID: e.ID, Name: e.Name, Preview: e.Preview, Cwd: e.Cwd}
			ctx, cancel := context.WithTimeout(s.context(), 30*time.Second)
			t, err := s.Gateway.Resume(ctx, r)
			cancel()
			if err == nil {
				return s.bind(key, sess, ws, t, true, true)
			}
			slog.Warn("workspace thread resume failed; starting fresh thread", "session_key", key, "error", err)
		}
	}
	return s.StartWorkspaceThread(key, sess, ws)
}

func (s *Service) ForkActiveConversation(key string, sess *domain.Session, ws *workspace.Workspace) (string, error) {
	if err := s.validate(sess, ws); err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(s.context(), 30*time.Second)
	defer cancel()
	t, err := s.Gateway.Fork(ctx, s.request(key, sess, ws))
	if err != nil {
		return "", err
	}
	// Claude can defer materializing a branch until its next input.
	if t.ID == "" && s.Backend != "claude" {
		return "", fmt.Errorf("fork thread returned empty thread id")
	}
	if s.Backend == "claude" {
		domain.ClearThreadContext(sess)
	}
	domain.SetThreadContext(sess, textutil.FirstNonEmpty(sess.WorkspaceID, ws.ID), t.ID, t.Name, t.Preview)
	sess.ActiveThreadCollaborationMode = nil
	domain.ResetActiveOperations(sess)
	sess.Status = domain.SessionStatusIdle.String()
	sess.Queue = nil
	sess.StagedImages = nil
	if err := s.Repository.SaveSession(sess); err != nil {
		return "", err
	}
	if s.Live != nil {
		if t.ID != "" {
			s.Live.MarkSessionThreadLive(key, t.ID)
		} else {
			s.Live.ClearSessionLiveThread(key)
		}
	}
	return t.ID, nil
}

func (s *Service) InterruptActiveTurn(ctx context.Context, _ string, sess *domain.Session) error {
	return s.Gateway.Interrupt(ctx, sess)
}
func (s *Service) ContinueActiveTurn(key, text string) error {
	sess := s.Repository.Session(key)
	if strings.TrimSpace(text) == "" || sess == nil || strings.TrimSpace(sess.ActiveThreadID) == "" || strings.TrimSpace(sess.ActiveTurnID) == "" {
		return fmt.Errorf("当前没有可补充的任务")
	}
	ctx, cancel := context.WithTimeout(s.context(), 20*time.Second)
	defer cancel()
	return s.Gateway.Steer(ctx, sess, strings.TrimSpace(text))
}
