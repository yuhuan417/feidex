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
		resp, err := a.completeThreadResume(a.commandActionFromMessage(msg, nil), sessionKey, strings.TrimSpace(args[1]))
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
		resp, err := a.completeThreadSandboxSet(a.commandActionFromMessage(msg, nil), sessionKey, threadID, strings.TrimSpace(args[1]))
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
		resp, err := a.completeThreadPolicySet(a.commandActionFromMessage(msg, nil), sessionKey, threadID, strings.TrimSpace(args[1]))
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
		resp, err := a.completeThreadResume(a.commandActionFromMessage(msg, nil), sessionKey, strings.TrimSpace(args[1]))
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
		resp, err := a.completeClaudeSessionPermissionModeSet(a.commandActionFromMessage(msg, nil), sessionKey, threadID, strings.TrimSpace(args[1]))
		if err != nil {
			return err
		}
		return a.replyCommandActionResponse(msg, resp)
	default:
		return fmt.Errorf("usage: %s", claudeSessionCommandUsage)
	}
}

func (a *App) renderThreadsCard(sessionKey string, includeAll bool) (map[string]any, error) {
	sess := a.appState().session(sessionKey)
	workspace := a.cfg.Workspaces[0]
	if sess != nil {
		if ws := config.FindWorkspace(a.cfg, sess.WorkspaceID); ws != nil {
			workspace = *ws
		}
	}
	if a.isClaudeBackend() {
		return a.renderClaudeThreadsCard(sessionKey, sess, &workspace, includeAll)
	}
	items, err := a.listWorkspaceThreads(sessionKey, &workspace, includeAll)
	if err != nil {
		return nil, err
	}
	sortThreadsByUpdated(items)
	currentLabel := "-"
	currentThreadID := "-"
	currentThreadSandbox := "-"
	currentThreadPolicy := "-"
	if sess != nil {
		currentLabel = currentThreadLabel(sess)
		if strings.TrimSpace(sess.ActiveThreadID) != "" {
			currentThreadID = strings.TrimSpace(sess.ActiveThreadID)
			currentThreadSandbox = renderThreadSettingValue(sess.ActiveThreadSandboxMode, workspace.SandboxMode)
			currentThreadPolicy = renderThreadSettingValue(sess.ActiveThreadApprovalPolicy, workspace.ApprovalPolicy)
		}
	}
	scopeLabel := "当前工作区"
	if includeAll {
		scopeLabel = "全部来源（仅命令入口）"
	}
	lines := []string{
		primaryConversationCurrentLabel(a.configuredBackend()) + ": " + currentLabel,
		"当前 " + primaryConversationIDLabel(a.configuredBackend()) + ": `" + currentThreadID + "`",
		"工作区: `" + workspace.ID + "`",
		"当前 thread sandbox: " + currentThreadSandbox,
		"当前 thread policy: " + currentThreadPolicy,
		"list 范围: " + scopeLabel,
		fmt.Sprintf("list 数量: `%d`", len(items)),
	}
	if len(items) == 0 {
		lines = append(lines, "", "当前没有可切换的线程。")
	} else {
		lines = append(lines, "", "通过下拉 list 选择要切换的线程。")
	}
	hasActiveThread := sess != nil && strings.TrimSpace(sess.ActiveThreadID) != ""
	if !hasActiveThread {
		lines = append(lines, "", "当前没有活动 thread，因此暂不显示 `/thread fork`、`/thread sandbox`、`/thread policy`。")
	}
	buttons := make([]feishu.Button, 0, 5)
	selectOptions := make([]selectStaticOption, 0, len(items))
	initialOption := ""
	for idx, item := range items {
		entry := fmt.Sprintf("%d. %s", idx+1, renderThreadListEntry(item.Name, item.Preview, item.ID))
		if sess != nil && item.ID == sess.ActiveThreadID {
			entry = fmt.Sprintf("%d. [当前] %s", idx+1, renderThreadListEntry(item.Name, item.Preview, item.ID))
			initialOption = item.ID
		}
		selectOptions = append(selectOptions, selectStaticOption{
			Text:  entry,
			Value: item.ID,
		})
	}
	buttons = append(buttons,
		feishu.Button{
			Text: commandLabel("新建线程", "/thread new"),
			Type: "default",
			Value: map[string]any{
				"action":        "menu.new",
				"session_key":   sessionKey,
				"parent_action": "menu.thread",
			},
		},
	)
	if hasActiveThread {
		buttons = append(buttons,
			feishu.Button{
				Text: commandLabel("派生线程", "/thread fork"),
				Type: "default",
				Value: map[string]any{
					"action":        "menu.fork",
					"session_key":   sessionKey,
					"parent_action": "menu.thread",
				},
			},
			feishu.Button{
				Text: submenuCommandLabel("配置线程沙箱", "/thread sandbox"),
				Type: "default",
				Value: map[string]any{
					"action":      "thread.sandbox.menu",
					"session_key": sessionKey,
				},
			},
			feishu.Button{
				Text: submenuCommandLabel("配置审批策略", "/thread policy"),
				Type: "default",
				Value: map[string]any{
					"action":      "thread.policy.menu",
					"session_key": sessionKey,
				},
			},
		)
	}
	buttons = append(buttons,
		feishu.Button{
			Text: "返回上一级",
			Type: "default",
			Value: map[string]any{
				"action":      "menu.root",
				"session_key": sessionKey,
			},
		},
	)
	body := strings.Join(lines, "\n")
	card := newMarkdownBodyCard(primaryConversationMenuLabel(a.configuredBackend()), "blue")
	appendMarkdownBodyCardElement(card, map[string]any{"tag": "markdown", "content": menuCardBodyForBackend(a.configuredBackend(), "menu.thread", body)})
	if len(selectOptions) > 0 {
		appendMarkdownBodyCardElement(card, buildSelectStaticElement(
			"thread_resume_select",
			"list",
			map[string]any{"action": "thread.resume.select", "session_key": sessionKey, "include_all": includeAll},
			selectOptions,
			initialOption,
		))
	}
	for _, row := range buildMarkdownBodyCardActionElements(buttons) {
		appendMarkdownBodyCardElement(card, row)
	}
	return card, nil
}

