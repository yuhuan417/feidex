package app

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

type autoRetryService struct {
	app *App
}
func newAutoRetryService(app *App) autoRetryService {
	return autoRetryService{app: app}
}

const (
	autoRetryInitialDelay = 1 * time.Second
	autoRetryMaxDelay     = 15 * time.Second
)

type delayedTask interface {
	Stop() bool
}

type autoRetryTracker struct {
	mu     sync.Mutex
	states map[string]*autoRetryState
	after  func(time.Duration, func()) delayedTask
}

func newAutoRetryTracker() *autoRetryTracker {
	return &autoRetryTracker{states: map[string]*autoRetryState{}}
}

func (s autoRetryService) autoRetryTracker() *autoRetryTracker {
	if s.app == nil {
		return nil
	}
	if s.app.autoRetries == nil {
		s.app.autoRetries = newAutoRetryTracker()
	}
	return s.app.autoRetries
}

type autoRetryState struct {
	SessionKey           string
	ThreadID             string
	WorkspaceID          string
	ChatID               string
	TriggerMessageID     string
	SourceRootMessageIDs []string
	RetryCount           int
	BackoffStep          int
	StatusMessageID      string
	Timer                delayedTask
	TimerSeq             uint64
	Canceled             bool
}

func (s autoRetryService) autoRetryEnabled() bool {
	cfg := feishuConfig(s.app)
	return cfg != nil && cfg.AutoRetry
}

func (s autoRetryService) updateAutoRetryEnabled(enabled bool) error {
	if s.app == nil || s.app.cfg == nil {
		return fmt.Errorf("nil config")
	}
	s.app.configMu.Lock()
	defer s.app.configMu.Unlock()
	cfg := feishuConfigUnlocked(s.app)
	if cfg == nil {
		return fmt.Errorf("frontend config not found")
	}
	cfg.AutoRetry = enabled
	if strings.TrimSpace(s.app.cfgPath) == "" {
		return nil
	}
	if err := s.app.cfg.Normalize(filepath.Dir(s.app.cfgPath)); err != nil {
		return err
	}
	return config.Save(s.app.cfgPath, s.app.cfg)
}

func autoRetryDelayForStep(step int) time.Duration {
	delay := autoRetryInitialDelay
	for i := 0; i < step; i++ {
		if delay >= autoRetryMaxDelay {
			return autoRetryMaxDelay
		}
		delay *= 2
	}
	if delay > autoRetryMaxDelay {
		return autoRetryMaxDelay
	}
	return delay
}

func formatAutoRetryDelay(delay time.Duration) string {
	if delay <= 0 {
		return "0s"
	}
	if delay < time.Second {
		return "<1s"
	}
	return fmt.Sprintf("%ds", int64((delay+500*time.Millisecond)/time.Second))
}

func (s autoRetryService) autoRetryTitle() string {
	backend := strings.TrimSpace(configuredBackend(s.app))
	switch normalizeRuntimeBackend(backend) {
	case backendClaude:
		return "Claude 自动重试"
	default:
		return "Codex 自动重试"
	}
}

func cloneAutoRetryState(src *autoRetryState) autoRetryState {
	if src == nil {
		return autoRetryState{}
	}
	cp := *src
	cp.SourceRootMessageIDs = append([]string(nil), src.SourceRootMessageIDs...)
	cp.Timer = nil
	return cp
}

func (s autoRetryService) scheduleDelayedTask(delay time.Duration, fn func()) delayedTask {
	if tracker := s.autoRetryTracker(); tracker != nil && tracker.after != nil {
		return tracker.after(delay, fn)
	}
	return time.AfterFunc(delay, fn)
}

func autoRetryStateWaiting(state *autoRetryState) bool {
	return state != nil && !state.Canceled && state.Timer != nil
}

func (s autoRetryService) hasPendingAutoRetry(sessionKey string) bool {
	if s.app == nil {
		return false
	}
	sessionKey = strings.TrimSpace(sessionKey)
	tracker := s.autoRetryTracker()
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	if sessionKey != "" {
		return autoRetryStateWaiting(tracker.states[sessionKey])
	}
	for _, state := range tracker.states {
		if autoRetryStateWaiting(state) {
			return true
		}
	}
	return false
}

