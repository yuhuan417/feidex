package app

import (
	"strings"
	"time"

	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

func (s workspaceRenderService) renderWorkspaceNewCard(sessionKey, requestID string, payload workspaceNewPayload) map[string]any {
	if payload.Picker != nil {
		card, err := newWorkspaceRenderService(s.app).renderPathPickerCard(requestID, *payload.Picker)
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
		"可以先选目录，再填写 `workspace_id` 和可选的 `name`。选完目录后会按目录名自动建议 `workspace_id`。点“确认”时才会校验 `workspace_id`。"
	if notice := strings.TrimSpace(payload.Notice); notice != "" {
		body = notice + "\n\n" + body
	}
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
		"elements":           append(append([]map[string]any{}, buttonRows...), workspaceIDInput, workspaceNameInput),
	}
	appendMarkdownBodyCardElement(card, form)
	return card
}

func (s workspaceRenderService) renderWorkspaceCloneCard(sessionKey, requestID string, payload workspaceClonePayload) map[string]any {
	if payload.Picker != nil {
		card, err := newWorkspaceRenderService(s.app).renderPathPickerCard(requestID, *payload.Picker)
		if err == nil {
			return card
		}
		payload.Picker = nil
	}
	var sess *state.Session
	if s.app.store != nil {
		sess = s.app.appState().session(sessionKey)
	}
	workspaceID := defaultWorkspaceID(s.app)
	if sess != nil && strings.TrimSpace(sess.WorkspaceID) != "" {
		workspaceID = sess.WorkspaceID
	}
	ws := config.FindWorkspace(s.app.cfg, workspaceID)
	rootPath := firstNonEmpty(strings.TrimSpace(payload.RootPath), newWorkspaceManagementService(s.app).defaultWorkspaceCloneRoot(ws))
	parentDir := strings.TrimSpace(payload.SelectedParentDir)
	if parentDir == "" {
		parentDir = firstNonEmpty(strings.TrimSpace(newWorkspaceManagementService(s.app).defaultWorkspaceCloneParent(ws)), rootPath)
	}

	card := newMarkdownBodyCard("从仓库创建工作区", "orange")
	body := menuCardBody("workspace.clone",
		"当前工作区: `"+workspaceID+"`\n"+
			"已选父目录: `"+firstNonEmpty(parentDir, "-")+"`\n"+
			"浏览根目录: `"+firstNonEmpty(rootPath, "-")+"`\n\n"+
			"先填写 Git 地址，再按需调整父目录和可选 `workspace_id`。不填 `workspace_id` 时，会从仓库名自动推导。",
	)
	if errText := strings.TrimSpace(payload.ErrorMessage); errText != "" {
		body += "\n\n最近一次创建失败：\n" + errText + "\n\n请修正后重试。"
	}
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
			Text:  "选父目录",
			Type:  "default",
			Name:  "workspace_clone_pickdir",
			Value: map[string]any{"action": "workspace.clone.pickdir", "request_id": requestID},
		},
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
	form := map[string]any{
		"tag":                "form",
		"name":               "workspace_clone_form",
		"direction":          "vertical",
		"horizontal_spacing": "8px",
		"vertical_spacing":   "8px",
		"elements":           append(append([]map[string]any{repoURLInput}, buttonRows...), workspaceIDInput),
	}
	appendMarkdownBodyCardElement(card, form)
	return card
}

func (s workspaceRenderService) renderWorkspaceClonePreparingCard(requestID string, payload workspaceClonePayload, parentDir string, snapshot workspaceCloneProgressSnapshot) map[string]any {
	repoURL := strings.TrimSpace(payload.RepoURL)
	parentDir = firstNonEmpty(strings.TrimSpace(parentDir), strings.TrimSpace(payload.SelectedParentDir), "-")
	workspaceID := strings.TrimSpace(payload.DraftID)
	if workspaceID == "" {
		workspaceID = "将从仓库名自动推导"
	}
	statusLine := "正在从仓库创建工作区。"
	if snapshot.State == "cancelling" {
		statusLine = "正在取消仓库克隆。"
	}
	lines := []string{
		statusLine,
		"",
		"仓库: `" + firstNonEmpty(repoURL, "-") + "`",
		"父目录: `" + parentDir + "`",
		"workspace_id: `" + workspaceID + "`",
	}
	if !snapshot.StartedAt.IsZero() {
		lines = append(lines, "已运行: `"+strings.TrimSpace(strings.TrimPrefix(formatTurnElapsedLine(time.Since(snapshot.StartedAt)), "elapsed: "))+"`")
	}
	if len(snapshot.Lines) == 0 {
		lines = append(lines, "", "尚未收到 git 进度输出。")
	} else {
		lines = append(lines, "", "最近进度:", markdownCodeBlock(strings.Join(snapshot.Lines, "\n")))
	}
	lines = append(lines, "", "这张卡片会自动刷新。")
	var buttons []feishu.Button
	if snapshot.State != "cancelling" {
		buttons = []feishu.Button{
			{
				Text: "取消克隆",
				Type: "default",
				Value: map[string]any{
					"action":     "workspace.clone.cancel",
					"request_id": requestID,
				},
			},
		}
	}
	return s.app.feishu.SimpleStatusCard("从仓库创建工作区", "blue", strings.Join(lines, "\n"), buttons)
}

