package workspacecmd

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"feidex/internal/app/appcore"
	appbackend "feidex/internal/app/backend"
	"feidex/internal/app/cardactions"
	appcards "feidex/internal/app/cards"
	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

// RenderWorkspaceNewCard renders the "new workspace" card.
func (s *RenderService) RenderWorkspaceNewCard(sessionKey, requestID string, payload NewPayload) map[string]any {
	if payload.Picker != nil {
		card, err := s.RenderPathPickerCard(requestID, *payload.Picker)
		if err == nil {
			return card
		}
		payload.Picker = nil
	}
	selectedCWD := strings.TrimSpace(payload.SelectedCWD)
	if selectedCWD == "" {
		selectedCWD = payload.RootPath
	}
	card := appcards.NewMarkdownBodyCard("新建工作区", "orange")
	body := "当前位置：主菜单 / workspace / new\n\n" +
		"已选目录: `" + appcore.FirstNonEmpty(selectedCWD, "-") + "`\n" +
		"浏览根目录: `" + appcore.FirstNonEmpty(strings.TrimSpace(payload.RootPath), "-") + "`\n\n" +
		"可以先选目录，再填写 `workspace_id` 和可选的 `name`。选完目录后会按目录名自动建议 `workspace_id`。点「确认」时才会校验 `workspace_id`。"
	if notice := strings.TrimSpace(payload.Notice); notice != "" {
		body = notice + "\n\n" + body
	}
	appcards.AppendMarkdownBodyCardElement(card, map[string]any{"tag": "markdown", "content": body})
	buttonRows := appcards.BuildMarkdownBodyCardActionElements([]feishu.Button{
		{
			Text:  "选目录",
			Type:  "default",
			Name:  "workspace_new_pickdir",
			Value: cardactions.RequestActionValue{Action: "workspace.new.pickdir", RequestID: requestID}.Map(),
		},
		{
			Text:  "确认",
			Type:  "primary",
			Name:  "workspace_new_submit",
			Value: cardactions.RequestActionValue{Action: "workspace.new.submit", RequestID: requestID}.Map(),
		},
		{
			Text:  "取消",
			Type:  "default",
			Name:  "workspace_new_cancel",
			Value: cardactions.RequestActionValue{Action: "pending_form.cancel", RequestID: requestID}.Map(),
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
	appcards.AppendMarkdownBodyCardElement(card, form)
	return card
}

// RenderWorkspaceCloneCard renders the "clone workspace" card.
func (s *RenderService) RenderWorkspaceCloneCard(sessionKey, requestID string, payload ClonePayload) map[string]any {
	if payload.Picker != nil {
		card, err := s.RenderPathPickerCard(requestID, *payload.Picker)
		if err == nil {
			return card
		}
		payload.Picker = nil
	}
	var sess *state.Session
	sess = s.GetSession(sessionKey)
	workspaceID := selectedWorkspaceIDForSession(s.App, sess)
	ws := config.FindWorkspace(s.App.Config(), workspaceID)
	rootPath := appcore.FirstNonEmpty(strings.TrimSpace(payload.RootPath), s.DefaultWorkspaceCloneRoot(ws))
	parentDir := strings.TrimSpace(payload.SelectedParentDir)
	if parentDir == "" {
		parentDir = appcore.FirstNonEmpty(strings.TrimSpace(s.DefaultWorkspaceCloneParent(ws)), rootPath)
	}

	card := appcards.NewMarkdownBodyCard("从仓库创建工作区", "orange")
	body := "当前工作区: `" + workspaceID + "`\n" +
		"已选父目录: `" + appcore.FirstNonEmpty(parentDir, "-") + "`\n" +
		"浏览根目录: `" + appcore.FirstNonEmpty(rootPath, "-") + "`\n\n" +
		"先填写 Git 地址，再按需调整父目录和可选 `workspace_id`。不填 `workspace_id` 时，会从仓库名自动推导。"
	body = s.FormatMenuBody("workspace.clone", body)
	if errText := strings.TrimSpace(payload.ErrorMessage); errText != "" {
		body += "\n\n最近一次创建失败：\n" + errText + "\n\n请修正后重试。"
	}
	appcards.AppendMarkdownBodyCardElement(card, map[string]any{"tag": "markdown", "content": body})

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
	buttonRows := appcards.BuildMarkdownBodyCardActionElements([]feishu.Button{
		{
			Text:  "选父目录",
			Type:  "default",
			Name:  "workspace_clone_pickdir",
			Value: cardactions.RequestActionValue{Action: "workspace.clone.pickdir", RequestID: requestID}.Map(),
		},
		{
			Text:  "确认",
			Type:  "primary",
			Name:  "workspace_clone_submit",
			Value: cardactions.RequestActionValue{Action: "workspace.clone.submit", RequestID: requestID}.Map(),
		},
		{
			Text:  "取消",
			Type:  "default",
			Name:  "workspace_clone_cancel",
			Value: cardactions.RequestActionValue{Action: "pending_form.cancel", RequestID: requestID}.Map(),
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
	appcards.AppendMarkdownBodyCardElement(card, form)
	return card
}

// RenderWorkspaceClonePreparingCard renders the clone in-progress card.
func (s *RenderService) RenderWorkspaceClonePreparingCard(requestID string, payload ClonePayload, parentDir string, snapshot CloneProgressSnapshot) map[string]any {
	repoURL := strings.TrimSpace(payload.RepoURL)
	parentDir = appcore.FirstNonEmpty(strings.TrimSpace(parentDir), strings.TrimSpace(payload.SelectedParentDir), "-")
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
		"仓库: `" + appcore.FirstNonEmpty(repoURL, "-") + "`",
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
				Text:  "取消克隆",
				Type:  "default",
				Value: cardactions.RequestActionValue{Action: "workspace.clone.cancel", RequestID: requestID}.Map(),
			},
		}
	}
	return s.App.Feishu().SimpleStatusCard("从仓库创建工作区", "blue", strings.Join(lines, "\n"), buttons)
}

// RenderWorkspaceCloneSuccessCard renders the clone success card.
func (s *RenderService) RenderWorkspaceCloneSuccessCard(sessionKey, workspaceID, targetDir string) map[string]any {
	buttons := []feishu.Button{
		{
			Text:  "返回工作区管理",
			Type:  "default",
			Value: cardactions.MenuActionValue{Action: "menu.workspace", SessionKey: sessionKey}.Map(),
		},
	}
	body := "已从仓库创建并切换到工作区 `" + workspaceID + "`\n\ncwd: `" + targetDir + "`"
	return s.App.Feishu().SimpleStatusCard("工作区已创建", "green", body, buttons)
}

// RenderWorkspaceSwitchExistingCard renders the "workspace already exists" card.
func (s *RenderService) RenderWorkspaceSwitchExistingCard(sessionKey, workspaceID, targetDir, notice string) map[string]any {
	body := strings.TrimSpace(notice)
	if body == "" {
		body = "该目录已经由现有工作区接管。"
	}
	body += "\n\n" +
		"目录: `" + appcore.FirstNonEmpty(strings.TrimSpace(targetDir), "-") + "`\n" +
		"workspace_id: `" + appcore.FirstNonEmpty(strings.TrimSpace(workspaceID), "-") + "`\n\n" +
		"是否直接切换到这个工作区？"
	buttons := []feishu.Button{
		{
			Text: "切换到该工作区",
			Type: "primary",
			Value: cardactions.WorkspaceActionValue{
				Action:      "workspace.use.existing",
				SessionKey:  sessionKey,
				WorkspaceID: strings.TrimSpace(workspaceID),
			}.Map(),
		},
		{
			Text:  "返回工作区管理",
			Type:  "default",
			Value: cardactions.MenuActionValue{Action: "menu.workspace", SessionKey: sessionKey}.Map(),
		},
	}
	return s.App.Feishu().SimpleStatusCard("工作区已存在", "blue", body, buttons)
}

// RenderWorkspaceCloneSwitchExistingCard renders the clone "target already exists" card.
func (s *RenderService) RenderWorkspaceCloneSwitchExistingCard(sessionKey, workspaceID, targetDir string) map[string]any {
	return s.RenderWorkspaceSwitchExistingCard(sessionKey, workspaceID, targetDir, "clone 目标目录已存在，并且已经由现有工作区接管。")
}

// RenderWorkspaceCloneManualHintCard renders the clone manual takeover hint card.
func (s *RenderService) RenderWorkspaceCloneManualHintCard(sessionKey, workspaceID, targetDir, errText string) map[string]any {
	lines := []string{
		"仓库已拉取，可手动接管。",
		"",
		"目录: `" + appcore.FirstNonEmpty(strings.TrimSpace(targetDir), "-") + "`",
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
			Text:  "返回工作区管理",
			Type:  "default",
			Value: cardactions.MenuActionValue{Action: "menu.workspace", SessionKey: sessionKey}.Map(),
		},
	}
	return s.App.Feishu().SimpleStatusCard("仓库已拉取", "orange", strings.Join(lines, "\n"), buttons)
}

// RenderWorkspaceCloneCanceledCard renders the clone canceled card.
func (s *RenderService) RenderWorkspaceCloneCanceledCard(sessionKey string, payload ClonePayload, parentDir string, snapshot CloneProgressSnapshot) map[string]any {
	repoURL := strings.TrimSpace(payload.RepoURL)
	parentDir = appcore.FirstNonEmpty(strings.TrimSpace(parentDir), strings.TrimSpace(payload.SelectedParentDir), "-")
	workspaceID := strings.TrimSpace(payload.DraftID)
	if workspaceID == "" {
		workspaceID = "将从仓库名自动推导"
	}
	lines := []string{
		"已取消仓库克隆。",
		"",
		"仓库: `" + appcore.FirstNonEmpty(repoURL, "-") + "`",
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
	return s.App.Feishu().SimpleStatusCard("仓库克隆已取消", "grey", strings.Join(lines, "\n"), buttons)
}

// RenderWorkspaceMenuCard renders the workspace management menu card.
func (s *RenderService) RenderWorkspaceMenuCard(sessionKey string) map[string]any {
	var sess *state.Session
	sess = s.GetSession(sessionKey)
	currentID := selectedWorkspaceIDForSession(s.App, sess)
	currentWS := config.FindWorkspace(s.App.Config(), currentID)
	bodyLines := []string{"当前工作区: `" + currentID + "`"}
	bodyLines = s.BackendWorkspaceSummaryLines(bodyLines, currentWS)
	buttons := make([]feishu.Button, 0, 6)
	workspaces := s.App.Config().Workspaces
	selectOptions := make([]appcards.SelectStaticOption, 0, len(workspaces))
	for _, ws := range workspaces {
		label := ws.ID
		if ws.ID == currentID {
			label = "当前 · " + ws.ID
		}
		selectOptions = append(selectOptions, appcards.SelectStaticOption{
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
			Text: submenuCommandLabel("从仓库创建", "/workspace clone"),
			Type: "default",
			Value: map[string]any{
				"action":      "workspace.clone",
				"session_key": sessionKey,
			},
		},
	)
	buttons = append(buttons, s.BackendWorkspaceConfigButtons(sessionKey)...)
	buttons = append(buttons,
		feishu.Button{
			Text: submenuCommandLabel("删除工作区", "/workspace delete"),
			Type: "default",
			Value: map[string]any{
				"action":      "workspace.delete.menu",
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
	card := appcards.NewMarkdownBodyCard("工作区管理", "blue")
	body := strings.Join(bodyLines, "\n")
	body = s.FormatMenuBody("menu.workspace", body)
	appcards.AppendMarkdownBodyCardElement(card, map[string]any{"tag": "markdown", "content": body})
	appcards.AppendMarkdownBodyCardElement(card, appcards.BuildSelectStaticElement(
		"workspace_select",
		"list",
		map[string]any{"action": "workspace.use.select", "session_key": sessionKey},
		selectOptions,
		currentID,
	))
	for _, row := range appcards.BuildMarkdownBodyCardActionElements(buttons) {
		appcards.AppendMarkdownBodyCardElement(card, row)
	}
	return card
}

// RenderWorkspaceChooseCard renders the workspace choose card with buttons sorted by recently used.
func (s *RenderService) RenderWorkspaceChooseCard(sessionKey string) map[string]any {
	var sess *state.Session
	sess = s.GetSession(sessionKey)
	currentID := selectedWorkspaceIDForSession(s.App, sess)
	var recentIDs []string
	if sess != nil {
		if selectionKey := appcore.MakeWorkspaceSelectionKeyForSession(s.App, sess); selectionKey != "" {
			if selectionSess := s.GetSession(selectionKey); selectionSess != nil {
				recentIDs = selectionSess.RecentWorkspaceIDs
			}
		}
		if len(recentIDs) == 0 {
			recentIDs = sess.RecentWorkspaceIDs
		}
	}

	workspaces := s.App.Config().Workspaces
	sorted := sortWorkspacesByRecent(workspaces, recentIDs, currentID)

	card := appcards.NewMarkdownBodyCard("选择工作区", "blue")
	buttons := make([]feishu.Button, 0, len(sorted))
	for _, ws := range sorted {
		label := ws.ID
		if ws.ID == currentID {
			label = ws.ID + " (当前)"
		}
		btnType := "default"
		if ws.ID == currentID {
			btnType = "primary"
		}
		buttons = append(buttons, feishu.Button{
			Text: label,
			Type: btnType,
			Value: map[string]any{
				"action":       "workspace.use.existing",
				"session_key":  sessionKey,
				"workspace_id": ws.ID,
			},
		})
	}
	for _, row := range appcards.BuildMarkdownBodyCardActionElements(buttons) {
		appcards.AppendMarkdownBodyCardElement(card, row)
	}
	return card
}

func sortWorkspacesByRecent(workspaces []config.Workspace, recentIDs []string, currentID string) []config.Workspace {
	if len(workspaces) == 0 {
		return nil
	}
	rank := make(map[string]int, len(recentIDs))
	for i, id := range recentIDs {
		if _, exists := rank[id]; !exists {
			rank[id] = i
		}
	}
	sorted := make([]config.Workspace, len(workspaces))
	copy(sorted, workspaces)
	sort.SliceStable(sorted, func(i, j int) bool {
		ri, iok := rank[sorted[i].ID]
		rj, jok := rank[sorted[j].ID]
		if iok && jok {
			return ri < rj
		}
		if iok {
			return true
		}
		if jok {
			return false
		}
		return sorted[i].ID < sorted[j].ID
	})
	return sorted
}

// RenderWorkspaceSandboxMenuCard renders the sandbox configuration menu card.
func (s *RenderService) RenderWorkspaceSandboxMenuCard(sessionKey string) (map[string]any, error) {
	return appbackend.DriverForApp(s.App).Permission().RenderWorkspaceSandboxMenu(sessionKey, appbackend.WorkspacePermissionRenderDeps{
		App:            s.App,
		FormatMenuBody: s.FormatMenuBody,
	})
}

// RenderWorkspacePolicyMenuCard renders the policy configuration menu card.
func (s *RenderService) RenderWorkspacePolicyMenuCard(sessionKey string) (map[string]any, error) {
	return appbackend.DriverForApp(s.App).Permission().RenderWorkspacePolicyMenu(sessionKey, appbackend.WorkspacePermissionRenderDeps{
		App:            s.App,
		FormatMenuBody: s.FormatMenuBody,
	})
}

// RenderWorkspaceDeleteMenuCard renders the workspace delete menu card.
func (s *RenderService) RenderWorkspaceDeleteMenuCard(sessionKey string) (map[string]any, error) {
	currentID := selectedWorkspaceIDForSession(s.App, s.GetSession(sessionKey))
	workspaces := s.App.Config().Workspaces
	lines := []string{
		"删除 workspace 只会移除配置，不会删除磁盘目录。",
		"",
		"当前工作区: `" + currentID + "`",
		"当前工作区不可删除，请先切换到其他工作区。",
	}
	deleteOptions := make([]appcards.SelectStaticOption, 0, len(workspaces))
	for _, ws := range workspaces {
		if strings.TrimSpace(ws.ID) == "" || ws.ID == currentID {
			continue
		}
		label := ws.ID
		if name := strings.TrimSpace(ws.Name); name != "" && name != ws.ID {
			label = name + " · " + ws.ID
		}
		label += " · " + strings.TrimSpace(ws.Cwd)
		deleteOptions = append(deleteOptions, appcards.SelectStaticOption{
			Text:  label,
			Value: ws.ID,
		})
	}
	if len(deleteOptions) == 0 {
		lines = append(lines, "", "当前没有可删除的其他工作区。")
	}
	card := appcards.NewMarkdownBodyCard("删除工作区", "orange")
	body := strings.Join(lines, "\n")
	body = s.FormatMenuBody("workspace.delete.menu", body)
	appcards.AppendMarkdownBodyCardElement(card, map[string]any{
		"tag":     "markdown",
		"content": body,
	})
	if len(deleteOptions) > 0 {
		appcards.AppendMarkdownBodyCardElement(card, appcards.BuildSelectStaticElement(
			"workspace_delete_select",
			"选择要删除的 workspace",
			map[string]any{"action": "workspace.delete.prompt", "session_key": sessionKey},
			deleteOptions,
			"",
		))
	}
	for _, row := range appcards.BuildMarkdownBodyCardActionElements([]feishu.Button{
		{
			Text: commandLabel("返回工作区", "/workspace"),
			Type: "default",
			Value: map[string]any{
				"action":      "menu.workspace",
				"session_key": sessionKey,
			},
		},
	}) {
		appcards.AppendMarkdownBodyCardElement(card, row)
	}
	return card, nil
}

// RenderWorkspaceDeleteConfirmCard renders the workspace delete confirmation card.
func (s *RenderService) RenderWorkspaceDeleteConfirmCard(sessionKey, workspaceID string) (map[string]any, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	ws := config.FindWorkspace(s.App.Config(), workspaceID)
	if ws == nil {
		return nil, fmt.Errorf("workspace %q 不存在", workspaceID)
	}
	body := []string{
		"即将删除工作区配置：`" + workspaceID + "`",
		"",
		"name: `" + appcore.FirstNonEmpty(strings.TrimSpace(ws.Name), workspaceID) + "`",
		"cwd: `" + strings.TrimSpace(ws.Cwd) + "`",
		"",
		"这只会删除配置项，不会删除磁盘目录。",
		"其他空闲 session 如果还引用这个 workspace，会自动切到剩余 workspace 并清空 thread 绑定。",
	}
	buttons := []feishu.Button{
		{
			Text: "确认删除",
			Type: "primary",
			Value: map[string]any{
				"action":       "workspace.delete.confirm",
				"session_key":  sessionKey,
				"workspace_id": workspaceID,
			},
		},
		{
			Text: "返回删除菜单",
			Type: "default",
			Value: map[string]any{
				"action":      "workspace.delete.menu",
				"session_key": sessionKey,
			},
		},
	}
	bodyText := strings.Join(body, "\n")
	bodyText = s.FormatMenuBody("workspace.delete.confirm", bodyText)
	return s.App.Feishu().SimpleStatusCard("确认删除工作区", "red", bodyText, buttons), nil
}
