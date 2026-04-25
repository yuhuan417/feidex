package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"feidex/internal/claudecli"
	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

type fakeClaudeEnsureCall struct {
	sessionKey  string
	workspaceID string
	resumeID    string
	model       string
}

type fakeClaudeForkCall struct {
	sessionKey      string
	workspaceID     string
	sourceSessionID string
	model           string
}

type fakeClaudeStartTurnCall struct {
	sessionKey string
	threadID   string
	turnID     string
	prompt     string
}

type fakeClaudeSetModelCall struct {
	sessionKey string
	model      string
}

type fakeClaudeSetEffortCall struct {
	sessionKey string
	effort     string
}

type fakeClaudePermissionModeCall struct {
	sessionKey string
	mode       string
}

type fakeClaudeApprovalCall struct {
	requestID  string
	resolution claudeApprovalResolution
}

type fakeClaudeUserInputCall struct {
	requestID string
	answers   map[string]string
}

type fakeClaudePlanCall struct {
	requestID string
	feedback  string
}

type fakeClaudeCancelCall struct {
	requestID string
	message   string
}

type fakeClaudeCore struct {
	mu               sync.Mutex
	ensureSessionID  string
	ensureSessionSet bool
	ensureSessionErr error
	forkSessionID    string
	forkSessionSet   bool
	forkSessionErr   error
	startTurnErr     error
	interruptErr     error
	setModelErr      error
	setEffortErr     error
	approvalErr      error
	userInputErr     error
	planErr          error
	cancelErr        error

	resetCalls int
	closed     bool
	resetKeys  []string

	updatedConfigs []config.ClaudeConfig

	ensureResults    []fakeClaudeEnsureResult
	forkResults      []fakeClaudeEnsureResult
	startTurnResults []error

	ensureCalls         []fakeClaudeEnsureCall
	forkCalls           []fakeClaudeForkCall
	startTurnCalls      []fakeClaudeStartTurnCall
	interruptCalls      []string
	setModelCalls       []fakeClaudeSetModelCall
	setEffortCalls      []fakeClaudeSetEffortCall
	setModelApplied     bool
	setEffortApplied    bool
	permissionModeCalls []fakeClaudePermissionModeCall
	approvalCalls       []fakeClaudeApprovalCall
	userInputCalls      []fakeClaudeUserInputCall
	planCalls           []fakeClaudePlanCall
	cancelCalls         []fakeClaudeCancelCall
}

type fakeClaudeEnsureResult struct {
	id  string
	err error
}

func (f *fakeClaudeCore) EnsureSession(_ context.Context, sessionKey string, ws *config.Workspace, resumeID, model string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ensureCalls = append(f.ensureCalls, fakeClaudeEnsureCall{
		sessionKey:  sessionKey,
		workspaceID: ws.ID,
		resumeID:    resumeID,
		model:       model,
	})
	if len(f.ensureResults) > 0 {
		result := f.ensureResults[0]
		f.ensureResults = append([]fakeClaudeEnsureResult(nil), f.ensureResults[1:]...)
		return strings.TrimSpace(result.id), result.err
	}
	if f.ensureSessionErr != nil {
		return "", f.ensureSessionErr
	}
	if f.ensureSessionSet {
		return strings.TrimSpace(f.ensureSessionID), nil
	}
	if strings.TrimSpace(f.ensureSessionID) == "" {
		return "claude-session-1", nil
	}
	return f.ensureSessionID, nil
}

func (f *fakeClaudeCore) ForkSession(_ context.Context, sessionKey string, ws *config.Workspace, sourceSessionID, model string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.forkCalls = append(f.forkCalls, fakeClaudeForkCall{
		sessionKey:      sessionKey,
		workspaceID:     ws.ID,
		sourceSessionID: sourceSessionID,
		model:           model,
	})
	if len(f.forkResults) > 0 {
		result := f.forkResults[0]
		f.forkResults = append([]fakeClaudeEnsureResult(nil), f.forkResults[1:]...)
		return strings.TrimSpace(result.id), result.err
	}
	if f.forkSessionErr != nil {
		return "", f.forkSessionErr
	}
	if f.forkSessionSet {
		return strings.TrimSpace(f.forkSessionID), nil
	}
	if strings.TrimSpace(f.forkSessionID) == "" {
		return "claude-fork-1", nil
	}
	return f.forkSessionID, nil
}

func (f *fakeClaudeCore) ResetSession(sessionKey string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.resetCalls++
	f.resetKeys = append(f.resetKeys, strings.TrimSpace(sessionKey))
	return nil
}

func (f *fakeClaudeCore) UpdateConfig(cfg config.ClaudeConfig) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.updatedConfigs = append(f.updatedConfigs, cfg)
}

func (f *fakeClaudeCore) StartTurn(_ context.Context, sessionKey, threadID, turnID, prompt string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.startTurnCalls = append(f.startTurnCalls, fakeClaudeStartTurnCall{
		sessionKey: sessionKey,
		threadID:   threadID,
		turnID:     turnID,
		prompt:     prompt,
	})
	if len(f.startTurnResults) > 0 {
		result := f.startTurnResults[0]
		f.startTurnResults = append([]error(nil), f.startTurnResults[1:]...)
		return result
	}
	return f.startTurnErr
}

func (f *fakeClaudeCore) Interrupt(_ context.Context, sessionKey string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.interruptCalls = append(f.interruptCalls, sessionKey)
	return f.interruptErr
}

func (f *fakeClaudeCore) SetModel(_ context.Context, sessionKey, model string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.setModelCalls = append(f.setModelCalls, fakeClaudeSetModelCall{
		sessionKey: sessionKey,
		model:      model,
	})
	return f.setModelApplied, f.setModelErr
}

func (f *fakeClaudeCore) SetEffort(_ context.Context, sessionKey, effort string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.setEffortCalls = append(f.setEffortCalls, fakeClaudeSetEffortCall{
		sessionKey: sessionKey,
		effort:     effort,
	})
	return f.setEffortApplied, f.setEffortErr
}

func (f *fakeClaudeCore) SetPermissionMode(_ context.Context, sessionKey, mode string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.permissionModeCalls = append(f.permissionModeCalls, fakeClaudePermissionModeCall{
		sessionKey: sessionKey,
		mode:       mode,
	})
	return nil
}

func (f *fakeClaudeCore) ResolveApproval(requestID string, resolution claudeApprovalResolution) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.approvalCalls = append(f.approvalCalls, fakeClaudeApprovalCall{
		requestID:  requestID,
		resolution: resolution,
	})
	return f.approvalErr
}

func (f *fakeClaudeCore) ResolveUserInput(requestID string, answers map[string]string) error {
	cp := map[string]string{}
	for k, v := range answers {
		cp[k] = v
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.userInputCalls = append(f.userInputCalls, fakeClaudeUserInputCall{
		requestID: requestID,
		answers:   cp,
	})
	return f.userInputErr
}

func (f *fakeClaudeCore) ResolvePlanFeedback(requestID, feedback string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.planCalls = append(f.planCalls, fakeClaudePlanCall{
		requestID: requestID,
		feedback:  feedback,
	})
	return f.planErr
}

func (f *fakeClaudeCore) CancelPending(requestID, message string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cancelCalls = append(f.cancelCalls, fakeClaudeCancelCall{
		requestID: requestID,
		message:   message,
	})
	return f.cancelErr
}

func (f *fakeClaudeCore) SessionStopped(_ string) bool {
	return false
}

func (f *fakeClaudeCore) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	return nil
}

func (f *fakeClaudeCore) ensureCallsSnapshot() []fakeClaudeEnsureCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]fakeClaudeEnsureCall(nil), f.ensureCalls...)
}

