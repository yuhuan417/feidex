// Package autoretry provides the auto-retry service extracted from the app god package.
package autoretry

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	appbackend "feidex/internal/app/backend"
	appcore "feidex/internal/app/appcore"
	apputil "feidex/internal/app/apputil"
	appsubmission "feidex/internal/app/submission"
	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

// ---------------------------------------------------------------------------
// App interface — what the service needs from the host application
// ---------------------------------------------------------------------------

// App defines the interface the auto-retry service requires from the host
// application. It embeds appcore.AppConfig so that appcore helpers like
// FeishuConfig, ConfiguredBackend, etc. can be called directly.
type App interface {
	appcore.AppConfig

	// Feishu returns the Feishu bot client.
	Feishu() appcore.FeishuClient
	// ConfigPath returns the filesystem path to the config file.
	ConfigPath() string
	// AutoRetries returns the auto-retry tracker (lazily initialized by the app).
	AutoRetries() *Tracker
	// AppState returns the narrowed app state provider.
	AppState() AppStateProvider
	// AutoRetryBackendRuntime returns the narrowed backend runtime provider.
	AutoRetryBackendRuntime() BackendRuntimeProvider
	// AutoRetryConversationBackend returns the narrowed conversation backend provider.
	AutoRetryConversationBackend() ConversationBackendProvider
	// RunAsync dispatches fn asynchronously.
	RunAsync(fn func())
	// MenuCardBody formats a menu card body with breadcrumb navigation.
	MenuCardBody(action, body string) string
	// SessionHasActiveWork reports whether the session has active work.
	SessionHasActiveWork(sess *state.Session) bool
	// SessionHasLiveThread reports whether the given thread is live for the session.
	SessionHasLiveThread(sessionKey, threadID string) bool
	// ClearSessionLiveThread clears the live thread for the session.
	ClearSessionLiveThread(sessionKey string)
	// ReplyCommandActionResponse replies to a command message with a card action response.
	ReplyCommandActionResponse(msg *feishu.InboundMessage, resp *callback.CardActionTriggerResponse) error
}

// ---------------------------------------------------------------------------
// Narrow provider interfaces
// ---------------------------------------------------------------------------

// AppStateProvider narrows app state access to the methods used by the service.
type AppStateProvider interface {
	Session(key string) *state.Session
	CreateSubmission(sub *state.Submission) (string, error)
	Submission(id string) *state.Submission
}

// BackendRuntimeProvider narrows backend runtime access to the methods used by
// the service. The original method took an *App parameter; the provider
// implementation already has the app reference, so the parameter is removed.
type BackendRuntimeProvider interface {
	DeferQueuedSubmissionsDuringRecovery() bool
}

// ConversationBackendProvider narrows conversation backend access to the
// methods used by the service.
type ConversationBackendProvider interface {
	StartQueuedSubmission(sessionKey string, sess *state.Session, sub *state.Submission, ws *config.Workspace, notifyFailure bool) error
}

// ---------------------------------------------------------------------------
// Local helpers (used by exported methods below)
// ---------------------------------------------------------------------------

var uniqueStrings = appsubmission.UniqueStrings

// ActionSessionKey extracts the "session_key" value from a card action.
func ActionSessionKey(action *feishu.CardAction) string {
	if action == nil {
		return ""
	}
	value, _ := action.ActionValue["session_key"].(string)
	return strings.TrimSpace(value)
}

// RawCard wraps a card map in a callback.Card for card action responses.
func RawCard(card map[string]any) *callback.Card {
	return &callback.Card{Type: "raw", Data: card}
}

// CommandActionFromMessage builds a CardAction from an inbound message and
// optional action value overrides.
func CommandActionFromMessage(msg *feishu.InboundMessage, actionValue map[string]any) *feishu.CardAction {
	if actionValue == nil {
		actionValue = map[string]any{}
	}
	if msg == nil {
		return &feishu.CardAction{ActionValue: actionValue}
	}
	return &feishu.CardAction{
		ActionValue: actionValue,
		UserID:      strings.TrimSpace(msg.UserID),
		ChatID:      strings.TrimSpace(msg.ChatID),
		MessageID:   strings.TrimSpace(msg.MessageID),
	}
}

