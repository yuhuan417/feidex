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

func (a *App) handleCommand(msg *feishu.InboundMessage, raw string) error {
	raw = strings.TrimSpace(raw)
	fields := strings.Fields(raw)
	if len(fields) == 0 {
		return nil
	}
	switch fields[0] {
	case "/new":
		return a.commandNew(msg)
	case "/threads":
		all := len(fields) > 1 && fields[1] == "all"
		return a.commandThreads(msg, all)
	case "/interrupt":
		return a.commandInterrupt(msg)
	case "/status":
		return a.commandStatus(msg)
	case "/append":
		if len(fields) < 2 {
			return fmt.Errorf("usage: /append TEXT")
		}
		return a.commandAppend(msg, strings.TrimSpace(strings.TrimPrefix(raw, "/append")))
	case "/workspace":
		return a.commandWorkspace(msg, fields[1:])
	case "/model":
		return a.commandModel(msg, fields[1:])
	default:
		return fmt.Errorf("unknown command: %s", fields[0])
	}
}

func (a *App) commandNew(msg *feishu.InboundMessage) error {
	sessionKey := a.makeSessionKey(msg)
	sess := a.store.GetSession(sessionKey)
	if sess == nil {
		sess = &state.Session{Key: sessionKey, WorkspaceID: a.cfg.Workspaces[0].ID, ChatID: msg.ChatID, ChatType: msg.ChatType, OwnerUserID: msg.UserID}
	}
	if sess.ActiveTurnID != "" {
		return fmt.Errorf("当前任务仍在运行，请先等待结束或中断")
	}
	clearSessionThreadContext(sess)
	sess.ActiveTurnID = ""
	sess.ActiveSubmissionID = ""
	sess.Status = "idle"
	sess.Queue = nil
	if err := a.store.UpsertSession(sess); err != nil {
		return err
	}
	return a.feishu.ReplyText(context.Background(), msg.MessageID, "已切换到新线程模式。下一条消息会创建新线程，当前工作区保持不变。", msg.ChatType == "group" && a.cfg.Feishu.ReplyInThread)
}

func (a *App) commandThreads(msg *feishu.InboundMessage, includeAll bool) error {
	sessionKey := a.makeSessionKey(msg)
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
		slog.Info("thread list query",
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
		slog.Info("thread list query result", "attempt", idx+1, "count", len(result.Data))
		if len(result.Data) > 0 {
			break
		}
	}
	if err != nil && len(result.Data) == 0 {
		return err
	}
	if len(result.Data) == 0 {
		return a.feishu.ReplyText(context.Background(), msg.MessageID, "没有可恢复的线程。", msg.ChatType == "group" && a.cfg.Feishu.ReplyInThread)
	}
	sort.Slice(result.Data, func(i, j int) bool { return result.Data[i].UpdatedAt > result.Data[j].UpdatedAt })
	currentLabel := "-"
	if sess != nil {
		currentLabel = currentThreadLabel(sess)
	}
	lines := make([]string, 0, len(result.Data)+2)
	lines = append(lines, "当前线程: "+currentLabel, "", "最近线程：")
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
				"session_key":    sessionKey,
			},
		})
	}
	body := strings.Join(lines, "\n")
	card := a.feishu.SimpleStatusCard("线程列表", "blue", body, buttons)
	_, err = a.feishu.ReplyCard(context.Background(), msg.MessageID, card, msg.ChatType == "group" && a.cfg.Feishu.ReplyInThread)
	return err
}