func (f *fakeClaudeCore) startTurnCallsSnapshot() []fakeClaudeStartTurnCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]fakeClaudeStartTurnCall(nil), f.startTurnCalls...)
}

func (f *fakeClaudeCore) interruptCallsSnapshot() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.interruptCalls...)
}

func (f *fakeClaudeCore) updatedConfigsSnapshot() []config.ClaudeConfig {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]config.ClaudeConfig(nil), f.updatedConfigs...)
}

func (f *fakeClaudeCore) approvalCallsSnapshot() []fakeClaudeApprovalCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]fakeClaudeApprovalCall(nil), f.approvalCalls...)
}

func (f *fakeClaudeCore) userInputCallsSnapshot() []fakeClaudeUserInputCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]fakeClaudeUserInputCall, len(f.userInputCalls))
	copy(out, f.userInputCalls)
	for i := range out {
		if out[i].answers == nil {
			continue
		}
		cp := make(map[string]string, len(out[i].answers))
		for key, value := range out[i].answers {
			cp[key] = value
		}
		out[i].answers = cp
	}
	return out
}

func (f *fakeClaudeCore) planCallsSnapshot() []fakeClaudePlanCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]fakeClaudePlanCall(nil), f.planCalls...)
}

func (f *fakeClaudeCore) cancelCallsSnapshot() []fakeClaudeCancelCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]fakeClaudeCancelCall(nil), f.cancelCalls...)
}

func TestStartNextSubmissionClaudeStartsTurnAndBindsSession(t *testing.T) {
	a, _, _ := newTestApp(t)
	a.cfg.Feishu.Backend = backendClaude
	a.codex = nil
	claude := &fakeClaudeCore{ensureSessionID: "claude-session-42"}
	a.claude = claude

	sessionKey := "feishu:p2p:chat:user"
	if err := a.store.UpsertSession(&state.Session{
		Key:                     sessionKey,
		WorkspaceID:             a.cfg.Workspaces[0].ID,
		ActiveThreadID:          "claude-prev",
		ActiveThreadWorkspaceID: a.cfg.Workspaces[0].ID,
		OwnerUserID:             "user",
		ChatID:                  "chat",
		ChatType:                "p2p",
		RootMessageID:           "root-1",
		Status:                  "idle",
	}); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}
	subID, err := a.store.CreateSubmission(&state.Submission{
		SessionKey:       sessionKey,
		WorkspaceID:      a.cfg.Workspaces[0].ID,
		UserID:           "user",
		ChatID:           "chat",
		TriggerMessageID: "msg-1",
		InputText:        "hello Claude",
		Status:           "queued",
	})
	if err != nil {
		t.Fatalf("CreateSubmission() error = %v", err)
	}
	if err := a.store.QueueSubmission(sessionKey, subID); err != nil {
		t.Fatalf("QueueSubmission() error = %v", err)
	}

	if err := startNextSubmission(a, sessionKey); err != nil {
		t.Fatalf("startNextSubmission() error = %v", err)
	}
	if len(claude.ensureCalls) != 1 {
		t.Fatalf("ensure calls = %d, want 1", len(claude.ensureCalls))
	}
	if claude.ensureCalls[0].resumeID != "claude-prev" {
		t.Fatalf("resumeID = %q, want claude-prev", claude.ensureCalls[0].resumeID)
	}
	if len(claude.startTurnCalls) != 1 {
		t.Fatalf("startTurn calls = %d, want 1", len(claude.startTurnCalls))
	}
	if !strings.Contains(claude.startTurnCalls[0].prompt, "hello Claude") {
		t.Fatalf("startTurn prompt = %q, want input text", claude.startTurnCalls[0].prompt)
	}

	sess := a.store.GetSession(sessionKey)
	if sess == nil {
		t.Fatal("session missing after start")
	}
	if sess.ActiveThreadID != "claude-session-42" || sess.ActiveTurnID == "" || sess.ActiveSubmissionID != subID || sess.Status != "turn_in_progress" {
		t.Fatalf("session after Claude start = %+v", sess)
	}

	sub := a.store.GetSubmission(subID)
	if sub == nil {
		t.Fatal("submission missing after start")
	}
	if sub.ThreadID != "claude-session-42" || sub.TurnID == "" || sub.Status != "running" {
		t.Fatalf("submission after Claude start = %+v", sub)
	}
}

func TestHandleFeishuMessageClaudeQueuesOrdinaryFollowupAndShowsQueuedCard(t *testing.T) {
	a, ff, _ := newTestApp(t)
	a.cfg.Feishu.Backend = backendClaude
	a.codex = nil
	claude := &fakeClaudeCore{ensureSessionID: "claude-thread-1"}
	a.claude = claude

	sessionKey := "feishu:p2p:chat:user"
	if err := a.store.UpsertSession(&state.Session{
		Key:                     sessionKey,
		WorkspaceID:             a.cfg.Workspaces[0].ID,
		ActiveThreadID:          "claude-thread-1",
		ActiveThreadWorkspaceID: a.cfg.Workspaces[0].ID,
		ActiveTurnID:            "claude-turn-current",
		ActiveSubmissionID:      "sub-running",
		OwnerUserID:             "user",
		ChatID:                  "chat",
		ChatType:                "p2p",
		RootMessageID:           "root-1",
		Status:                  "turn_in_progress",
		ActiveOperations: []state.SessionActiveOperation{{
			Kind:         sessionOpKindSubmission,
			SubmissionID: "sub-running",
			ThreadID:     "claude-thread-1",
			TurnID:       "claude-turn-current",
		}},
	}); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}
	if _, err := a.store.CreateSubmission(&state.Submission{
		ID:               "sub-running",
		SessionKey:       sessionKey,
		WorkspaceID:      a.cfg.Workspaces[0].ID,
		ThreadID:         "claude-thread-1",
		TurnID:           "claude-turn-current",
		UserID:           "user",
		ChatID:           "chat",
		TriggerMessageID: "msg-running",
		InputText:        "running",
		Status:           "running",
	}); err != nil {
		t.Fatalf("CreateSubmission(sub-running) error = %v", err)
	}

	a.HandleFeishuMessage(&feishu.InboundMessage{
		MessageID: "msg-queued",
		ChatID:    "chat",
		ChatType:  "p2p",
		UserID:    "user",
		Text:      "follow-up task",
	})

	if len(claude.startTurnCalls) != 0 {
		t.Fatalf("ordinary follow-up should not start immediately, startTurnCalls = %#v", claude.startTurnCalls)
	}
	sess := a.store.GetSession(sessionKey)
	if sess == nil || len(sess.Queue) != 1 || sess.ActiveSubmissionID != "sub-running" || sess.ActiveTurnID != "claude-turn-current" || sess.Status != "queued" {
		t.Fatalf("session after queued Claude follow-up = %+v", sess)
	}
	queuedSubID := strings.TrimSpace(sess.Queue[0])
	queuedSub := a.store.GetSubmission(queuedSubID)
	if queuedSub == nil || queuedSub.InputText != "follow-up task" || queuedSub.Status != "queued" {
		t.Fatalf("queued Claude submission = %+v", queuedSub)
	}
	if replyCards := ff.replyCardsSnapshot(); len(replyCards) == 0 || !strings.Contains(cardMarkdownContent(t, replyCards[len(replyCards)-1]), "已加入队列") {
		t.Fatalf("queued notice cards = %+v", replyCards)
	}

	finishTurn(a, "claude-thread-1", "claude-turn-current", "completed")

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		sess = a.store.GetSession(sessionKey)
		queuedSub = a.store.GetSubmission(queuedSubID)
		startTurnCalls := claude.startTurnCallsSnapshot()
		if len(startTurnCalls) == 1 &&
			sess != nil &&
			queuedSub != nil &&
			sess.ActiveSubmissionID == queuedSubID &&
			sess.ActiveTurnID == queuedSub.TurnID &&
			sess.Status == "turn_in_progress" &&
			queuedSub.ThreadID == "claude-thread-1" &&
			queuedSub.Status == "running" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	ensureCalls := claude.ensureCallsSnapshot()
	if len(ensureCalls) != 1 || ensureCalls[0].resumeID != "claude-thread-1" {
		t.Fatalf("ensure calls after queued Claude follow-up = %#v", ensureCalls)
	}
	startTurnCalls := claude.startTurnCallsSnapshot()
	if len(startTurnCalls) != 1 || !strings.Contains(startTurnCalls[0].prompt, "follow-up task") {
		t.Fatalf("startTurn calls after queued Claude follow-up = %#v", startTurnCalls)
	}
	sess = a.store.GetSession(sessionKey)
	if sess == nil || sess.ActiveSubmissionID != queuedSubID || sess.Status != "turn_in_progress" {
		t.Fatalf("session after queued Claude follow-up start = %+v", sess)
	}
	if replyCards := ff.replyCardsSnapshot(); len(replyCards) < 2 || !strings.Contains(cardMarkdownContent(t, replyCards[len(replyCards)-1]), "已轮到这条消息") {
		t.Fatalf("started notice cards = %+v", replyCards)
	}
}

