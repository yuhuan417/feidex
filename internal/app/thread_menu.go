package app

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"feidex/internal/codexrpc"
	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

const threadCommandUsage = "/thread | /thread list [all] | /thread new | /thread fork | /thread resume THREAD_ID | /thread sandbox [MODE] | /thread policy [POLICY]"

func (a *App) commandNew(msg *feishu.InboundMessage) error {
	return a.commandThreadsNew(msg)
}

func (a *App) startFreshThread(sessionKey, userID, chatID, chatType string) (int, *workspaceThreadBinding, error) {
	if a == nil || a.store == nil {
		return 0, nil, fmt.Errorf("store not initialized")
	}
	appState := a.appState()
	defaultWorkspaceID := "default"
	if a.cfg != nil && len(a.cfg.Workspaces) > 0 {
		defaultWorkspaceID = a.cfg.Workspaces[0].ID
	}
	sess := appState.session(sessionKey)
	if sess != nil && sessionHasActiveWork(sess) {
		return 0, nil, fmt.Errorf("当前任务仍在运行，请先等待结束或中断")
	}
	if sess == nil {
		sess = &state.Session{
			Key:         sessionKey,
			WorkspaceID: defaultWorkspaceID,
			ChatID:      chatID,
			ChatType:    chatType,
			OwnerUserID: userID,
		}
	}
	if strings.TrimSpace(sess.WorkspaceID) == "" {
		sess.WorkspaceID = defaultWorkspaceID
	}
	discarded := a.discardSessionPendingInputs(sessionKey)
	sess = appState.session(sessionKey)
	if sess == nil {
		sess = &state.Session{
			Key:         sessionKey,
			WorkspaceID: defaultWorkspaceID,
			ChatID:      chatID,
			ChatType:    chatType,
			OwnerUserID: userID,
		}
	}
	if strings.TrimSpace(sess.OwnerUserID) == "" {
		sess.OwnerUserID = userID
	}
	if strings.TrimSpace(sess.ChatID) == "" {
		sess.ChatID = chatID
	}
	if strings.TrimSpace(sess.ChatType) == "" {
		sess.ChatType = chatType
	}
	workspaceID := firstNonEmpty(strings.TrimSpace(sess.WorkspaceID), defaultWorkspaceID)
	ws := config.FindWorkspace(a.cfg, workspaceID)
	if ws == nil {
		return discarded, nil, fmt.Errorf("workspace %q not found", workspaceID)
	}
	binding, err := a.startWorkspaceThread(sessionKey, sess, ws)
	if err != nil {
		return discarded, nil, err
	}
	return discarded, binding, nil
}

func (a *App) commandThreadsNew(msg *feishu.InboundMessage) error {
	sessionKey := a.makeSessionKey(msg)
	discarded, binding, err := a.startFreshThread(sessionKey, msg.UserID, msg.ChatID, msg.ChatType)
	if err != nil {
		return err
	}
	noun := primaryConversationNoun(a.configuredBackend())
	reply := "已创建新" + noun + "并切换过去。"
	if binding != nil && strings.TrimSpace(binding.ThreadID) != "" {
		reply += " " + primaryConversationSummaryLabel(a.configuredBackend()) + ": `" + binding.ThreadID + "`。"
	}
	if discarded > 0 {
		reply += fmt.Sprintf(" 已丢弃 %d 条排队或暂存输入。", discarded)
	}
	return a.feishu.ReplyText(context.Background(), msg.MessageID, reply, a.replyInThreadEnabled(msg.ChatType))
}

func (a *App) commandThreads(msg *feishu.InboundMessage, includeAll bool) error {
	card, err := a.renderThreadsCard(a.makeSessionKey(msg), includeAll)
	if err != nil {
		return err
	}
	_, err = a.feishu.ReplyCard(context.Background(), msg.MessageID, card, a.replyInThreadEnabled(msg.ChatType))
	return err
}

