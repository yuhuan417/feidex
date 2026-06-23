package submission

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"feidex/internal/app/appcore"
	"feidex/internal/codexrpc"
	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

func TestStartNextSubmissionAsyncCoalescesConcurrentStarts(t *testing.T) {
	store, err := state.Open(t.TempDir() + "/state.json")
	if err != nil {
		t.Fatalf("Open(store) error = %v", err)
	}
	sessionKey := "sess-1"
	if err := store.UpsertSession(&state.Session{
		Key:         sessionKey,
		WorkspaceID: "default",
		Status:      state.SessionStatusQueued.String(),
		Queue:       []string{"sub-1"},
	}); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}
	if _, err := store.CreateSubmission(&state.Submission{
		ID:         "sub-1",
		SessionKey: sessionKey,
		WorkspaceID: "default",
		Status:     state.SubmissionStatusQueued.String(),
	}); err != nil {
		t.Fatalf("CreateSubmission() error = %v", err)
	}

	app := newConcurrentStartTestApp(store, sessionKey)
	svc := NewSubmissionQueueService(app)

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
	if sess.Status == state.SessionStatusIdle.String() {
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
			store:            store,
			barrierSessionKey: sessionKey,
			barrierReady:      make(chan struct{}),
		},
		backend: &concurrentStartBackend{
			started: make(chan struct{}, 1),
			release: make(chan struct{}),
		},
	}
}

func (a *concurrentStartTestApp) SubmissionQueueAppState() QueueAppStateProvider {
	return &a.state
}

func (a *concurrentStartTestApp) SubmissionQueueSkillResolver() QueueSkillResolver {
	return concurrentStartNoopSkillResolver{}
}

func (a *concurrentStartTestApp) SubmissionQueueAttachmentResolver() QueueAttachmentResolver {
	return concurrentStartNoopAttachmentResolver{}
}

func (a *concurrentStartTestApp) SubmissionQueueLiveThread() QueueLiveThreadProvider {
	return concurrentStartNoopLiveThread{}
}

func (a *concurrentStartTestApp) SubmissionQueuePendingQueue() QueuePendingQueueProvider {
	return concurrentStartNoopPendingQueue{}
}

func (a *concurrentStartTestApp) SubmissionQueueRuntimeState() QueueRuntimeStateProvider {
	return concurrentStartNoopRuntimeState{}
}

func (a *concurrentStartTestApp) SubmissionQueueRuntimeMaintenance() QueueRuntimeMaintenanceProvider {
	return concurrentStartNoopRuntimeMaintenance{}
}

func (a *concurrentStartTestApp) SubmissionQueueReplyContinuation() QueueReplyContinuationProvider {
	return concurrentStartNoopReplyContinuation{}
}

func (a *concurrentStartTestApp) SubmissionQueueTurnStream() QueueTurnStreamProvider {
	return concurrentStartNoopTurnStream{}
}

func (a *concurrentStartTestApp) SubmissionQueueAutoRetry() QueueAutoRetryProvider {
	return concurrentStartNoopAutoRetry{}
}

func (a *concurrentStartTestApp) SubmissionQueueConversationBackend() QueueConversationBackendProvider {
	return a.backend
}

func (a *concurrentStartTestApp) SubmissionQueueBackendRuntime() QueueBackendRuntimeProvider {
	return nil
}

func (a *concurrentStartTestApp) SubmissionQueueDefaultWorkspaceID() string {
	return "default"
}

func (a *concurrentStartTestApp) SubmissionQueueWorkspace(id string) *config.Workspace {
	return &config.Workspace{ID: id, Cwd: "."}
}

func (a *concurrentStartTestApp) SubmissionQueueReplyInThreadEnabled(string) bool {
	return false
}

func (a *concurrentStartTestApp) SubmissionQueueReplyInThreadForSubmission(*state.Submission) bool {
	return false
}

func (a *concurrentStartTestApp) SubmissionQueueConfiguredInflightMode() QueueInflightMode {
	return InflightSingle
}

func (a *concurrentStartTestApp) SubmissionQueueInflightAllowsAdditional(QueueInflightMode) bool {
	return false
}

