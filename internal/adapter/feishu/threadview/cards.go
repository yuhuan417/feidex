package threadview

import (
	appcards "feidex/internal/adapter/feishu/cards"
	appmenuutil "feidex/internal/adapter/feishu/menuutil"
	backendcaps "feidex/internal/application/backendcaps"
	"feidex/internal/config"
	"feidex/internal/domain/conversation"
	"feidex/internal/feishu"
	"feidex/internal/textutil"
	"fmt"
	"strings"
)

func firstNonEmpty(values ...string) string {
	return textutil.FirstNonEmpty(values...)
}

func commandLabel(label, slash string) string {
	label = strings.TrimSpace(label)
	slash = strings.TrimSpace(slash)
	if label == "" {
		return slash
	}
	if slash == "" {
		return label
	}
	return label + " " + slash
}

func submenuCommandLabel(label, slash string) string {
	label = strings.TrimSpace(label)
	slash = strings.TrimSpace(slash)
	if label == "" && slash == "" {
		return ">"
	}
	if slash == "" {
		return label + " >"
	}
	if label == "" {
		return slash + " >"
	}
	return label + " " + slash + " >"
}

func renderThreadListEntry(name, preview, id string) string {
	return RenderThreadListEntry(name, preview, id)
}

func renderThreadSettingValue(override, fallback string) string {
	return RenderThreadSettingValue(override, fallback)
}

func currentThreadLabel(sess *conversation.Session) string {
	if sess == nil {
		return "-"
	}
	return CurrentThreadLabel(sess.ActiveThreadName, sess.ActiveThreadPreview, sess.ActiveThreadID)
}

func effectiveClaudePermissionMode(sess *conversation.Session, ws *config.Workspace, cfg config.ClaudeConfig) string {
	if sess != nil && strings.TrimSpace(sess.ActiveClaudePermissionMode) != "" {
		return normalizeClaudePermissionModeValue(sess.ActiveClaudePermissionMode)
	}
	if ws != nil && strings.TrimSpace(ws.ClaudePermissionMode) != "" {
		return normalizeClaudePermissionModeValue(ws.ClaudePermissionMode)
	}
	return normalizeClaudePermissionModeValue(cfg.PermissionMode)
}

func normalizeClaudePermissionModeValue(value string) string {
	switch strings.TrimSpace(value) {
	case "", "default":
		return "default"
	case "acceptEdits":
		return "acceptEdits"
	case "bypassPermissions":
		return "bypassPermissions"
	case "plan":
		return "plan"
	default:
		return strings.TrimSpace(value)
	}
}

func claudePermissionModeLabel(value string) string {
	value = normalizeClaudePermissionModeValue(value)
	if value == "" {
		value = "default"
	}
	return "`" + value + "`"
}

// ConversationThreadsCardView holds the data needed to build a conversation
// threads card.
type ConversationThreadsCardView struct {
	Title          string
	Backend        string
	BodyLines      []string
	Buttons        []feishu.Button
	Items          []conversation.ThreadEntry
	ActiveThreadID string
	IncludeAll     bool
}

// BuildConversationThreadsCard builds a conversation threads card from the
// given view data.
func BuildConversationThreadsCard(sessionKey string, view ConversationThreadsCardView) map[string]any {
	selectOptions := make([]appcards.SelectStaticOption, 0, len(view.Items))
	initialOption := ""
	for idx, item := range view.Items {
		entry := fmt.Sprintf("%d. %s", idx+1, renderThreadListEntry(item.Name, item.Preview, item.ID))
		if strings.TrimSpace(view.ActiveThreadID) != "" && item.ID == strings.TrimSpace(view.ActiveThreadID) {
			entry = fmt.Sprintf("%d. [current] %s", idx+1, renderThreadListEntry(item.Name, item.Preview, item.ID))
			initialOption = item.ID
		}
		selectOptions = append(selectOptions, appcards.SelectStaticOption{
			Text:  entry,
			Value: item.ID,
		})
	}
	elements := []map[string]any{}
	if len(selectOptions) > 0 {
		elements = append(elements, appcards.BuildSelectStaticElement(
			"thread_resume_select",
			"list",
			map[string]any{"action": "thread.resume.select", "session_key": sessionKey, "include_all": view.IncludeAll},
			selectOptions,
			initialOption,
		))
	}
	return appmenuutil.MarkdownPageCard{
		Node: "menu.thread", Backend: view.Backend, SessionKey: sessionKey,
		Title:    strings.TrimSpace(view.Title),
		Color:    "blue",
		Body:     strings.Join(view.BodyLines, "\n"),
		Elements: elements,
		Buttons:  view.Buttons,
	}.Render()
}

