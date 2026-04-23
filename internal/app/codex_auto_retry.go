package app

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

const (
	codexAutoRetryInitialDelay = 1 * time.Second
	codexAutoRetryMaxDelay     = 15 * time.Second
)

type delayedTask interface {
	Stop() bool
}

type codexAutoRetryState struct {
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

func (a *App) ensureCodexAutoRetryMapLocked() {
	if a != nil && a.codexAutoRetries == nil {
		a.codexAutoRetries = map[string]*codexAutoRetryState{}
	}
}

func (a *App) codexAutoRetryEnabled() bool {
	cfg := a.feishuConfig()
	return cfg != nil && cfg.CodexAutoRetry
}

func (a *App) updateCodexAutoRetryEnabled(enabled bool) error {
	if a == nil || a.cfg == nil {
		return fmt.Errorf("nil config")
	}
	a.configMu.Lock()
	defer a.configMu.Unlock()
	cfg := a.feishuConfigUnlocked()
	if cfg == nil {
		return fmt.Errorf("frontend config not found")
	}
	cfg.CodexAutoRetry = enabled
	if strings.TrimSpace(a.cfgPath) == "" {
		return nil
	}
	if err := a.cfg.Normalize(filepath.Dir(a.cfgPath)); err != nil {
		return err
	}
	return config.Save(a.cfgPath, a.cfg)
}

func codexAutoRetryDelayForStep(step int) time.Duration {
	delay := codexAutoRetryInitialDelay
	for i := 0; i < step; i++ {
		if delay >= codexAutoRetryMaxDelay {
			return codexAutoRetryMaxDelay
		}
		delay *= 2
	}
	if delay > codexAutoRetryMaxDelay {
		return codexAutoRetryMaxDelay
	}
	return delay
}

func formatCodexAutoRetryDelay(delay time.Duration) string {
	if delay <= 0 {
		return "0s"
	}
	if delay < time.Second {
		return "<1s"
	}
	return fmt.Sprintf("%ds", int64((delay+500*time.Millisecond)/time.Second))
}

func cloneCodexAutoRetryState(src *codexAutoRetryState) codexAutoRetryState {
	if src == nil {
		return codexAutoRetryState{}
	}
	cp := *src
	cp.SourceRootMessageIDs = append([]string(nil), src.SourceRootMessageIDs...)
	cp.Timer = nil
	return cp
}

func (a *App) scheduleDelayedTask(delay time.Duration, fn func()) delayedTask {
	if a != nil && a.codexAutoRetryAfter != nil {
		return a.codexAutoRetryAfter(delay, fn)
	}
	return time.AfterFunc(delay, fn)
}

func (a *App) currentCodexAutoRetryState(sessionKey string) (codexAutoRetryState, bool) {
	if a == nil {
		return codexAutoRetryState{}, false
	}
	a.codexAutoRetryMu.Lock()
	defer a.codexAutoRetryMu.Unlock()
	state := a.codexAutoRetries[strings.TrimSpace(sessionKey)]
	if state == nil {
		return codexAutoRetryState{}, false
	}
	return cloneCodexAutoRetryState(state), true
}

func (a *App) observeCodexAutoRetryTerminal(sessionKey, threadID, status string, updatedSess *state.Session, sub *state.Submission) {
	if a == nil {
		return
	}
	sessionKey = strings.TrimSpace(sessionKey)
	threadID = strings.TrimSpace(threadID)
	status = strings.TrimSpace(status)
	if sessionKey == "" || threadID == "" {
		return
	}
	if status != "failed" {
		a.finishCodexAutoRetryOnTerminal(sessionKey, threadID, status)
		return
	}
	a.scheduleCodexAutoRetryAfterFailure(sessionKey, threadID, updatedSess, sub)
}

func (a *App) finishCodexAutoRetryOnTerminal(sessionKey, threadID, status string) {
	if a == nil {
		return
	}
	sessionKey = strings.TrimSpace(sessionKey)
	threadID = strings.TrimSpace(threadID)
	status = strings.TrimSpace(status)
	if sessionKey == "" || threadID == "" {
		return
	}
	var snapshot codexAutoRetryState
	found := false
	a.codexAutoRetryMu.Lock()
	if state := a.codexAutoRetries[sessionKey]; state != nil && strings.TrimSpace(state.ThreadID) == threadID {
		if state.Timer != nil {
			state.Timer.Stop()
			state.Timer = nil
		}
		snapshot = cloneCodexAutoRetryState(state)
		delete(a.codexAutoRetries, sessionKey)
		found = true
	}
	a.codexAutoRetryMu.Unlock()
	if !found {
		return
	}
	if snapshot.Canceled {
		a.deliverCodexAutoRetryCard(snapshot, a.renderCodexAutoRetryLoopCard(snapshot, "stopped", "已停止自动重试。"))
		return
	}
	switch status {
	case "completed":
		a.deliverCodexAutoRetryCard(snapshot, a.renderCodexAutoRetryLoopCard(snapshot, "completed", "已收到非 failed 终态，自动重试结束。"))
	case "interrupted":
		a.deliverCodexAutoRetryCard(snapshot, a.renderCodexAutoRetryLoopCard(snapshot, "interrupted", "任务已中断，自动重试结束。"))
	}
}

func (a *App) scheduleCodexAutoRetryAfterFailure(sessionKey, threadID string, updatedSess *state.Session, sub *state.Submission) {
	if a == nil {
		return
	}
	sessionKey = strings.TrimSpace(sessionKey)
	threadID = strings.TrimSpace(threadID)
	if sessionKey == "" || threadID == "" {
		return
	}
	if !a.codexAutoRetryEnabled() || normalizeRuntimeBackend(a.configuredBackend()) != backendCodex {
		return
	}
	if updatedSess == nil || strings.TrimSpace(updatedSess.ActiveThreadID) != threadID || strings.TrimSpace(updatedSess.ActiveThreadID) == "" {
		return
	}
	if firstNonEmpty(strings.TrimSpace(updatedSess.Status), "idle") != "idle" || sessionHasActiveWork(updatedSess) || len(updatedSess.Queue) > 0 {
		return
	}

	var snapshot codexAutoRetryState
	a.codexAutoRetryMu.Lock()
	a.ensureCodexAutoRetryMapLocked()
	state := a.codexAutoRetries[sessionKey]
	if state == nil {
		state = &codexAutoRetryState{
			SessionKey: sessionKey,
			ThreadID:   threadID,
		}
		a.codexAutoRetries[sessionKey] = state
	}
	state.Canceled = false
	refreshCodexAutoRetryState(state, updatedSess, sub, threadID)
	if state.Timer == nil {
		delay := codexAutoRetryDelayForStep(state.BackoffStep)
		state.TimerSeq++
		seq := state.TimerSeq
		state.Timer = a.scheduleDelayedTask(delay, func() {
			a.runAsync(func() {
				a.runCodexAutoRetryTimer(sessionKey, seq)
			})
		})
	}
	snapshot = cloneCodexAutoRetryState(state)
	a.codexAutoRetryMu.Unlock()
	a.deliverCodexAutoRetryCard(snapshot, a.renderCodexAutoRetryLoopCard(snapshot, "waiting", "当前任务 failed，准备自动发送“继续”。"))
}

func refreshCodexAutoRetryState(state *codexAutoRetryState, sess *state.Session, sub *state.Submission, threadID string) {
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

func (a *App) runCodexAutoRetryTimer(sessionKey string, expectedSeq uint64) {
	if a == nil {
		return
	}
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return
	}

	var snapshot codexAutoRetryState
	a.codexAutoRetryMu.Lock()
	state := a.codexAutoRetries[sessionKey]
	if state == nil || state.TimerSeq != expectedSeq {
		a.codexAutoRetryMu.Unlock()
		return
	}
	state.Timer = nil
	snapshot = cloneCodexAutoRetryState(state)
	a.codexAutoRetryMu.Unlock()

	if snapshot.Canceled {
		a.finishCodexAutoRetryWithMessage(sessionKey, "stopped", "已停止自动重试。")
		return
	}
	if !a.codexAutoRetryEnabled() {
		a.finishCodexAutoRetryWithMessage(sessionKey, "stopped", "Codex 自动重试已关闭。")
		return
	}
	if normalizeRuntimeBackend(a.configuredBackend()) != backendCodex {
		a.finishCodexAutoRetryWithMessage(sessionKey, "stopped", "当前 frontend 已不再使用 Codex backend。")
		return
	}

	sess := a.appState().session(sessionKey)
	if sess == nil {
		a.finishCodexAutoRetryWithMessage(sessionKey, "stopped", "当前会话已不存在。")
		return
	}
	if strings.TrimSpace(sess.ActiveThreadID) == "" {
		a.finishCodexAutoRetryWithMessage(sessionKey, "stopped", "当前会话已经没有活动线程。")
		return
	}
	if strings.TrimSpace(sess.ActiveThreadID) != strings.TrimSpace(snapshot.ThreadID) {
		a.finishCodexAutoRetryWithMessage(sessionKey, "stopped", "当前会话已切换到其他线程。")
		return
	}
	if sessionHasActiveWork(sess) || len(sess.Queue) > 0 {
		a.finishCodexAutoRetryWithMessage(sessionKey, "stopped", "检测到当前线程已有新任务，自动重试结束。")
		return
	}
	if firstNonEmpty(strings.TrimSpace(sess.Status), "idle") != "idle" {
		a.finishCodexAutoRetryWithMessage(sessionKey, "stopped", "当前会话已不再处于空闲态。")
		return
	}
	if a.codexRuntimeRecovering() {
		a.bumpCodexAutoRetryBackoffAndReschedule(sessionKey, "Codex runtime 正在恢复，继续等待后重试。")
		return
	}

	sub, err := a.startCodexAutoRetrySubmission(sessionKey, sess, snapshot)
	if err != nil {
		a.finishCodexAutoRetryWithMessage(sessionKey, "stopped", "自动重试启动失败: "+err.Error())
		return
	}
	a.markCodexAutoRetryAttemptStarted(sessionKey, sub)
}

func (a *App) bumpCodexAutoRetryBackoffAndReschedule(sessionKey, notice string) {
	if a == nil {
		return
	}
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return
	}
	var snapshot codexAutoRetryState
	a.codexAutoRetryMu.Lock()
	state := a.codexAutoRetries[sessionKey]
	if state == nil || state.Canceled {
		a.codexAutoRetryMu.Unlock()
		return
	}
	state.BackoffStep++
	delay := codexAutoRetryDelayForStep(state.BackoffStep)
	state.TimerSeq++
	seq := state.TimerSeq
	state.Timer = a.scheduleDelayedTask(delay, func() {
		a.runAsync(func() {
			a.runCodexAutoRetryTimer(sessionKey, seq)
		})
	})
	snapshot = cloneCodexAutoRetryState(state)
	a.codexAutoRetryMu.Unlock()
	a.deliverCodexAutoRetryCard(snapshot, a.renderCodexAutoRetryLoopCard(snapshot, "waiting", notice))
}

