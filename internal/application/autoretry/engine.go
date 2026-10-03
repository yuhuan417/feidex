// Package autoretry owns retry policy and submission coordination for one frontend.
package autoretry

import (
	"context"
	"feidex/internal/domain/conversation"
	domainsubmission "feidex/internal/domain/submission"
	"feidex/internal/domain/workspace"
	"feidex/internal/textutil"
	"fmt"
	"strings"
	"time"
)

type Repository interface {
	Session(string) *conversation.Session
	CreateSubmission(*domainsubmission.Submission) (string, error)
	Submission(string) *domainsubmission.Submission
}
type LiveThreads interface{ SessionHasLiveThread(string, string) bool }
type SubmissionStarter interface {
	StartQueuedSubmission(string, *conversation.Session, *domainsubmission.Submission, *workspace.Workspace, bool) error
}

// Presenter performs an outbound presentation effect, never enters another use case.
type Presenter interface {
	RetryStatus(RetryState, string, string) string
}
type Dependencies struct {
	Context            func() context.Context
	Tracker            *Tracker
	Repository         Repository
	Live               LiveThreads
	Enabled            func() bool
	SaveEnabled        func(bool) error
	Recovering         func() bool
	DefaultWorkspaceID func() string
	Workspace          func(string) *workspace.Workspace
	Starter            func() SubmissionStarter
	DispatchTimer      func(string, uint64)
	Presenter          Presenter
}
type Engine struct{ deps Dependencies }

func NewEngine(deps Dependencies) Engine { return Engine{deps: deps} }
func (s Engine) context() context.Context {
	if s.deps.Context != nil {
		return s.deps.Context()
	}
	return context.Background()
}
func (s Engine) AutoRetryTracker() *Tracker { return s.deps.Tracker }
func (s Engine) AutoRetryEnabled() bool     { return s.deps.Enabled != nil && s.deps.Enabled() }

func (s Engine) UpdateAutoRetryEnabled(enabled bool) error {
	if err := s.deps.SaveEnabled(enabled); err != nil {
		return err
	}
	if !enabled {
		s.CancelAllAutoRetry("已关闭自动重试。")
	}
	return nil
}

var uniqueStrings = uniqueSourceRoots

func (s Engine) notify(snapshot RetryState, phase, notice string) {
	if s.deps.Presenter == nil {
		return
	}
	sentID := strings.TrimSpace(s.deps.Presenter.RetryStatus(snapshot, phase, notice))
	if sentID == "" {
		return
	}
	tracker := s.AutoRetryTracker()
	tracker.Mu.Lock()
	defer tracker.Mu.Unlock()
	if st := tracker.States[snapshot.SessionKey]; st != nil {
		if current := strings.TrimSpace(st.StatusMessageID); current == "" || current == strings.TrimSpace(snapshot.StatusMessageID) {
			st.StatusMessageID = sentID
		}
	}
}

func uniqueSourceRoots(values []string) []string {
	seen := map[string]bool{}
	result := []string{}
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v != "" && !seen[v] {
			seen[v] = true
			result = append(result, v)
		}
	}
	return result
}

// ScheduleDelayedTask delegates execution to the composed runtime scheduler.
func (s Engine) ScheduleDelayedTask(delay time.Duration, fn func()) DelayedTask {
	return s.AutoRetryTracker().After(delay, fn)
}

// HasPendingAutoRetry reports whether there is a pending auto-retry for the
// given session. If sessionKey is empty it checks all sessions.
func (s Engine) HasPendingAutoRetry(sessionKey string) bool {
	if s.deps.Tracker == nil {
		return false
	}
	sessionKey = strings.TrimSpace(sessionKey)
	tracker := s.AutoRetryTracker()
	tracker.Mu.Lock()
	defer tracker.Mu.Unlock()
	if sessionKey != "" {
		return StateWaiting(tracker.States[sessionKey])
	}
	for _, st := range tracker.States {
		if StateWaiting(st) {
			return true
		}
	}
	return false
}