func (s autoRetryService) currentAutoRetryState(sessionKey string) (autoRetryState, bool) {
	if s.app == nil {
		return autoRetryState{}, false
	}
	tracker := s.autoRetryTracker()
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	state := tracker.states[strings.TrimSpace(sessionKey)]
	if state == nil {
		return autoRetryState{}, false
	}
	return cloneAutoRetryState(state), true
}

func (s autoRetryService) observeAutoRetryTerminal(sessionKey, threadID, status string, updatedSess *state.Session, sub *state.Submission, reuseMessageID string) bool {
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
		s.finishAutoRetryOnTerminal(sessionKey, threadID, status)
		return false
	}
	return s.scheduleAutoRetryAfterFailure(sessionKey, threadID, updatedSess, sub, reuseMessageID)
}

func (s autoRetryService) finishAutoRetryOnTerminal(sessionKey, threadID, status string) {
	if s.app == nil {
		return
	}
	sessionKey = strings.TrimSpace(sessionKey)
	threadID = strings.TrimSpace(threadID)
	status = strings.TrimSpace(status)
	if sessionKey == "" || threadID == "" {
		return
	}
	var snapshot autoRetryState
	found := false
	tracker := s.autoRetryTracker()
	tracker.mu.Lock()
	if state := tracker.states[sessionKey]; state != nil && strings.TrimSpace(state.ThreadID) == threadID {
		if state.Timer != nil {
			state.Timer.Stop()
			state.Timer = nil
		}
		snapshot = cloneAutoRetryState(state)
		delete(tracker.states, sessionKey)
		found = true
	}
	tracker.mu.Unlock()
	if !found {
		return
	}
	if snapshot.Canceled {
		s.deliverAutoRetryCard(snapshot, s.renderAutoRetryLoopCard(snapshot, "stopped", "已停止自动重试。"))
		return
	}
	switch status {
	case "completed":
		s.deliverAutoRetryCard(snapshot, s.renderAutoRetryLoopCard(snapshot, "completed", "已收到非 failed 终态，自动重试结束。"))
	case "interrupted":
		s.deliverAutoRetryCard(snapshot, s.renderAutoRetryLoopCard(snapshot, "interrupted", "任务已中断，自动重试结束。"))
	}
}

func (s autoRetryService) scheduleAutoRetryAfterFailure(sessionKey, threadID string, updatedSess *state.Session, sub *state.Submission, reuseMessageID string) bool {
	if s.app == nil {
		return false
	}
	sessionKey = strings.TrimSpace(sessionKey)
	threadID = strings.TrimSpace(threadID)
	if sessionKey == "" || threadID == "" {
		return false
	}
	if !s.autoRetryEnabled() {
		return false
	}
	if updatedSess == nil || strings.TrimSpace(updatedSess.ActiveThreadID) != threadID || strings.TrimSpace(updatedSess.ActiveThreadID) == "" {
		return false
	}
	if firstNonEmpty(strings.TrimSpace(updatedSess.Status), "idle") != "idle" || sessionHasActiveWork(updatedSess) || len(updatedSess.Queue) > 0 {
		return false
	}

	var (
		snapshot autoRetryState
		waiting  bool
	)
	tracker := s.autoRetryTracker()
	tracker.mu.Lock()
	state := tracker.states[sessionKey]
	if state == nil {
		state = &autoRetryState{
			SessionKey: sessionKey,
			ThreadID:   threadID,
		}
		tracker.states[sessionKey] = state
	}
	state.Canceled = false
	refreshAutoRetryState(state, updatedSess, sub, threadID)
	if strings.TrimSpace(state.StatusMessageID) == "" {
		state.StatusMessageID = strings.TrimSpace(reuseMessageID)
	}
	if state.Timer == nil {
		delay := autoRetryDelayForStep(state.BackoffStep)
		state.TimerSeq++
		seq := state.TimerSeq
		state.Timer = s.scheduleDelayedTask(delay, func() {
			runAsync(s.app, func() {
				s.runAutoRetryTimer(sessionKey, seq)
			})
		})
	}
	waiting = autoRetryStateWaiting(state)
	snapshot = cloneAutoRetryState(state)
	tracker.mu.Unlock()
	s.deliverAutoRetryCard(snapshot, s.renderAutoRetryLoopCard(snapshot, "waiting", "当前任务 failed，准备自动发送“继续”。"))
	return waiting
}

