package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"feidex/internal/codexrpc"
	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

type workspaceSettingOption struct {
	Value string
	Label string
}

func (a *App) handleCommand(msg *feishu.InboundMessage, raw string) error {
	raw = strings.TrimSpace(raw)
	fields := strings.Fields(raw)
	if len(fields) == 0 {
		return nil
	}
	switch fields[0] {
	case "/menu":
		return a.sendCommandMenu(msg)
	case "/help":
		return a.commandHelp(msg, fields[1:])
	case "/history":
		return a.commandHistory(msg, fields[1:])
	case "/usage":
		return a.commandUsage(msg, fields[1:])
	case "/model":
		return a.commandModel(msg)
	case "/quiet":
		return a.commandQuiet(msg, fields[1:])
	case "/debug":
		return a.commandDebug(msg, fields[1:])
	case "/fast":
		return a.commandFast(msg, fields[1:])
	case "/download":
		return a.commandDownload(msg, fields[1:])
	case "/compact":
		return a.commandCompact(msg, fields[1:])
	case "/fork":
		return a.commandFork(msg, fields[1:])
	case "/new":
		return a.commandNew(msg)
	case "/thread":
		return a.commandThread(msg, fields[1:])
	case "/threads":
		return a.commandThread(msg, legacyThreadAliasArgs(fields[1:]))
	case "/interrupt", "/stop":
		return a.commandInterrupt(msg)
	case "/status":
		return a.commandStatus(msg)
	case "/upgrade":
		return a.commandUpgrade(msg, fields[1:])
	case "/workspace":
		return a.commandWorkspace(msg, fields[1:])
	default:
		return fmt.Errorf("unknown command: %s", fields[0])
	}
}

func legacyThreadAliasArgs(args []string) []string {
	if len(args) == 0 {
		return []string{"list"}
	}
	switch strings.TrimSpace(args[0]) {
	case "all":
		return []string{"list", "all"}
	case "new":
		return []string{"new"}
	case "fork":
		return []string{"fork"}
	case "sandbox":
		return []string{"sandbox"}
	case "policy":
		return []string{"policy"}
	default:
		return args
	}
}

func isLocalCommand(raw string) bool {
	raw = strings.TrimSpace(raw)
	fields := strings.Fields(raw)
	if len(fields) == 0 {
		return false
	}
	switch fields[0] {
	case "/model":
		return len(fields) == 1
	case "/quiet", "/debug":
		return true
	case "/menu", "/help", "/history", "/usage", "/download", "/compact", "/fork", "/new", "/thread", "/threads", "/interrupt", "/stop", "/status", "/workspace", "/upgrade", "/fast":
		return true
	default:
		return false
	}
}

func (a *App) commandHelp(msg *feishu.InboundMessage, args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("usage: /help")
	}
	card := a.renderHelpCard(a.makeSessionKey(msg))
	_, err := a.feishu.ReplyCard(context.Background(), msg.MessageID, card, msg.ChatType == "group" && a.cfg.Feishu.ReplyInThread)
	return err
}

func (a *App) commandNew(msg *feishu.InboundMessage) error {
	return a.commandThreadsNew(msg)
}

func (a *App) startFreshThread(sessionKey, userID, chatID, chatType string) (int, *workspaceThreadBinding, error) {
	if a == nil || a.store == nil {
		return 0, nil, fmt.Errorf("store not initialized")
	}
	defaultWorkspaceID := "default"
	if a.cfg != nil && len(a.cfg.Workspaces) > 0 {
		defaultWorkspaceID = a.cfg.Workspaces[0].ID
	}
	sess := a.store.GetSession(sessionKey)
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
	sess = a.store.GetSession(sessionKey)
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
	reply := "已创建新线程并切换过去。"
	if binding != nil && strings.TrimSpace(binding.ThreadID) != "" {
		reply += " thread: `" + binding.ThreadID + "`。"
	}
	if discarded > 0 {
		reply += fmt.Sprintf(" 已丢弃 %d 条排队或暂存输入。", discarded)
	}
	return a.feishu.ReplyText(context.Background(), msg.MessageID, reply, msg.ChatType == "group" && a.cfg.Feishu.ReplyInThread)
}

func (a *App) commandThreads(msg *feishu.InboundMessage, includeAll bool) error {
	card, err := a.renderThreadsCard(a.makeSessionKey(msg), includeAll)
	if err != nil {
		return err
	}
	_, err = a.feishu.ReplyCard(context.Background(), msg.MessageID, card, msg.ChatType == "group" && a.cfg.Feishu.ReplyInThread)
	return err
}