func (a *App) renderClaudeThreadsCard(sessionKey string, sess *state.Session, ws *config.Workspace, includeAll bool) (map[string]any, error) {
	items, err := a.listClaudeSessions(sessionKey, ws, includeAll)
	if err != nil {
		return nil, err
	}
	sortThreadsByUpdated(items)
	workspaceID := "-"
	if ws != nil {
		workspaceID = firstNonEmpty(strings.TrimSpace(ws.ID), workspaceID)
	}
	currentLabel := "-"
	currentThreadID := "-"
	workspacePermission := "-"
	sessionPermission := "跟随工作区"
	effectivePermission := "-"
	if sess != nil {
		currentLabel = currentThreadLabel(sess)
		if strings.TrimSpace(sess.ActiveThreadID) != "" {
			currentThreadID = strings.TrimSpace(sess.ActiveThreadID)
		}
	}
	if ws != nil {
		workspacePermission = claudePermissionModeLabel(effectiveClaudePermissionMode(nil, ws, a.cfg.Claude))
		effectivePermission = claudePermissionModeLabel(effectiveClaudePermissionMode(sess, ws, a.cfg.Claude))
	}
	if sess != nil && strings.TrimSpace(sess.ActiveClaudePermissionMode) != "" {
		sessionPermission = claudePermissionModeLabel(sess.ActiveClaudePermissionMode)
	}
	scopeLabel := "当前工作区"
	if includeAll {
		scopeLabel = "全部 Claude 会话"
	}
	lines := []string{
		"当前 backend: `claude`",
		"当前会话: " + currentLabel,
		"当前 session id: `" + currentThreadID + "`",
		"工作区: `" + workspaceID + "`",
		"工作区默认权限: " + workspacePermission,
		"会话覆盖: " + sessionPermission,
		"当前生效权限: " + effectivePermission,
		"list 范围: " + scopeLabel,
		fmt.Sprintf("list 数量: `%d`", len(items)),
	}
	if len(items) == 0 {
		lines = append(lines, "", "当前没有可切换的 Claude 会话。")
	} else {
		lines = append(lines, "", "通过下拉 list 选择要切换的 Claude 会话。")
	}
	lines = append(lines, "", "提示：`/session new` 和 `/session fork` 需要在开始对话后，才会生成真实的 Claude 会话和 session id。")
	hasActiveSession := sess != nil && strings.TrimSpace(sess.ActiveThreadID) != ""
	if !hasActiveSession {
		lines = append(lines, "", "当前没有活动 Claude 会话，因此暂不显示 `/session fork` 和 `/session permissions`。")
	}
	buttons := []feishu.Button{
		{
			Text: commandLabel("新建会话", "/session new"),
			Type: "default",
			Value: map[string]any{
				"action":        "menu.new",
				"session_key":   sessionKey,
				"parent_action": "menu.thread",
			},
		},
	}
	if hasActiveSession {
		buttons = append(buttons,
			feishu.Button{
				Text: commandLabel("派生会话", "/session fork"),
				Type: "default",
				Value: map[string]any{
					"action":        "menu.fork",
					"session_key":   sessionKey,
					"parent_action": "menu.thread",
				},
			},
			feishu.Button{
				Text: submenuCommandLabel("会话权限", "/session permissions"),
				Type: "default",
				Value: map[string]any{
					"action":      "thread.permission_mode.menu",
					"session_key": sessionKey,
				},
			},
		)
	}
	buttons = append(buttons, feishu.Button{
		Text: "返回上一级",
		Type: "default",
		Value: map[string]any{
			"action":      "menu.root",
			"session_key": sessionKey,
		},
	})
	selectOptions := make([]selectStaticOption, 0, len(items))
	initialOption := ""
	for idx, item := range items {
		entry := fmt.Sprintf("%d. %s", idx+1, renderThreadListEntry(item.Name, item.Preview, item.ID))
		if sess != nil && item.ID == sess.ActiveThreadID {
			entry = fmt.Sprintf("%d. [当前] %s", idx+1, renderThreadListEntry(item.Name, item.Preview, item.ID))
			initialOption = item.ID
		}
		selectOptions = append(selectOptions, selectStaticOption{
			Text:  entry,
			Value: item.ID,
		})
	}
	card := newMarkdownBodyCard("会话管理", "blue")
	appendMarkdownBodyCardElement(card, map[string]any{"tag": "markdown", "content": menuCardBodyForBackend(a.configuredBackend(), "menu.thread", strings.Join(lines, "\n"))})
	if len(selectOptions) > 0 {
		appendMarkdownBodyCardElement(card, buildSelectStaticElement(
			"thread_resume_select",
			"list",
			map[string]any{"action": "thread.resume.select", "session_key": sessionKey, "include_all": includeAll},
			selectOptions,
			initialOption,
		))
	}
	for _, row := range buildMarkdownBodyCardActionElements(buttons) {
		appendMarkdownBodyCardElement(card, row)
	}
	return card, nil
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
	sess := a.appState().session(sessionKey)
	discarded := a.discardSessionPendingInputs(sessionKey)
	if sess == nil || sess.ActiveTurnID == "" || sess.ActiveThreadID == "" {
		if discarded > 0 {
			return a.feishu.ReplyText(context.Background(), msg.MessageID, fmt.Sprintf("已清空 %d 条排队或暂存输入。", discarded), a.replyInThreadEnabled(msg.ChatType))
		}
		return fmt.Errorf("当前没有运行中的任务")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if a.configuredBackend() == backendClaude {
		if err := a.claude.Interrupt(ctx, sessionKey); err != nil {
			return err
		}
	} else {
		if err := a.codex.Call(ctx, "turn/interrupt", map[string]any{
			"threadId": sess.ActiveThreadID,
			"turnId":   sess.ActiveTurnID,
		}, nil); err != nil {
			return err
		}
	}
	reply := "已请求中断当前任务。"
	if discarded > 0 {
		reply += fmt.Sprintf(" 已清空 %d 条排队或暂存输入。", discarded)
	}
	return a.feishu.ReplyText(context.Background(), msg.MessageID, reply, a.replyInThreadEnabled(msg.ChatType))
}

func (a *App) commandAppend(msg *feishu.InboundMessage, text string) error {
	sessionKey := a.makeSessionKey(msg)
	sess := a.appState().session(sessionKey)
	if sess == nil || sess.ActiveTurnID == "" || sess.ActiveThreadID == "" {
		return fmt.Errorf("当前没有可补充的任务")
	}
	if a.configuredBackend() == backendClaude {
		return a.continueClaudeSessionWithText(sessionKey, text)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	return a.codex.Call(ctx, "turn/steer", map[string]any{
		"threadId":       sess.ActiveThreadID,
		"expectedTurnId": sess.ActiveTurnID,
		"input": []map[string]any{
			{"type": "text", "text": strings.TrimSpace(text), "text_elements": []any{}},
		},
	}, nil)
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