func refreshAutoRetryState(state *autoRetryState, sess *state.Session, sub *state.Submission, threadID string) {
	if state == nil {
		return
	}
	if strings.TrimSpace(threadID) != "" {
		state.ThreadID = strings.TrimSpace(threadID)
	}
	if sub != nil {
		state.WorkspaceID = firstNonEmpty(strings.TrimSpace(sub.WorkspaceID), state.WorkspaceID)
		state.ChatID = firstNonEmpty(strings.TrimSpace(sub.ChatID), state.ChatID)
		state.TriggerMessageID = firstNonEmpty(strings.TrimSpace(sub.TriggerMessageID), state.TriggerMessageID)
		if len(sub.SourceRootMessageIDs) > 0 {
			state.SourceRootMessageIDs = append([]string(nil), sub.SourceRootMessageIDs...)
		}
	}
	if sess != nil {
		state.WorkspaceID = firstNonEmpty(strings.TrimSpace(sess.WorkspaceID), state.WorkspaceID)
		state.ChatID = firstNonEmpty(strings.TrimSpace(sess.ChatID), state.ChatID)
		if rootMessageID := strings.TrimSpace(sess.RootMessageID); len(state.SourceRootMessageIDs) == 0 && rootMessageID != "" {
			state.SourceRootMessageIDs = []string{rootMessageID}
		}
		if strings.TrimSpace(state.TriggerMessageID) == "" {
			state.TriggerMessageID = strings.TrimSpace(sess.RootMessageID)
		}
	}
}

func (s autoRetryService) runAutoRetryTimer(sessionKey string, expectedSeq uint64) {
	if s.app == nil {
		return
	}
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return
	}

	var snapshot autoRetryState
	tracker := s.autoRetryTracker()
	tracker.mu.Lock()
	state := tracker.states[sessionKey]
	if state == nil || state.TimerSeq != expectedSeq {
		tracker.mu.Unlock()
		return
	}
	state.Timer = nil
	snapshot = cloneAutoRetryState(state)
	tracker.mu.Unlock()

	if snapshot.Canceled {
		s.finishAutoRetryWithMessage(sessionKey, "stopped", "已停止自动重试。")
		return
	}
	if !s.autoRetryEnabled() {
		s.finishAutoRetryWithMessage(sessionKey, "stopped", "自动重试已关闭。")
		return
	}

	sess := appState(s.app).session(sessionKey)
	if sess == nil {
		s.finishAutoRetryWithMessage(sessionKey, "stopped", "当前会话已不存在。")
		return
	}
	if strings.TrimSpace(sess.ActiveThreadID) == "" {
		s.finishAutoRetryWithMessage(sessionKey, "stopped", "当前会话已经没有活动线程。")
		return
	}
	if strings.TrimSpace(sess.ActiveThreadID) != strings.TrimSpace(snapshot.ThreadID) {
		s.finishAutoRetryWithMessage(sessionKey, "stopped", "当前会话已切换到其他线程。")
		return
	}
	if sessionHasActiveWork(sess) || len(sess.Queue) > 0 {
		s.finishAutoRetryWithMessage(sessionKey, "stopped", "检测到当前线程已有新任务，自动重试结束。")
		return
	}
	if firstNonEmpty(strings.TrimSpace(sess.Status), "idle") != "idle" {
		s.finishAutoRetryWithMessage(sessionKey, "stopped", "当前会话已不再处于空闲态。")
		return
	}
	if runtime := backendRuntime(s.app); runtime != nil && runtime.deferQueuedSubmissionsDuringRecovery(s.app) {
		s.bumpAutoRetryBackoffAndReschedule(sessionKey, "运行时正在恢复，继续等待后重试。")
		return
	}

	sub, err := s.startAutoRetrySubmission(sessionKey, sess, snapshot)
	if err != nil {
		if s.hasPendingAutoRetry(sessionKey) {
			return
		}
		s.finishAutoRetryWithMessage(sessionKey, "stopped", "自动重试启动失败: "+err.Error())
		return
	}
	s.markAutoRetryAttemptStarted(sessionKey, sub)
}