// RenderCodexThreadsCard renders the codex threads card for a session.
func RenderCodexThreadsCard(sessionKey string, sess *conversation.Session, workspace config.Workspace, backend string, items []conversation.ThreadEntry, includeAll bool) (map[string]any, error) {
	conversation.SortThreadsByUpdated(items)
	currentLabel := "-"
	currentThreadID := "-"
	currentThreadSandbox := "-"
	currentThreadPolicy := "-"
	currentThreadMultiAgent := "-"
	if sess != nil {
		currentLabel = currentThreadLabel(sess)
		if strings.TrimSpace(sess.ActiveThreadID) != "" {
			currentThreadID = strings.TrimSpace(sess.ActiveThreadID)
			currentThreadSandbox = renderThreadSettingValue(sess.ActiveThreadSandboxMode, workspace.SandboxMode)
			currentThreadPolicy = renderThreadSettingValue(sess.ActiveThreadApprovalPolicy, workspace.ApprovalPolicy)
			currentThreadMultiAgent = renderThreadSettingValue(sess.ActiveThreadMultiAgentMode, workspace.MultiAgentMode)
		}
	}
	scopeLabel := "current workspace"
	if includeAll {
		scopeLabel = "all sources (command entry only)"
	}
	lines := []string{
		primaryConversationCurrentLabel(backend) + ": " + currentLabel,
		"当前 " + primaryConversationIDLabel(backend) + ": `" + currentThreadID + "`",
		"workspace: `" + workspace.ID + "`",
		"current thread sandbox: " + currentThreadSandbox,
		"current thread policy: " + currentThreadPolicy,
		"current thread multi-agent: " + currentThreadMultiAgent,
		"list scope: " + scopeLabel,
		fmt.Sprintf("list count: `%d`", len(items)),
	}
	if len(items) == 0 {
		lines = append(lines, "", "no switchable threads available.")
	} else {
		lines = append(lines, "", "select a thread from the dropdown to switch.")
	}
	hasActiveThread := sess != nil && strings.TrimSpace(sess.ActiveThreadID) != ""
	if !hasActiveThread {
		lines = append(lines, "", "no active thread, so /thread fork, /thread sandbox, /thread policy, /thread multiagent are not shown.")
	}
	buttons := []feishu.Button{
		{
			Text: commandLabel("new thread", "/thread new"),
			Type: "default",
			Value: map[string]any{
				"action":        "thread.new.start",
				"session_key":   sessionKey,
				"parent_action": "menu.thread",
			},
		},
	}
	if hasActiveThread {
		buttons = append(buttons,
			feishu.Button{
				Text: commandLabel("fork thread", "/thread fork"),
				Type: "default",
				Value: map[string]any{
					"action":        "thread.fork.start",
					"session_key":   sessionKey,
					"parent_action": "menu.thread",
				},
			},
			feishu.Button{
				Text: submenuCommandLabel("配置沙箱", "/thread sandbox"),
				Type: "default",
				Value: map[string]any{
					"action":      "thread.sandbox.menu",
					"session_key": sessionKey,
				},
			},
			feishu.Button{
				Text: submenuCommandLabel("配置策略", "/thread policy"),
				Type: "default",
				Value: map[string]any{
					"action":      "thread.policy.menu",
					"session_key": sessionKey,
				},
			},
			feishu.Button{
				Text: submenuCommandLabel("配置多智能体模式", "/thread multiagent"),
				Type: "default",
				Value: map[string]any{
					"action":      "thread.multiagent.menu",
					"session_key": sessionKey,
				},
			},
		)
	}

	return BuildConversationThreadsCard(sessionKey, ConversationThreadsCardView{
		Title:          primaryConversationMenuLabel(backend),
		Backend:        backend,
		BodyLines:      lines,
		Buttons:        buttons,
		Items:          items,
		ActiveThreadID: currentThreadID,
		IncludeAll:     includeAll,
	}), nil
}

