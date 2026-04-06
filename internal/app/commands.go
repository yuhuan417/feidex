package app

import (
	"context"
	"fmt"
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
			return fmt.Errorf("usage: /append <text>")
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
	sess.ActiveThreadID = ""
	sess.ActiveTurnID = ""
	sess.ActiveSubmissionID = ""
	sess.Status = "idle"
	sess.Queue = nil
	if err := a.store.UpsertSession(sess); err != nil {
		return err
	}
	return a.feishu.ReplyText(context.Background(), msg.MessageID, "已切换到新会话。", msg.ChatType == "group" && a.cfg.Feishu.ReplyInThread)
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
	params := map[string]any{
		"limit":    8,
		"cwd":      workspace.Cwd,
		"archived": false,
	}
	if includeAll {
		params["sourceKinds"] = []string{"appServer", "cli", "vscode", "exec"}
	} else {
		params["sourceKinds"] = []string{"appServer"}
	}
	var result codexrpc.ThreadListResult
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := a.codex.Call(ctx, "thread/list", params, &result); err != nil {
		return err
	}
	if len(result.Data) == 0 {
		return a.feishu.ReplyText(context.Background(), msg.MessageID, "没有可恢复的线程。", msg.ChatType == "group" && a.cfg.Feishu.ReplyInThread)
	}
	sort.Slice(result.Data, func(i, j int) bool { return result.Data[i].UpdatedAt > result.Data[j].UpdatedAt })
	body := "选择一个线程恢复："
	buttons := make([]feishu.Button, 0, len(result.Data))
	for _, item := range result.Data {
		label := item.Name
		if strings.TrimSpace(label) == "" {
			label = item.Preview
		}
		if strings.TrimSpace(label) == "" {
			label = item.ID
		}
		if len(label) > 24 {
			label = label[:24] + "..."
		}
		buttons = append(buttons, feishu.Button{
			Text: label,
			Type: "default",
			Value: map[string]any{
				"action":      "thread.resume",
				"thread_id":   item.ID,
				"session_key": sessionKey,
			},
		})
	}
	card := a.feishu.SimpleStatusCard("线程列表", "blue", body, buttons)
	_, err := a.feishu.ReplyCard(context.Background(), msg.MessageID, card, msg.ChatType == "group" && a.cfg.Feishu.ReplyInThread)
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
		fmt.Sprintf("状态: %s\n线程: %s\n排队: %d", sess.Status, firstNonEmpty(sess.ActiveThreadID, "-"), len(sess.Queue)),
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
	if len(args) == 0 || args[0] == "list" {
		lines := make([]string, 0, len(a.cfg.Workspaces))
		for _, ws := range a.cfg.Workspaces {
			lines = append(lines, fmt.Sprintf("- %s: %s", ws.ID, ws.Cwd))
		}
		return a.feishu.ReplyText(context.Background(), msg.MessageID, strings.Join(lines, "\n"), msg.ChatType == "group" && a.cfg.Feishu.ReplyInThread)
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
		sess.WorkspaceID = ws.ID
		if err := a.store.UpsertSession(sess); err != nil {
			return err
		}
		return a.feishu.ReplyText(context.Background(), msg.MessageID, "已切换工作区到 "+ws.ID, msg.ChatType == "group" && a.cfg.Feishu.ReplyInThread)
	}
	return fmt.Errorf("usage: /workspace list | /workspace use <id>")
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
	return fmt.Errorf("usage: /model list | /model set <id>")
}