func (s autoRetryService) bumpAutoRetryBackoffAndReschedule(sessionKey, notice string) {
	if s.app == nil {
		return
	}
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return
	}
	var snapshot autoRetryState
	tracker := s.autoRetryTracker()
	tracker.mu.Lock()
	state := tracker.states[sessionKey]
	if state == nil || state.Canceled {
		tracker.mu.Unlock()
		return
	}
	state.BackoffStep++
	delay := autoRetryDelayForStep(state.BackoffStep)
	state.TimerSeq++
	seq := state.TimerSeq
	state.Timer = s.scheduleDelayedTask(delay, func() {
		runAsync(s.app, func() {
			s.runAutoRetryTimer(sessionKey, seq)
		})
	})
	snapshot = cloneAutoRetryState(state)
	tracker.mu.Unlock()
	s.deliverAutoRetryCard(snapshot, s.renderAutoRetryLoopCard(snapshot, "waiting", notice))
}

func (s autoRetryService) startAutoRetrySubmission(sessionKey string, sess *state.Session, snapshot autoRetryState) (*state.Submission, error) {
	if s.app == nil || sess == nil {
		return nil, fmt.Errorf("session missing")
	}
	if strings.TrimSpace(snapshot.ThreadID) == "" || strings.TrimSpace(sess.ActiveThreadID) != strings.TrimSpace(snapshot.ThreadID) {
		return nil, fmt.Errorf("active thread changed")
	}
	if !sessionHasLiveThread(s.app, sessionKey, snapshot.ThreadID) {
		return nil, fmt.Errorf("active thread is not live")
	}
	workspaceID := firstNonEmpty(strings.TrimSpace(sess.WorkspaceID), strings.TrimSpace(snapshot.WorkspaceID), defaultWorkspaceID(s.app))
	ws := config.FindWorkspace(s.app.cfg, workspaceID)
	if ws == nil {
		return nil, fmt.Errorf("workspace %q not found", workspaceID)
	}
	triggerMessageID := firstNonEmpty(strings.TrimSpace(snapshot.TriggerMessageID), strings.TrimSpace(sess.RootMessageID))
	sourceRootMessageIDs := append([]string(nil), snapshot.SourceRootMessageIDs...)
	if len(sourceRootMessageIDs) == 0 && strings.TrimSpace(sess.RootMessageID) != "" {
		sourceRootMessageIDs = []string{strings.TrimSpace(sess.RootMessageID)}
	}
	sub := &state.Submission{
		SessionKey:           strings.TrimSpace(sessionKey),
		WorkspaceID:          workspaceID,
		UserID:               strings.TrimSpace(sess.OwnerUserID),
		ChatID:               firstNonEmpty(strings.TrimSpace(sess.ChatID), strings.TrimSpace(snapshot.ChatID)),
		TriggerMessageID:     triggerMessageID,
		SourceRootMessageIDs: uniqueStrings(sourceRootMessageIDs),
		InputText:            "继续",
		Status:               "queued",
	}
	id, err := appState(s.app).createSubmission(sub)
	if err != nil {
		return nil, err
	}
	sub.ID = id
	if err := conversationBackend(s.app).startQueuedSubmission(sessionKey, sess, sub, ws, false); err != nil {
		if current := appState(s.app).submission(sub.ID); current != nil {
			sub = current
		}
		return sub, err
	}
	return sub, nil
}

