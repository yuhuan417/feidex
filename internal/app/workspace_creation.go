package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"

	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

var workspaceGitClone = func(ctx context.Context, repoURL, targetDir string) error {
	cmd := exec.CommandContext(ctx, "git", "clone", repoURL, targetDir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message == "" {
			message = err.Error()
		}
		return fmt.Errorf("git clone failed: %s", message)
	}
	return nil
}

type workspaceNewPayload struct {
	RootPath    string             `json:"root_path"`
	SelectedCWD string             `json:"selected_cwd"`
	DraftID     string             `json:"draft_id,omitempty"`
	DraftName   string             `json:"draft_name,omitempty"`
	Picker      *pathPickerPayload `json:"picker,omitempty"`
}

type workspaceClonePayload struct {
	RepoURL string `json:"repo_url,omitempty"`
	DraftID string `json:"draft_id,omitempty"`
}

func workspaceNewPayloadFromPending(pending *state.PendingRequest) workspaceNewPayload {
	var payload workspaceNewPayload
	if pending != nil && strings.TrimSpace(pending.PayloadJSON) != "" {
		_ = json.Unmarshal([]byte(pending.PayloadJSON), &payload)
	}
	return payload
}

func workspaceClonePayloadFromPending(pending *state.PendingRequest) workspaceClonePayload {
	var payload workspaceClonePayload
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
		"可以先选目录，再填写 `workspace_id` 和可选的 `name`。点“确认”时才会校验 `workspace_id`。"
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
		"required":    false,
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

func (a *App) renderWorkspaceCloneCard(sessionKey, requestID string, payload workspaceClonePayload) map[string]any {
	var sess *state.Session
	if a.store != nil {
		sess = a.appState().session(sessionKey)
	}
	workspaceID := a.defaultWorkspaceID()
	if sess != nil && strings.TrimSpace(sess.WorkspaceID) != "" {
		workspaceID = sess.WorkspaceID
	}
	ws := config.FindWorkspace(a.cfg, workspaceID)
	parentDir := a.defaultWorkspaceCloneParent(ws)

	card := newMarkdownBodyCard("从仓库创建工作区", "orange")
	body := menuCardBody("workspace.clone",
		"当前工作区: `"+workspaceID+"`\n"+
			"目标父目录: `"+parentDir+"`\n\n"+
			"填写 Git 地址；可选填写 `workspace_id`。不填 `workspace_id` 时，会从仓库名自动推导。",
	)
	appendMarkdownBodyCardElement(card, map[string]any{"tag": "markdown", "content": body})

	repoURLInput := map[string]any{
		"tag":         "input",
		"name":        "repo_url",
		"required":    false,
		"placeholder": map[string]any{"tag": "plain_text", "content": "git 地址，例如 https://github.com/org/repo.git"},
	}
	if value := strings.TrimSpace(payload.RepoURL); value != "" {
		repoURLInput["default_value"] = value
	}
	workspaceIDInput := map[string]any{
		"tag":         "input",
		"name":        "workspace_id",
		"required":    false,
		"placeholder": map[string]any{"tag": "plain_text", "content": "workspace_id（可选）"},
	}
	if value := strings.TrimSpace(payload.DraftID); value != "" {
		workspaceIDInput["default_value"] = value
	}
	buttonRows := buildMarkdownBodyCardActionElements([]feishu.Button{
		{
			Text:  "确认",
			Type:  "primary",
			Name:  "workspace_clone_submit",
			Value: map[string]any{"action": "workspace.clone.submit", "request_id": requestID},
		},
		{
			Text:  "取消",
			Type:  "default",
			Name:  "workspace_clone_cancel",
			Value: map[string]any{"action": "pending_form.cancel", "request_id": requestID},
		},
	})
	for _, row := range buttonRows {
		columns := row["columns"].([]map[string]any)
		if len(columns) == 0 {
			continue
		}
		button := columns[0]["elements"].([]map[string]any)[0]
		button["form_action_type"] = "submit"
	}
	form := map[string]any{
		"tag":                "form",
		"name":               "workspace_clone_form",
		"direction":          "vertical",
		"horizontal_spacing": "8px",
		"vertical_spacing":   "8px",
		"elements": append([]map[string]any{
			repoURLInput,
			workspaceIDInput,
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

func mergeWorkspaceCloneFormValues(payload workspaceClonePayload, values map[string]any) workspaceClonePayload {
	if value, ok := formValueString(values, "repo_url"); ok {
		payload.RepoURL = value
	}
	if value, ok := formValueString(values, "workspace_id"); ok {
		payload.DraftID = value
	}
	return payload
}

func workspaceCloneRepoName(repoURL string) (string, error) {
	repoURL = strings.TrimSpace(repoURL)
	if repoURL == "" {
		return "", fmt.Errorf("git 地址不能为空")
	}
	pathPart := repoURL
	if !strings.Contains(repoURL, "://") {
		if idx := strings.Index(repoURL, ":"); idx > 0 && !strings.Contains(repoURL[:idx], "/") && strings.Contains(repoURL[idx+1:], "/") {
			pathPart = repoURL[idx+1:]
		}
	} else if parsed, err := url.Parse(repoURL); err == nil {
		pathPart = firstNonEmpty(strings.TrimSpace(parsed.Path), strings.TrimSpace(parsed.Opaque))
	}
	base := strings.TrimSpace(strings.TrimSuffix(path.Base(strings.TrimSuffix(pathPart, "/")), ".git"))
	if base == "" || base == "." || base == "/" {
		return "", fmt.Errorf("无法从 git 地址推导仓库名")
	}
	return base, nil
}

func workspaceCloneDefaultID(repoName string) string {
	repoName = strings.ToLower(strings.TrimSpace(repoName))
	var out strings.Builder
	lastDash := false
	for _, r := range repoName {
		switch {
		case r >= 'a' && r <= 'z':
			out.WriteRune(r)
			lastDash = false
		case r >= '0' && r <= '9':
			out.WriteRune(r)
			lastDash = false
		case r == '-' || r == '_':
			if out.Len() > 0 && !lastDash {
				out.WriteByte('-')
				lastDash = true
			}
		default:
			if out.Len() > 0 && !lastDash {
				out.WriteByte('-')
				lastDash = true
			}
		}
	}
	return strings.Trim(out.String(), "-")
}

func (a *App) defaultWorkspaceCloneParent(ws *config.Workspace) string {
	if ws != nil && strings.TrimSpace(ws.Cwd) != "" {
		return filepath.Dir(strings.TrimSpace(ws.Cwd))
	}
	if strings.TrimSpace(a.cfgPath) != "" {
		return filepath.Dir(strings.TrimSpace(a.cfgPath))
	}
	return "."
}

func (a *App) cloneWorkspaceAndSwitch(msg *feishu.InboundMessage, repoURL, explicitID string) error {
	if msg == nil {
		return nil
	}
	repoName, err := workspaceCloneRepoName(repoURL)
	if err != nil {
		return err
	}
	workspaceID := strings.TrimSpace(explicitID)
	if workspaceID == "" {
		workspaceID = workspaceCloneDefaultID(repoName)
		if workspaceID == "" {
			return fmt.Errorf("无法从 git 地址推导 workspace_id，请手动指定")
		}
	}
	if config.FindWorkspace(a.cfg, workspaceID) != nil {
		return fmt.Errorf("workspace %q 已存在，请指定新的 workspace_id", workspaceID)
	}

	sessionKey, _, ws := a.currentWorkspaceForMessage(msg)
	targetName := repoName
	if strings.TrimSpace(explicitID) != "" {
		targetName = workspaceID
	}
	targetDir := filepath.Join(a.defaultWorkspaceCloneParent(ws), targetName)
	if _, statErr := os.Stat(targetDir); statErr == nil {
		return fmt.Errorf("目标目录已存在: %s", targetDir)
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return statErr
	}
	if err := os.MkdirAll(filepath.Dir(targetDir), 0o755); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := workspaceGitClone(ctx, strings.TrimSpace(repoURL), targetDir); err != nil {
		return err
	}
	if err := a.createWorkspaceAndSwitch(sessionKey, msg.UserID, msg.ChatID, msg.ChatType, workspaceID, workspaceID, targetDir); err != nil {
		return fmt.Errorf("仓库已拉取到 %q，但创建工作区失败: %w", targetDir, err)
	}
	reply := "已从仓库创建并切换到工作区 " + workspaceID + "\n" + "cwd: " + targetDir
	return a.feishu.ReplyText(context.Background(), msg.MessageID, reply, msg.ChatType == "group" && a.cfg.Feishu.ReplyInThread)
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