func TestStartNextSubmissionClaudeRetriesFreshSessionAfterResumedStartFailure(t *testing.T) {
	a, _, _ := newTestApp(t)
	a.cfg.Feishu.Backend = backendClaude
	a.codex = nil
	claude := &fakeClaudeCore{
		ensureResults: []fakeClaudeEnsureResult{
			{id: "claude-stale"},
			{id: "claude-fresh"},
		},
		startTurnResults: []error{
			errors.New("process error: Claude resume session became unavailable: exit status 1"),
			nil,
		},
	}
	a.claude = claude

	sessionKey := "feishu:p2p:chat:user"
	if err := a.store.UpsertSession(&state.Session{
		Key:                     sessionKey,
		WorkspaceID:             a.cfg.Workspaces[0].ID,
		ActiveThreadID:          "claude-stale",
		ActiveThreadWorkspaceID: a.cfg.Workspaces[0].ID,
		OwnerUserID:             "user",
		ChatID:                  "chat",
		ChatType:                "p2p",
		RootMessageID:           "root-1",
		Status:                  "idle",
	}); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}
	subID, err := a.store.CreateSubmission(&state.Submission{
		SessionKey:       sessionKey,
		WorkspaceID:      a.cfg.Workspaces[0].ID,
		UserID:           "user",
		ChatID:           "chat",
		TriggerMessageID: "msg-1",
		InputText:        "hello Claude",
		Status:           "queued",
	})
	if err != nil {
		t.Fatalf("CreateSubmission() error = %v", err)
	}
	if err := a.store.QueueSubmission(sessionKey, subID); err != nil {
		t.Fatalf("QueueSubmission() error = %v", err)
	}

	if err := startNextSubmission(a, sessionKey); err != nil {
		t.Fatalf("startNextSubmission() error = %v", err)
	}
	if len(claude.ensureCalls) != 2 {
		t.Fatalf("ensure calls = %d, want 2", len(claude.ensureCalls))
	}
	if claude.ensureCalls[0].resumeID != "claude-stale" {
		t.Fatalf("first resumeID = %q, want claude-stale", claude.ensureCalls[0].resumeID)
	}
	if claude.ensureCalls[1].resumeID != "" {
		t.Fatalf("second resumeID = %q, want empty for fresh retry", claude.ensureCalls[1].resumeID)
	}
	if len(claude.startTurnCalls) != 2 {
		t.Fatalf("startTurn calls = %d, want 2", len(claude.startTurnCalls))
	}
	if claude.startTurnCalls[0].threadID != "claude-stale" {
		t.Fatalf("first startTurn threadID = %q, want claude-stale", claude.startTurnCalls[0].threadID)
	}
	if claude.startTurnCalls[1].threadID != "claude-fresh" {
		t.Fatalf("second startTurn threadID = %q, want claude-fresh", claude.startTurnCalls[1].threadID)
	}
	if claude.startTurnCalls[0].turnID == claude.startTurnCalls[1].turnID {
		t.Fatalf("retry reused Claude turnID %q, want a fresh local turn id", claude.startTurnCalls[0].turnID)
	}
	sess := a.store.GetSession(sessionKey)
	if sess == nil {
		t.Fatal("session missing after retry")
	}
	if sess.ActiveThreadID != "claude-fresh" || sess.ActiveTurnID == "" || sess.ActiveSubmissionID != subID || sess.Status != "turn_in_progress" {
		t.Fatalf("session after retry = %+v", sess)
	}

	sub := a.store.GetSubmission(subID)
	if sub == nil {
		t.Fatal("submission missing after retry")
	}
	if sub.ThreadID != "claude-fresh" || sub.TurnID == "" || sub.Status != "running" {
		t.Fatalf("submission after retry = %+v", sub)
	}
	if sub.TurnID != claude.startTurnCalls[1].turnID {
		t.Fatalf("submission turnID = %q, want retried turn id %q", sub.TurnID, claude.startTurnCalls[1].turnID)
	}
}

func TestClaudeHandleTurnCompleteSuppressesFailedCompletionDuringStart(t *testing.T) {
	a, _, _ := newTestApp(t)
	a.cfg.Feishu.Backend = backendClaude
	runtime := newClaudeRuntime(a, a.cfg.Claude).(*claudeRuntime)

	sessionKey := "feishu:p2p:chat:user"
	if err := a.store.UpsertSession(&state.Session{
		Key:                sessionKey,
		WorkspaceID:        a.cfg.Workspaces[0].ID,
		ActiveThreadID:     "claude-stale",
		ActiveTurnID:       "claude-turn-1",
		ActiveSubmissionID: "sub-1",
		OwnerUserID:        "user",
		ChatID:             "chat",
		ChatType:           "p2p",
		Status:             "turn_in_progress",
		ActiveOperations: []state.SessionActiveOperation{{
			Kind:         sessionOpKindSubmission,
			SubmissionID: "sub-1",
			ThreadID:     "claude-stale",
			TurnID:       "claude-turn-1",
		}},
	}); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}
	if _, err := a.store.CreateSubmission(&state.Submission{
		ID:               "sub-1",
		SessionKey:       sessionKey,
		WorkspaceID:      a.cfg.Workspaces[0].ID,
		ThreadID:         "claude-stale",
		TurnID:           "claude-turn-1",
		UserID:           "user",
		ChatID:           "chat",
		TriggerMessageID: "msg-1",
		InputText:        "hello Claude",
		Status:           "running",
	}); err != nil {
		t.Fatalf("CreateSubmission() error = %v", err)
	}
	newRuntimeStateService(a).bindTurnSubmission("claude-stale", "claude-turn-1", sessionKey, "sub-1")

	state := &claudeSessionState{
		sessionKey: sessionKey,
		sessionID:  "claude-stale",
		turns: map[int]*claudeTurnState{
			1: {
				TurnNumber:               1,
				TurnID:                   "claude-turn-1",
				SuppressFailedCompletion: true,
			},
		},
	}

	runtime.handleTurnComplete(state, claudecli.TurnCompleteEvent{
		TurnNumber: 1,
		Success:    false,
		Error:      errors.New("No conversation found with session ID: stale"),
	})

	sub := a.store.GetSubmission("sub-1")
	if sub == nil || sub.Finalized || sub.Status != "running" {
		t.Fatalf("submission after suppressed completion = %+v", sub)
	}
	if _, bound := newRuntimeStateService(a).boundSubmissionForTurn("claude-turn-1"); bound == nil {
		t.Fatalf("turn binding should remain until retry cleanup")
	}
	if state.turns[1] != nil {
		t.Fatalf("turn state should be cleared after suppressed completion: %+v", state.turns[1])
	}
}