func (s autoRetryService) markAutoRetryAttemptStarted(sessionKey string, sub *state.Submission) {
	if s.app == nil {
		return
	}
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return
	}
	var snapshot autoRetryState
	tracker := s.autoRetryTracker()
	tracker.mu.Lock()
	state := tracker.states[sessionKey]
	if state == nil || state.Canceled {
		tracker.mu.Unlock()
		return
	}
	state.RetryCount++
	state.BackoffStep++
	refreshAutoRetryState(state, appState(s.app).session(sessionKey), sub, firstNonEmpty(strings.TrimSpace(sub.ThreadID), state.ThreadID))
	snapshot = cloneAutoRetryState(state)
	tracker.mu.Unlock()
	s.deliverAutoRetryCard(snapshot, s.renderAutoRetryLoopCard(snapshot, "running", "已自动发送“继续”，等待新的任务结果。"))
}

func (s autoRetryService) cancelAutoRetry(sessionKey string, keepUntilTerminal bool, notice string) bool {
	if s.app == nil {
		return false
	}
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return false
	}
	var snapshot autoRetryState
	canceled := false
	tracker := s.autoRetryTracker()
	tracker.mu.Lock()
	state := tracker.states[sessionKey]
	if state != nil {
		if state.Timer != nil {
			state.Timer.Stop()
			state.Timer = nil
		}
		state.Canceled = true
		snapshot = cloneAutoRetryState(state)
		if !keepUntilTerminal {
			delete(tracker.states, sessionKey)
		}
		canceled = true
	}
	tracker.mu.Unlock()
	if canceled {
		s.deliverAutoRetryCard(snapshot, s.renderAutoRetryLoopCard(snapshot, "stopped", firstNonEmpty(strings.TrimSpace(notice), "已停止自动重试。")))
	}
	return canceled
}

func (s autoRetryService) cancelAllAutoRetry(notice string) int {
	if s.app == nil {
		return 0
	}
	notice = firstNonEmpty(strings.TrimSpace(notice), "已关闭自动重试。")
	type pendingCard struct {
		snapshot autoRetryState
	}
	pending := []pendingCard{}
	tracker := s.autoRetryTracker()
	tracker.mu.Lock()
	for sessionKey, state := range tracker.states {
		if state == nil {
			delete(tracker.states, sessionKey)
			continue
		}
		if state.Timer != nil {
			state.Timer.Stop()
			state.Timer = nil
		}
		pending = append(pending, pendingCard{snapshot: cloneAutoRetryState(state)})
		delete(tracker.states, sessionKey)
	}
	tracker.mu.Unlock()
	for _, item := range pending {
		s.deliverAutoRetryCard(item.snapshot, s.renderAutoRetryLoopCard(item.snapshot, "stopped", notice))
	}
	return len(pending)
}

func (s autoRetryService) finishAutoRetryWithMessage(sessionKey, phase, notice string) {
	if s.app == nil {
		return
	}
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return
	}
	var snapshot autoRetryState
	tracker := s.autoRetryTracker()
	tracker.mu.Lock()
	state := tracker.states[sessionKey]
	if state == nil {
		tracker.mu.Unlock()
		return
	}
	if state.Timer != nil {
		state.Timer.Stop()
		state.Timer = nil
	}
	snapshot = cloneAutoRetryState(state)
	delete(tracker.states, sessionKey)
	tracker.mu.Unlock()
	s.deliverAutoRetryCard(snapshot, s.renderAutoRetryLoopCard(snapshot, phase, notice))
}

func (s autoRetryService) deliverAutoRetryCard(snapshot autoRetryState, card map[string]any) {
	if s.app == nil || s.app.feishu == nil || card == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	messageID := strings.TrimSpace(snapshot.StatusMessageID)
	if messageID != "" {
		if err := s.app.feishu.PatchCard(ctx, messageID, card); err == nil {
			return
		}
	}
	var sentID string
	var err error
	replyInThread := false
	if sess := appState(s.app).session(snapshot.SessionKey); sess != nil {
		replyInThread = replyInThreadEnabled(s.app, sess.ChatType)
	}
	switch {
	case strings.TrimSpace(snapshot.TriggerMessageID) != "":
		sentID, err = s.app.feishu.ReplyCard(ctx, snapshot.TriggerMessageID, card, replyInThread)
	case strings.TrimSpace(snapshot.ChatID) != "":
		sentID, err = s.app.feishu.SendCard(ctx, snapshot.ChatID, card)
	default:
		return
	}
	if err != nil || strings.TrimSpace(sentID) == "" {
		return
	}
	tracker := s.autoRetryTracker()
	tracker.mu.Lock()
	if state := tracker.states[snapshot.SessionKey]; state != nil {
		current := strings.TrimSpace(state.StatusMessageID)
		if current == "" || current == messageID {
			state.StatusMessageID = strings.TrimSpace(sentID)
		}
	}
	tracker.mu.Unlock()
}