// ---------------------------------------------------------------------------
// Service — manages the auto-retry lifecycle
// ---------------------------------------------------------------------------

// Service manages the auto-retry lifecycle for a single app instance.
type Service struct {
	app App
}

// NewService creates a new auto-retry service bound to the given app.
func NewService(app App) Service {
	return Service{app: app}
}

// AutoRetryTracker returns the auto-retry tracker via the app (lazy init
// happens in the App implementation).
func (s Service) AutoRetryTracker() *Tracker {
	if s.app == nil {
		return nil
	}
	return s.app.AutoRetries()
}

// AutoRetryEnabled reports whether auto-retry is currently enabled in config.
func (s Service) AutoRetryEnabled() bool {
	cfg := appcore.FeishuConfig(s.app)
	return cfg != nil && cfg.AutoRetry
}

// UpdateAutoRetryEnabled persists the auto-retry enabled flag to config.
func (s Service) UpdateAutoRetryEnabled(enabled bool) error {
	if s.app == nil || s.app.Config() == nil {
		return fmt.Errorf("nil config")
	}
	s.app.ConfigMu().Lock()
	defer s.app.ConfigMu().Unlock()
	cfg := appcore.FeishuConfigUnlocked(s.app)
	if cfg == nil {
		return fmt.Errorf("frontend config not found")
	}
	cfg.AutoRetry = enabled
	cfgPath := s.app.ConfigPath()
	if strings.TrimSpace(cfgPath) == "" {
		return nil
	}
	if err := s.app.Config().Normalize(filepath.Dir(cfgPath)); err != nil {
		return err
	}
	return config.Save(cfgPath, s.app.Config())
}

// AutoRetryTitle returns the display title for auto-retry cards based on the
// active backend.
func (s Service) AutoRetryTitle() string {
	return appbackend.DriverForApp(s.app).Runtime().AutoRetryTitle()
}

// ScheduleDelayedTask schedules a delayed function, using the tracker's After
// hook if available, otherwise falling back to time.AfterFunc.
func (s Service) ScheduleDelayedTask(delay time.Duration, fn func()) DelayedTask {
	if tracker := s.AutoRetryTracker(); tracker != nil && tracker.After != nil {
		return tracker.After(delay, fn)
	}
	return time.AfterFunc(delay, fn)
}

