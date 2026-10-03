package json

import (
	"context"
	configadapter "feidex/internal/adapter/config"
	applicationmodelconfig "feidex/internal/application/modelconfig"
	appsubmission "feidex/internal/application/submission"
	"feidex/internal/config"
	"feidex/internal/domain/conversation"
	domainsubmission "feidex/internal/domain/submission"
	"feidex/internal/runtime/turnbinding"
	"feidex/internal/state"
	"sync"
	"testing"
	"time"
)

func TestStartNextSubmissionAsyncCoalescesConcurrentStarts(t *testing.T) {
	store, err := state.Open(t.TempDir() + "/state.json")
	if err != nil {
		t.Fatalf("Open(store) error = %v", err)
	}
	sessionKey := "sess-1"
	if err := store.UpsertSession(&conversation.Session{
		Key:         sessionKey,
		WorkspaceID: "default",
		Status:      conversation.SessionStatusQueued.String(),
		Queue:       []string{"sub-1"},
	}); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}
	if _, err := store.CreateSubmission(&domainsubmission.Submission{
		ID:          "sub-1",
		SessionKey:  sessionKey,
		WorkspaceID: "default",
		Status:      domainsubmission.SubmissionStatusQueued.String(),
	}); err != nil {
		t.Fatalf("CreateSubmission() error = %v", err)
	}

	app := newConcurrentStartTestApp(store, sessionKey)
	svc := appsubmission.NewSubmissionQueueService(appsubmission.Dependencies{
		AppState: &app.state,
		StartConversation: func(context.Context, *config.Workspace, *conversation.Session, *domainsubmission.Submission, string) (appsubmission.ConversationStarted, error) {
			return appsubmission.ConversationStarted{ID: "thread-1"}, nil
		},
		StartSubmissionTurn: func(ctx context.Context, key, thread string, sub *domainsubmission.Submission, cwd, policy, sandbox, tier, model, effort, multi string) (string, error) {
			return "turn-1", app.backend.StartQueuedSubmission(key, nil, sub, nil, false)
		},
		ModelSettings: applicationmodelconfig.SnapshotService{Repository: configadapter.ModelSourceRepository{Config: config.Default()}},
		LiveThread:    concurrencyLive{}, RuntimeState: concurrencyRuntime{Tracker: turnbinding.NewTracker(store)},
		MarkSubmissionRunningReactions: func(*domainsubmission.Submission) {}, IsReviewSubmission: func(*domainsubmission.Submission) bool { return false },
		ReplyContinuation: concurrencyLinks{}, TurnStream: concurrencyStream{},

		Workspace:                app.SubmissionQueueWorkspace,
		DefaultWorkspaceID:       app.SubmissionQueueDefaultWorkspaceID,
		ConfiguredInflightMode:   app.SubmissionQueueConfiguredInflightMode,
		InflightAllowsAdditional: app.SubmissionQueueInflightAllowsAdditional,
		TryBeginStart:            app.SubmissionQueueTryBeginStart,
		FinishStart:              app.SubmissionQueueFinishStart,
		RunAsync:                 app.SubmissionQueueRunAsync,
		LogSessionState:          app.SubmissionQueueLogSessionState,
		AutoRetry:                concurrentStartNoopAutoRetry{},
	})

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		svc.StartNextSubmissionAsync(sessionKey, "g1")
	}()
	go func() {
		defer wg.Done()
		svc.StartNextSubmissionAsync(sessionKey, "g2")
	}()

	select {
	case <-app.backend.started:
	case <-time.After(2 * time.Second):
		t.Fatal("expected one queued start to reach backend")
	}

	sess := store.GetSession(sessionKey)
	if sess == nil {
		t.Fatal("expected session")
	}
	if sess.Status == conversation.SessionStatusIdle.String() {
		t.Fatalf("session should not be reset to idle during concurrent start: %+v", sess)
	}
	if app.backend.calls() != 1 {
		t.Fatalf("backend start call count = %d, want 1", app.backend.calls())
	}

	close(app.backend.release)
	wg.Wait()
}

type concurrentStartTestApp struct {
	state   concurrentStartTestState
	backend *concurrentStartBackend
	guard   concurrentStartGuard
}

func newConcurrentStartTestApp(store *state.Store, sessionKey string) *concurrentStartTestApp {
	return &concurrentStartTestApp{
		state: concurrentStartTestState{
			store:             store,
			barrierSessionKey: sessionKey,
			barrierReady:      make(chan struct{}),
		},
		backend: &concurrentStartBackend{
			started: make(chan struct{}, 1),
			release: make(chan struct{}),
		},
	}
}

func (a *concurrentStartTestApp) SubmissionQueueDefaultWorkspaceID() string {
	return "default"
}

func (a *concurrentStartTestApp) SubmissionQueueWorkspace(id string) *config.Workspace {
	return &config.Workspace{ID: id, Cwd: "."}
}

func (a *concurrentStartTestApp) SubmissionQueueConfiguredInflightMode() appsubmission.QueueInflightMode {
	return appsubmission.InflightSingle
}

func (a *concurrentStartTestApp) SubmissionQueueInflightAllowsAdditional(appsubmission.QueueInflightMode) bool {
	return false
}

func (a *concurrentStartTestApp) SubmissionQueueRunAsync(fn func()) {
	go fn()
}

func (a *concurrentStartTestApp) SubmissionQueueTryBeginStart(sessionKey string) bool {
	return a.guard.tryBegin(sessionKey)
}

