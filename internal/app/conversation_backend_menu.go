package app

import (
	"fmt"
	"strings"

	"feidex/internal/codexrpc"
	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

type conversationThreadsCardView struct {
	Title          string
	Backend        string
	BodyLines      []string
	Buttons        []feishu.Button
	Items          []codexrpc.ThreadListEntry
	ActiveThreadID string
	IncludeAll     bool
}

func buildConversationThreadsCard(sessionKey string, view conversationThreadsCardView) map[string]any {
	selectOptions := make([]selectStaticOption, 0, len(view.Items))
	initialOption := ""
	for idx, item := range view.Items {
		entry := fmt.Sprintf("%d. %s", idx+1, renderThreadListEntry(item.Name, item.Preview, item.ID))
		if strings.TrimSpace(view.ActiveThreadID) != "" && item.ID == strings.TrimSpace(view.ActiveThreadID) {
			entry = fmt.Sprintf("%d. [当前] %s", idx+1, renderThreadListEntry(item.Name, item.Preview, item.ID))
			initialOption = item.ID
		}
		selectOptions = append(selectOptions, selectStaticOption{
			Text:  entry,
			Value: item.ID,
		})
	}
	card := newMarkdownBodyCard(strings.TrimSpace(view.Title), "blue")
	appendMarkdownBodyCardElement(card, map[string]any{
		"tag":     "markdown",
		"content": menuCardBodyForBackend(view.Backend, "menu.thread", strings.Join(view.BodyLines, "\n")),
	})
	if len(selectOptions) > 0 {
		appendMarkdownBodyCardElement(card, buildSelectStaticElement(
			"thread_resume_select",
			"list",
			map[string]any{"action": "thread.resume.select", "session_key": sessionKey, "include_all": view.IncludeAll},
			selectOptions,
			initialOption,
		))
	}
	for _, row := range buildMarkdownBodyCardActionElements(view.Buttons) {
		appendMarkdownBodyCardElement(card, row)
	}
	return card
}

func (a *App) renderCodexThreadsCard(sessionKey string, includeAll bool) (map[string]any, error) {
	sess := a.appState().session(sessionKey)
	workspace := a.cfg.Workspaces[0]
	if sess != nil {
		if ws := config.FindWorkspace(a.cfg, sess.WorkspaceID); ws != nil {
			workspace = *ws
		}
	}
	items, err := newWorkspaceThreadService(a).listWorkspaceThreads(sessionKey, &workspace, includeAll)
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
	buttons := []feishu.Button{
		{
			Text: commandLabel("新建线程", "/thread new"),
			Type: "default",
			Value: map[string]any{
				"action":        "menu.new",
				"session_key":   sessionKey,
				"parent_action": "menu.thread",
			},
		},
	}
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
	buttons = append(buttons, feishu.Button{
		Text: "返回上一级",
		Type: "default",
		Value: map[string]any{
			"action":      "menu.root",
			"session_key": sessionKey,
		},
	})
	return buildConversationThreadsCard(sessionKey, conversationThreadsCardView{
		Title:          primaryConversationMenuLabel(a.configuredBackend()),
		Backend:        a.configuredBackend(),
		BodyLines:      lines,
		Buttons:        buttons,
		Items:          items,
		ActiveThreadID: currentThreadID,
		IncludeAll:     includeAll,
	}), nil
}

func (a *App) renderClaudeThreadsCardForCurrentBackend(sessionKey string, includeAll bool) (map[string]any, error) {
	sess := a.appState().session(sessionKey)
	workspace := a.cfg.Workspaces[0]
	if sess != nil {
		if ws := config.FindWorkspace(a.cfg, sess.WorkspaceID); ws != nil {
			workspace = *ws
		}
	}
	return a.renderClaudeThreadsCard(sessionKey, sess, &workspace, includeAll)
}

func (a *App) renderClaudeThreadsCard(sessionKey string, sess *state.Session, ws *config.Workspace, includeAll bool) (map[string]any, error) {
	items, err := listClaudeSessions(sessionKey, ws, includeAll)
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
	return buildConversationThreadsCard(sessionKey, conversationThreadsCardView{
		Title:          "会话管理",
		Backend:        a.configuredBackend(),
		BodyLines:      lines,
		Buttons:        buttons,
		Items:          items,
		ActiveThreadID: currentThreadID,
		IncludeAll:     includeAll,
	}), nil
}