// HasBlockingAutoRetry reports whether an auto-retry loop should take
// priority over ordinary queued input for the given session. If sessionKey is
// empty it checks all sessions.
func (s Engine) HasBlockingAutoRetry(sessionKey string) bool {
	if s.deps.Tracker == nil {
		return false
	}
	sessionKey = strings.TrimSpace(sessionKey)
	tracker := s.AutoRetryTracker()
	tracker.Mu.Lock()
	defer tracker.Mu.Unlock()
	if sessionKey != "" {
		return StateBlocksQueue(tracker.States[sessionKey])
	}
	for _, st := range tracker.States {
		if StateBlocksQueue(st) {
			return true
		}
	}
	return false
}

// CurrentAutoRetryState returns a cloned snapshot of the retry state for the
// given session.
func (s Engine) CurrentAutoRetryState(sessionKey string) (RetryState, bool) {
	if s.deps.Tracker == nil {
		return RetryState{}, false
	}
	tracker := s.AutoRetryTracker()
	tracker.Mu.Lock()
	defer tracker.Mu.Unlock()
	st := tracker.States[strings.TrimSpace(sessionKey)]
	if st == nil {
		return RetryState{}, false
	}
	return CloneState(st), true
}

// ObserveAutoRetryTerminal inspects a terminal turn status. On failure it
// schedules an auto-retry; on other terminals it cleans up retry state.
// Returns true if a retry is pending after the observation.
func (s Engine) ObserveAutoRetryTerminal(sessionKey, threadID, status string, updatedSess *conversation.Session, sub *domainsubmission.Submission, reuseMessageID, lastError string) bool {
	if s.deps.Tracker == nil {
		return false
	}
	sessionKey = strings.TrimSpace(sessionKey)
	threadID = strings.TrimSpace(threadID)
	status = strings.TrimSpace(status)
	if sessionKey == "" || threadID == "" {
		return false
	}
	if domainsubmission.NormalizeSubmissionStatus(status) != domainsubmission.SubmissionStatusFailed {
		s.FinishAutoRetryOnTerminal(sessionKey, threadID, status)
		return false
	}
	return s.ScheduleAutoRetryAfterFailure(sessionKey, threadID, updatedSess, sub, reuseMessageID, lastError)
}

// FinishAutoRetryOnTerminal cleans up retry state on non-failure terminal
// status (completed, interrupted, or already canceled).
func (s Engine) FinishAutoRetryOnTerminal(sessionKey, threadID, status string) {
	if s.deps.Tracker == nil {
		return
	}
	sessionKey = strings.TrimSpace(sessionKey)
	threadID = strings.TrimSpace(threadID)
	status = strings.TrimSpace(status)
	if sessionKey == "" || threadID == "" {
		return
	}
	var snapshot RetryState
	found := false
	tracker := s.AutoRetryTracker()
	tracker.Mu.Lock()
	if st := tracker.States[sessionKey]; st != nil && strings.TrimSpace(st.ThreadID) == threadID {
		if st.Timer != nil {
			st.Timer.Stop()
			st.Timer = nil
		}
		snapshot = CloneState(st)
		delete(tracker.States, sessionKey)
		found = true
	}
	tracker.Mu.Unlock()
	if !found {
		return
	}
	if snapshot.Canceled {
		s.notify(snapshot, "stopped", "已停止自动重试。")
		return
	}
	switch status {
	case "completed":
		s.notify(snapshot, "completed", "已收到非 failed 终态，自动重试结束。")
	case "interrupted":
		s.notify(snapshot, "interrupted", "任务已中断，自动重试结束。")
	}
}

