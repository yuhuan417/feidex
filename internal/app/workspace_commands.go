package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

type workspaceSettingOption struct {
	Value string
	Label string
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
		appState := a.appState()
		ws := config.FindWorkspace(a.cfg, args[1])
		if ws == nil {
			return fmt.Errorf("workspace %q not found", args[1])
		}
		sessionKey := a.makeSessionKey(msg)
		sess := appState.session(sessionKey)
		if sess == nil {
			sess = &state.Session{Key: sessionKey, ChatID: msg.ChatID, ChatType: msg.ChatType, OwnerUserID: msg.UserID}
		}
		switchSessionWorkspace(sess, ws.ID)
		if err := appState.saveSession(sess); err != nil {
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

func (a *App) showWorkspaceMenu(msg *feishu.InboundMessage) error {
	card := a.renderWorkspaceMenuCard(a.makeSessionKey(msg))
	_, err := a.feishu.ReplyCard(context.Background(), msg.MessageID, card, msg.ChatType == "group" && a.cfg.Feishu.ReplyInThread)
	return err
}

func (a *App) renderWorkspaceMenuCard(sessionKey string) map[string]any {
	var sess *state.Session
	if a.store != nil {
		sess = a.appState().session(sessionKey)
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
	sess = a.appState().session(sessionKey)
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
		sess = a.appState().session(sessionKey)
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
		sess = a.appState().session(sessionKey)
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
	appState := a.appState()
	sessionKey, _, ws := a.currentWorkspaceForMessage(msg)
	requestID, err := appState.nextLocalID("workspace")
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
	return appState.savePending(&state.PendingRequest{
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
	appState := a.appState()
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
	sess := appState.session(sessionKey)
	if sess == nil {
		sess = &state.Session{Key: sessionKey, ChatID: chatID, ChatType: chatType, OwnerUserID: userID}
	}
	switchSessionWorkspace(sess, id)
	if err := appState.saveSession(sess); err != nil {
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
	appState := a.appState()
	payload := workspaceNewPayloadFromPending(pending)
	parts := strings.Fields(strings.TrimSpace(msg.Text))
	if len(parts) < 1 {
		return fmt.Errorf("格式错误，需发送: workspace_id [name]")
	}
	id := parts[0]
	cwd := strings.TrimSpace(payload.SelectedCWD)
	name := id
	if cwd == "" && len(parts) >= 2 {
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
	_ = appState.updatePending(pending.ID, func(req *state.PendingRequest) { req.Status = "resolved" })
	if pending.FeishuMsgID != "" {
		_ = a.feishu.PatchCard(context.Background(), pending.FeishuMsgID, a.feishu.SimpleStatusCard("工作区已创建", "green", "已创建并切换到工作区 `"+id+"`\n\ncwd: `"+cwd+"`", nil))
	}
	return a.feishu.ReplyText(context.Background(), msg.MessageID, "已创建并切换到工作区 "+id, msg.ChatType == "group" && a.cfg.Feishu.ReplyInThread)
}