func (a *concurrentStartTestApp) SubmissionQueueFinishStart(sessionKey string) bool {
	return a.guard.finish(sessionKey)
}

func (a *concurrentStartTestApp) SubmissionQueueLogSessionState(string, string, *conversation.Session) {
}

type concurrentStartTestState struct {
	store             *state.Store
	barrierSessionKey string
	mu                sync.Mutex
	sessionCalls      int
	barrierReady      chan struct{}
}

func (s *concurrentStartTestState) Session(key string) *conversation.Session {
	s.mu.Lock()
	if key == s.barrierSessionKey && s.sessionCalls < 2 {
		s.sessionCalls++
		callNum := s.sessionCalls
		ready := s.barrierReady
		s.mu.Unlock()
		if callNum == 2 {
			close(ready)
		} else {
			<-ready
		}
		return s.store.GetSession(key)
	}
	s.mu.Unlock()
	return s.store.GetSession(key)
}

func (s *concurrentStartTestState) Submission(id string) *domainsubmission.Submission {
	return s.store.GetSubmission(id)
}

func (s *concurrentStartTestState) SaveSession(sess *conversation.Session) error {
	return s.store.UpsertSession(sess)
}

func (s *concurrentStartTestState) CreateSubmission(sub *domainsubmission.Submission) (string, error) {
	return s.store.CreateSubmission(sub)
}

func (s *concurrentStartTestState) QueueSubmission(sessionKey, id string) error {
	return s.store.QueueSubmission(sessionKey, id)
}

func (s *concurrentStartTestState) DequeueSubmission(sessionKey string) (string, error) {
	return s.store.DequeueSubmission(sessionKey)
}

func (s *concurrentStartTestState) MarkSubmissionRunning(id, threadID, turnID string) error {
	return s.store.UpdateSubmission(id, func(sub *domainsubmission.Submission) {
		sub.ThreadID = threadID
		sub.TurnID = turnID
		sub.Status = domainsubmission.SubmissionStatusRunning.String()
	})
}

func (s *concurrentStartTestState) FinalizeSubmission(id, status string) error {
	return s.store.UpdateSubmission(id, func(sub *domainsubmission.Submission) {
		sub.Status = status
		sub.Finalized = true
	})
}

func (s *concurrentStartTestState) UpdateSession(key string, mutate func(*conversation.Session)) (*conversation.Session, error) {
	return s.store.UpdateSession(key, mutate)
}

func (s *concurrentStartTestState) NextLocalID(prefix string) (string, error) {
	return prefix + "-1", nil
}

func (s *concurrentStartTestState) DeletePendingRequests(func(*state.PendingRequest) bool) {}

func (s *concurrentStartTestState) DeleteMessageLinks(func(*state.MessageLink) bool) {}

func (s *concurrentStartTestState) UpdateSubmission(id string, mutate func(*domainsubmission.Submission)) error {
	return s.store.UpdateSubmission(id, mutate)
}

func (s *concurrentStartTestState) Sessions() []*conversation.Session {
	return s.store.AllSessions()
}

type concurrentStartBackend struct {
	mu      sync.Mutex
	count   int
	started chan struct{}
	release chan struct{}
}

func (b *concurrentStartBackend) StartQueuedSubmission(string, *conversation.Session, *domainsubmission.Submission, *config.Workspace, bool) error {
	b.mu.Lock()
	b.count++
	b.mu.Unlock()
	select {
	case b.started <- struct{}{}:
	default:
	}
	<-b.release
	return nil
}

func (b *concurrentStartBackend) calls() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.count
}

type concurrentStartGuard struct {
	mu      sync.Mutex
	running map[string]bool
	pending map[string]bool
}

func (g *concurrentStartGuard) tryBegin(sessionKey string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.running == nil {
		g.running = map[string]bool{}
	}
	if g.pending == nil {
		g.pending = map[string]bool{}
	}
	if g.running[sessionKey] {
		g.pending[sessionKey] = true
		return false
	}
	g.running[sessionKey] = true
	delete(g.pending, sessionKey)
	return true
}

func (g *concurrentStartGuard) finish(sessionKey string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	rerun := g.pending[sessionKey]
	delete(g.running, sessionKey)
	delete(g.pending, sessionKey)
	return rerun
}

type concurrentStartNoopAutoRetry struct{}

func (concurrentStartNoopAutoRetry) ObserveAutoRetryTerminal(string, string, string, *conversation.Session, *domainsubmission.Submission, string, string) bool {
	return false
}

func (concurrentStartNoopAutoRetry) HasBlockingAutoRetry(string) bool { return false }

type concurrencyLive struct{}

func (concurrencyLive) MarkSessionThreadLive(string, string)     {}
func (concurrencyLive) SessionHasLiveThread(string, string) bool { return true }
func (concurrencyLive) ClearSessionLiveThread(string)            {}

type concurrencyLinks struct{}

func (concurrencyLinks) RecordSubmissionSourceLinks(*domainsubmission.Submission) {}
func (concurrencyLinks) RecordRootTurnBinding(string, string, string, string)     {}

type concurrencyStream struct{}

func (concurrencyStream) NoteTurnStarted(string, *domainsubmission.Submission) {}
func (concurrencyStream) DeleteTurnStream(string)                              {}

type concurrencyRuntime struct{ *turnbinding.Tracker }

func (concurrencyRuntime) ClearTurnItemStates(string) {}