func TestStartNextSubmissionClaudeBindsThreadAfterReady(t *testing.T) {
	a, _, _ := newTestApp(t)
	a.cfg.Feishu.Backend = backendClaude
	a.codex = nil
	claude := &fakeClaudeCore{ensureSessionSet: true}
	a.claude = claude

	sessionKey := "feishu:group:chat-1:root:root-1"
	if err := a.store.UpsertSession(&state.Session{
		Key:           sessionKey,
		WorkspaceID:   a.cfg.Workspaces[0].ID,
		OwnerUserID:   "user",
		ChatID:        "chat-1",
		ChatType:      "group",
		RootMessageID: "root-1",
		Status:        "idle",
	}); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}
	subID, err := a.store.CreateSubmission(&state.Submission{
		SessionKey:           sessionKey,
		WorkspaceID:          a.cfg.Workspaces[0].ID,
		UserID:               "user",
		ChatID:               "chat-1",
		TriggerMessageID:     "msg-1",
		SourceMessageIDs:     []string{"msg-1"},
		SourceRootMessageIDs: []string{"root-1"},
		InputText:            "hello Claude",
		Status:               "queued",
	})
	if err != nil {
		t.Fatalf("CreateSubmission() error = %v", err)
	}
	if err := a.store.QueueSubmission(sessionKey, subID); err != nil {
		t.Fatalf("QueueSubmission() error = %v", err)
	}

	if err := startNextSubmission(a, sessionKey); err != nil {
		t.Fatalf("startNextSubmission() error = %v", err)
	}

	sess := a.store.GetSession(sessionKey)
	if sess == nil {
		t.Fatal("session missing after start")
	}
	if sess.ActiveThreadID != "" || sess.ActiveTurnID == "" || sess.ActiveSubmissionID != subID || sess.Status != "turn_in_progress" {
		t.Fatalf("session before Claude ready = %+v", sess)
	}

	sub := a.store.GetSubmission(subID)
	if sub == nil {
		t.Fatal("submission missing after start")
	}
	if sub.ThreadID != "" || sub.TurnID == "" || sub.Status != "running" {
		t.Fatalf("submission before Claude ready = %+v", sub)
	}

	bindClaudeSessionThread(a, sessionKey, sub.TurnID, "claude-session-ready")

	sess = a.store.GetSession(sessionKey)
	if sess == nil || sess.ActiveThreadID != "claude-session-ready" {
		t.Fatalf("session after Claude ready = %+v", sess)
	}

	sub = a.store.GetSubmission(subID)
	if sub == nil || sub.ThreadID != "claude-session-ready" {
		t.Fatalf("submission after Claude ready = %+v", sub)
	}

	binding := newRuntimeStateService(a).turnBindingTracker().bindings[sub.TurnID]
	if binding.ThreadID != "claude-session-ready" {
		t.Fatalf("turn binding after Claude ready = %+v", binding)
	}

	rootLink := a.store.GetMessageLink("root-1")
	if rootLink == nil || rootLink.ThreadID != "claude-session-ready" || rootLink.TurnID != sub.TurnID {
		t.Fatalf("root link after Claude ready = %+v", rootLink)
	}
}

func TestBindClaudeSessionThreadReadyDoesNotClearRootTurnBinding(t *testing.T) {
	a, _, _ := newTestApp(t)
	a.cfg.Feishu.Backend = backendClaude

	sessionKey := "feishu:group:chat-1:root:root-1"
	subID, err := a.store.CreateSubmission(&state.Submission{
		SessionKey:           sessionKey,
		WorkspaceID:          a.cfg.Workspaces[0].ID,
		UserID:               "user",
		ChatID:               "chat-1",
		TriggerMessageID:     "msg-1",
		SourceMessageIDs:     []string{"msg-1"},
		SourceRootMessageIDs: []string{"root-1"},
		InputText:            "hello Claude",
		TurnID:               "claude-turn-1",
		Status:               "running",
	})
	if err != nil {
		t.Fatalf("CreateSubmission() error = %v", err)
	}
	if err := a.store.UpsertSession(&state.Session{
		Key:           sessionKey,
		WorkspaceID:   a.cfg.Workspaces[0].ID,
		OwnerUserID:   "user",
		ChatID:        "chat-1",
		ChatType:      "group",
		RootMessageID: "root-1",
		Status:        "turn_in_progress",
		ActiveOperations: []state.SessionActiveOperation{{
			Kind:         sessionOpKindSubmission,
			SubmissionID: subID,
			TurnID:       "claude-turn-1",
		}},
	}); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}
	if err := a.store.UpsertMessageLink(&state.MessageLink{
		MessageID:  "root-1",
		SessionKey: sessionKey,
		TurnID:     "claude-turn-1",
	}); err != nil {
		t.Fatalf("UpsertMessageLink() error = %v", err)
	}

	bindClaudeSessionThread(a, sessionKey, "", "claude-session-ready")

	rootLink := a.store.GetMessageLink("root-1")
	if rootLink == nil || rootLink.ThreadID != "claude-session-ready" || rootLink.TurnID != "claude-turn-1" {
		t.Fatalf("root link after ready bind = %+v", rootLink)
	}
}

func TestStartNextSubmissionClaudeKeepsQueuedFollowupPendingWhileTurnActive(t *testing.T) {
	a, _, _ := newTestApp(t)
	a.cfg.Feishu.Backend = backendClaude
	a.codex = nil
	claude := &fakeClaudeCore{ensureSessionID: "claude-thread-1"}
	a.claude = claude

	sessionKey := "feishu:group:chat-1:root:root-1"
	if err := a.store.UpsertSession(&state.Session{
		Key:                     sessionKey,
		WorkspaceID:             a.cfg.Workspaces[0].ID,
		ActiveThreadID:          "claude-thread-1",
		ActiveThreadWorkspaceID: a.cfg.Workspaces[0].ID,
		ActiveTurnID:            "claude-turn-1",
		ActiveSubmissionID:      "sub-running",
		OwnerUserID:             "user",
		ChatID:                  "chat-1",
		ChatType:                "group",
		RootMessageID:           "root-1",
		Status:                  "turn_in_progress",
	}); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}
	if _, err := a.store.CreateSubmission(&state.Submission{
		ID:                   "sub-running",
		SessionKey:           sessionKey,
		WorkspaceID:          a.cfg.Workspaces[0].ID,
		UserID:               "user",
		ChatID:               "chat-1",
		TriggerMessageID:     "msg-running",
		SourceMessageIDs:     []string{"msg-running"},
		SourceRootMessageIDs: []string{"root-1"},
		InputText:            "running",
		ThreadID:             "claude-thread-1",
		TurnID:               "claude-turn-1",
		Status:               "running",
	}); err != nil {
		t.Fatalf("CreateSubmission(sub-running) error = %v", err)
	}
	subID, err := a.store.CreateSubmission(&state.Submission{
		SessionKey:           sessionKey,
		WorkspaceID:          a.cfg.Workspaces[0].ID,
		UserID:               "user",
		ChatID:               "chat-1",
		TriggerMessageID:     "msg-2",
		SourceMessageIDs:     []string{"msg-2"},
		SourceRootMessageIDs: []string{"root-1"},
		InputText:            "follow up",
		Status:               "queued",
	})
	if err != nil {
		t.Fatalf("CreateSubmission() error = %v", err)
	}
	if err := a.store.QueueSubmission(sessionKey, subID); err != nil {
		t.Fatalf("QueueSubmission() error = %v", err)
	}

	if err := startNextSubmission(a, sessionKey); err != nil {
		t.Fatalf("startNextSubmission() error = %v", err)
	}
	if len(claude.ensureCalls) != 0 {
		t.Fatalf("ensure calls = %#v, want no start while another turn is active", claude.ensureCalls)
	}
	if len(claude.startTurnCalls) != 0 {
		t.Fatalf("startTurn calls = %#v, want queued follow-up to remain pending", claude.startTurnCalls)
	}

	sess := a.store.GetSession(sessionKey)
	if sess == nil {
		t.Fatal("session missing after queued start check")
	}
	if len(sess.Queue) != 1 || sess.ActiveTurnID != "claude-turn-1" || sess.ActiveSubmissionID != "sub-running" || sess.Status != "turn_in_progress" {
		t.Fatalf("session after queued Claude follow-up check = %+v", sess)
	}

	sub := a.store.GetSubmission(subID)
	if sub == nil || sub.ThreadID != "" || sub.TurnID != "" || sub.Status != "queued" {
		t.Fatalf("submission after queued Claude follow-up check = %+v", sub)
	}
}