func (s workspaceRenderService) renderWorkspaceCloneSuccessCard(sessionKey, workspaceID, targetDir string) map[string]any {
	buttons := []feishu.Button{
		{
			Text: "返回工作区管理",
			Type: "default",
			Value: map[string]any{
				"action":      "menu.workspace",
				"session_key": sessionKey,
			},
		},
	}
	body := "已从仓库创建并切换到工作区 `" + workspaceID + "`\n\ncwd: `" + targetDir + "`"
	return s.app.feishu.SimpleStatusCard("工作区已创建", "green", body, buttons)
}

func (s workspaceRenderService) renderWorkspaceSwitchExistingCard(sessionKey, workspaceID, targetDir, notice string) map[string]any {
	body := strings.TrimSpace(notice)
	if body == "" {
		body = "该目录已经由现有工作区接管。"
	}
	body += "\n\n" +
		"目录: `" + firstNonEmpty(strings.TrimSpace(targetDir), "-") + "`\n" +
		"workspace_id: `" + firstNonEmpty(strings.TrimSpace(workspaceID), "-") + "`\n\n" +
		"是否直接切换到这个工作区？"
	buttons := []feishu.Button{
		{
			Text: "切换到该工作区",
			Type: "primary",
			Value: map[string]any{
				"action":       "workspace.use.existing",
				"session_key":  sessionKey,
				"workspace_id": strings.TrimSpace(workspaceID),
			},
		},
		{
			Text: "返回工作区管理",
			Type: "default",
			Value: map[string]any{
				"action":      "menu.workspace",
				"session_key": sessionKey,
			},
		},
	}
	return s.app.feishu.SimpleStatusCard("工作区已存在", "blue", body, buttons)
}

func (s workspaceRenderService) renderWorkspaceCloneSwitchExistingCard(sessionKey, workspaceID, targetDir string) map[string]any {
	return newWorkspaceRenderService(s.app).renderWorkspaceSwitchExistingCard(sessionKey, workspaceID, targetDir, "clone 目标目录已存在，并且已经由现有工作区接管。")
}

func (s workspaceRenderService) renderWorkspaceCloneManualHintCard(sessionKey, workspaceID, targetDir, errText string) map[string]any {
	lines := []string{
		"仓库已拉取，可手动接管。",
		"",
		"目录: `" + firstNonEmpty(strings.TrimSpace(targetDir), "-") + "`",
	}
	if workspaceID = strings.TrimSpace(workspaceID); workspaceID != "" {
		lines = append(lines, "建议 workspace_id: `"+workspaceID+"`")
	}
	lines = append(lines, "", "自动创建或切换工作区失败。仓库目录已保留，可稍后通过 `/workspace new` 手动接管。")
	if errText = strings.TrimSpace(errText); errText != "" {
		lines = append(lines, "", "错误: "+errText)
	}
	buttons := []feishu.Button{
		{
			Text: "返回工作区管理",
			Type: "default",
			Value: map[string]any{
				"action":      "menu.workspace",
				"session_key": sessionKey,
			},
		},
	}
	return s.app.feishu.SimpleStatusCard("仓库已拉取", "orange", strings.Join(lines, "\n"), buttons)
}

func (s workspaceRenderService) renderWorkspaceCloneCanceledCard(sessionKey string, payload workspaceClonePayload, parentDir string, snapshot workspaceCloneProgressSnapshot) map[string]any {
	repoURL := strings.TrimSpace(payload.RepoURL)
	parentDir = firstNonEmpty(strings.TrimSpace(parentDir), strings.TrimSpace(payload.SelectedParentDir), "-")
	workspaceID := strings.TrimSpace(payload.DraftID)
	if workspaceID == "" {
		workspaceID = "将从仓库名自动推导"
	}
	lines := []string{
		"已取消仓库克隆。",
		"",
		"仓库: `" + firstNonEmpty(repoURL, "-") + "`",
		"父目录: `" + parentDir + "`",
		"workspace_id: `" + workspaceID + "`",
		"",
		"如果目标目录有残留，请清理后重新发起。",
	}
	if len(snapshot.Lines) > 0 {
		lines = append(lines, "", "取消前最后进度:", markdownCodeBlock(strings.Join(snapshot.Lines, "\n")))
	}
	buttons := []feishu.Button{
		{
			Text: "返回工作区管理",
			Type: "default",
			Value: map[string]any{
				"action":      "menu.workspace",
				"session_key": sessionKey,
			},
		},
	}
	return s.app.feishu.SimpleStatusCard("仓库克隆已取消", "grey", strings.Join(lines, "\n"), buttons)
}