// ScheduleAutoRetryAfterFailure attempts to schedule an auto-retry after a
// failed turn. Returns true if a retry is now pending.
func (s Engine) ScheduleAutoRetryAfterFailure(sessionKey, threadID string, updatedSess *conversation.Session, sub *domainsubmission.Submission, reuseMessageID, lastError string) bool {
	if s.deps.Tracker == nil {
		return false
	}
	sessionKey = strings.TrimSpace(sessionKey)
	threadID = strings.TrimSpace(threadID)
	if sessionKey == "" || threadID == "" {
		return false
	}
	if !s.AutoRetryEnabled() {
		return false
	}
	if updatedSess == nil || strings.TrimSpace(updatedSess.ActiveThreadID) != threadID || strings.TrimSpace(updatedSess.ActiveThreadID) == "" {
		return false
	}
	sessionStatus := conversation.NormalizeSessionStatus(textutil.FirstNonEmpty(strings.TrimSpace(updatedSess.Status), conversation.SessionStatusIdle.String()))
	if conversation.HasActiveWork(updatedSess) || (sessionStatus != conversation.SessionStatusIdle && sessionStatus != conversation.SessionStatusQueued) {
		return false
	}

	var (
		snapshot RetryState
		waiting  bool
	)
	tracker := s.AutoRetryTracker()
	tracker.Mu.Lock()
	st := tracker.States[sessionKey]
	if st == nil {
		st = &RetryState{
			SessionKey: sessionKey,
			ThreadID:   threadID,
		}
		tracker.States[sessionKey] = st
	}
	if st.Canceled {
		// A late failed completion must consume the stop marker, never restart it.
		delete(tracker.States, sessionKey)
		tracker.Mu.Unlock()
		return false
	}
	RefreshState(st, updatedSess, sub, threadID)
	st.LastError = strings.TrimSpace(lastError)
	if strings.TrimSpace(st.StatusMessageID) == "" {
		st.StatusMessageID = strings.TrimSpace(reuseMessageID)
	}
	if st.Timer == nil {
		delay := DelayForStep(st.BackoffStep)
		tracker.nextTimerSeq++
		st.TimerSeq = tracker.nextTimerSeq
		seq := st.TimerSeq
		st.Timer = s.ScheduleDelayedTask(delay, func() {
			s.deps.DispatchTimer(sessionKey, seq)
		})
	}
	waiting = StateWaiting(st)
	snapshot = CloneState(st)
	tracker.Mu.Unlock()
	s.notify(snapshot, "waiting", "当前任务 failed，准备自动发送“继续”。")
	return waiting
}

// RunAutoRetryTimer is the callback invoked when the backoff timer fires.
func (s Engine) RunAutoRetryTimer(sessionKey string, expectedSeq uint64) {
	if s.context().Err() != nil {
		return
	}
	if s.deps.Tracker == nil {
		return
	}
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return
	}

	unlock := s.AutoRetryTracker().LockDispatch(sessionKey)
	defer unlock()

	var snapshot RetryState
	tracker := s.AutoRetryTracker()
	tracker.Mu.Lock()
	st := tracker.States[sessionKey]
	if st == nil || st.Canceled || st.Timer == nil || st.TimerSeq != expectedSeq {
		tracker.Mu.Unlock()
		return
	}
	st.Timer = nil
	snapshot = CloneState(st)
	tracker.Mu.Unlock()

	if !s.AutoRetryEnabled() {
		s.FinishAutoRetryWithMessage(sessionKey, "stopped", "自动重试已关闭。")
		return
	}

	sess := s.deps.Repository.Session(sessionKey)
	if sess == nil {
		s.FinishAutoRetryWithMessage(sessionKey, "stopped", "当前会话已不存在。")
		return
	}
	if strings.TrimSpace(sess.ActiveThreadID) == "" {
		s.FinishAutoRetryWithMessage(sessionKey, "stopped", "当前会话已经没有活动线程。")
		return
	}
	if strings.TrimSpace(sess.ActiveThreadID) != strings.TrimSpace(snapshot.ThreadID) {
		s.FinishAutoRetryWithMessage(sessionKey, "stopped", "当前会话已切换到其他线程。")
		return
	}
	if conversation.HasActiveWork(sess) {
		s.FinishAutoRetryWithMessage(sessionKey, "stopped", "检测到当前线程已有新任务，自动重试结束。")
		return
	}
	sessionStatus := conversation.NormalizeSessionStatus(textutil.FirstNonEmpty(strings.TrimSpace(sess.Status), conversation.SessionStatusIdle.String()))
	if sessionStatus != conversation.SessionStatusIdle && sessionStatus != conversation.SessionStatusQueued {
		s.FinishAutoRetryWithMessage(sessionKey, "stopped", "当前会话已不再处于空闲态。")
		return
	}
	if s.deps.Recovering != nil && s.deps.Recovering() {
		s.BumpAutoRetryBackoffAndReschedule(sessionKey, "运行时正在恢复，继续等待后重试。")
		return
	}

	sub, err := s.StartAutoRetrySubmission(sessionKey, sess, snapshot)
	if err != nil {
		if s.HasPendingAutoRetry(sessionKey) {
			return
		}
		s.FinishAutoRetryWithMessage(sessionKey, "stopped", "自动重试启动失败: "+err.Error())
		return
	}
	s.MarkAutoRetryAttemptStarted(sessionKey, sub)
}