func TestCompleteApprovalActionUsesClaudeResolver(t *testing.T) {
	a, ff, _ := newTestApp(t)
	a.cfg.Feishu.Backend = backendClaude
	a.codex = nil
	claude := &fakeClaudeCore{}
	a.claude = claude
	a.feishu = ff

	if err := a.store.UpsertPending(&state.PendingRequest{
		ID:          "approve-1",
		Backend:     backendClaude,
		Kind:        "command",
		OwnerUserID: "user-1",
		PayloadJSON: mustJSON(map[string]any{"body": "命令审批"}),
		Status:      "pending",
	}); err != nil {
		t.Fatalf("UpsertPending() error = %v", err)
	}

	resp, err := completeApprovalAction(a, &feishu.CardAction{
		UserID:      "user-1",
		ActionValue: map[string]any{"request_id": "approve-1"},
	}, "approval.command.accept_session")
	if err != nil {
		t.Fatalf("completeApprovalAction() error = %v", err)
	}
	if resp == nil || resp.Toast == nil || resp.Toast.Type != "success" {
		t.Fatalf("approval response = %#v, want success toast", resp)
	}
	if len(claude.approvalCalls) != 1 {
		t.Fatalf("approval calls = %d, want 1", len(claude.approvalCalls))
	}
	if claude.approvalCalls[0].requestID != "approve-1" {
		t.Fatalf("approval request id = %q, want approve-1", claude.approvalCalls[0].requestID)
	}
	if claude.approvalCalls[0].resolution.Behavior != "allow" || claude.approvalCalls[0].resolution.Scope != "session" {
		t.Fatalf("approval resolution = %+v", claude.approvalCalls[0].resolution)
	}
	pending := a.store.PendingByID("approve-1")
	if pending == nil || pending.Status != "resolved" {
		t.Fatalf("pending after approval = %+v, want resolved", pending)
	}
}

func TestSendClaudePendingCardsStoreBackendAndStatus(t *testing.T) {
	a, ff, _ := newTestApp(t)
	a.cfg.Feishu.Backend = backendClaude
	a.codex = nil

	sub := seedActiveSubmission(t, a, "sess-1", "claude-thread-1", "claude-turn-1")

	if err := sendClaudeApprovalCardWithPayload(a,
		"command",
		"approve-card-1",
		"sess-1",
		sub,
		"claude-thread-1",
		"claude-turn-1",
		"item-1",
		"需要审批",
		map[string]any{"command": "pwd"},
		"允许本会话",
	); err != nil {
		t.Fatalf("sendClaudeApprovalCardWithPayload() error = %v", err)
	}
	if pending := a.store.PendingByID("approve-card-1"); pending == nil || pending.Backend != backendClaude || pending.Kind != "command" || pending.Status != "pending" {
		t.Fatalf("approval pending = %+v, want Claude pending command", pending)
	}

	if err := sendClaudePlanModeCard(a, "plan-card-1", "sess-1", sub, "claude-thread-1", "claude-turn-1", "plan body"); err != nil {
		t.Fatalf("sendClaudePlanModeCard() error = %v", err)
	}
	if pending := a.store.PendingByID("plan-card-1"); pending == nil || pending.Backend != backendClaude || pending.Kind != claudePlanModePendingKind || pending.Status != "pending" {
		t.Fatalf("plan pending = %+v, want Claude plan pending", pending)
	}
	if len(ff.sendCards) != 2 {
		t.Fatalf("sendCards = %d, want 2", len(ff.sendCards))
	}
	if updated := a.store.GetSubmission(sub.ID); updated == nil || updated.Status != "waiting_user_input" {
		t.Fatalf("submission after Claude pending cards = %+v, want waiting_user_input", updated)
	}
}

func TestCompleteUserInputAnswerUsesClaudeResolver(t *testing.T) {
	a, ff, _ := newTestApp(t)
	a.cfg.Feishu.Backend = backendClaude
	a.codex = nil
	claude := &fakeClaudeCore{}
	a.claude = claude
	a.feishu = ff

	payload := toolUserInputPayload{
		ThreadID: "claude-thread-1",
		TurnID:   "claude-turn-1",
		ItemID:   "item-1",
		Questions: []toolUserInputQuestion{
			{ID: "q1", Question: "Choose a mode", Options: []toolUserInputOption{{Label: "Fast"}, {Label: "Safe"}}},
		},
	}
	if err := a.store.UpsertPending(&state.PendingRequest{
		ID:          "question-1",
		Backend:     backendClaude,
		Kind:        "tool_request_user_input",
		OwnerUserID: "user-1",
		PayloadJSON: mustJSON(payload),
		Status:      "pending",
	}); err != nil {
		t.Fatalf("UpsertPending() error = %v", err)
	}

	resp, err := completeUserInputAnswer(a, &feishu.CardAction{
		UserID:      "user-1",
		ActionValue: map[string]any{"request_id": "question-1", "question_id": "q1", "answer": "Fast"},
	})
	if err != nil {
		t.Fatalf("completeUserInputAnswer() error = %v", err)
	}
	if resp == nil || resp.Toast == nil || resp.Toast.Type != "success" {
		t.Fatalf("user input response = %#v, want success toast", resp)
	}
	if len(claude.userInputCalls) != 1 {
		t.Fatalf("user input calls = %d, want 1", len(claude.userInputCalls))
	}
	if got := claude.userInputCalls[0].answers["Choose a mode"]; got != "Fast" {
		t.Fatalf("resolved Claude answer = %q, want Fast", got)
	}
	pending := a.store.PendingByID("question-1")
	if pending == nil || pending.Status != "resolved" {
		t.Fatalf("pending after answer = %+v, want resolved", pending)
	}
}

