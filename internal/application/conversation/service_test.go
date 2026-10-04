package conversation

import (
	"context"
	"errors"
	"strings"
	"testing"

	domain "feidex/internal/domain/conversation"
	"feidex/internal/domain/modelconfig"
	"feidex/internal/domain/workspace"
)

type gatewayStub struct {
	Gateway
	result        Thread
	err           error
	startResult   Thread
	startErr      error
	requests      []Request
	startRequests []Request
}

func (g *gatewayStub) Resume(_ context.Context, r Request) (Thread, error) {
	g.requests = append(g.requests, r)
	return g.result, g.err
}

func (g *gatewayStub) Start(_ context.Context, r Request) (Thread, error) {
	g.startRequests = append(g.startRequests, r)
	return g.startResult, g.startErr
}

type repositoryStub struct {
	saved *domain.Session
	err   error
}

func (r *repositoryStub) SaveSession(s *domain.Session) error {
	if r.err != nil {
		return r.err
	}
	r.saved = domain.CloneSession(s)
	return nil
}
func (r *repositoryStub) Session(string) *domain.Session { return domain.CloneSession(r.saved) }

type liveStub struct{ marked string }

func (l *liveStub) MarkSessionThreadLive(_, id string) { l.marked = id }
func (l *liveStub) ClearSessionLiveThread(string)      { l.marked = "" }

func TestResumePersistenceFailureKeepsLineageAndDoesNotPublishLiveThread(t *testing.T) {
	failure := errors.New("disk full")
	g := &gatewayStub{result: Thread{ID: "new", Applied: &modelconfig.Snapshot{Model: "new-model"}}}
	r := &repositoryStub{err: failure}
	live := &liveStub{}
	s := Service{Deps: Dependencies{Backend: func() string { return "codex" }, Gateway: g, Repository: r, Live: live}}
	sess := &domain.Session{Key: "session", ActiveThreadID: "old", ActiveThreadSandboxMode: "read-only", AppliedModelConfig: modelconfig.Snapshot{Model: "old-model"}}
	_, err := s.ResumeSelectedThread("session", sess, &workspace.Workspace{ID: "ws", Cwd: "/repo"}, domain.ThreadSelection{ThreadID: "new", Cwd: "/repo"})
	if !errors.Is(err, failure) || sess.ActiveThreadID != "old" || sess.ActiveThreadSandboxMode != "read-only" || sess.AppliedModelConfig.Model != "old-model" || live.marked != "" {
		t.Fatalf("error=%v session=%#v live=%q", err, sess, live.marked)
	}
}

func TestExplicitSelectionRejectsForeignWorkspaceBeforeBackendCall(t *testing.T) {
	g := &gatewayStub{}
	s := Service{Deps: Dependencies{Backend: func() string { return "codex" }, Gateway: g, Repository: &repositoryStub{}}}
	_, err := s.ResumeSelectedThread("session", &domain.Session{}, &workspace.Workspace{Cwd: "/repo"}, domain.ThreadSelection{ThreadID: "t", Cwd: "/other"})
	if !domain.IsWarning(err) || len(g.requests) != 0 {
		t.Fatalf("error=%v requests=%v", err, g.requests)
	}
}

// The Claude CLI emits system/init — the only frame carrying session_id — with
// the first user message, so starting a fresh session returns no id yet. That
// is a bound session without an id, not a failed start.
func TestClaudeStartBindsDeferredThreadIDWithoutLiveMark(t *testing.T) {
	g := &gatewayStub{startResult: Thread{Name: "Claude", Preview: "preview"}}
	r := &repositoryStub{}
	live := &liveStub{}
	s := Service{Deps: Dependencies{Backend: func() string { return "claude" }, Gateway: g, Repository: r, Live: live}}
	sess := &domain.Session{Key: "session", ActiveThreadID: "previous", ActiveThreadWorkspaceID: "ws"}

	binding, err := s.StartWorkspaceThread("session", sess, &workspace.Workspace{ID: "ws"})
	if err != nil {
		t.Fatalf("StartWorkspaceThread() error = %v", err)
	}
	if binding == nil || binding.ThreadID != "" || binding.Resumed || binding.Name != "Claude" {
		t.Fatalf("binding = %#v, want an empty, non-resumed Claude binding", binding)
	}
	if r.saved == nil || r.saved.ActiveThreadID != "" || r.saved.ActiveThreadWorkspaceID != "ws" || r.saved.ActiveThreadName != "Claude" || r.saved.Status != domain.SessionStatusIdle.String() {
		t.Fatalf("saved session = %#v", r.saved)
	}
	if sess.ActiveThreadID != "" || sess.ActiveThreadWorkspaceID != "ws" {
		t.Fatalf("session = %#v, want the deferred id bound to the workspace", sess)
	}
	if live.marked != "" {
		t.Fatalf("live thread = %q, want no live mark before the id exists", live.marked)
	}
}

func TestStartRejectsEmptyThreadIDForBackendsThatMaterializeEagerly(t *testing.T) {
	g := &gatewayStub{}
	r := &repositoryStub{}
	s := Service{Deps: Dependencies{Backend: func() string { return "codex" }, Gateway: g, Repository: r}}
	sess := &domain.Session{Key: "session", ActiveThreadID: "previous"}

	binding, err := s.StartWorkspaceThread("session", sess, &workspace.Workspace{ID: "ws"})
	if err == nil || !strings.Contains(err.Error(), "empty thread id") {
		t.Fatalf("binding = %#v, error = %v, want an empty thread id failure", binding, err)
	}
	if r.saved != nil || sess.ActiveThreadID != "previous" {
		t.Fatalf("saved = %#v, session = %#v, want no state change on failure", r.saved, sess)
	}
}

func TestConfirmedResumePublishesAppliedSnapshotAfterSaving(t *testing.T) {
	g := &gatewayStub{result: Thread{ID: "new", Name: "name", Applied: &modelconfig.Snapshot{Model: "confirmed"}}}
	r := &repositoryStub{}
	live := &liveStub{}
	s := Service{Deps: Dependencies{Backend: func() string { return "codex" }, Gateway: g, Repository: r, Live: live}}
	sess := &domain.Session{Key: "session", ActiveThreadID: "old", ModelConfigError: "previous failure"}
	if _, err := s.ResumeSelectedThread("session", sess, &workspace.Workspace{ID: "ws"}, domain.ThreadSelection{ThreadID: "new"}); err != nil {
		t.Fatal(err)
	}
	if r.saved.AppliedModelConfig.Model != "confirmed" || sess.ModelConfigError != "" || live.marked != "new" || !g.requests[0].SelectionExplicit {
		t.Fatalf("saved=%#v session=%#v live=%q", r.saved, sess, live.marked)
	}
}