func (a *App) startCodexAutoRetrySubmission(sessionKey string, sess *state.Session, snapshot codexAutoRetryState) (*state.Submission, error) {
	if a == nil || sess == nil {
		return nil, fmt.Errorf("session missing")
	}
	if strings.TrimSpace(snapshot.ThreadID) == "" || strings.TrimSpace(sess.ActiveThreadID) != strings.TrimSpace(snapshot.ThreadID) {
		return nil, fmt.Errorf("active thread changed")
	}
	if !a.sessionHasLiveThread(sessionKey, snapshot.ThreadID) {
		return nil, fmt.Errorf("active thread is not live")
	}
	workspaceID := firstNonEmpty(strings.TrimSpace(sess.WorkspaceID), strings.TrimSpace(snapshot.WorkspaceID), a.defaultWorkspaceID())
	ws := config.FindWorkspace(a.cfg, workspaceID)
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
	id, err := a.appState().createSubmission(sub)
	if err != nil {
		return nil, err
	}
	sub.ID = id
	if err := newSubmissionWorkflow(a).startNextCodexSubmissionWithFailureNotice(sessionKey, sess, sub, ws, false); err != nil {
		if current := a.appState().submission(sub.ID); current != nil {
			sub = current
		}
		return sub, nil
	}
	return sub, nil
}