func TestCompleteUserInputAnswerUsesClaudeResolverForFormSubmit(t *testing.T) {
	a, ff, _ := newTestApp(t)
	a.cfg.Feishu.Backend = backendClaude
	a.codex = nil
	claude := &fakeClaudeCore{}
	a.claude = claude
	a.feishu = ff

	payload := toolUserInputPayload{
		ThreadID: "claude-thread-1",
		TurnID:   "claude-turn-1",
		ItemID:   "item-1",
		Questions: []toolUserInputQuestion{
			{ID: "q1", Question: "Choose a mode", Options: []toolUserInputOption{{Label: "Fast"}, {Label: "Safe"}}},
			{ID: "q2", Question: "Provide secret", IsSecret: true},
		},
	}
	if err := a.store.UpsertPending(&state.PendingRequest{
		ID:          "question-form-1",
		Backend:     backendClaude,
		Kind:        "tool_request_user_input_form",
		OwnerUserID: "user-1",
		PayloadJSON: mustJSON(payload),
		Status:      "pending",
	}); err != nil {
		t.Fatalf("UpsertPending() error = %v", err)
	}

	resp, err := completeUserInputAnswer(a, &feishu.CardAction{
		UserID:      "user-1",
		ActionValue: map[string]any{"request_id": "question-form-1"},
		FormValue: map[string]any{
			"q1": "Safe",
			"q2": "hidden",
		},
	})
	if err != nil {
		t.Fatalf("completeUserInputAnswer(form) error = %v", err)
	}
	if resp == nil || resp.Toast == nil || resp.Toast.Type != "success" {
		t.Fatalf("user input form response = %#v, want success toast", resp)
	}
	if len(claude.userInputCalls) != 1 {
		t.Fatalf("user input calls = %d, want 1", len(claude.userInputCalls))
	}
	if got := claude.userInputCalls[0].answers["Choose a mode"]; got != "Safe" {
		t.Fatalf("resolved Claude form answer = %q, want Safe", got)
	}
	if got := claude.userInputCalls[0].answers["Provide secret"]; got != "hidden" {
		t.Fatalf("resolved Claude secret answer = %q, want hidden", got)
	}
	pending := a.store.PendingByID("question-form-1")
	if pending == nil || pending.Status != "resolved" {
		t.Fatalf("pending after form answer = %+v, want resolved", pending)
	}
}

func TestCompleteToolUserInputTextUsesClaudeResolver(t *testing.T) {
	a, ff, _ := newTestApp(t)
	a.cfg.Feishu.Backend = backendClaude
	a.codex = nil
	claude := &fakeClaudeCore{}
	a.claude = claude
	a.feishu = ff

	if err := a.store.UpsertPending(&state.PendingRequest{
		ID:          "question-text-1",
		Backend:     backendClaude,
		Kind:        "tool_request_user_input_form",
		OwnerUserID: "user-1",
		FeishuMsgID: "card-1",
		PayloadJSON: mustJSON(toolUserInputPayload{
			ThreadID: "claude-thread-1",
			TurnID:   "claude-turn-1",
			ItemID:   "item-1",
			Questions: []toolUserInputQuestion{
				{ID: "q1", Question: "Choose a mode", Options: []toolUserInputOption{{Label: "Fast"}, {Label: "Safe"}}},
			},
		}),
		Status: "pending",
	}); err != nil {
		t.Fatalf("UpsertPending() error = %v", err)
	}

	if err := newPendingInputService(a).completeToolUserInputText(&feishu.InboundMessage{Text: "Fast"}, a.store.PendingByID("question-text-1")); err != nil {
		t.Fatalf("completeToolUserInputText() error = %v", err)
	}
	if len(claude.userInputCalls) != 1 {
		t.Fatalf("user input calls = %d, want 1", len(claude.userInputCalls))
	}
	if claude.userInputCalls[0].requestID != "question-text-1" {
		t.Fatalf("request id = %q, want question-text-1", claude.userInputCalls[0].requestID)
	}
	if got := claude.userInputCalls[0].answers["Choose a mode"]; got != "Fast" {
		t.Fatalf("resolved Claude text answer = %q, want Fast", got)
	}
	if pending := a.store.PendingByID("question-text-1"); pending == nil || pending.Status != "resolved" {
		t.Fatalf("pending after text answer = %+v, want resolved", pending)
	}
	if len(ff.patchedCards) != 1 {
		t.Fatalf("patched cards = %d, want 1", len(ff.patchedCards))
	}
}

func TestClaudeQuestionsAsToolUserInputPreservesMultiSelect(t *testing.T) {
	questions := []claudecli.Question{
		{
			Text:        "Pick targets",
			Options:     []claudecli.QuestionOption{{Label: "A"}, {Label: "B"}},
			MultiSelect: true,
		},
	}
	got := claudeQuestionsAsToolUserInput(questions)
	if len(got) != 1 {
		t.Fatalf("claudeQuestionsAsToolUserInput() len = %d, want 1", len(got))
	}
	if !got[0].MultiSelect {
		t.Fatalf("claudeQuestionsAsToolUserInput() = %+v, want multiSelect=true", got[0])
	}
	if got[0].IsOther {
		t.Fatalf("claudeQuestionsAsToolUserInput() = %+v, want isOther=false", got[0])
	}
}

func TestCommandInterruptUsesClaudeBackend(t *testing.T) {
	a, ff, _ := newTestApp(t)
	a.cfg.Feishu.Backend = backendClaude
	a.codex = nil
	claude := &fakeClaudeCore{}
	a.claude = claude
	a.feishu = ff

	sessionKey := "feishu:p2p:chat:user"
	if err := a.store.UpsertSession(&state.Session{
		Key:            sessionKey,
		WorkspaceID:    a.cfg.Workspaces[0].ID,
		ActiveThreadID: "claude-thread-1",
		ActiveTurnID:   "claude-turn-1",
		Status:         "turn_in_progress",
	}); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}

	msg := &feishu.InboundMessage{MessageID: "msg-1", ChatID: "chat", ChatType: "p2p", UserID: "user"}
	if err := commandInterrupt(a, msg); err != nil {
		t.Fatalf("commandInterrupt() error = %v", err)
	}
	if len(claude.interruptCalls) != 1 || claude.interruptCalls[0] != sessionKey {
		t.Fatalf("interrupt calls = %#v, want session key %q", claude.interruptCalls, sessionKey)
	}
	if len(ff.replyTexts) == 0 || !strings.Contains(ff.replyTexts[0], "已请求中断当前任务") {
		t.Fatalf("interrupt reply = %#v, want success text", ff.replyTexts)
	}
}

func TestClaudePlanFilePathFromTool(t *testing.T) {
	got := claudePlanFilePathFromTool("Write", map[string]interface{}{
		"file_path": "/home/yuhuan/.claude/plans/example-plan.md",
	})
	if got != "/home/yuhuan/.claude/plans/example-plan.md" {
		t.Fatalf("claudePlanFilePathFromTool() = %q", got)
	}
	if got := claudePlanFilePathFromTool("Edit", map[string]interface{}{"file_path": "/home/yuhuan/.claude/plans/example-plan.md"}); got != "" {
		t.Fatalf("claudePlanFilePathFromTool(non-write) = %q, want empty", got)
	}
}