// HasPendingAutoRetry reports whether there is a pending auto-retry for the
// given session. If sessionKey is empty it checks all sessions.
func (s Service) HasPendingAutoRetry(sessionKey string) bool {
	if s.app == nil {
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

// CurrentAutoRetryState returns a cloned snapshot of the retry state for the
// given session.
func (s Service) CurrentAutoRetryState(sessionKey string) (RetryState, bool) {
	if s.app == nil {
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
func (s Service) ObserveAutoRetryTerminal(sessionKey, threadID, status string, updatedSess *state.Session, sub *state.Submission, reuseMessageID string) bool {
	if s.app == nil {
		return false
	}
	sessionKey = strings.TrimSpace(sessionKey)
	threadID = strings.TrimSpace(threadID)
	status = strings.TrimSpace(status)
	if sessionKey == "" || threadID == "" {
		return false
	}
	if status != "failed" {
		s.FinishAutoRetryOnTerminal(sessionKey, threadID, status)
		return false
	}
	return s.ScheduleAutoRetryAfterFailure(sessionKey, threadID, updatedSess, sub, reuseMessageID)
}

// FinishAutoRetryOnTerminal cleans up retry state on non-failure terminal
// status (completed, interrupted, or already canceled).
func (s Service) FinishAutoRetryOnTerminal(sessionKey, threadID, status string) {
	if s.app == nil {
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
		s.DeliverAutoRetryCard(snapshot, s.RenderAutoRetryLoopCard(snapshot, "stopped", "已停止自动重试。"))
		return
	}
	switch status {
	case "completed":
		s.DeliverAutoRetryCard(snapshot, s.RenderAutoRetryLoopCard(snapshot, "completed", "已收到非 failed 终态，自动重试结束。"))
	case "interrupted":
		s.DeliverAutoRetryCard(snapshot, s.RenderAutoRetryLoopCard(snapshot, "interrupted", "任务已中断，自动重试结束。"))
	}
}

// ScheduleAutoRetryAfterFailure attempts to schedule an auto-retry after a
// failed turn. Returns true if a retry is now pending.
func (s Service) ScheduleAutoRetryAfterFailure(sessionKey, threadID string, updatedSess *state.Session, sub *state.Submission, reuseMessageID string) bool {
	if s.app == nil {
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
	if apputil.FirstNonEmpty(strings.TrimSpace(updatedSess.Status), "idle") != "idle" || s.app.SessionHasActiveWork(updatedSess) || len(updatedSess.Queue) > 0 {
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
	st.Canceled = false
	RefreshState(st, updatedSess, sub, threadID)
	if strings.TrimSpace(st.StatusMessageID) == "" {
		st.StatusMessageID = strings.TrimSpace(reuseMessageID)
	}
	if st.Timer == nil {
		delay := DelayForStep(st.BackoffStep)
		st.TimerSeq++
		seq := st.TimerSeq
		st.Timer = s.ScheduleDelayedTask(delay, func() {
			s.app.RunAsync(func() {
				s.RunAutoRetryTimer(sessionKey, seq)
			})
		})
	}
	waiting = StateWaiting(st)
	snapshot = CloneState(st)
	tracker.Mu.Unlock()
	s.DeliverAutoRetryCard(snapshot, s.RenderAutoRetryLoopCard(snapshot, "waiting", "当前任务 failed，准备自动发送“继续”。"))
	return waiting
}

// RunAutoRetryTimer is the callback invoked when the backoff timer fires.
func (s Service) RunAutoRetryTimer(sessionKey string, expectedSeq uint64) {
	if s.app == nil {
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
	if st == nil || st.TimerSeq != expectedSeq {
		tracker.Mu.Unlock()
		return
	}
	st.Timer = nil
	snapshot = CloneState(st)
	tracker.Mu.Unlock()

	if snapshot.Canceled {
		s.FinishAutoRetryWithMessage(sessionKey, "stopped", "已停止自动重试。")
		return
	}
	if !s.AutoRetryEnabled() {
		s.FinishAutoRetryWithMessage(sessionKey, "stopped", "自动重试已关闭。")
		return
	}

	sess := s.app.AppState().Session(sessionKey)
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
	if s.app.SessionHasActiveWork(sess) || len(sess.Queue) > 0 {
		s.FinishAutoRetryWithMessage(sessionKey, "stopped", "检测到当前线程已有新任务，自动重试结束。")
		return
	}
	if apputil.FirstNonEmpty(strings.TrimSpace(sess.Status), "idle") != "idle" {
		s.FinishAutoRetryWithMessage(sessionKey, "stopped", "当前会话已不再处于空闲态。")
		return
	}
	if runtime := s.app.AutoRetryBackendRuntime(); runtime != nil && runtime.DeferQueuedSubmissionsDuringRecovery() {
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
func (s Service) BumpAutoRetryBackoffAndReschedule(sessionKey, notice string) {
	if s.app == nil {
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
	st.TimerSeq++
	seq := st.TimerSeq
	st.Timer = s.ScheduleDelayedTask(delay, func() {
		s.app.RunAsync(func() {
			s.RunAutoRetryTimer(sessionKey, seq)
		})
	})
	snapshot = CloneState(st)
	tracker.Mu.Unlock()
	s.DeliverAutoRetryCard(snapshot, s.RenderAutoRetryLoopCard(snapshot, "waiting", notice))
}

// StartAutoRetrySubmission creates and starts a "继续" submission for the
// auto-retry cycle.
func (s Service) StartAutoRetrySubmission(sessionKey string, sess *state.Session, snapshot RetryState) (*state.Submission, error) {
	if s.app == nil || sess == nil {
		return nil, fmt.Errorf("session missing")
	}
	if strings.TrimSpace(snapshot.ThreadID) == "" || strings.TrimSpace(sess.ActiveThreadID) != strings.TrimSpace(snapshot.ThreadID) {
		return nil, fmt.Errorf("active thread changed")
	}
	if !s.app.SessionHasLiveThread(sessionKey, snapshot.ThreadID) {
		return nil, fmt.Errorf("active thread is not live")
	}
	workspaceID := apputil.FirstNonEmpty(strings.TrimSpace(sess.WorkspaceID), strings.TrimSpace(snapshot.WorkspaceID), appcore.DefaultWorkspaceID(s.app))
	ws := config.FindWorkspace(s.app.Config(), workspaceID)
	if ws == nil {
		return nil, fmt.Errorf("workspace %q not found", workspaceID)
	}
	triggerMessageID := apputil.FirstNonEmpty(strings.TrimSpace(snapshot.TriggerMessageID), strings.TrimSpace(sess.RootMessageID))
	sourceRootMessageIDs := append([]string(nil), snapshot.SourceRootMessageIDs...)
	if len(sourceRootMessageIDs) == 0 && strings.TrimSpace(sess.RootMessageID) != "" {
		sourceRootMessageIDs = []string{strings.TrimSpace(sess.RootMessageID)}
	}
	sub := &state.Submission{
		SessionKey:           strings.TrimSpace(sessionKey),
		WorkspaceID:          workspaceID,
		UserID:               strings.TrimSpace(sess.OwnerUserID),
		ChatID:               apputil.FirstNonEmpty(strings.TrimSpace(sess.ChatID), strings.TrimSpace(snapshot.ChatID)),
		TriggerMessageID:     triggerMessageID,
		SourceRootMessageIDs: uniqueStrings(sourceRootMessageIDs),
		InputText:            "继续",
		Status:               "queued",
	}
	id, err := s.app.AppState().CreateSubmission(sub)
	if err != nil {
		return nil, err
	}
	sub.ID = id
	if err := s.app.AutoRetryConversationBackend().StartQueuedSubmission(sessionKey, sess, sub, ws, false); err != nil {
		if current := s.app.AppState().Submission(sub.ID); current != nil {
			sub = current
		}
		return sub, err
	}
	return sub, nil
}

// MarkAutoRetryAttemptStarted records that an auto-retry attempt has been
// dispatched and delivers an updated status card.
func (s Service) MarkAutoRetryAttemptStarted(sessionKey string, sub *state.Submission) {
	if s.app == nil {
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
	RefreshState(st, s.app.AppState().Session(sessionKey), sub, apputil.FirstNonEmpty(strings.TrimSpace(sub.ThreadID), st.ThreadID))
	snapshot = CloneState(st)
	tracker.Mu.Unlock()
	s.DeliverAutoRetryCard(snapshot, s.RenderAutoRetryLoopCard(snapshot, "running", "已自动发送“继续”，等待新的任务结果。"))
}

// CancelAutoRetry cancels the auto-retry for a session. If keepUntilTerminal
// is true the state entry is kept (marked canceled) until the running turn
// reaches a terminal status.
func (s Service) CancelAutoRetry(sessionKey string, keepUntilTerminal bool, notice string) bool {
	if s.app == nil {
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
		s.DeliverAutoRetryCard(snapshot, s.RenderAutoRetryLoopCard(snapshot, "stopped", apputil.FirstNonEmpty(strings.TrimSpace(notice), "已停止自动重试。")))
	}
	return canceled
}

// CancelAllAutoRetry cancels all pending auto-retries and returns the count of
// canceled entries.
func (s Service) CancelAllAutoRetry(notice string) int {
	if s.app == nil {
		return 0
	}
	notice = apputil.FirstNonEmpty(strings.TrimSpace(notice), "已关闭自动重试。")
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
		s.DeliverAutoRetryCard(item.snapshot, s.RenderAutoRetryLoopCard(item.snapshot, "stopped", notice))
	}
	return len(pending)
}

// FinishAutoRetryWithMessage removes the retry state entry and delivers a
// final status card with the given phase and notice.
func (s Service) FinishAutoRetryWithMessage(sessionKey, phase, notice string) {
	if s.app == nil {
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
	s.DeliverAutoRetryCard(snapshot, s.RenderAutoRetryLoopCard(snapshot, phase, notice))
}

// DeliverAutoRetryCard delivers or updates the auto-retry status card. It
// first tries to patch the existing card; if that fails it sends a new one.
func (s Service) DeliverAutoRetryCard(snapshot RetryState, card map[string]any) {
	if s.app == nil || s.app.Feishu() == nil || card == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	messageID := strings.TrimSpace(snapshot.StatusMessageID)
	if messageID != "" {
		if err := s.app.Feishu().PatchCard(ctx, messageID, card); err == nil {
			return
		}
	}
	var sentID string
	var err error
	replyInThread := false
	if sess := s.app.AppState().Session(snapshot.SessionKey); sess != nil {
		replyInThread = appcore.ReplyInThreadEnabled(s.app, sess.ChatType)
	}
	switch {
	case strings.TrimSpace(snapshot.TriggerMessageID) != "":
		sentID, err = s.app.Feishu().ReplyCard(ctx, snapshot.TriggerMessageID, card, replyInThread)
	case strings.TrimSpace(snapshot.ChatID) != "":
		sentID, err = s.app.Feishu().SendCard(ctx, snapshot.ChatID, card)
	default:
		return
	}
	if err != nil || strings.TrimSpace(sentID) == "" {
		return
	}
	tracker := s.AutoRetryTracker()
	tracker.Mu.Lock()
	if st := tracker.States[snapshot.SessionKey]; st != nil {
		current := strings.TrimSpace(st.StatusMessageID)
		if current == "" || current == messageID {
			st.StatusMessageID = strings.TrimSpace(sentID)
		}
	}
	tracker.Mu.Unlock()
}

// RenderAutoRetryLoopCard builds the card for the retry loop status display.
func (s Service) RenderAutoRetryLoopCard(snapshot RetryState, phase, notice string) map[string]any {
	lines := []string{
		"当前线程: `" + apputil.FirstNonEmpty(strings.TrimSpace(snapshot.ThreadID), "-") + "`",
		"累计已重试: `" + fmt.Sprintf("%d", snapshot.RetryCount) + "` 次",
	}
	switch strings.TrimSpace(phase) {
	case "waiting":
		lines = append(lines,
			"下一次自动重试: 第 `"+fmt.Sprintf("%d", snapshot.RetryCount+1)+"` 次",
			"下一次间隔: `"+FormatDelay(DelayForStep(snapshot.BackoffStep))+"`",
		)
	case "running":
		lines = append(lines,
			"当前状态: 已发起第 `"+fmt.Sprintf("%d", snapshot.RetryCount)+"` 次自动重试",
		)
	}
	if text := strings.TrimSpace(notice); text != "" {
		lines = append([]string{text, ""}, lines...)
	}
	lines = append(lines, "", "如需终止，请发送 `/stop`。")
	color := "blue"
	switch strings.TrimSpace(phase) {
	case "waiting":
		color = "orange"
	case "running":
		color = "blue"
	case "completed":
		color = "green"
	case "interrupted", "stopped":
		color = "grey"
	}
	return s.app.Feishu().SimpleStatusCard(s.AutoRetryTitle(), color, strings.Join(lines, "\n"), nil)
}

// RenderAutoRetryConfigCard builds the configuration card for auto-retry.
func (s Service) RenderAutoRetryConfigCard(sessionKey string) map[string]any {
	enabled := s.AutoRetryEnabled()
	lines := []string{
		"当前 frontend: `" + apputil.FirstNonEmpty(strings.TrimSpace(s.app.FrontendID()), config.DefaultFrontendID) + "`",
		"当前 backend: `" + apputil.FirstNonEmpty(appcore.ConfiguredBackend(s.app), "unset") + "`",
		"开关状态: `" + map[bool]string{true: "on", false: "off"}[enabled] + "`",
		"",
		"当 turn 终态为 `failed` 且当前 session 仍保留活动线程时，会按“继续”自动重试。",
		"重试间隔会逐步增大，最长 `15s`。",
		"`/stop` 会停止当前 session 的自动重试流程。",
	}
	if snapshot, ok := s.CurrentAutoRetryState(sessionKey); ok {
		lines = append(lines,
			"",
			"当前 session 正在自动重试中。",
			"当前线程: `"+apputil.FirstNonEmpty(strings.TrimSpace(snapshot.ThreadID), "-")+"`",
			"累计已重试: `"+fmt.Sprintf("%d", snapshot.RetryCount)+"` 次",
		)
	}
	buttons := []feishu.Button{
		{
			Text: "开启",
			Type: func() string {
				if enabled {
					return "primary"
				}
				return "default"
			}(),
			Value: map[string]any{
				"action":      "auto_retry.set",
				"enabled":     "on",
				"session_key": sessionKey,
			},
		},
		{
			Text: "关闭",
			Type: func() string {
				if !enabled {
					return "primary"
				}
				return "default"
			}(),
			Value: map[string]any{
				"action":      "auto_retry.set",
				"enabled":     "off",
				"session_key": sessionKey,
			},
		},
		{
			Text:  "返回上一级",
			Type:  "default",
			Value: map[string]any{"action": "menu.group.backend", "session_key": sessionKey},
		},
	}
	return s.app.Feishu().SimpleStatusCard(s.AutoRetryTitle(), "blue", s.app.MenuCardBody("menu.auto_retry", strings.Join(lines, "\n")), buttons)
}

// CompleteAutoRetrySet handles the card action to toggle auto-retry on or off.
func (s Service) CompleteAutoRetrySet(action *feishu.CardAction, enabled bool) (*callback.CardActionTriggerResponse, error) {
	sessionKey := ActionSessionKey(action)
	if err := s.UpdateAutoRetryEnabled(enabled); err != nil {
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "error", Content: err.Error()},
			Card:  RawCard(s.RenderAutoRetryConfigCard(sessionKey)),
		}, nil
	}
	if !enabled {
		s.CancelAllAutoRetry("已关闭自动重试。")
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已更新自动重试"},
		Card:  RawCard(s.RenderAutoRetryConfigCard(sessionKey)),
	}, nil
}

// CommandAutoRetry handles /backend retry commands.
func (s Service) CommandAutoRetry(msg *feishu.InboundMessage, args []string) error {
	if len(args) > 1 {
		return fmt.Errorf("usage: /backend retry | /backend retry status | /backend retry on | /backend retry off")
	}
	if len(args) == 0 || strings.TrimSpace(args[0]) == "status" {
		card := s.RenderAutoRetryConfigCard(appcore.MakeSessionKey(s.app, msg))
		_, err := s.app.Feishu().ReplyCard(context.Background(), msg.MessageID, card, appcore.ReplyInThreadEnabled(s.app, msg.ChatType))
		return err
	}
	enabled := false
	switch strings.TrimSpace(args[0]) {
	case "on":
		enabled = true
	case "off":
		enabled = false
	default:
		return fmt.Errorf("usage: /backend retry | /backend retry status | /backend retry on | /backend retry off")
	}
	resp, err := s.CompleteAutoRetrySet(CommandActionFromMessage(msg, nil), enabled)
	if err != nil {
		return err
	}
	return s.app.ReplyCommandActionResponse(msg, resp)
}