func (a *App) commandThread(msg *feishu.InboundMessage, args []string) error {
	if len(args) == 0 {
		return a.commandThreads(msg, false)
	}
	sessionKey := a.makeSessionKey(msg)
	switch strings.TrimSpace(args[0]) {
	case "list":
		includeAll := false
		if len(args) > 2 {
			return fmt.Errorf("usage: %s", threadCommandUsage)
		}
		if len(args) == 2 {
			if strings.TrimSpace(args[1]) != "all" {
				return fmt.Errorf("usage: %s", threadCommandUsage)
			}
			includeAll = true
		}
		return a.commandThreads(msg, includeAll)
	case "new":
		if len(args) != 1 {
			return fmt.Errorf("usage: /thread new")
		}
		return a.commandThreadsNew(msg)
	case "fork":
		if len(args) != 1 {
			return fmt.Errorf("usage: /thread fork")
		}
		return a.commandFork(msg, nil)
	case "resume":
		if len(args) != 2 {
			return fmt.Errorf("usage: /thread resume THREAD_ID")
		}
		resp, err := newThreadActionService(a).completeThreadResume(a.commandActionFromMessage(msg, nil), sessionKey, strings.TrimSpace(args[1]))
		if err != nil {
			return err
		}
		return a.replyCommandActionResponse(msg, resp)
	case "sandbox":
		if len(args) == 1 {
			return a.showThreadSandboxMenu(msg)
		}
		if len(args) != 2 {
			return fmt.Errorf("usage: /thread sandbox [MODE]")
		}
		_, _, _, threadID, err := a.currentThreadForMessage(msg)
		if err != nil {
			return err
		}
		resp, err := newThreadActionService(a).completeThreadSandboxSet(a.commandActionFromMessage(msg, nil), sessionKey, threadID, strings.TrimSpace(args[1]))
		if err != nil {
			return err
		}
		return a.replyCommandActionResponse(msg, resp)
	case "policy":
		if len(args) == 1 {
			return a.showThreadPolicyMenu(msg)
		}
		if len(args) != 2 {
			return fmt.Errorf("usage: /thread policy [POLICY]")
		}
		_, _, _, threadID, err := a.currentThreadForMessage(msg)
		if err != nil {
			return err
		}
		resp, err := newThreadActionService(a).completeThreadPolicySet(a.commandActionFromMessage(msg, nil), sessionKey, threadID, strings.TrimSpace(args[1]))
		if err != nil {
			return err
		}
		return a.replyCommandActionResponse(msg, resp)
	default:
		return fmt.Errorf("usage: %s", threadCommandUsage)
	}
}

func (a *App) commandSession(msg *feishu.InboundMessage, args []string) error {
	if len(args) == 0 {
		return a.commandThreads(msg, false)
	}
	sessionKey := a.makeSessionKey(msg)
	switch strings.TrimSpace(args[0]) {
	case "list":
		includeAll := false
		if len(args) > 2 {
			return fmt.Errorf("usage: %s", claudeSessionCommandUsage)
		}
		if len(args) == 2 {
			if strings.TrimSpace(args[1]) != "all" {
				return fmt.Errorf("usage: %s", claudeSessionCommandUsage)
			}
			includeAll = true
		}
		return a.commandThreads(msg, includeAll)
	case "new":
		if len(args) != 1 {
			return fmt.Errorf("usage: /session new")
		}
		return a.commandThreadsNew(msg)
	case "fork":
		if len(args) != 1 {
			return fmt.Errorf("usage: /session fork")
		}
		return a.commandFork(msg, nil)
	case "resume":
		if len(args) != 2 {
			return fmt.Errorf("usage: /session resume SESSION_ID")
		}
		resp, err := newThreadActionService(a).completeThreadResume(a.commandActionFromMessage(msg, nil), sessionKey, strings.TrimSpace(args[1]))
		if err != nil {
			return err
		}
		return a.replyCommandActionResponse(msg, resp)
	case "permissions":
		if len(args) == 1 {
			return a.showClaudeSessionPermissionMenu(msg)
		}
		if len(args) != 2 {
			return fmt.Errorf("usage: /session permissions [MODE|inherit]")
		}
		_, _, _, threadID, err := a.currentThreadForMessage(msg)
		if err != nil {
			return err
		}
		resp, err := newThreadActionService(a).completeClaudeSessionPermissionModeSet(a.commandActionFromMessage(msg, nil), sessionKey, threadID, strings.TrimSpace(args[1]))
		if err != nil {
			return err
		}
		return a.replyCommandActionResponse(msg, resp)
	default:
		return fmt.Errorf("usage: %s", claudeSessionCommandUsage)
	}
}

func (a *App) renderThreadsCard(sessionKey string, includeAll bool) (map[string]any, error) {
	return a.conversationBackend().renderThreadsCard(sessionKey, includeAll)
}

func renderThreadSettingValue(override, fallback string) string {
	override = strings.TrimSpace(override)
	fallback = strings.TrimSpace(fallback)
	if override != "" {
		return "`" + override + "`"
	}
	if fallback != "" {
		return "`" + fallback + "` (follow workspace)"
	}
	return "-"
}