func (a *App) commandInterrupt(msg *feishu.InboundMessage) error {
	sessionKey := a.makeSessionKey(msg)
	sess := a.store.GetSession(sessionKey)
	if sess == nil || sess.ActiveTurnID == "" || sess.ActiveThreadID == "" {
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
	return a.feishu.ReplyText(context.Background(), msg.MessageID, "已请求中断当前任务。", msg.ChatType == "group" && a.cfg.Feishu.ReplyInThread)
}

func (a *App) commandStatus(msg *feishu.InboundMessage) error {
	sessionKey := a.makeSessionKey(msg)
	sess := a.store.GetSession(sessionKey)
	if sess == nil {
		return a.feishu.ReplyText(context.Background(), msg.MessageID, "当前会话还没有任务。", msg.ChatType == "group" && a.cfg.Feishu.ReplyInThread)
	}
	return a.feishu.ReplyText(context.Background(), msg.MessageID,
		fmt.Sprintf("状态: %s\n工作区: %s\n线程: %s\n线程ID: %s\n排队: %d", sess.Status, firstNonEmpty(sess.WorkspaceID, a.defaultWorkspaceID()), currentThreadLabel(sess), firstNonEmpty(sess.ActiveThreadID, "-"), len(sess.Queue)),
		msg.ChatType == "group" && a.cfg.Feishu.ReplyInThread)
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
	return fmt.Errorf("usage: /workspace | /workspace list | /workspace new | /workspace use ID")
}

func (a *App) commandModel(msg *feishu.InboundMessage, args []string) error {
	sessionKey := a.makeSessionKey(msg)
	sess := a.store.GetSession(sessionKey)
	if sess == nil {
		sess = &state.Session{Key: sessionKey, ChatID: msg.ChatID, ChatType: msg.ChatType, OwnerUserID: msg.UserID, WorkspaceID: a.cfg.Workspaces[0].ID}
	}
	if len(args) == 0 || args[0] == "list" {
		var result struct {
			Data []struct {
				ID string `json:"id"`
			} `json:"data"`
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := a.codex.Call(ctx, "model/list", map[string]any{"limit": 20, "includeHidden": false}, &result); err != nil {
			return err
		}
		names := make([]string, 0, len(result.Data))
		for _, item := range result.Data {
			names = append(names, "- "+item.ID)
		}
		return a.feishu.ReplyText(context.Background(), msg.MessageID, strings.Join(names, "\n"), msg.ChatType == "group" && a.cfg.Feishu.ReplyInThread)
	}
	if args[0] == "set" && len(args) >= 2 {
		sess.ModelOverride = args[1]
		if err := a.store.UpsertSession(sess); err != nil {
			return err
		}
		return a.feishu.ReplyText(context.Background(), msg.MessageID, "已设置模型为 "+args[1], msg.ChatType == "group" && a.cfg.Feishu.ReplyInThread)
	}
	return fmt.Errorf("usage: /model list | /model set ID")
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

func (a *App) showWorkspaceMenu(msg *feishu.InboundMessage) error {
	sessionKey := a.makeSessionKey(msg)
	sess := a.store.GetSession(sessionKey)
	currentID := a.defaultWorkspaceID()
	if sess != nil && strings.TrimSpace(sess.WorkspaceID) != "" {
		currentID = sess.WorkspaceID
	}
	body := "选择工作区：\n\n当前工作区: `" + currentID + "`"
	buttons := make([]feishu.Button, 0, len(a.cfg.Workspaces)+1)
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
	buttons = append(buttons, feishu.Button{
		Text: "新建工作区",
		Type: "default",
		Value: map[string]any{
			"action":      "workspace.new",
			"session_key": sessionKey,
		},
	})
	card := a.feishu.SimpleStatusCard("工作区", "blue", body, buttons)
	_, err := a.feishu.ReplyCard(context.Background(), msg.MessageID, card, msg.ChatType == "group" && a.cfg.Feishu.ReplyInThread)
	return err
}

func (a *App) beginWorkspaceNew(msg *feishu.InboundMessage) error {
	sessionKey := a.makeSessionKey(msg)
	requestID, err := a.store.NextLocalID("workspace")
	if err != nil {
		return err
	}
	body := "请直接发送一行文本来创建工作区。\n\n格式：\n`workspace_id cwd`\n或\n`workspace_id cwd name`\n\n说明：\n- `name` 是可选的，不用输入方括号\n- 如果不填 name，就默认用 workspace_id 作为显示名\n\n示例：\n`op /home/yuhuan/obfs-sniproxy`\n`op /home/yuhuan/obfs-sniproxy ObfsSniproxy`\n\n发送 `/` 或其它命令前，可先完成本次创建。"
	card := a.feishu.SimpleStatusCard("新建工作区", "orange", body, []feishu.Button{
		{Text: "取消", Type: "default", Value: map[string]any{"action": "pending_form.cancel", "request_id": requestID}},
	})
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
		Model:          a.defaultModel(),
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