// BumpAutoRetryBackoffAndReschedule increases the backoff step and reschedules
// the timer.
func (s Engine) BumpAutoRetryBackoffAndReschedule(sessionKey, notice string) {
	if s.deps.Tracker == nil {
		return
	}
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return
	}
	var snapshot RetryState
	tracker := s.AutoRetryTracker()
	tracker.Mu.Lock()
	st := tracker.States[sessionKey]
	if st == nil || st.Canceled {
		tracker.Mu.Unlock()
		return
	}
	st.BackoffStep++
	delay := DelayForStep(st.BackoffStep)
	tracker.nextTimerSeq++
	st.TimerSeq = tracker.nextTimerSeq
	seq := st.TimerSeq
	st.Timer = s.ScheduleDelayedTask(delay, func() {
		s.deps.DispatchTimer(sessionKey, seq)
	})
	snapshot = CloneState(st)
	tracker.Mu.Unlock()
	s.notify(snapshot, "waiting", notice)
}

// StartAutoRetrySubmission creates and starts a "继续" submission for the
// auto-retry cycle.
func (s Engine) StartAutoRetrySubmission(sessionKey string, sess *conversation.Session, snapshot RetryState) (*domainsubmission.Submission, error) {
	if s.deps.Tracker == nil || sess == nil {
		return nil, fmt.Errorf("session missing")
	}
	if strings.TrimSpace(snapshot.ThreadID) == "" || strings.TrimSpace(sess.ActiveThreadID) != strings.TrimSpace(snapshot.ThreadID) {
		return nil, fmt.Errorf("active thread changed")
	}
	if !s.deps.Live.SessionHasLiveThread(sessionKey, snapshot.ThreadID) {
		return nil, fmt.Errorf("active thread is not live")
	}
	workspaceID := textutil.FirstNonEmpty(strings.TrimSpace(sess.WorkspaceID), strings.TrimSpace(snapshot.WorkspaceID), s.deps.DefaultWorkspaceID())
	ws := s.deps.Workspace(workspaceID)
	if ws == nil {
		return nil, fmt.Errorf("workspace %q not found", workspaceID)
	}
	triggerMessageID := textutil.FirstNonEmpty(strings.TrimSpace(snapshot.TriggerMessageID), strings.TrimSpace(sess.RootMessageID))
	sourceRootMessageIDs := append([]string(nil), snapshot.SourceRootMessageIDs...)
	if len(sourceRootMessageIDs) == 0 && strings.TrimSpace(sess.RootMessageID) != "" {
		sourceRootMessageIDs = []string{strings.TrimSpace(sess.RootMessageID)}
	}
	sub := &domainsubmission.Submission{
		SessionKey:           strings.TrimSpace(sessionKey),
		BindingID:            strings.TrimSpace(sess.BindingID),
		WorkspaceID:          workspaceID,
		UserID:               strings.TrimSpace(sess.OwnerUserID),
		ChatID:               textutil.FirstNonEmpty(strings.TrimSpace(sess.ChatID), strings.TrimSpace(snapshot.ChatID)),
		TriggerMessageID:     triggerMessageID,
		SourceRootMessageIDs: uniqueStrings(sourceRootMessageIDs),
		InputText:            "继续",
		Status:               domainsubmission.SubmissionStatusQueued.String(),
	}
	id, err := s.deps.Repository.CreateSubmission(sub)
	if err != nil {
		return nil, err
	}
	sub.ID = id
	if err := s.deps.Starter().StartQueuedSubmission(sessionKey, sess, sub, ws, false); err != nil {
		if current := s.deps.Repository.Submission(sub.ID); current != nil {
			sub = current
		}
		return sub, err
	}
	return sub, nil
}