func TestReadClaudePlanTextFallsBackToLatestHomePlan(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	plansDir := filepath.Join(home, ".claude", "plans")
	if err := os.MkdirAll(plansDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	oldPlan := filepath.Join(plansDir, "old-plan.md")
	if err := os.WriteFile(oldPlan, []byte("old plan"), 0o644); err != nil {
		t.Fatalf("WriteFile(oldPlan) error = %v", err)
	}
	oldTime := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(oldPlan, oldTime, oldTime); err != nil {
		t.Fatalf("Chtimes(oldPlan) error = %v", err)
	}

	latestPlan := filepath.Join(plansDir, "latest-plan.md")
	if err := os.WriteFile(latestPlan, []byte("1. inspect\n2. implement\n3. test"), 0o644); err != nil {
		t.Fatalf("WriteFile(latestPlan) error = %v", err)
	}

	got := readClaudePlanText("", "", time.Now().Add(-30*time.Minute))
	if got != "1. inspect\n2. implement\n3. test" {
		t.Fatalf("readClaudePlanText() = %q", got)
	}
}

func TestCompleteClaudePlanModeTextPreservesOriginalPlanBody(t *testing.T) {
	a, ff, _ := newTestApp(t)
	a.cfg.Feishu.Backend = backendClaude
	a.codex = nil
	claude := &fakeClaudeCore{}
	a.claude = claude

	pending := &state.PendingRequest{
		ID:          "plan-1",
		Backend:     backendClaude,
		Kind:        claudePlanModePendingKind,
		OwnerUserID: "user-1",
		FeishuMsgID: "msg-1",
		PayloadJSON: mustJSON(map[string]any{
			"body": "Claude 已完成计划阶段，请直接回复下一条消息作为反馈。\n\n计划：\n1. inspect /tmp\n2. remove stale files",
		}),
		Status: "pending",
	}
	if err := a.store.UpsertPending(pending); err != nil {
		t.Fatalf("UpsertPending() error = %v", err)
	}

	if err := newPendingInputService(a).completeClaudePlanModeText(&feishu.InboundMessage{Text: "退出plan"}, pending); err != nil {
		t.Fatalf("completeClaudePlanModeText() error = %v", err)
	}
	if len(claude.planCalls) != 1 || claude.planCalls[0].requestID != "plan-1" || claude.planCalls[0].feedback != "退出plan" {
		t.Fatalf("plan calls = %#v", claude.planCalls)
	}
	if got := a.store.PendingByID("plan-1"); got == nil || got.Status != "resolved" {
		t.Fatalf("pending after submit = %+v", got)
	}
	if len(ff.patchedCards) != 1 {
		t.Fatalf("patched cards = %d, want 1", len(ff.patchedCards))
	}
	if got := cardHeaderTitle(t, ff.patchedCards[0]); got != "计划反馈已提交" {
		t.Fatalf("patched card title = %q", got)
	}
	body := cardMarkdownContent(t, ff.patchedCards[0])
	for _, want := range []string{"你的反馈：", "退出plan", "原计划：", "1. inspect /tmp", "2. remove stale files"} {
		if !strings.Contains(body, want) {
			t.Fatalf("patched card body missing %q: %q", want, body)
		}
	}
}

func TestCompletePendingFormCancelClaudePlanPreservesOriginalPlanBody(t *testing.T) {
	a, _, _ := newTestApp(t)
	a.cfg.Feishu.Backend = backendClaude
	a.codex = nil
	claude := &fakeClaudeCore{}
	a.claude = claude

	pending := &state.PendingRequest{
		ID:          "plan-cancel-1",
		Backend:     backendClaude,
		Kind:        claudePlanModePendingKind,
		SessionKey:  "sess-1",
		OwnerUserID: "user-1",
		PayloadJSON: mustJSON(map[string]any{
			"body": "Claude 已完成计划阶段，请直接回复下一条消息作为反馈。\n\n计划：\n1. inspect /tmp\n2. remove stale files",
		}),
		Status: "pending",
	}
	if err := a.store.UpsertPending(pending); err != nil {
		t.Fatalf("UpsertPending() error = %v", err)
	}

	resp, err := completePendingFormCancel(a, &feishu.CardAction{
		UserID:      "user-1",
		ActionValue: map[string]any{"request_id": "plan-cancel-1"},
	})
	if err != nil {
		t.Fatalf("completePendingFormCancel() error = %v", err)
	}
	if resp == nil || resp.Toast == nil || resp.Card == nil {
		t.Fatalf("completePendingFormCancel() = %#v", resp)
	}
	if len(claude.cancelCalls) != 1 || claude.cancelCalls[0].requestID != "plan-cancel-1" {
		t.Fatalf("cancel calls = %#v", claude.cancelCalls)
	}
	card, _ := resp.Card.Data.(map[string]any)
	if got := cardHeaderTitle(t, card); got != "计划确认已取消" {
		t.Fatalf("cancelled card title = %q", got)
	}
	body := cardMarkdownContent(t, card)
	for _, want := range []string{"已取消本次计划确认。", "原计划：", "1. inspect /tmp", "2. remove stale files"} {
		if !strings.Contains(body, want) {
			t.Fatalf("cancelled card body missing %q: %q", want, body)
		}
	}
}

func TestCompletePendingFormCancelClaudeReviewSkipsBackendCancel(t *testing.T) {
	a, _, _ := newTestApp(t)
	a.cfg.Feishu.Backend = backendClaude
	a.codex = nil
	claude := &fakeClaudeCore{}
	a.claude = claude

	if err := a.store.UpsertPending(&state.PendingRequest{
		ID:          "review-cancel-1",
		Kind:        pendingKindReview,
		SessionKey:  "sess-1",
		OwnerUserID: "user-1",
		PayloadJSON: mustJSON(reviewPendingPayload{
			Mode:         reviewFormModeCustom,
			Instructions: "focus on backend adapters",
		}),
		Status: "pending",
	}); err != nil {
		t.Fatalf("UpsertPending() error = %v", err)
	}

	resp, err := completePendingFormCancel(a, &feishu.CardAction{
		UserID:      "user-1",
		ActionValue: map[string]any{"request_id": "review-cancel-1"},
	})
	if err != nil {
		t.Fatalf("completePendingFormCancel() error = %v", err)
	}
	if resp == nil || resp.Toast == nil || resp.Toast.Type != "success" || resp.Card == nil {
		t.Fatalf("completePendingFormCancel() = %#v", resp)
	}
	if len(claude.cancelCalls) != 0 {
		t.Fatalf("cancel calls = %#v, want none", claude.cancelCalls)
	}
	card, _ := resp.Card.Data.(map[string]any)
	if got := cardHeaderTitle(t, card); got != "Review 已取消" {
		t.Fatalf("cancelled card title = %q", got)
	}
}

func TestHandleFeishuMessageReplyStartsAdditionalClaudeTurn(t *testing.T) {
	a, _, _ := newTestApp(t)
	a.cfg.Feishu.Backend = backendClaude
	a.codex = nil
	claude := &fakeClaudeCore{ensureSessionID: "claude-thread-1"}
	a.claude = claude

	targetSessionKey := "feishu:group:chat-1:root:root-msg"
	if err := a.store.UpsertSession(&state.Session{
		Key:                targetSessionKey,
		WorkspaceID:        a.cfg.Workspaces[0].ID,
		ChatID:             "chat-1",
		ChatType:           "group",
		OwnerUserID:        "user-1",
		RootMessageID:      "root-msg",
		ActiveThreadID:     "claude-thread-1",
		ActiveTurnID:       "claude-turn-1",
		ActiveSubmissionID: "sub-running",
		Status:             "turn_in_progress",
	}); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}
	if _, err := a.store.CreateSubmission(&state.Submission{
		ID:                   "sub-running",
		SessionKey:           targetSessionKey,
		WorkspaceID:          a.cfg.Workspaces[0].ID,
		UserID:               "user-1",
		ChatID:               "chat-1",
		TriggerMessageID:     "msg-running",
		SourceMessageIDs:     []string{"msg-running"},
		SourceRootMessageIDs: []string{"root-msg"},
		InputText:            "running",
		ThreadID:             "claude-thread-1",
		TurnID:               "claude-turn-1",
		Status:               "running",
	}); err != nil {
		t.Fatalf("CreateSubmission(sub-running) error = %v", err)
	}
	if err := a.store.UpsertMessageLink(&state.MessageLink{
		MessageID:  "root-msg",
		SessionKey: targetSessionKey,
		ThreadID:   "claude-thread-1",
		TurnID:     "claude-turn-1",
	}); err != nil {
		t.Fatalf("UpsertMessageLink() error = %v", err)
	}

	a.HandleFeishuMessage(&feishu.InboundMessage{
		MessageID:       "reply-1",
		ChatID:          "chat-1",
		ChatType:        "group",
		UserID:          "user-1",
		Text:            "300字就行了",
		RootMessageID:   "root-msg",
		ParentMessageID: "some-parent",
	})

	if len(claude.interruptCalls) != 0 {
		t.Fatalf("interrupt calls = %#v, want none", claude.interruptCalls)
	}
	if len(claude.startTurnCalls) != 1 || !strings.Contains(claude.startTurnCalls[0].prompt, "300字就行了") {
		t.Fatalf("startTurn calls = %#v", claude.startTurnCalls)
	}
	sess := a.store.GetSession(targetSessionKey)
	if sess == nil || len(sess.Queue) != 0 || len(sess.ActiveOperations) != 2 || sess.ActiveTurnID != "claude-turn-1" || sess.ActiveSubmissionID != "sub-running" || sess.Status != "turn_in_progress" {
		t.Fatalf("session after reply continuation = %+v", sess)
	}
	nextSubmissionID := strings.TrimSpace(sess.ActiveOperations[0].SubmissionID)
	sub := a.store.GetSubmission(nextSubmissionID)
	if sub == nil || sub.InputText != "300字就行了" || sub.SessionKey != targetSessionKey || sub.TurnID == "" || sub.Status != "running" {
		t.Fatalf("follow-up submission = %+v", sub)
	}
}