func (a *App) markCodexAutoRetryAttemptStarted(sessionKey string, sub *state.Submission) {
	if a == nil {
		return
	}
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return
	}
	var snapshot codexAutoRetryState
	a.codexAutoRetryMu.Lock()
	state := a.codexAutoRetries[sessionKey]
	if state == nil || state.Canceled {
		a.codexAutoRetryMu.Unlock()
		return
	}
	state.RetryCount++
	state.BackoffStep++
	refreshCodexAutoRetryState(state, a.appState().session(sessionKey), sub, firstNonEmpty(strings.TrimSpace(sub.ThreadID), state.ThreadID))
	snapshot = cloneCodexAutoRetryState(state)
	a.codexAutoRetryMu.Unlock()
	a.deliverCodexAutoRetryCard(snapshot, a.renderCodexAutoRetryLoopCard(snapshot, "running", "已自动发送“继续”，等待新的任务结果。"))
}

func (a *App) cancelCodexAutoRetry(sessionKey string, keepUntilTerminal bool, notice string) bool {
	if a == nil {
		return false
	}
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return false
	}
	var snapshot codexAutoRetryState
	canceled := false
	a.codexAutoRetryMu.Lock()
	a.ensureCodexAutoRetryMapLocked()
	state := a.codexAutoRetries[sessionKey]
	if state != nil {
		if state.Timer != nil {
			state.Timer.Stop()
			state.Timer = nil
		}
		state.Canceled = true
		snapshot = cloneCodexAutoRetryState(state)
		if !keepUntilTerminal {
			delete(a.codexAutoRetries, sessionKey)
		}
		canceled = true
	}
	a.codexAutoRetryMu.Unlock()
	if canceled {
		a.deliverCodexAutoRetryCard(snapshot, a.renderCodexAutoRetryLoopCard(snapshot, "stopped", firstNonEmpty(strings.TrimSpace(notice), "已停止自动重试。")))
	}
	return canceled
}