func (a *App) commandThread(msg *feishu.InboundMessage, args []string) error {
	if len(args) == 0 {
		return a.commandThreads(msg, false)
	}
	switch strings.TrimSpace(args[0]) {
	case "list":
		includeAll := false
		if len(args) > 2 {
			return fmt.Errorf("usage: /thread | /thread list [all] | /thread new | /thread fork | /thread sandbox | /thread policy")
		}
		if len(args) == 2 {
			if strings.TrimSpace(args[1]) != "all" {
				return fmt.Errorf("usage: /thread | /thread list [all] | /thread new | /thread fork | /thread sandbox | /thread policy")
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
	case "sandbox":
		if len(args) != 1 {
			return fmt.Errorf("usage: /thread sandbox")
		}
		return a.showThreadSandboxMenu(msg)
	case "policy":
		if len(args) != 1 {
			return fmt.Errorf("usage: /thread policy")
		}
		return a.showThreadPolicyMenu(msg)
	default:
		return fmt.Errorf("usage: /thread | /thread list [all] | /thread new | /thread fork | /thread sandbox | /thread policy")
	}
}

func (a *App) renderThreadsCard(sessionKey string, includeAll bool) (map[string]any, error) {
	sess := a.store.GetSession(sessionKey)
	workspace := a.cfg.Workspaces[0]
	if sess != nil {
		if ws := config.FindWorkspace(a.cfg, sess.WorkspaceID); ws != nil {
			workspace = *ws
		}
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
		"当前线程: " + currentLabel,
		"当前 thread id: `" + currentThreadID + "`",
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
	if sess != nil && strings.TrimSpace(sess.ActiveThreadID) != "" {
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
	card := newMarkdownBodyCard("线程管理", "blue")
	appendMarkdownBodyCardElement(card, map[string]any{"tag": "markdown", "content": menuCardBody("menu.thread", body)})
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
	sess := a.store.GetSession(sessionKey)
	discarded := a.discardSessionPendingInputs(sessionKey)
	if sess == nil || sess.ActiveTurnID == "" || sess.ActiveThreadID == "" {
		if discarded > 0 {
			return a.feishu.ReplyText(context.Background(), msg.MessageID, fmt.Sprintf("已清空 %d 条排队或暂存输入。", discarded), msg.ChatType == "group" && a.cfg.Feishu.ReplyInThread)
		}
		return fmt.Errorf("当前没有运行中的任务")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := a.codex.Call(ctx, "turn/interrupt", map[string]any{
		"threadId": sess.ActiveThreadID,
		"turnId":   sess.ActiveTurnID,
	}, nil); err != nil {
		return err
	}
	reply := "已请求中断当前任务。"
	if discarded > 0 {
		reply += fmt.Sprintf(" 已清空 %d 条排队或暂存输入。", discarded)
	}
	return a.feishu.ReplyText(context.Background(), msg.MessageID, reply, msg.ChatType == "group" && a.cfg.Feishu.ReplyInThread)
}

func (a *App) commandAppend(msg *feishu.InboundMessage, text string) error {
	sessionKey := a.makeSessionKey(msg)
	sess := a.store.GetSession(sessionKey)
	if sess == nil || sess.ActiveTurnID == "" || sess.ActiveThreadID == "" {
		return fmt.Errorf("当前没有可补充的任务")
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

func (a *App) commandWorkspace(msg *feishu.InboundMessage, args []string) error {
	if len(args) == 0 {
		return a.showWorkspaceMenu(msg)
	}
	if args[0] == "list" {
		return a.showWorkspaceMenu(msg)
	}
	if args[0] == "new" {
		return a.beginWorkspaceNew(msg)
	}
	if args[0] == "sandbox" {
		return a.showWorkspaceSandboxMenu(msg)
	}
	if args[0] == "policy" {
		return a.showWorkspacePolicyMenu(msg)
	}
	if len(args) >= 2 && args[0] == "use" {
		ws := config.FindWorkspace(a.cfg, args[1])
		if ws == nil {
			return fmt.Errorf("workspace %q not found", args[1])
		}
		sessionKey := a.makeSessionKey(msg)
		sess := a.store.GetSession(sessionKey)
		if sess == nil {
			sess = &state.Session{Key: sessionKey, ChatID: msg.ChatID, ChatType: msg.ChatType, OwnerUserID: msg.UserID}
		}
		switchSessionWorkspace(sess, ws.ID)
		if err := a.store.UpsertSession(sess); err != nil {
			return err
		}
		reply := "已切换工作区到 " + ws.ID
		if sessionHasInFlightSubmission(sess) {
			reply += "。当前运行中的任务仍归属原线程；后续新任务会使用新工作区。"
			return a.feishu.ReplyText(context.Background(), msg.MessageID, reply, msg.ChatType == "group" && a.cfg.Feishu.ReplyInThread)
		}
		binding, err := a.ensureWorkspaceThreadBinding(sessionKey, sess, ws)
		if err != nil {
			slog.Warn("workspace switch thread binding failed",
				"session_key", sessionKey,
				"workspace_id", ws.ID,
				"cwd", ws.Cwd,
				"error", err,
			)
			reply += "。自动绑定 thread 失败，可稍后重试。"
			return a.feishu.ReplyText(context.Background(), msg.MessageID, reply, msg.ChatType == "group" && a.cfg.Feishu.ReplyInThread)
		}
		if binding.Resumed {
			reply += "。已自动恢复该工作区最近使用的线程。"
		} else {
			reply += "。已自动创建新线程。"
		}
		return a.feishu.ReplyText(context.Background(), msg.MessageID, reply, msg.ChatType == "group" && a.cfg.Feishu.ReplyInThread)
	}
	return fmt.Errorf("usage: /workspace | /workspace list | /workspace new | /workspace use ID | /workspace sandbox | /workspace policy")
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

func (a *App) renderToolsMenuCard(sessionKey string) map[string]any {
	spec, _ := menuGroupSpec("menu.tools")
	return a.feishu.SimpleStatusCard(spec.Label, "blue", menuCardBody(spec.Action, spec.Description), renderGroupMenuButtons(spec.Action, sessionKey))
}

func (a *App) renderSessionMenuCard(sessionKey string) map[string]any {
	return a.renderToolsMenuCard(sessionKey)
}

func (a *App) renderContextMenuCard(sessionKey string) map[string]any {
	return a.renderCommandMenuCard(sessionKey)
}

func (a *App) renderModelMenuCard(sessionKey string) map[string]any {
	modelValue := firstNonEmpty(configuredGlobalModel(a.cfg), "(default)")
	effortValue := firstNonEmpty(configuredGlobalReasoningEffort(a.cfg), "(default)")
	fastValue := "-"
	if a.store != nil {
		if sess := a.store.GetSession(sessionKey); sess != nil {
			fastValue = renderServiceTierValue(sess.ActiveThreadServiceTier)
		}
	}
	body := strings.Join([]string{
		"当前 model: `" + modelValue + "`",
		"当前 reasoning: `" + effortValue + "`",
		"当前 fast: " + fastValue,
	}, "\n")
	buttons := []feishu.Button{
		{Text: submenuCommandLabel("模型配置", "/model"), Type: "default", Value: map[string]any{"action": "menu.model", "session_key": sessionKey}},
		{Text: submenuCommandLabel("响应速度", "/fast"), Type: "default", Value: map[string]any{"action": "menu.fast", "session_key": sessionKey}},
		{Text: "返回上一级", Type: "default", Value: map[string]any{"action": "menu.root", "session_key": sessionKey}},
	}
	return a.feishu.SimpleStatusCard("模型配置", "blue", menuCardBody("menu.group.model", body), buttons)
}

func (a *App) renderSystemMenuCard(sessionKey string) map[string]any {
	spec, _ := menuGroupSpec("menu.group.system")
	body := spec.Description + "\n\n当前 slog 日志级别: " + renderRuntimeLogLevelValue() + "\n当前版本: `" + currentVersion() + "`"
	return a.feishu.SimpleStatusCard(spec.Label, "blue", menuCardBody(spec.Action, body), renderGroupMenuButtons(spec.Action, sessionKey))
}

func (a *App) renderHelpCard(sessionKey string) map[string]any {
	buttons := []feishu.Button{
		{Text: "返回上一级", Type: "default", Value: map[string]any{"action": "menu.group.system", "session_key": sessionKey}},
	}
	return a.feishu.SimpleStatusCard("帮助说明", "blue", menuCardBody("menu.help", renderHelpBodyFromSpecs()), buttons)
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

func (a *App) showWorkspaceMenu(msg *feishu.InboundMessage) error {
	card := a.renderWorkspaceMenuCard(a.makeSessionKey(msg))
	var err error
	_, err = a.feishu.ReplyCard(context.Background(), msg.MessageID, card, msg.ChatType == "group" && a.cfg.Feishu.ReplyInThread)
	return err
}

func (a *App) renderWorkspaceMenuCard(sessionKey string) map[string]any {
	var sess *state.Session
	if a.store != nil {
		sess = a.store.GetSession(sessionKey)
	}
	currentID := a.defaultWorkspaceID()
	if sess != nil && strings.TrimSpace(sess.WorkspaceID) != "" {
		currentID = sess.WorkspaceID
	}
	currentWS := config.FindWorkspace(a.cfg, currentID)
	body := "当前工作区: `" + currentID + "`"
	if currentWS != nil {
		body += "\n默认 sandbox: `" + currentWS.SandboxMode + "`"
		body += "\n默认 policy: `" + currentWS.ApprovalPolicy + "`"
	}
	buttons := make([]feishu.Button, 0, 4)
	selectOptions := make([]selectStaticOption, 0, len(a.cfg.Workspaces))
	for _, ws := range a.cfg.Workspaces {
		label := ws.ID
		if ws.ID == currentID {
			label = "当前 · " + ws.ID
		}
		selectOptions = append(selectOptions, selectStaticOption{
			Text:  label,
			Value: ws.ID,
		})
	}
	buttons = append(buttons,
		feishu.Button{
			Text: submenuCommandLabel("新建工作区", "/workspace new"),
			Type: "default",
			Value: map[string]any{
				"action":      "workspace.new",
				"session_key": sessionKey,
			},
		},
		feishu.Button{
			Text: submenuCommandLabel("配置默认沙箱", "/workspace sandbox"),
			Type: "default",
			Value: map[string]any{
				"action":      "workspace.sandbox.menu",
				"session_key": sessionKey,
			},
		},
		feishu.Button{
			Text: submenuCommandLabel("配置默认策略", "/workspace policy"),
			Type: "default",
			Value: map[string]any{
				"action":      "workspace.policy.menu",
				"session_key": sessionKey,
			},
		},
		feishu.Button{
			Text: "返回上一级",
			Type: "default",
			Value: map[string]any{
				"action":      "menu.root",
				"session_key": sessionKey,
			},
		},
	)
	card := newMarkdownBodyCard("工作区管理", "blue")
	appendMarkdownBodyCardElement(card, map[string]any{"tag": "markdown", "content": menuCardBody("menu.workspace", body)})
	appendMarkdownBodyCardElement(card, buildSelectStaticElement(
		"workspace_select",
		"list",
		map[string]any{"action": "workspace.use.select", "session_key": sessionKey},
		selectOptions,
		currentID,
	))
	for _, row := range buildMarkdownBodyCardActionElements(buttons) {
		appendMarkdownBodyCardElement(card, row)
	}
	return card
}

func workspaceSandboxOptions() []workspaceSettingOption {
	return []workspaceSettingOption{
		{Value: "read-only", Label: "read-only"},
		{Value: "workspace-write", Label: "workspace-write"},
		{Value: "danger-full-access", Label: "danger-full-access"},
	}
}

func workspaceApprovalPolicyOptions() []workspaceSettingOption {
	return []workspaceSettingOption{
		{Value: "untrusted", Label: "untrusted"},
		{Value: "on-request", Label: "on-request"},
		{Value: "never", Label: "never"},
	}
}

func (a *App) currentWorkspaceForMessage(msg *feishu.InboundMessage) (sessionKey string, sess *state.Session, ws *config.Workspace) {
	sessionKey = a.makeSessionKey(msg)
	sess = a.store.GetSession(sessionKey)
	workspaceID := a.defaultWorkspaceID()
	if sess != nil && strings.TrimSpace(sess.WorkspaceID) != "" {
		workspaceID = sess.WorkspaceID
	}
	return sessionKey, sess, config.FindWorkspace(a.cfg, workspaceID)
}

func (a *App) currentThreadForMessage(msg *feishu.InboundMessage) (sessionKey string, sess *state.Session, ws *config.Workspace, threadID string, err error) {
	sessionKey, sess, ws = a.currentWorkspaceForMessage(msg)
	if sess == nil || strings.TrimSpace(sess.ActiveThreadID) == "" {
		return sessionKey, sess, ws, "", fmt.Errorf("当前没有活动线程")
	}
	return sessionKey, sess, ws, strings.TrimSpace(sess.ActiveThreadID), nil
}

func (a *App) showWorkspaceSandboxMenu(msg *feishu.InboundMessage) error {
	card, err := a.renderWorkspaceSandboxMenuCard(a.makeSessionKey(msg))
	if err != nil {
		return err
	}
	_, err = a.feishu.ReplyCard(context.Background(), msg.MessageID, card, msg.ChatType == "group" && a.cfg.Feishu.ReplyInThread)
	return err
}

func (a *App) renderWorkspaceSandboxMenuCard(sessionKey string) (map[string]any, error) {
	var sess *state.Session
	if a.store != nil {
		sess = a.store.GetSession(sessionKey)
	}
	workspaceID := a.defaultWorkspaceID()
	if sess != nil && strings.TrimSpace(sess.WorkspaceID) != "" {
		workspaceID = sess.WorkspaceID
	}
	ws := config.FindWorkspace(a.cfg, workspaceID)
	if ws == nil {
		return nil, fmt.Errorf("current workspace not found")
	}
	body := "配置当前工作区默认 sandbox。\n\n当前工作区: `" + ws.ID + "`\n当前值: `" + ws.SandboxMode + "`"
	buttons := make([]feishu.Button, 0, len(workspaceSandboxOptions())+1)
	for _, opt := range workspaceSandboxOptions() {
		btnType := "default"
		label := opt.Label
		if opt.Value == ws.SandboxMode {
			btnType = "primary"
			label = "当前 · " + label
		}
		buttons = append(buttons, feishu.Button{
			Text: label,
			Type: btnType,
			Value: map[string]any{
				"action":       "workspace.sandbox.set",
				"session_key":  sessionKey,
				"workspace_id": ws.ID,
				"sandbox_mode": opt.Value,
			},
		})
	}
	buttons = append(buttons, feishu.Button{
		Text: commandLabel("返回工作区", "/workspace"),
		Type: "default",
		Value: map[string]any{
			"action":      "menu.workspace",
			"session_key": sessionKey,
		},
	})
	return a.feishu.SimpleStatusCard("配置 Sandbox", "blue", menuCardBody("workspace.sandbox.menu", body), buttons), nil
}

func (a *App) showWorkspacePolicyMenu(msg *feishu.InboundMessage) error {
	card, err := a.renderWorkspacePolicyMenuCard(a.makeSessionKey(msg))
	if err != nil {
		return err
	}
	_, err = a.feishu.ReplyCard(context.Background(), msg.MessageID, card, msg.ChatType == "group" && a.cfg.Feishu.ReplyInThread)
	return err
}

func (a *App) renderWorkspacePolicyMenuCard(sessionKey string) (map[string]any, error) {
	var sess *state.Session
	if a.store != nil {
		sess = a.store.GetSession(sessionKey)
	}
	workspaceID := a.defaultWorkspaceID()
	if sess != nil && strings.TrimSpace(sess.WorkspaceID) != "" {
		workspaceID = sess.WorkspaceID
	}
	ws := config.FindWorkspace(a.cfg, workspaceID)
	if ws == nil {
		return nil, fmt.Errorf("current workspace not found")
	}
	body := "配置当前工作区默认 approval policy。\n\n当前工作区: `" + ws.ID + "`\n当前值: `" + ws.ApprovalPolicy + "`"
	buttons := make([]feishu.Button, 0, len(workspaceApprovalPolicyOptions())+1)
	for _, opt := range workspaceApprovalPolicyOptions() {
		btnType := "default"
		label := opt.Label
		if opt.Value == ws.ApprovalPolicy {
			btnType = "primary"
			label = "当前 · " + label
		}
		buttons = append(buttons, feishu.Button{
			Text: label,
			Type: btnType,
			Value: map[string]any{
				"action":          "workspace.policy.set",
				"session_key":     sessionKey,
				"workspace_id":    ws.ID,
				"approval_policy": opt.Value,
			},
		})
	}
	buttons = append(buttons, feishu.Button{
		Text: commandLabel("返回工作区", "/workspace"),
		Type: "default",
		Value: map[string]any{
			"action":      "menu.workspace",
			"session_key": sessionKey,
		},
	})
	return a.feishu.SimpleStatusCard("配置 Policy", "blue", menuCardBody("workspace.policy.menu", body), buttons), nil
}

func (a *App) showThreadSandboxMenu(msg *feishu.InboundMessage) error {
	card, err := a.renderThreadSandboxMenuCard(a.makeSessionKey(msg))
	if err != nil {
		return err
	}
	_, err = a.feishu.ReplyCard(context.Background(), msg.MessageID, card, msg.ChatType == "group" && a.cfg.Feishu.ReplyInThread)
	return err
}

func (a *App) renderThreadSandboxMenuCard(sessionKey string) (map[string]any, error) {
	sess := a.store.GetSession(sessionKey)
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
	_, err = a.feishu.ReplyCard(context.Background(), msg.MessageID, card, msg.ChatType == "group" && a.cfg.Feishu.ReplyInThread)
	return err
}

func (a *App) renderThreadPolicyMenuCard(sessionKey string) (map[string]any, error) {
	sess := a.store.GetSession(sessionKey)
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

type workspaceNewPayload struct {
	RootPath    string             `json:"root_path"`
	SelectedCWD string             `json:"selected_cwd"`
	DraftID     string             `json:"draft_id,omitempty"`
	DraftName   string             `json:"draft_name,omitempty"`
	Picker      *pathPickerPayload `json:"picker,omitempty"`
}

func workspaceNewPayloadFromPending(pending *state.PendingRequest) workspaceNewPayload {
	var payload workspaceNewPayload
	if pending != nil && strings.TrimSpace(pending.PayloadJSON) != "" {
		_ = json.Unmarshal([]byte(pending.PayloadJSON), &payload)
	}
	return payload
}

func (a *App) defaultWorkspaceNewRoot(ws *config.Workspace) string {
	return "/"
}

func (a *App) renderWorkspaceNewCard(sessionKey, requestID string, payload workspaceNewPayload) map[string]any {
	if payload.Picker != nil {
		card, err := a.renderPathPickerCard(requestID, *payload.Picker)
		if err == nil {
			return card
		}
		payload.Picker = nil
	}
	selectedCWD := strings.TrimSpace(payload.SelectedCWD)
	if selectedCWD == "" {
		selectedCWD = payload.RootPath
	}
	card := newMarkdownBodyCard("新建工作区", "orange")
	body := "当前位置：主菜单 / workspace / new\n\n" +
		"已选目录: `" + firstNonEmpty(selectedCWD, "-") + "`\n" +
		"浏览根目录: `" + firstNonEmpty(strings.TrimSpace(payload.RootPath), "-") + "`\n\n" +
		"填写 `workspace_id` 和可选的 `name`，需要换目录时点“选目录”，最后点“确认”。"
	appendMarkdownBodyCardElement(card, map[string]any{"tag": "markdown", "content": body})
	buttonRows := buildMarkdownBodyCardActionElements([]feishu.Button{
		{
			Text:  "选目录",
			Type:  "default",
			Name:  "workspace_new_pickdir",
			Value: map[string]any{"action": "workspace.new.pickdir", "request_id": requestID},
		},
		{
			Text:  "确认",
			Type:  "primary",
			Name:  "workspace_new_submit",
			Value: map[string]any{"action": "workspace.new.submit", "request_id": requestID},
		},
		{
			Text:  "取消",
			Type:  "default",
			Name:  "workspace_new_cancel",
			Value: map[string]any{"action": "pending_form.cancel", "request_id": requestID},
		},
	})
	for idx, row := range buttonRows {
		columns := row["columns"].([]map[string]any)
		if len(columns) == 0 {
			continue
		}
		button := columns[0]["elements"].([]map[string]any)[0]
		if idx < 2 {
			button["form_action_type"] = "submit"
		}
	}
	workspaceIDInput := map[string]any{
		"tag":         "input",
		"name":        "workspace_id",
		"required":    true,
		"placeholder": map[string]any{"tag": "plain_text", "content": "workspace_id"},
	}
	if value := strings.TrimSpace(payload.DraftID); value != "" {
		workspaceIDInput["default_value"] = value
	}
	workspaceNameInput := map[string]any{
		"tag":         "input",
		"name":        "workspace_name",
		"required":    false,
		"placeholder": map[string]any{"tag": "plain_text", "content": "name（可选）"},
	}
	if value := strings.TrimSpace(payload.DraftName); value != "" {
		workspaceNameInput["default_value"] = value
	}
	form := map[string]any{
		"tag":                "form",
		"name":               "workspace_new_form",
		"direction":          "vertical",
		"horizontal_spacing": "8px",
		"vertical_spacing":   "8px",
		"elements": append([]map[string]any{
			workspaceIDInput,
			workspaceNameInput,
		}, buttonRows...),
	}
	appendMarkdownBodyCardElement(card, form)
	return card
}

func (a *App) beginWorkspaceNew(msg *feishu.InboundMessage) error {
	sessionKey, _, ws := a.currentWorkspaceForMessage(msg)
	requestID, err := a.store.NextLocalID("workspace")
	if err != nil {
		return err
	}
	payload := workspaceNewPayload{
		RootPath: a.defaultWorkspaceNewRoot(ws),
		SelectedCWD: firstNonEmpty(func() string {
			if ws == nil {
				return ""
			}
			return strings.TrimSpace(ws.Cwd)
		}(), "/"),
	}
	card := a.renderWorkspaceNewCard(sessionKey, requestID, payload)
	msgID, err := a.feishu.ReplyCard(context.Background(), msg.MessageID, card, msg.ChatType == "group" && a.cfg.Feishu.ReplyInThread)
	if err != nil {
		return err
	}
	return a.store.UpsertPending(&state.PendingRequest{
		ID:          requestID,
		Kind:        "workspace_new",
		SessionKey:  sessionKey,
		OwnerUserID: msg.UserID,
		FeishuMsgID: msgID,
		PayloadJSON: mustJSON(payload),
		Status:      "pending",
		CreatedAt:   time.Now().Unix(),
		ExpiresAt:   time.Now().Add(10 * time.Minute).Unix(),
	})
}

func formValueString(values map[string]any, key string) (string, bool) {
	if len(values) == 0 {
		return "", false
	}
	raw, ok := values[key]
	if !ok {
		return "", false
	}
	switch v := raw.(type) {
	case string:
		return strings.TrimSpace(v), true
	default:
		return strings.TrimSpace(fmt.Sprint(v)), true
	}
}

func mergeWorkspaceNewFormValues(payload workspaceNewPayload, values map[string]any) workspaceNewPayload {
	if value, ok := formValueString(values, "workspace_id"); ok && value != "" {
		payload.DraftID = value
	}
	if value, ok := formValueString(values, "workspace_name"); ok {
		payload.DraftName = value
	}
	return payload
}

func (a *App) createWorkspaceAndSwitch(sessionKey, userID, chatID, chatType, id, name, cwd string) error {
	if config.FindWorkspace(a.cfg, id) != nil {
		return fmt.Errorf("workspace %q 已存在", id)
	}
	a.cfg.Workspaces = append(a.cfg.Workspaces, config.Workspace{
		ID:             id,
		Name:           name,
		Cwd:            cwd,
		Model:          "",
		ApprovalPolicy: "on-request",
		SandboxMode:    "workspace-write",
	})
	if err := a.cfg.Normalize(filepath.Dir(a.cfgPath)); err != nil {
		a.cfg.Workspaces = a.cfg.Workspaces[:len(a.cfg.Workspaces)-1]
		return err
	}
	if err := config.Save(a.cfgPath, a.cfg); err != nil {
		a.cfg.Workspaces = a.cfg.Workspaces[:len(a.cfg.Workspaces)-1]
		return err
	}
	sess := a.store.GetSession(sessionKey)
	if sess == nil {
		sess = &state.Session{Key: sessionKey, ChatID: chatID, ChatType: chatType, OwnerUserID: userID}
	}
	switchSessionWorkspace(sess, id)
	if err := a.store.UpsertSession(sess); err != nil {
		return err
	}
	if sessionHasInFlightSubmission(sess) {
		return nil
	}
	ws := config.FindWorkspace(a.cfg, id)
	if ws == nil {
		return nil
	}
	if _, err := a.ensureWorkspaceThreadBinding(sessionKey, sess, ws); err != nil {
		slog.Warn("workspace create thread binding failed",
			"session_key", sessionKey,
			"workspace_id", id,
			"cwd", cwd,
			"error", err,
		)
	}
	return nil
}

func (a *App) completeWorkspaceNewText(msg *feishu.InboundMessage, pending *state.PendingRequest) error {
	payload := workspaceNewPayloadFromPending(pending)
	parts := strings.Fields(strings.TrimSpace(msg.Text))
	if len(parts) < 1 {
		return fmt.Errorf("格式错误，需发送: workspace_id [name]")
	}
	id := parts[0]
	cwd := strings.TrimSpace(payload.SelectedCWD)
	name := id
	if cwd == "" && len(parts) >= 2 {
		// 兼容旧格式: workspace_id cwd [name]
		cwd = parts[1]
		if len(parts) > 2 {
			name = strings.Join(parts[2:], " ")
		}
	} else if len(parts) > 1 {
		name = strings.Join(parts[1:], " ")
	}
	if strings.TrimSpace(cwd) == "" {
		return fmt.Errorf("请先选择目录")
	}
	sessionKey := a.makeSessionKey(msg)
	if err := a.createWorkspaceAndSwitch(sessionKey, msg.UserID, msg.ChatID, msg.ChatType, id, name, cwd); err != nil {
		return err
	}
	_ = a.store.UpdatePending(pending.ID, func(req *state.PendingRequest) { req.Status = "resolved" })
	if pending.FeishuMsgID != "" {
		_ = a.feishu.PatchCard(context.Background(), pending.FeishuMsgID, a.feishu.SimpleStatusCard("工作区已创建", "green", "已创建并切换到工作区 `"+id+"`\n\ncwd: `"+cwd+"`", nil))
	}
	return a.feishu.ReplyText(context.Background(), msg.MessageID, "已创建并切换到工作区 "+id, msg.ChatType == "group" && a.cfg.Feishu.ReplyInThread)
}

func (a *App) completeWorkspaceNewPickDir(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	requestID, _ := action.ActionValue["request_id"].(string)
	pending := a.store.PendingByID(requestID)
	if pending == nil || pending.Kind != "workspace_new" {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "工作区创建请求已过期"}}, nil
	}
	if pending.OwnerUserID != "" && pending.OwnerUserID != action.UserID {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "你没有权限处理这个工作区请求"}}, nil
	}
	payload := mergeWorkspaceNewFormValues(workspaceNewPayloadFromPending(pending), action.FormValue)
	currentPath := firstNonEmpty(strings.TrimSpace(payload.SelectedCWD), "/")
	payload.Picker = &pathPickerPayload{
		Mode:        pathPickerModeDirectory,
		Style:       pathPickerStyleDropdown,
		RootPath:    firstNonEmpty(strings.TrimSpace(payload.RootPath), "/"),
		CurrentPath: currentPath,
	}
	_ = a.store.UpdatePending(requestID, func(req *state.PendingRequest) { req.PayloadJSON = mustJSON(payload) })
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "info", Content: "已打开目录选择"},
		Card:  rawCard(a.renderWorkspaceNewCard(pending.SessionKey, requestID, payload)),
	}, nil
}

func (a *App) completeWorkspaceNewSubmit(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	requestID, _ := action.ActionValue["request_id"].(string)
	pending := a.store.PendingByID(requestID)
	if pending == nil || pending.Kind != "workspace_new" {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "工作区创建请求已过期"}}, nil
	}
	if pending.OwnerUserID != "" && pending.OwnerUserID != action.UserID {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "你没有权限处理这个工作区请求"}}, nil
	}
	payload := mergeWorkspaceNewFormValues(workspaceNewPayloadFromPending(pending), action.FormValue)
	id := strings.TrimSpace(payload.DraftID)
	if id == "" {
		_ = a.store.UpdatePending(requestID, func(req *state.PendingRequest) { req.PayloadJSON = mustJSON(payload) })
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "warning", Content: "请填写 workspace_id"},
			Card:  rawCard(a.renderWorkspaceNewCard(pending.SessionKey, requestID, payload)),
		}, nil
	}
	cwd := strings.TrimSpace(payload.SelectedCWD)
	if cwd == "" {
		_ = a.store.UpdatePending(requestID, func(req *state.PendingRequest) { req.PayloadJSON = mustJSON(payload) })
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "warning", Content: "请先选择目录"},
			Card:  rawCard(a.renderWorkspaceNewCard(pending.SessionKey, requestID, payload)),
		}, nil
	}
	name := strings.TrimSpace(payload.DraftName)
	if name == "" {
		name = id
	}
	sess := a.store.GetSession(pending.SessionKey)
	chatID := action.ChatID
	chatType := ""
	if sess != nil {
		chatID = firstNonEmpty(chatID, sess.ChatID)
		chatType = sess.ChatType
	}
	if err := a.createWorkspaceAndSwitch(pending.SessionKey, action.UserID, chatID, chatType, id, name, cwd); err != nil {
		_ = a.store.UpdatePending(requestID, func(req *state.PendingRequest) { req.PayloadJSON = mustJSON(payload) })
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "warning", Content: err.Error()},
			Card:  rawCard(a.renderWorkspaceNewCard(pending.SessionKey, requestID, payload)),
		}, nil
	}
	_ = a.store.UpdatePending(requestID, func(req *state.PendingRequest) {
		req.Status = "resolved"
		req.PayloadJSON = mustJSON(payload)
	})
	body := "已创建并切换到工作区 `" + id + "`\n\ncwd: `" + cwd + "`"
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已创建工作区"},
		Card:  rawCard(a.feishu.SimpleStatusCard("工作区已创建", "green", body, nil)),
	}, nil
}