func (s autoRetryService) renderAutoRetryLoopCard(snapshot autoRetryState, phase, notice string) map[string]any {
	lines := []string{
		"当前线程: `" + firstNonEmpty(strings.TrimSpace(snapshot.ThreadID), "-") + "`",
		"累计已重试: `" + fmt.Sprintf("%d", snapshot.RetryCount) + "` 次",
	}
	switch strings.TrimSpace(phase) {
	case "waiting":
		lines = append(lines,
			"下一次自动重试: 第 `"+fmt.Sprintf("%d", snapshot.RetryCount+1)+"` 次",
			"下一次间隔: `"+formatAutoRetryDelay(autoRetryDelayForStep(snapshot.BackoffStep))+"`",
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
	return s.app.feishu.SimpleStatusCard(s.autoRetryTitle(), color, strings.Join(lines, "\n"), nil)
}

func (s autoRetryService) renderAutoRetryConfigCard(sessionKey string) map[string]any {
	enabled := s.autoRetryEnabled()
	lines := []string{
		"当前 frontend: `" + firstNonEmpty(strings.TrimSpace(s.app.frontendID), config.DefaultFrontendID) + "`",
		"当前 backend: `" + firstNonEmpty(configuredBackend(s.app), "unset") + "`",
		"开关状态: `" + map[bool]string{true: "on", false: "off"}[enabled] + "`",
		"",
		"当 turn 终态为 `failed` 且当前 session 仍保留活动线程时，会按“继续”自动重试。",
		"重试间隔会逐步增大，最长 `15s`。",
		"`/stop` 会停止当前 session 的自动重试流程。",
	}
	if snapshot, ok := s.currentAutoRetryState(sessionKey); ok {
		lines = append(lines,
			"",
			"当前 session 正在自动重试中。",
			"当前线程: `"+firstNonEmpty(strings.TrimSpace(snapshot.ThreadID), "-")+"`",
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
	return s.app.feishu.SimpleStatusCard(s.autoRetryTitle(), "blue", menuCardBody("menu.auto_retry", strings.Join(lines, "\n")), buttons)
}

func (s autoRetryService) completeAutoRetrySet(action *feishu.CardAction, enabled bool) (*callback.CardActionTriggerResponse, error) {
	sessionKey := actionSessionKey(action)
	if err := s.updateAutoRetryEnabled(enabled); err != nil {
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "error", Content: err.Error()},
			Card:  rawCard(s.renderAutoRetryConfigCard(sessionKey)),
		}, nil
	}
	if !enabled {
		s.cancelAllAutoRetry("已关闭自动重试。")
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已更新自动重试"},
		Card:  rawCard(s.renderAutoRetryConfigCard(sessionKey)),
	}, nil
}

func (s autoRetryService) commandAutoRetry(msg *feishu.InboundMessage, args []string) error {
	if len(args) > 1 {
		return fmt.Errorf("usage: /backend retry | /backend retry status | /backend retry on | /backend retry off")
	}
	if len(args) == 0 || strings.TrimSpace(args[0]) == "status" {
		card := s.renderAutoRetryConfigCard(makeSessionKey(s.app, msg))
		_, err := s.app.feishu.ReplyCard(context.Background(), msg.MessageID, card, replyInThreadEnabled(s.app, msg.ChatType))
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
	resp, err := s.completeAutoRetrySet(commandActionFromMessage(msg, nil), enabled)
	if err != nil {
		return err
	}
	return replyCommandActionResponse(s.app, msg, resp)
}