func (a *App) cancelAllCodexAutoRetry(notice string) int {
	if a == nil {
		return 0
	}
	notice = firstNonEmpty(strings.TrimSpace(notice), "已关闭 Codex 自动重试。")
	type pendingCard struct {
		snapshot codexAutoRetryState
	}
	pending := []pendingCard{}
	a.codexAutoRetryMu.Lock()
	a.ensureCodexAutoRetryMapLocked()
	for sessionKey, state := range a.codexAutoRetries {
		if state == nil {
			delete(a.codexAutoRetries, sessionKey)
			continue
		}
		if state.Timer != nil {
			state.Timer.Stop()
			state.Timer = nil
		}
		pending = append(pending, pendingCard{snapshot: cloneCodexAutoRetryState(state)})
		delete(a.codexAutoRetries, sessionKey)
	}
	a.codexAutoRetryMu.Unlock()
	for _, item := range pending {
		a.deliverCodexAutoRetryCard(item.snapshot, a.renderCodexAutoRetryLoopCard(item.snapshot, "stopped", notice))
	}
	return len(pending)
}

func (a *App) finishCodexAutoRetryWithMessage(sessionKey, phase, notice string) {
	if a == nil {
		return
	}
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return
	}
	var snapshot codexAutoRetryState
	a.codexAutoRetryMu.Lock()
	state := a.codexAutoRetries[sessionKey]
	if state == nil {
		a.codexAutoRetryMu.Unlock()
		return
	}
	if state.Timer != nil {
		state.Timer.Stop()
		state.Timer = nil
	}
	snapshot = cloneCodexAutoRetryState(state)
	delete(a.codexAutoRetries, sessionKey)
	a.codexAutoRetryMu.Unlock()
	a.deliverCodexAutoRetryCard(snapshot, a.renderCodexAutoRetryLoopCard(snapshot, phase, notice))
}