func (a *App) commandInterrupt(msg *feishu.InboundMessage) error {
	sessionKey := a.makeSessionKey(msg)
	discarded := a.discardSessionPendingInputs(sessionKey)
	sess := a.appState().session(sessionKey)
	sess = a.reconcileCompletedCodexTurnFromFinalOutput(sessionKey, sess)
	if sess == nil {
		sess = a.appState().session(sessionKey)
	}
	canceledRetry := a.cancelAutoRetry(sessionKey, sess != nil && sess.ActiveTurnID != "" && sess.ActiveThreadID != "", "已停止当前 session 的自动重试。")
	if sess == nil || sess.ActiveTurnID == "" || sess.ActiveThreadID == "" {
		if canceledRetry {
			reply := "已停止当前 session 的自动重试。"
			if discarded > 0 {
				reply += fmt.Sprintf(" 已清空 %d 条排队或暂存输入。", discarded)
			}
			return a.feishu.ReplyText(context.Background(), msg.MessageID, reply, a.replyInThreadEnabled(msg.ChatType))
		}
		if discarded > 0 {
			return a.feishu.ReplyText(context.Background(), msg.MessageID, fmt.Sprintf("已清空 %d 条排队或暂存输入。", discarded), a.replyInThreadEnabled(msg.ChatType))
		}
		return fmt.Errorf("当前没有运行中的任务")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := a.conversationBackend().interruptActiveTurn(ctx, sessionKey, sess); err != nil {
		return err
	}
	reply := "已请求中断当前任务。"
	if discarded > 0 {
		reply += fmt.Sprintf(" 已清空 %d 条排队或暂存输入。", discarded)
	}
	if canceledRetry {
		reply += " 当前 session 的自动重试也已停止。"
	}
	return a.feishu.ReplyText(context.Background(), msg.MessageID, reply, a.replyInThreadEnabled(msg.ChatType))
}

func (a *App) commandAppend(msg *feishu.InboundMessage, text string) error {
	sessionKey := a.makeSessionKey(msg)
	sess := a.appState().session(sessionKey)
	if sess == nil || sess.ActiveTurnID == "" || sess.ActiveThreadID == "" {
		return fmt.Errorf("当前没有可补充的任务")
	}
	return a.conversationBackend().continueActiveTurn(sessionKey, text)
}

func currentThreadLabel(sess *state.Session) string {
	if sess == nil {
		return "-"
	}
	if strings.TrimSpace(sess.ActiveThreadName) != "" {
		return truncate(sess.ActiveThreadName, 32)
	}
	if strings.TrimSpace(sess.ActiveThreadPreview) != "" {
		return truncate(sess.ActiveThreadPreview, 32)
	}
	if strings.TrimSpace(sess.ActiveThreadID) != "" {
		return truncate(sess.ActiveThreadID, 32)
	}
	return "-"
}

func renderThreadButtonLabel(name, preview, id string) string {
	switch {
	case strings.TrimSpace(name) != "":
		return truncate(name, 18)
	case strings.TrimSpace(preview) != "":
		return truncate(preview, 18)
	default:
		return truncate(id, 18)
	}
}

func renderThreadListEntry(name, preview, id string) string {
	base := renderThreadListEntryBase(name, preview, id)
	shortID := shortThreadID(id)
	if shortID == "" {
		return base
	}
	return truncate(base, 38) + " [" + shortID + "]"
}

func renderThreadListEntryBase(name, preview, id string) string {
	switch {
	case strings.TrimSpace(name) != "" && strings.TrimSpace(preview) != "":
		return truncate(name, 18) + " | " + truncate(preview, 36)
	case strings.TrimSpace(name) != "":
		return truncate(name, 48)
	case strings.TrimSpace(preview) != "":
		return truncate(preview, 48)
	default:
		return truncate(id, 48)
	}
}

func shortThreadID(id string) string {
	id = strings.TrimSpace(id)
	if len(id) <= 8 {
		return id
	}
	return id[:8]
}

func filterThreadsByWorkspaceCWD(items []codexrpc.ThreadListEntry, workspaceCWD string) []codexrpc.ThreadListEntry {
	workspaceCWD = strings.TrimSpace(workspaceCWD)
	if workspaceCWD == "" || len(items) == 0 {
		return items
	}
	filtered := make([]codexrpc.ThreadListEntry, 0, len(items))
	for _, item := range items {
		if sameWorkspaceCWD(item.Cwd, workspaceCWD) {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func sameWorkspaceCWD(a, b string) bool {
	a = strings.TrimSpace(a)
	b = strings.TrimSpace(b)
	if a == "" || b == "" {
		return false
	}
	return filepath.Clean(a) == filepath.Clean(b)
}

func (a *App) showThreadSandboxMenu(msg *feishu.InboundMessage) error {
	card, err := a.renderThreadSandboxMenuCard(a.makeSessionKey(msg))
	if err != nil {
		return err
	}
	_, err = a.feishu.ReplyCard(context.Background(), msg.MessageID, card, a.replyInThreadEnabled(msg.ChatType))
	return err
}

func (a *App) renderThreadSandboxMenuCard(sessionKey string) (map[string]any, error) {
	sess := a.appState().session(sessionKey)
	workspaceID := a.defaultWorkspaceID()
	if sess != nil && strings.TrimSpace(sess.WorkspaceID) != "" {
		workspaceID = sess.WorkspaceID
	}
	ws := config.FindWorkspace(a.cfg, workspaceID)
	if sess == nil || strings.TrimSpace(sess.ActiveThreadID) == "" {
		return nil, fmt.Errorf("当前没有活动线程")
	}
	threadID := strings.TrimSpace(sess.ActiveThreadID)
	current := effectiveThreadSandboxMode(sess, ws)
	body := "配置当前 thread 默认 sandbox。\n\nthread: `" + threadID + "`\n当前值: `" + current + "`"
	buttons := make([]feishu.Button, 0, len(workspaceSandboxOptions())+1)
	for _, opt := range workspaceSandboxOptions() {
		btnType := "default"
		label := opt.Label
		if opt.Value == current {
			btnType = "primary"
			label = "当前 · " + label
		}
		buttons = append(buttons, feishu.Button{
			Text: label,
			Type: btnType,
			Value: map[string]any{
				"action":       "thread.sandbox.set",
				"session_key":  sessionKey,
				"thread_id":    threadID,
				"sandbox_mode": opt.Value,
			},
		})
	}
	buttons = append(buttons, feishu.Button{
		Text: commandLabel("返回 thread", "/thread"),
		Type: "default",
		Value: map[string]any{
			"action":      "menu.thread",
			"session_key": sessionKey,
		},
	})
	return a.feishu.SimpleStatusCard("配置 Thread Sandbox", "blue", menuCardBody("thread.sandbox.menu", body), buttons), nil
}

func (a *App) showThreadPolicyMenu(msg *feishu.InboundMessage) error {
	card, err := a.renderThreadPolicyMenuCard(a.makeSessionKey(msg))
	if err != nil {
		return err
	}
	_, err = a.feishu.ReplyCard(context.Background(), msg.MessageID, card, a.replyInThreadEnabled(msg.ChatType))
	return err
}

func (a *App) renderThreadPolicyMenuCard(sessionKey string) (map[string]any, error) {
	sess := a.appState().session(sessionKey)
	workspaceID := a.defaultWorkspaceID()
	if sess != nil && strings.TrimSpace(sess.WorkspaceID) != "" {
		workspaceID = sess.WorkspaceID
	}
	ws := config.FindWorkspace(a.cfg, workspaceID)
	if sess == nil || strings.TrimSpace(sess.ActiveThreadID) == "" {
		return nil, fmt.Errorf("当前没有活动线程")
	}
	threadID := strings.TrimSpace(sess.ActiveThreadID)
	current := effectiveThreadApprovalPolicy(sess, ws)
	body := "配置当前 thread 默认 approval policy。\n\nthread: `" + threadID + "`\n当前值: `" + current + "`"
	buttons := make([]feishu.Button, 0, len(workspaceApprovalPolicyOptions())+1)
	for _, opt := range workspaceApprovalPolicyOptions() {
		btnType := "default"
		label := opt.Label
		if opt.Value == current {
			btnType = "primary"
			label = "当前 · " + label
		}
		buttons = append(buttons, feishu.Button{
			Text: label,
			Type: btnType,
			Value: map[string]any{
				"action":          "thread.policy.set",
				"session_key":     sessionKey,
				"thread_id":       threadID,
				"approval_policy": opt.Value,
			},
		})
	}
	buttons = append(buttons, feishu.Button{
		Text: commandLabel("返回 thread", "/thread"),
		Type: "default",
		Value: map[string]any{
			"action":      "menu.thread",
			"session_key": sessionKey,
		},
	})
	return a.feishu.SimpleStatusCard("配置 Thread Policy", "blue", menuCardBody("thread.policy.menu", body), buttons), nil
}