func (a *concurrentStartTestApp) SubmissionQueueResolveWorkspaceID(_ *feishu.InboundMessage, _ *state.Session, _ bool) string {
	return "default"
}

func (a *concurrentStartTestApp) SubmissionQueueReplyText(context.Context, string, string, bool) error {
	return nil
}

func (a *concurrentStartTestApp) SubmissionQueueSendQueuedNotice(context.Context, *state.Submission) {}

func (a *concurrentStartTestApp) SubmissionQueueSendStartFailureNotice(context.Context, *state.Submission, error, bool) {
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

func (a *concurrentStartTestApp) SubmissionQueueLogSessionState(string, string, *state.Session) {}

func (a *concurrentStartTestApp) SubmissionQueueMarkSubmissionQueuedReactions(*state.Submission) {}

func (a *concurrentStartTestApp) SubmissionQueueMarkSubmissionRunningReactions(*state.Submission) {}

func (a *concurrentStartTestApp) SubmissionQueueClearSubmissionProcessingReactions(*state.Submission) {}

func (a *concurrentStartTestApp) SubmissionQueueIsReviewSubmission(*state.Submission) bool {
	return false
}

func (a *concurrentStartTestApp) SubmissionQueueStartSubmissionTurn(context.Context, string, string, *state.Submission, string, string, string, string, string, string, string) (string, error) {
	return "", nil
}

func (a *concurrentStartTestApp) SubmissionQueueStartSubmissionReview(context.Context, string, *state.Submission) (string, error) {
	return "", nil
}

func (a *concurrentStartTestApp) SubmissionQueueBuildThreadStartParams(*config.Workspace, *state.Session, string) codexrpc.ThreadStartParams {
	return codexrpc.ThreadStartParams{}
}

func (a *concurrentStartTestApp) SubmissionQueueRequireCodexClient() (appcore.CodexClient, error) {
	return concurrentStartNoopCodexClient{}, nil
}

func (a *concurrentStartTestApp) SubmissionQueueClaudeClient() QueueClaudeClient {
	return nil
}

func (a *concurrentStartTestApp) SubmissionQueueConfiguredClaudeModel() string {
	return ""
}

type concurrentStartTestState struct {
	store             *state.Store
	barrierSessionKey string
	mu                sync.Mutex
	sessionCalls      int
	barrierReady      chan struct{}
}

func (s *concurrentStartTestState) Session(key string) *state.Session {
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

func (s *concurrentStartTestState) Submission(id string) *state.Submission {
	return s.store.GetSubmission(id)
}

func (s *concurrentStartTestState) SaveSession(sess *state.Session) error {
	return s.store.UpsertSession(sess)
}

func (s *concurrentStartTestState) CreateSubmission(sub *state.Submission) (string, error) {
	return s.store.CreateSubmission(sub)
}

func (s *concurrentStartTestState) QueueSubmission(sessionKey, id string) error {
	return s.store.QueueSubmission(sessionKey, id)
}

func (s *concurrentStartTestState) DequeueSubmission(sessionKey string) (string, error) {
	return s.store.DequeueSubmission(sessionKey)
}

func (s *concurrentStartTestState) MarkSubmissionRunning(id, threadID, turnID string) error {
	return s.store.UpdateSubmission(id, func(sub *state.Submission) {
		sub.ThreadID = threadID
		sub.TurnID = turnID
		sub.Status = state.SubmissionStatusRunning.String()
	})
}

func (s *concurrentStartTestState) FinalizeSubmission(id, status string) error {
	return s.store.UpdateSubmission(id, func(sub *state.Submission) {
		sub.Status = status
		sub.Finalized = true
	})
}

func (s *concurrentStartTestState) UpdateSession(key string, mutate func(*state.Session)) (*state.Session, error) {
	return s.store.UpdateSession(key, mutate)
}

func (s *concurrentStartTestState) NextLocalID(prefix string) (string, error) {
	return prefix + "-1", nil
}

func (s *concurrentStartTestState) DeletePendingRequests(func(*state.PendingRequest) bool) {}

func (s *concurrentStartTestState) DeleteMessageLinks(func(*state.MessageLink) bool) {}

func (s *concurrentStartTestState) UpdateSubmission(id string, mutate func(*state.Submission)) error {
	return s.store.UpdateSubmission(id, mutate)
}

func (s *concurrentStartTestState) Sessions() []*state.Session {
	return s.store.AllSessions()
}

type concurrentStartBackend struct {
	mu      sync.Mutex
	count   int
	started chan struct{}
	release chan struct{}
}

func (b *concurrentStartBackend) StartQueuedSubmission(string, *state.Session, *state.Submission, *config.Workspace, bool) error {
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

type concurrentStartNoopSkillResolver struct{}

func (concurrentStartNoopSkillResolver) ResolveSubmissionSkill(string, string, string, []state.SubmissionAttachment) QueueSkillResolution {
	return QueueSkillResolution{}
}
func (concurrentStartNoopSkillResolver) SetSessionPendingSkill(string, state.SubmissionSkill) {}
func (concurrentStartNoopSkillResolver) ClearSessionPendingSkill(string)                {}

type concurrentStartNoopAttachmentResolver struct{}

func (concurrentStartNoopAttachmentResolver) ResolveInboundAttachments(*feishu.InboundMessage, string, string) ([]state.SubmissionAttachment, error) {
	return nil, nil
}

type concurrentStartNoopLiveThread struct{}

func (concurrentStartNoopLiveThread) MarkSessionThreadLive(string, string) {}
func (concurrentStartNoopLiveThread) SessionHasLiveThread(string, string) bool {
	return false
}
func (concurrentStartNoopLiveThread) ClearSessionLiveThread(string) {}

type concurrentStartNoopPendingQueue struct{}

func (concurrentStartNoopPendingQueue) PendingInputSessionKey(*feishu.InboundMessage) string {
	return ""
}
func (concurrentStartNoopPendingQueue) CollectPendingStagedImages(string, string) []state.SessionStagedImage {
	return nil
}
func (concurrentStartNoopPendingQueue) ClearPendingStagedImages(string, string) error {
	return nil
}

type concurrentStartNoopRuntimeState struct{}

func (concurrentStartNoopRuntimeState) NotePendingTurnBinding(string, string, string) {}
func (concurrentStartNoopRuntimeState) ClearPendingTurnBindingForSubmission(string, string) {}
func (concurrentStartNoopRuntimeState) BindTurnSubmission(string, string, string, string)   {}
func (concurrentStartNoopRuntimeState) MarkTurnStartedAt(string, time.Time)                  {}
func (concurrentStartNoopRuntimeState) ClearTurnBinding(string)                              {}
func (concurrentStartNoopRuntimeState) ClearTurnItemStates(string)                           {}
func (concurrentStartNoopRuntimeState) BoundSubmissionForTurn(string) (string, *state.Submission) {
	return "", nil
}

type concurrentStartNoopRuntimeMaintenance struct{}

func (concurrentStartNoopRuntimeMaintenance) CleanupSubmissionRuntimeState(*state.Submission) {}

type concurrentStartNoopReplyContinuation struct{}

func (concurrentStartNoopReplyContinuation) RecordSubmissionSourceLinks(*state.Submission) {}
func (concurrentStartNoopReplyContinuation) RecordRootTurnBinding(string, string, string, string) {
}

type concurrentStartNoopTurnStream struct{}

func (concurrentStartNoopTurnStream) NoteTurnStarted(string, *state.Submission) {}
func (concurrentStartNoopTurnStream) DeleteTurnStream(string)                    {}

type concurrentStartNoopAutoRetry struct{}

func (concurrentStartNoopAutoRetry) ObserveAutoRetryTerminal(string, string, string, *state.Session, *state.Submission, string) bool {
	return false
}

type concurrentStartNoopCodexClient struct{}

func (concurrentStartNoopCodexClient) SetHandlers(func(string, json.RawMessage), func(codexrpc.RequestEnvelope)) {
}
func (concurrentStartNoopCodexClient) Start(context.Context, bool) error { return nil }
func (concurrentStartNoopCodexClient) Close() error                       { return nil }
func (concurrentStartNoopCodexClient) Call(context.Context, string, any, any) error {
	return nil
}
func (concurrentStartNoopCodexClient) Reply(json.RawMessage, any) error { return nil }
func (concurrentStartNoopCodexClient) ReplyError(json.RawMessage, int, string) error {
	return nil
}
