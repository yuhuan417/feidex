package app

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"feidex/internal/codexrpc"
	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"
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
	case "/model":
		return a.commandModel(msg)
	case "/quiet":
		return a.commandQuiet(msg, fields[1:])
	case "/fast":
		return a.commandFast(msg, fields[1:])
	case "/new":
		return a.commandNew(msg)
	case "/threads":
		if len(fields) > 1 {
			switch fields[1] {
			case "new":
				return a.commandThreadsNew(msg)
			case "sandbox":
				return a.showThreadSandboxMenu(msg)
			case "policy":
				return a.showThreadPolicyMenu(msg)
			}
		}
		all := len(fields) > 1 && fields[1] == "all"
		return a.commandThreads(msg, all)
	case "/interrupt", "/stop":
		return a.commandInterrupt(msg)
	case "/status":
		return a.commandStatus(msg)
	case "/upgrade":
		return a.commandUpgrade(msg)
	case "/workspace", "/cd":
		return a.commandWorkspace(msg, fields[1:])
	default:
		return fmt.Errorf("unknown command: %s", fields[0])
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
	case "/quiet":
		return true
	case "/menu", "/help", "/new", "/threads", "/interrupt", "/stop", "/status", "/workspace", "/cd", "/upgrade", "/fast":
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

func (a *App) commandThreadsNew(msg *feishu.InboundMessage) error {
	sessionKey := a.makeSessionKey(msg)
	sess := a.store.GetSession(sessionKey)
	if sess == nil {
		sess = &state.Session{Key: sessionKey, WorkspaceID: a.cfg.Workspaces[0].ID, ChatID: msg.ChatID, ChatType: msg.ChatType, OwnerUserID: msg.UserID}
	}
	if sess.ActiveTurnID != "" {
		return fmt.Errorf("当前任务仍在运行，请先等待结束或中断")
	}
	discarded := a.discardSessionPendingInputs(sessionKey)
	sess = a.store.GetSession(sessionKey)
	if sess == nil {
		sess = &state.Session{Key: sessionKey, WorkspaceID: a.cfg.Workspaces[0].ID, ChatID: msg.ChatID, ChatType: msg.ChatType, OwnerUserID: msg.UserID}
	}
	clearSessionThreadContext(sess)
	a.clearSessionLiveThread(sessionKey)
	sess.ActiveTurnID = ""
	sess.ActiveSubmissionID = ""
	sess.Status = "idle"
	sess.Queue = nil
	sess.StagedImages = nil
	if err := a.store.UpsertSession(sess); err != nil {
		return err
	}
	reply := "已切换到新线程模式。下一条消息会创建新线程，当前工作区保持不变。"
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

func (a *App) renderThreadsCard(sessionKey string, includeAll bool) (map[string]any, error) {
	sess := a.store.GetSession(sessionKey)
	workspace := a.cfg.Workspaces[0]
	if sess != nil {
		if ws := config.FindWorkspace(a.cfg, sess.WorkspaceID); ws != nil {
			workspace = *ws
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	queries := []map[string]any{
		{
			"limit":    8,
			"cwd":      workspace.Cwd,
			"archived": false,
		},
		{
			"limit":    8,
			"cwd":      workspace.Cwd,
			"archived": false,
		},
		{
			"limit":    8,
			"archived": false,
		},
	}
	if includeAll {
		queries[0]["sourceKinds"] = []string{"appServer", "cli", "vscode", "exec"}
	} else {
		queries[0]["sourceKinds"] = []string{"appServer"}
	}

	var result codexrpc.ThreadListResult
	var err error
	for idx, params := range queries {
		slog.Debug("thread list query",
			"attempt", idx+1,
			"session_key", sessionKey,
			"params", fmt.Sprintf("%v", params),
		)
		result = codexrpc.ThreadListResult{}
		err = a.codex.Call(ctx, "thread/list", params, &result)
		if err != nil {
			slog.Error("thread list query failed", "attempt", idx+1, "error", err)
			continue
		}
		slog.Debug("thread list query result", "attempt", idx+1, "count", len(result.Data))
		if len(result.Data) > 0 {
			break
		}
	}
	if err != nil && len(result.Data) == 0 {
		return nil, err
	}
	result.Data = filterThreadsByWorkspaceCWD(result.Data, workspace.Cwd)
	if len(result.Data) == 0 {
		buttons := []feishu.Button{
			{Text: "新会话", Type: "default", Value: map[string]any{"action": "menu.new", "session_key": sessionKey, "parent_action": "menu.threads"}},
			{Text: "返回上一级", Type: "default", Value: map[string]any{"action": "menu.group.context", "session_key": sessionKey}},
		}
		return a.feishu.SimpleStatusCard("线程列表", "blue", menuCardBody("menu.threads", "没有可恢复的线程。"), buttons), nil
	}
	sort.Slice(result.Data, func(i, j int) bool { return result.Data[i].UpdatedAt > result.Data[j].UpdatedAt })
	currentLabel := "-"
	if sess != nil {
		currentLabel = currentThreadLabel(sess)
	}
	lines := make([]string, 0, len(result.Data)+2)
	lines = append(lines, "当前线程: "+currentLabel, "", "最近线程：")
	if sess != nil && strings.TrimSpace(sess.ActiveThreadID) != "" {
		lines = append(lines,
			"当前 thread sandbox: "+renderThreadSettingValue(sess.ActiveThreadSandboxMode, workspace.SandboxMode),
			"当前 thread policy: "+renderThreadSettingValue(sess.ActiveThreadApprovalPolicy, workspace.ApprovalPolicy),
			"",
		)
	}
	buttons := make([]feishu.Button, 0, len(result.Data))
	for idx, item := range result.Data {
		label := renderThreadButtonLabel(item.Name, item.Preview, item.ID)
		btnType := "default"
		entry := fmt.Sprintf("%d. %s", idx+1, renderThreadListEntry(item.Name, item.Preview, item.ID))
		if sess != nil && item.ID == sess.ActiveThreadID {
			label = "当前 · " + label
			btnType = "primary"
			entry = fmt.Sprintf("%d. [当前] %s", idx+1, renderThreadListEntry(item.Name, item.Preview, item.ID))
		}
		lines = append(lines, entry)
		buttons = append(buttons, feishu.Button{
			Text: label,
			Type: btnType,
			Value: map[string]any{
				"action":         "thread.resume",
				"thread_id":      item.ID,
				"thread_name":    item.Name,
				"thread_preview": item.Preview,
				"thread_cwd":     item.Cwd,
				"session_key":    sessionKey,
			},
		})
	}
	if sess != nil && strings.TrimSpace(sess.ActiveThreadID) != "" {
		buttons = append(buttons,
			feishu.Button{
				Text: submenuLabel("配置 Thread Sandbox"),
				Type: "default",
				Value: map[string]any{
					"action":      "thread.sandbox.menu",
					"session_key": sessionKey,
				},
			},
			feishu.Button{
				Text: submenuLabel("配置 Thread Policy"),
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
			Text: "新会话",
			Type: "default",
			Value: map[string]any{
				"action":        "menu.new",
				"session_key":   sessionKey,
				"parent_action": "menu.threads",
			},
		},
		feishu.Button{
			Text: "刷新列表",
			Type: "default",
			Value: map[string]any{
				"action":      "menu.threads",
				"session_key": sessionKey,
			},
		},
		feishu.Button{
			Text: "返回上一级",
			Type: "default",
			Value: map[string]any{
				"action":      "menu.group.context",
				"session_key": sessionKey,
			},
		},
	)
	body := strings.Join(lines, "\n")
	return a.feishu.SimpleStatusCard("线程列表", "blue", menuCardBody("menu.threads", body), buttons), nil
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
		lines := make([]string, 0, len(a.cfg.Workspaces))
		for _, ws := range a.cfg.Workspaces {
			lines = append(lines, fmt.Sprintf("- %s: %s", ws.ID, ws.Cwd))
		}
		return a.feishu.ReplyText(context.Background(), msg.MessageID, strings.Join(lines, "\n"), msg.ChatType == "group" && a.cfg.Feishu.ReplyInThread)
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
		if sess.ActiveTurnID != "" {
			reply += "。当前运行中的任务仍归属原线程；后续新任务会使用新工作区。"
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

func (a *App) renderSessionMenuCard(sessionKey string) map[string]any {
	buttons := []feishu.Button{
		{Text: "中断任务", Type: "default", Value: map[string]any{"action": "menu.interrupt", "session_key": sessionKey, "parent_action": "menu.group.session"}},
		{Text: submenuLabel("Quiet 模式"), Type: "default", Value: map[string]any{"action": "menu.quiet", "session_key": sessionKey}},
		{Text: "返回上一级", Type: "default", Value: map[string]any{"action": "menu.root", "session_key": sessionKey}},
	}
	return a.feishu.SimpleStatusCard("会话行为", "blue", menuCardBody("menu.group.session", "控制当前会话的行为与输出方式。"), buttons)
}

func (a *App) renderContextMenuCard(sessionKey string) map[string]any {
	buttons := []feishu.Button{
		{Text: "新线程", Type: "default", Value: map[string]any{"action": "menu.new", "session_key": sessionKey, "parent_action": "menu.group.context"}},
		{Text: submenuLabel("工作区管理"), Type: "default", Value: map[string]any{"action": "menu.workspace", "session_key": sessionKey}},
		{Text: submenuLabel("线程管理"), Type: "default", Value: map[string]any{"action": "menu.threads", "session_key": sessionKey}},
		{Text: "返回上一级", Type: "default", Value: map[string]any{"action": "menu.root", "session_key": sessionKey}},
	}
	return a.feishu.SimpleStatusCard("会话管理", "blue", menuCardBody("menu.group.context", "管理线程与工作区上下文。"), buttons)
}

func (a *App) renderModelMenuCard(sessionKey string) map[string]any {
	buttons := []feishu.Button{
		{Text: submenuLabel("模型配置"), Type: "default", Value: map[string]any{"action": "menu.model", "session_key": sessionKey}},
		{Text: submenuLabel("推理强度"), Type: "default", Value: map[string]any{"action": "menu.reasoning", "session_key": sessionKey}},
		{Text: submenuLabel("响应速度"), Type: "default", Value: map[string]any{"action": "menu.fast", "session_key": sessionKey}},
		{Text: "返回上一级", Type: "default", Value: map[string]any{"action": "menu.root", "session_key": sessionKey}},
	}
	return a.feishu.SimpleStatusCard("模型能力", "blue", menuCardBody("menu.group.model", "配置模型、推理强度与响应速度。"), buttons)
}

func (a *App) renderSystemMenuCard(sessionKey string) map[string]any {
	buttons := []feishu.Button{
		{Text: submenuLabel("状态面板"), Type: "default", Value: map[string]any{"action": "menu.status", "session_key": sessionKey}},
		{Text: submenuLabel("升级服务"), Type: "default", Value: map[string]any{"action": "menu.upgrade", "session_key": sessionKey}},
		{Text: submenuLabel("帮助说明"), Type: "default", Value: map[string]any{"action": "menu.help", "session_key": sessionKey}},
		{Text: "返回上一级", Type: "default", Value: map[string]any{"action": "menu.root", "session_key": sessionKey}},
	}
	return a.feishu.SimpleStatusCard("服务管理", "blue", menuCardBody("menu.group.system", "查看服务状态、执行升级，或查阅命令帮助。"), buttons)
}

func (a *App) renderHelpCard(sessionKey string) map[string]any {
	body := strings.Join([]string{
		"命令说明：",
		"",
		"`/menu`",
		"打开命令菜单。",
		"",
		"`/help`",
		"查看所有本地命令与说明。",
		"",
		"会话行为：",
		"`/interrupt` 或 `/stop`",
		"中断当前运行中的任务，并清空排队/暂存输入。",
		"`/quiet`",
		"切换 Quiet 模式。",
		"`/quiet on`",
		"开启 Quiet 模式。",
		"`/quiet off`",
		"关闭 Quiet 模式。",
		"",
		"会话管理：",
		"`/new`",
		"切换到新线程模式，下一条消息会新建线程。",
		"`/threads`",
		"查看当前工作区可恢复的线程。",
		"`/threads all`",
		"查看更多来源的线程。",
		"`/threads new`",
		"等价于 `/new`。",
		"`/threads sandbox`",
		"配置当前线程的 sandbox。",
		"`/threads policy`",
		"配置当前线程的 approval policy。",
		"`/workspace` 或 `/cd`",
		"打开工作区菜单。",
		"`/workspace list`",
		"列出所有工作区。",
		"`/workspace new`",
		"创建新工作区。",
		"`/workspace use ID`",
		"切换到指定工作区。",
		"`/workspace sandbox`",
		"配置当前工作区默认 sandbox。",
		"`/workspace policy`",
		"配置当前工作区默认 approval policy。",
		"",
		"模型能力：",
		"`/model`",
		"打开模型与推理强度配置。",
		"`/fast`",
		"切换当前线程的响应速度设置。",
		"",
		"服务管理：",
		"`/status`",
		"查看当前会话、线程、工作区与模型状态。",
		"`/upgrade`",
		"检查新版本并发起服务升级。",
	}, "\n")
	buttons := []feishu.Button{
		{Text: "返回上一级", Type: "default", Value: map[string]any{"action": "menu.group.system", "session_key": sessionKey}},
	}
	return a.feishu.SimpleStatusCard("帮助说明", "blue", menuCardBody("menu.help", body), buttons)
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
	body := "选择工作区：\n\n当前工作区: `" + currentID + "`"
	if currentWS != nil {
		body += "\n默认 sandbox: `" + currentWS.SandboxMode + "`"
		body += "\n默认 policy: `" + currentWS.ApprovalPolicy + "`"
	}
	buttons := make([]feishu.Button, 0, len(a.cfg.Workspaces)+4)
	for _, ws := range a.cfg.Workspaces {
		label := ws.ID
		btnType := "default"
		if ws.ID == currentID {
			label = "当前 · " + ws.ID
			btnType = "primary"
		}
		buttons = append(buttons, feishu.Button{
			Text: label,
			Type: btnType,
			Value: map[string]any{
				"action":       "workspace.use",
				"workspace_id": ws.ID,
				"session_key":  sessionKey,
			},
		})
	}
	buttons = append(buttons,
		feishu.Button{
			Text: submenuLabel("新建工作区"),
			Type: "default",
			Value: map[string]any{
				"action":      "workspace.new",
				"session_key": sessionKey,
			},
		},
		feishu.Button{
			Text: submenuLabel("配置 Sandbox"),
			Type: "default",
			Value: map[string]any{
				"action":      "workspace.sandbox.menu",
				"session_key": sessionKey,
			},
		},
		feishu.Button{
			Text: submenuLabel("配置 Policy"),
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
				"action":      "menu.group.context",
				"session_key": sessionKey,
			},
		},
	)
	return a.feishu.SimpleStatusCard("工作区", "blue", menuCardBody("menu.workspace", body), buttons)
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
		Text: "返回工作区",
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
		Text: "返回工作区",
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
		Text: "返回线程列表",
		Type: "default",
		Value: map[string]any{
			"action":      "menu.threads",
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
		Text: "返回线程列表",
		Type: "default",
		Value: map[string]any{
			"action":      "menu.threads",
			"session_key": sessionKey,
		},
	})
	return a.feishu.SimpleStatusCard("配置 Thread Policy", "blue", menuCardBody("thread.policy.menu", body), buttons), nil
}

func (a *App) renderWorkspaceNewCard(sessionKey, requestID string) map[string]any {
	body := "请直接发送一行文本来创建工作区。\n\n格式：\n`workspace_id cwd`\n或\n`workspace_id cwd name`\n\n说明：\n- `name` 是可选的，不用输入方括号\n- 如果不填 name，就默认用 workspace_id 作为显示名\n\n示例：\n`op /home/yuhuan/obfs-sniproxy`\n`op /home/yuhuan/obfs-sniproxy ObfsSniproxy`\n\n发送 `/` 或其它命令前，可先完成本次创建。"
	return a.feishu.SimpleStatusCard("新建工作区", "orange", menuCardBody("workspace.new", body), []feishu.Button{
		{Text: "返回上一级", Type: "default", Value: map[string]any{"action": "pending_form.cancel", "request_id": requestID}},
	})
}

func (a *App) beginWorkspaceNew(msg *feishu.InboundMessage) error {
	sessionKey := a.makeSessionKey(msg)
	requestID, err := a.store.NextLocalID("workspace")
	if err != nil {
		return err
	}
	card := a.renderWorkspaceNewCard(sessionKey, requestID)
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
		Status:      "pending",
		CreatedAt:   time.Now().Unix(),
		ExpiresAt:   time.Now().Add(10 * time.Minute).Unix(),
	})
}

func (a *App) completeWorkspaceNewText(msg *feishu.InboundMessage, pending *state.PendingRequest) error {
	parts := strings.Fields(strings.TrimSpace(msg.Text))
	if len(parts) < 2 {
		return fmt.Errorf("格式错误，需发送: workspace_id cwd [name]")
	}
	id := parts[0]
	cwd := parts[1]
	name := id
	if len(parts) > 2 {
		name = strings.Join(parts[2:], " ")
	}
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
		// rollback
		a.cfg.Workspaces = a.cfg.Workspaces[:len(a.cfg.Workspaces)-1]
		return err
	}
	if err := config.Save(a.cfgPath, a.cfg); err != nil {
		a.cfg.Workspaces = a.cfg.Workspaces[:len(a.cfg.Workspaces)-1]
		return err
	}
	sessionKey := a.makeSessionKey(msg)
	sess := a.store.GetSession(sessionKey)
	if sess == nil {
		sess = &state.Session{Key: sessionKey, ChatID: msg.ChatID, ChatType: msg.ChatType, OwnerUserID: msg.UserID}
	}
	switchSessionWorkspace(sess, id)
	if err := a.store.UpsertSession(sess); err != nil {
		return err
	}
	_ = a.store.UpdatePending(pending.ID, func(req *state.PendingRequest) { req.Status = "resolved" })
	if pending.FeishuMsgID != "" {
		_ = a.feishu.PatchCard(context.Background(), pending.FeishuMsgID, a.feishu.SimpleStatusCard("工作区已创建", "green", "已创建并切换到工作区 `"+id+"`\n\ncwd: `"+cwd+"`", nil))
	}
	return a.feishu.ReplyText(context.Background(), msg.MessageID, "已创建并切换到工作区 "+id, msg.ChatType == "group" && a.cfg.Feishu.ReplyInThread)
}