// MarkAutoRetryAttemptStarted records that an auto-retry attempt has been
// dispatched and delivers an updated status card.
func (s Engine) MarkAutoRetryAttemptStarted(sessionKey string, sub *domainsubmission.Submission) {
	if s.deps.Tracker == nil {
		return
	}
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return
	}
	var snapshot RetryState
	tracker := s.AutoRetryTracker()
	tracker.Mu.Lock()
	st := tracker.States[sessionKey]
	if st == nil || st.Canceled {
		tracker.Mu.Unlock()
		return
	}
	st.RetryCount++
	st.BackoffStep++
	RefreshState(st, s.deps.Repository.Session(sessionKey), sub, textutil.FirstNonEmpty(strings.TrimSpace(sub.ThreadID), st.ThreadID))
	snapshot = CloneState(st)
	tracker.Mu.Unlock()
	s.notify(snapshot, "running", "已自动发送“继续”，等待新的任务结果。")
}

// CancelAutoRetry cancels the auto-retry for a session. If keepUntilTerminal
// is true the state entry is kept (marked canceled) until the running turn
// reaches a terminal status.
func (s Engine) CancelAutoRetry(sessionKey string, keepUntilTerminal bool, notice string) bool {
	if s.deps.Tracker == nil {
		return false
	}
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return false
	}
	var snapshot RetryState
	canceled := false
	tracker := s.AutoRetryTracker()
	tracker.Mu.Lock()
	st := tracker.States[sessionKey]
	if st != nil && st.Canceled {
		tracker.Mu.Unlock()
		return false
	}

	if st == nil && keepUntilTerminal && s.AutoRetryEnabled() {
		// Also guard the first failing turn, before it has created a retry loop.
		if sess := s.deps.Repository.Session(sessionKey); sess != nil && sess.ActiveThreadID != "" {
			tracker.States[sessionKey] = &RetryState{SessionKey: sessionKey, ThreadID: sess.ActiveThreadID, Canceled: true}
		}
	}

	if st != nil {
		if st.Timer != nil {
			st.Timer.Stop()
			st.Timer = nil
		}
		st.Canceled = true
		snapshot = CloneState(st)
		if !keepUntilTerminal {
			delete(tracker.States, sessionKey)
		}
		canceled = true
	}
	tracker.Mu.Unlock()
	if canceled {
		s.notify(snapshot, "stopped", textutil.FirstNonEmpty(strings.TrimSpace(notice), "已停止自动重试。"))
	}
	return canceled
}

// CancelAllAutoRetry cancels all pending auto-retries and returns the count of
// canceled entries.
func (s Engine) CancelAllAutoRetry(notice string) int {
	if s.deps.Tracker == nil {
		return 0
	}
	notice = textutil.FirstNonEmpty(strings.TrimSpace(notice), "已关闭自动重试。")
	type pendingCard struct {
		snapshot RetryState
	}
	pending := []pendingCard{}
	tracker := s.AutoRetryTracker()
	tracker.Mu.Lock()
	for sessionKey, st := range tracker.States {
		if st == nil {
			delete(tracker.States, sessionKey)
			continue
		}
		if st.Timer != nil {
			st.Timer.Stop()
			st.Timer = nil
		}
		pending = append(pending, pendingCard{snapshot: CloneState(st)})
		delete(tracker.States, sessionKey)
	}
	tracker.Mu.Unlock()
	for _, item := range pending {
		s.notify(item.snapshot, "stopped", notice)
	}
	return len(pending)
}

// FinishAutoRetryWithMessage removes the retry state entry and delivers a
// final status card with the given phase and notice.
func (s Engine) FinishAutoRetryWithMessage(sessionKey, phase, notice string) {
	if s.deps.Tracker == nil {
		return
	}
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return
	}
	var snapshot RetryState
	tracker := s.AutoRetryTracker()
	tracker.Mu.Lock()
	st := tracker.States[sessionKey]
	if st == nil {
		tracker.Mu.Unlock()
		return
	}
	if st.Timer != nil {
		st.Timer.Stop()
		st.Timer = nil
	}
	snapshot = CloneState(st)
	delete(tracker.States, sessionKey)
	tracker.Mu.Unlock()
	s.notify(snapshot, phase, notice)
}