func TestTryClaudeReplyContinuationUsesActiveSessionDespiteStaleLink(t *testing.T) {
	a, _, _ := newTestApp(t)
	a.cfg.Feishu.Backend = backendClaude
	a.codex = nil
	claude := &fakeClaudeCore{ensureSessionID: "claude-thread-1"}
	a.claude = claude

	sessionKey := "feishu:group:chat-1:root:root-msg"
	if err := a.store.UpsertSession(&state.Session{
		Key:                     sessionKey,
		WorkspaceID:             a.cfg.Workspaces[0].ID,
		ChatID:                  "chat-1",
		ChatType:                "group",
		OwnerUserID:             "user-1",
		RootMessageID:           "root-msg",
		ActiveThreadID:          "claude-thread-1",
		ActiveThreadWorkspaceID: a.cfg.Workspaces[0].ID,
		ActiveTurnID:            "claude-turn-current",
		ActiveSubmissionID:      "sub-running",
		Status:                  "turn_in_progress",
	}); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}
	if _, err := a.store.CreateSubmission(&state.Submission{
		ID:                   "sub-running",
		SessionKey:           sessionKey,
		WorkspaceID:          a.cfg.Workspaces[0].ID,
		UserID:               "user-1",
		ChatID:               "chat-1",
		TriggerMessageID:     "msg-running",
		SourceMessageIDs:     []string{"msg-running"},
		SourceRootMessageIDs: []string{"root-msg"},
		InputText:            "running",
		ThreadID:             "claude-thread-1",
		TurnID:               "claude-turn-current",
		Status:               "running",
	}); err != nil {
		t.Fatalf("CreateSubmission(sub-running) error = %v", err)
	}

	sess := a.store.GetSession(sessionKey)
	if sess == nil {
		t.Fatal("session missing")
	}
	steered, err := newReplyContinuationService(a).tryClaudeReplyContinuation(&feishu.InboundMessage{
		MessageID:       "reply-1",
		ChatID:          "chat-1",
		ChatType:        "group",
		UserID:          "user-1",
		Text:            "follow up from stale link",
		RootMessageID:   "root-msg",
		ParentMessageID: "target-msg",
	}, &state.MessageLink{
		SessionKey: sessionKey,
		ThreadID:   "stale-thread",
		TurnID:     "stale-turn",
	}, sessionKey, sess)
	if err != nil || !steered {
		t.Fatalf("tryClaudeReplyContinuation() = %v, %v", steered, err)
	}
	if len(claude.startTurnCalls) != 1 || !strings.Contains(claude.startTurnCalls[0].prompt, "follow up from stale link") {
		t.Fatalf("startTurn calls = %#v", claude.startTurnCalls)
	}
	updated := a.store.GetSession(sessionKey)
	if updated == nil || len(updated.ActiveOperations) != 2 || updated.ActiveTurnID != "claude-turn-current" || updated.ActiveSubmissionID != "sub-running" {
		t.Fatalf("session after stale-link continuation = %+v", updated)
	}
}

func TestCommandAppendUsesClaudeContinuation(t *testing.T) {
	a, _, _ := newTestApp(t)
	a.cfg.Feishu.Backend = backendClaude
	a.codex = nil
	claude := &fakeClaudeCore{ensureSessionID: "claude-thread-1"}
	a.claude = claude

	msg := &feishu.InboundMessage{
		MessageID:     "cmd-msg-1",
		ChatID:        "chat-1",
		ChatType:      "group",
		UserID:        "user-1",
		RootMessageID: "root-msg",
	}
	sessionKey := makeSessionKey(a, msg)
	if err := a.store.UpsertSession(&state.Session{
		Key:                     sessionKey,
		WorkspaceID:             a.cfg.Workspaces[0].ID,
		ChatID:                  "chat-1",
		ChatType:                "group",
		OwnerUserID:             "user-1",
		RootMessageID:           "root-msg",
		ActiveThreadID:          "claude-thread-1",
		ActiveThreadWorkspaceID: a.cfg.Workspaces[0].ID,
		ActiveTurnID:            "claude-turn-current",
		ActiveSubmissionID:      "sub-running",
		Status:                  "turn_in_progress",
	}); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}
	if _, err := a.store.CreateSubmission(&state.Submission{
		ID:                   "sub-running",
		SessionKey:           sessionKey,
		WorkspaceID:          a.cfg.Workspaces[0].ID,
		UserID:               "user-1",
		ChatID:               "chat-1",
		TriggerMessageID:     "msg-running",
		SourceMessageIDs:     []string{"msg-running"},
		SourceRootMessageIDs: []string{"root-msg"},
		InputText:            "running",
		ThreadID:             "claude-thread-1",
		TurnID:               "claude-turn-current",
		Status:               "running",
	}); err != nil {
		t.Fatalf("CreateSubmission(sub-running) error = %v", err)
	}

	if err := commandAppend(a, msg, "  append from command  "); err != nil {
		t.Fatalf("commandAppend() error = %v", err)
	}
	if len(claude.startTurnCalls) != 1 || !strings.Contains(claude.startTurnCalls[0].prompt, "append from command") {
		t.Fatalf("startTurn calls = %#v", claude.startTurnCalls)
	}
	updated := a.store.GetSession(sessionKey)
	if updated == nil || len(updated.ActiveOperations) != 2 || updated.ActiveTurnID != "claude-turn-current" || updated.ActiveSubmissionID != "sub-running" {
		t.Fatalf("session after Claude command append = %+v", updated)
	}
	nextSubmissionID := strings.TrimSpace(updated.ActiveOperations[0].SubmissionID)
	sub := a.store.GetSubmission(nextSubmissionID)
	if sub == nil || sub.InputText != "append from command" || sub.TriggerMessageID != "" || len(sub.SourceRootMessageIDs) != 1 || sub.SourceRootMessageIDs[0] != "root-msg" {
		t.Fatalf("Claude append submission = %+v", sub)
	}
}