func (a *App) deliverCodexAutoRetryCard(snapshot codexAutoRetryState, card map[string]any) {
	if a == nil || a.feishu == nil || card == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	messageID := strings.TrimSpace(snapshot.StatusMessageID)
	if messageID != "" {
		if err := a.feishu.PatchCard(ctx, messageID, card); err == nil {
			return
		}
	}
	var sentID string
	var err error
	replyInThread := false
	if sess := a.appState().session(snapshot.SessionKey); sess != nil {
		replyInThread = a.replyInThreadEnabled(sess.ChatType)
	}
	switch {
	case strings.TrimSpace(snapshot.TriggerMessageID) != "":
		sentID, err = a.feishu.ReplyCard(ctx, snapshot.TriggerMessageID, card, replyInThread)
	case strings.TrimSpace(snapshot.ChatID) != "":
		sentID, err = a.feishu.SendCard(ctx, snapshot.ChatID, card)
	default:
		return
	}
	if err != nil || strings.TrimSpace(sentID) == "" {
		return
	}
	a.codexAutoRetryMu.Lock()
	if state := a.codexAutoRetries[snapshot.SessionKey]; state != nil && strings.TrimSpace(state.StatusMessageID) == "" {
		state.StatusMessageID = strings.TrimSpace(sentID)
	}
	a.codexAutoRetryMu.Unlock()
}

func (a *App) renderCodexAutoRetryLoopCard(snapshot codexAutoRetryState, phase, notice string) map[string]any {
	lines := []string{
		"当前线程: `" + firstNonEmpty(strings.TrimSpace(snapshot.ThreadID), "-") + "`",
		"累计已重试: `" + fmt.Sprintf("%d", snapshot.RetryCount) + "` 次",
	}
	switch strings.TrimSpace(phase) {
	case "waiting":
		lines = append(lines,
			"下一次自动重试: 第 `"+fmt.Sprintf("%d", snapshot.RetryCount+1)+"` 次",
			"下一次间隔: `"+formatCodexAutoRetryDelay(codexAutoRetryDelayForStep(snapshot.BackoffStep))+"`",
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
	return a.feishu.SimpleStatusCard("Codex 自动重试", color, strings.Join(lines, "\n"), nil)
}

func (a *App) renderCodexAutoRetryConfigCard(sessionKey string) map[string]any {
	enabled := a.codexAutoRetryEnabled()
	lines := []string{
		"当前 frontend: `" + firstNonEmpty(strings.TrimSpace(a.frontendID), config.DefaultFrontendID) + "`",
		"当前 backend: `" + firstNonEmpty(a.configuredBackend(), "unset") + "`",
		"开关状态: `" + map[bool]string{true: "on", false: "off"}[enabled] + "`",
		"",
		"只对 Codex 生效。",
		"当 turn 终态为 `failed` 且当前 session 仍保留活动线程时，会按“继续”自动重试。",
		"重试间隔会逐步增大，最长 `15s`。",
		"`/stop` 会停止当前 session 的自动重试流程。",
	}
	if snapshot, ok := a.currentCodexAutoRetryState(sessionKey); ok {
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
				"action":      "codex_retry.set",
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
				"action":      "codex_retry.set",
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
	return a.feishu.SimpleStatusCard("Codex 自动重试", "blue", menuCardBody("menu.codex_retry", strings.Join(lines, "\n")), buttons)
}

func (a *App) completeCodexAutoRetrySet(action *feishu.CardAction, enabled bool) (*callback.CardActionTriggerResponse, error) {
	sessionKey := actionSessionKey(action)
	if err := a.updateCodexAutoRetryEnabled(enabled); err != nil {
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "error", Content: err.Error()},
			Card:  rawCard(a.renderCodexAutoRetryConfigCard(sessionKey)),
		}, nil
	}
	if !enabled {
		a.cancelAllCodexAutoRetry("已关闭 Codex 自动重试。")
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已更新 Codex 自动重试"},
		Card:  rawCard(a.renderCodexAutoRetryConfigCard(sessionKey)),
	}, nil
}

func (a *App) commandCodexAutoRetry(msg *feishu.InboundMessage, args []string) error {
	if len(args) > 1 {
		return fmt.Errorf("usage: /backend retry | /backend retry status | /backend retry on | /backend retry off")
	}
	if len(args) == 0 || strings.TrimSpace(args[0]) == "status" {
		card := a.renderCodexAutoRetryConfigCard(a.makeSessionKey(msg))
		_, err := a.feishu.ReplyCard(context.Background(), msg.MessageID, card, a.replyInThreadEnabled(msg.ChatType))
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
	resp, err := a.completeCodexAutoRetrySet(a.commandActionFromMessage(msg, nil), enabled)
	if err != nil {
		return err
	}
	return a.replyCommandActionResponse(msg, resp)
}