// RenderClaudeThreadsCard renders the claude threads card.
func RenderClaudeThreadsCard(sessionKey string, sess *conversation.Session, ws *config.Workspace, backend string, cfg config.ClaudeConfig, items []conversation.ThreadEntry, includeAll bool) (map[string]any, error) {
	conversation.SortThreadsByUpdated(items)
	workspaceID := "-"
	if ws != nil {
		workspaceID = firstNonEmpty(strings.TrimSpace(ws.ID), workspaceID)
	}
	currentLabel := "-"
	currentThreadID := "-"
	workspacePermission := "-"
	sessionPermission := "follow workspace"
	effectivePermission := "-"
	if sess != nil {
		currentLabel = currentThreadLabel(sess)
		if strings.TrimSpace(sess.ActiveThreadID) != "" {
			currentThreadID = strings.TrimSpace(sess.ActiveThreadID)
		}
	}
	if ws != nil {
		workspacePermission = claudePermissionModeLabel(effectiveClaudePermissionMode(nil, ws, cfg))
		effectivePermission = claudePermissionModeLabel(effectiveClaudePermissionMode(sess, ws, cfg))
	}
	if sess != nil && strings.TrimSpace(sess.ActiveClaudePermissionMode) != "" {
		sessionPermission = claudePermissionModeLabel(sess.ActiveClaudePermissionMode)
	}
	scopeLabel := "current workspace"
	if includeAll {
		scopeLabel = "all Claude sessions"
	}
	lines := []string{
		"current backend: `claude`",
		"current session: " + currentLabel,
		"current session id: `" + currentThreadID + "`",
		"workspace: `" + workspaceID + "`",
		"workspace default permission: " + workspacePermission,
		"session override: " + sessionPermission,
		"effective permission: " + effectivePermission,
		"list scope: " + scopeLabel,
		fmt.Sprintf("list count: `%d`", len(items)),
	}
	if len(items) == 0 {
		lines = append(lines, "", "no switchable Claude sessions available.")
	} else {
		lines = append(lines, "", "select a Claude session from the dropdown to switch.")
	}
	lines = append(lines, "", "tip: /session new and /session fork require starting a conversation first to generate a real Claude session and session id.")
	hasActiveSession := sess != nil && strings.TrimSpace(sess.ActiveThreadID) != ""
	if !hasActiveSession {
		lines = append(lines, "", "no active Claude session, so /session fork and /session permissions are not shown.")
	}
	buttons := []feishu.Button{
		{
			Text: commandLabel("new session", "/session new"),
			Type: "default",
			Value: map[string]any{
				"action":        "thread.new.start",
				"session_key":   sessionKey,
				"parent_action": "menu.thread",
			},
		},
	}
	if hasActiveSession {
		buttons = append(buttons,
			feishu.Button{
				Text: commandLabel("fork session", "/session fork"),
				Type: "default",
				Value: map[string]any{
					"action":        "thread.fork.start",
					"session_key":   sessionKey,
					"parent_action": "menu.thread",
				},
			},
			feishu.Button{
				Text: submenuCommandLabel("session permissions", "/session permissions"),
				Type: "default",
				Value: map[string]any{
					"action":      "thread.permission_mode.menu",
					"session_key": sessionKey,
				},
			},
		)
	}

	return BuildConversationThreadsCard(sessionKey, ConversationThreadsCardView{
		Title:          "session management",
		Backend:        backend,
		BodyLines:      lines,
		Buttons:        buttons,
		Items:          items,
		ActiveThreadID: currentThreadID,
		IncludeAll:     includeAll,
	}), nil
}

func primaryConversationMenuLabel(backend string) string {
	return backendcaps.ForKind(backend).Conversation.MenuLabel
}

func primaryConversationCurrentLabel(backend string) string {
	return backendcaps.ForKind(backend).CurrentConversationLabel()
}

func primaryConversationIDLabel(backend string) string {
	return backendcaps.ForKind(backend).Conversation.IDLabel
}
