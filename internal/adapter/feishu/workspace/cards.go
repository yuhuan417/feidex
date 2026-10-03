package workspace

import (
	"strings"
	"time"

	"feidex/internal/adapter/feishu/cardactions"
	appcards "feidex/internal/adapter/feishu/cards"
	appselection "feidex/internal/application/workspace"
	domain "feidex/internal/domain/workspace"
	"feidex/internal/feishu"
)

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

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
		"已选目录: `" + firstNonEmpty(selectedCWD, "-") + "`\n" +
		"浏览根目录: `" + firstNonEmpty(strings.TrimSpace(payload.RootPath), "-") + "`\n\n" +
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
func (s *RenderService) RenderWorkspaceCloneCard(view appselection.View, sessionKey, requestID string, payload ClonePayload) map[string]any {
	if payload.Picker != nil {
		card, err := s.RenderPathPickerCard(requestID, *payload.Picker)
		if err == nil {
			return card
		}
		payload.Picker = nil
	}
	workspaceID := view.CurrentID
	rootPath := firstNonEmpty(strings.TrimSpace(payload.RootPath), view.CloneRoot)
	parentDir := strings.TrimSpace(payload.SelectedParentDir)
	if parentDir == "" {
		parentDir = firstNonEmpty(strings.TrimSpace(view.CloneParent), rootPath)
	}
	cloneMode := NormalizeCloneMode(payload.CloneMode)
	workspaceLabel := "(未配置)"
	if strings.TrimSpace(workspaceID) != "" {
		workspaceLabel = "`" + workspaceID + "`"
	}

	card := appcards.NewMarkdownBodyCard("从仓库创建工作区", "orange")
	body := "当前工作区: " + workspaceLabel + "\n" +
		"已选父目录: `" + firstNonEmpty(parentDir, "-") + "`\n" +
		"创建方式: `" + cloneMode + "`\n" +
		"浏览根目录: `" + firstNonEmpty(rootPath, "-") + "`\n\n" +
		"先填写 Git 地址，再按需调整父目录和 `workspace_id`；`workspace_id` 留空会按仓库名自动推导。选择 `clone 后创建 worktree` 后，点「更新表单」会显示 worktree 分支、workspace_id 和目录名；这些字段留空会按 bot 显示名 + base project 自动推导，目录名默认等于 worktree workspace_id。"
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
		"placeholder": map[string]any{"tag": "plain_text", "content": "workspace_id（留空按仓库名推导；worktree 时作底座目录名）"},
	}
	if value := strings.TrimSpace(payload.DraftID); value != "" {
		workspaceIDInput["default_value"] = value
	}
	modeSelect := appcards.BuildFormSelectStaticElement("clone_mode", "创建方式", []appcards.SelectStaticOption{
		{Text: "普通 clone 工作区", Value: CloneModeWorkspace},
		{Text: "clone 后创建 worktree", Value: CloneModeWorktree},
	}, cloneMode)
	worktreeBranchInput := map[string]any{
		"tag":         "input",
		"name":        "worktree_branch_name",
		"required":    false,
		"placeholder": map[string]any{"tag": "plain_text", "content": "worktree 分支（留空按 bot 名 + 项目名推导）"},
	}
	if value := strings.TrimSpace(payload.WorktreeBranchName); value != "" {
		worktreeBranchInput["default_value"] = value
	}
	worktreeWorkspaceIDInput := map[string]any{
		"tag":         "input",
		"name":        "worktree_workspace_id",
		"required":    false,
		"placeholder": map[string]any{"tag": "plain_text", "content": "worktree workspace_id（留空按 bot 名 + 项目名推导）"},
	}
	if value := strings.TrimSpace(payload.WorktreeWorkspaceID); value != "" {
		worktreeWorkspaceIDInput["default_value"] = value
	}
	worktreeDirectoryNameInput := map[string]any{
		"tag":         "input",
		"name":        "worktree_directory_name",
		"required":    false,
		"placeholder": map[string]any{"tag": "plain_text", "content": "worktree 目录名（留空默认等于 worktree workspace_id）"},
	}
	if value := strings.TrimSpace(payload.WorktreeDirectoryName); value != "" {
		worktreeDirectoryNameInput["default_value"] = value
	}
	buttonRows := appcards.BuildMarkdownBodyCardActionElements([]feishu.Button{
		{
			Text:  "选父目录",
			Type:  "default",
			Name:  "workspace_clone_pickdir",
			Value: cardactions.RequestActionValue{Action: "workspace.clone.pickdir", RequestID: requestID}.Map(),
		},
		{
			Text:  "更新表单",
			Type:  "default",
			Name:  "workspace_clone_refresh",
			Value: cardactions.RequestActionValue{Action: "workspace.clone.refresh", RequestID: requestID}.Map(),
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
		if idx < 3 {
			button["form_action_type"] = "submit"
		}
	}
	formElements := append(append([]map[string]any{repoURLInput, modeSelect}, buttonRows...), workspaceIDInput)
	if cloneMode == CloneModeWorktree {
		formElements = append(formElements, worktreeBranchInput, worktreeWorkspaceIDInput, worktreeDirectoryNameInput)
	}
	form := map[string]any{
		"tag":                "form",
		"name":               "workspace_clone_form",
		"direction":          "vertical",
		"horizontal_spacing": "8px",
		"vertical_spacing":   "8px",
		"elements":           formElements,
	}
	appcards.AppendMarkdownBodyCardElement(card, form)
	return card
}

// RenderWorkspaceClonePreparingCard renders the clone in-progress card.
func (s *RenderService) RenderWorkspaceClonePreparingCard(requestID string, payload ClonePayload, parentDir string, snapshot CloneProgressSnapshot) map[string]any {
	repoURL := strings.TrimSpace(payload.RepoURL)
	parentDir = firstNonEmpty(strings.TrimSpace(parentDir), strings.TrimSpace(payload.SelectedParentDir), "-")
	workspaceID := strings.TrimSpace(payload.DraftID)
	if workspaceID == "" {
		workspaceID = "将从仓库名自动推导"
	}
	mode := NormalizeCloneMode(payload.CloneMode)
	statusLine := "正在从仓库创建工作区。"
	if snapshot.State == "cancelling" {
		statusLine = "正在取消仓库克隆。"
	} else if mode == CloneModeWorktree {
		statusLine = "正在 clone 仓库并创建 Worktree 工作区。"
	}
	lines := []string{
		statusLine,
		"",
		"仓库: `" + firstNonEmpty(repoURL, "-") + "`",
		"父目录: `" + parentDir + "`",
		"创建方式: `" + mode + "`",
		"workspace_id: `" + workspaceID + "`",
	}
	if mode == CloneModeWorktree {
		lines = append(lines,
			"worktree 分支: `"+firstNonEmpty(strings.TrimSpace(payload.WorktreeBranchName), "自动推导")+"`",
			"worktree workspace_id: `"+firstNonEmpty(strings.TrimSpace(payload.WorktreeWorkspaceID), "自动推导")+"`",
			"worktree 目标目录: `"+firstNonEmpty(strings.TrimSpace(payload.WorktreeTargetDir), "自动推导")+"`",
		)
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
	return feishu.SimpleStatusCard("从仓库创建工作区", "blue", strings.Join(lines, "\n"), buttons)
}

// RenderWorkspaceCloneSuccessCard renders the clone success card.
func (s *RenderService) RenderWorkspaceCloneSuccessCard(sessionKey, workspaceID, targetDir string) map[string]any {
	buttons := []feishu.Button{
		{
			Text:  feishu.MenuBackButtonText,
			Type:  "default",
			Value: cardactions.MenuActionValue{Action: "menu.workspace", SessionKey: sessionKey}.Map(),
		},
	}
	body := "已从仓库创建并切换到工作区 `" + workspaceID + "`\n\ncwd: `" + targetDir + "`"
	return feishu.SimpleStatusCard("工作区已创建", "green", body, buttons)
}

// RenderWorkspaceSwitchExistingCard renders the "workspace already exists" card.
func (s *RenderService) RenderWorkspaceSwitchExistingCard(sessionKey, workspaceID, targetDir, notice string) map[string]any {
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
			Value: cardactions.WorkspaceActionValue{
				Action:      "workspace.use.existing",
				SessionKey:  sessionKey,
				WorkspaceID: strings.TrimSpace(workspaceID),
			}.Map(),
		},
		{
			Text:  feishu.MenuBackButtonText,
			Type:  "default",
			Value: cardactions.MenuActionValue{Action: "menu.workspace", SessionKey: sessionKey}.Map(),
		},
	}
	return feishu.SimpleStatusCard("工作区已存在", "blue", body, buttons)
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
			Text:  feishu.MenuBackButtonText,
			Type:  "default",
			Value: cardactions.MenuActionValue{Action: "menu.workspace", SessionKey: sessionKey}.Map(),
		},
	}
	return feishu.SimpleStatusCard("仓库已拉取", "orange", strings.Join(lines, "\n"), buttons)
}

// RenderWorkspaceCloneCanceledCard renders the clone canceled card.
func (s *RenderService) RenderWorkspaceCloneCanceledCard(sessionKey string, payload ClonePayload, parentDir string, snapshot CloneProgressSnapshot) map[string]any {
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
		"创建方式: `" + NormalizeCloneMode(payload.CloneMode) + "`",
		"workspace_id: `" + workspaceID + "`",
		"",
		"如果目标目录有残留，请清理后重新发起。",
	}
	if len(snapshot.Lines) > 0 {
		lines = append(lines, "", "取消前最后进度:", markdownCodeBlock(strings.Join(snapshot.Lines, "\n")))
	}
	buttons := []feishu.Button{
		{
			Text: feishu.MenuBackButtonText,
			Type: "default",
			Value: map[string]any{
				"action":      "menu.workspace",
				"session_key": sessionKey,
			},
		},
	}
	return feishu.SimpleStatusCard("仓库克隆已取消", "grey", strings.Join(lines, "\n"), buttons)
}

// RenderWorkspaceWorktreeCard renders the git worktree workspace card.
func (s *RenderService) RenderWorkspaceWorktreeCard(view appselection.View, sessionKey, requestID string, payload WorktreePayload) map[string]any {
	baseWorkspaceID := strings.TrimSpace(payload.BaseWorkspaceID)
	branchName := strings.TrimSpace(payload.BranchName)
	workspaceID := strings.TrimSpace(payload.WorkspaceID)
	directoryName := strings.TrimSpace(payload.DirectoryName)
	targetDir := strings.TrimSpace(payload.TargetDir)

	workspaces := view.Workspaces
	baseOptions := make([]appcards.SelectStaticOption, 0, len(workspaces)+1)
	seenBase := false
	for _, ws := range workspaces {
		id := strings.TrimSpace(ws.ID)
		if id == "" {
			continue
		}
		label := id
		if id == baseWorkspaceID {
			label = "当前 · " + label
			seenBase = true
		}
		if cwd := strings.TrimSpace(ws.Cwd); cwd != "" {
			label += " · " + cwd
		}
		baseOptions = append(baseOptions, appcards.SelectStaticOption{Text: label, Value: id})
	}
	if baseWorkspaceID != "" && !seenBase {
		baseOptions = append(baseOptions, appcards.SelectStaticOption{Text: baseWorkspaceID, Value: baseWorkspaceID})
	}

	card := appcards.NewMarkdownBodyCard("从 Worktree 创建工作区", "orange")
	body := strings.Join([]string{
		"基准工作区: `" + firstNonEmpty(baseWorkspaceID, "-") + "`",
		"新分支: `" + firstNonEmpty(branchName, "-") + "`",
		"workspace_id: `" + firstNonEmpty(workspaceID, "-") + "`",
		"目录名: `" + firstNonEmpty(directoryName, "-") + "`",
		"目标目录: `" + firstNonEmpty(targetDir, "确认时从基准 Git 根目录推导") + "`",
		"",
		"默认会用 bot 显示名和基准项目名生成分支、workspace_id 和目录名。通常只需要确认；需要隔离到其他基准仓库时再调整下拉。",
		"",
		"下面三个输入框对应:",
		"- 新分支 / branch_name: 要创建的 Git branch，提交时会传给 `git worktree add -b`。",
		"- 工作区 ID / workspace_id: Feidex 里显示和切换用的新工作区 ID。",
		"- 目录名 / directory_name: 实际创建的 worktree 目录名，默认等于 workspace_id，位置在基准仓库同级目录。",
	}, "\n")
	body = s.FormatMenuBody("workspace.worktree", body)
	if errText := strings.TrimSpace(payload.ErrorMessage); errText != "" {
		body += "\n\n最近一次创建失败：\n" + errText + "\n\n请修正后重试。"
	}
	appcards.AppendMarkdownBodyCardElement(card, map[string]any{"tag": "markdown", "content": body})

	baseSelect := appcards.BuildFormSelectStaticElement("base_workspace_id", "基准工作区", baseOptions, baseWorkspaceID)
	branchInput := map[string]any{
		"tag":         "input",
		"name":        "branch_name",
		"required":    false,
		"placeholder": map[string]any{"tag": "plain_text", "content": "新分支名，例如 work/project/bot"},
	}
	if branchName != "" {
		branchInput["default_value"] = branchName
	}
	workspaceIDInput := map[string]any{
		"tag":         "input",
		"name":        "workspace_id",
		"required":    false,
		"placeholder": map[string]any{"tag": "plain_text", "content": "workspace_id"},
	}
	if workspaceID != "" {
		workspaceIDInput["default_value"] = workspaceID
	}
	directoryNameInput := map[string]any{
		"tag":         "input",
		"name":        "directory_name",
		"required":    false,
		"placeholder": map[string]any{"tag": "plain_text", "content": "目录名（默认等于 workspace_id）"},
	}
	if directoryName != "" {
		directoryNameInput["default_value"] = directoryName
	}
	buttonRows := appcards.BuildMarkdownBodyCardActionElements([]feishu.Button{
		{
			Text:  "确认创建",
			Type:  "primary",
			Name:  "workspace_worktree_submit",
			Value: cardactions.RequestActionValue{Action: "workspace.worktree.submit", RequestID: requestID}.Map(),
		},
		{
			Text:  "取消",
			Type:  "default",
			Name:  "workspace_worktree_cancel",
			Value: cardactions.RequestActionValue{Action: "pending_form.cancel", RequestID: requestID}.Map(),
		},
	})
	for idx, row := range buttonRows {
		columns := row["columns"].([]map[string]any)
		if len(columns) == 0 {
			continue
		}
		button := columns[0]["elements"].([]map[string]any)[0]
		if idx == 0 {
			button["form_action_type"] = "submit"
		}
	}
	form := map[string]any{
		"tag":                "form",
		"name":               "workspace_worktree_form",
		"direction":          "vertical",
		"horizontal_spacing": "8px",
		"vertical_spacing":   "8px",
		"elements":           append(append([]map[string]any{baseSelect, branchInput}, buttonRows...), workspaceIDInput, directoryNameInput),
	}
	appcards.AppendMarkdownBodyCardElement(card, form)
	return card
}

// RenderWorkspaceWorktreePreparingCard renders a worktree creation progress card.
func (s *RenderService) RenderWorkspaceWorktreePreparingCard(requestID string, payload WorktreePayload, plan *WorktreePlan, snapshot CloneProgressSnapshot) map[string]any {
	statusLine := "正在创建 Worktree 工作区。"
	if snapshot.State == "cancelling" {
		statusLine = "正在取消 Worktree 创建。"
	}
	baseWorkspaceID := strings.TrimSpace(payload.BaseWorkspaceID)
	branchName := strings.TrimSpace(payload.BranchName)
	workspaceID := strings.TrimSpace(payload.WorkspaceID)
	targetDir := strings.TrimSpace(payload.TargetDir)
	if plan != nil {
		baseWorkspaceID = firstNonEmpty(strings.TrimSpace(plan.BaseWorkspaceID), baseWorkspaceID)
		branchName = firstNonEmpty(strings.TrimSpace(plan.BranchName), branchName)
		workspaceID = firstNonEmpty(strings.TrimSpace(plan.WorkspaceID), workspaceID)
		targetDir = firstNonEmpty(strings.TrimSpace(plan.TargetDir), targetDir)
	}
	lines := []string{
		statusLine,
		"",
		"基准工作区: `" + firstNonEmpty(baseWorkspaceID, "-") + "`",
		"分支: `" + firstNonEmpty(branchName, "-") + "`",
		"workspace_id: `" + firstNonEmpty(workspaceID, "-") + "`",
		"目标目录: `" + firstNonEmpty(targetDir, "-") + "`",
	}
	if !snapshot.StartedAt.IsZero() {
		lines = append(lines, "已运行: `"+strings.TrimSpace(strings.TrimPrefix(formatTurnElapsedLine(time.Since(snapshot.StartedAt)), "elapsed: "))+"`")
	}
	if len(snapshot.Lines) > 0 {
		lines = append(lines, "", "最近进度:", markdownCodeBlock(strings.Join(snapshot.Lines, "\n")))
	} else {
		lines = append(lines, "", "正在执行 `git worktree add`。")
	}
	lines = append(lines, "", "这张卡片会自动刷新。")
	var buttons []feishu.Button
	if snapshot.State != "cancelling" {
		buttons = []feishu.Button{{
			Text:  "取消创建",
			Type:  "default",
			Value: cardactions.RequestActionValue{Action: "workspace.worktree.cancel", RequestID: requestID}.Map(),
		}}
	}
	return feishu.SimpleStatusCard("从 Worktree 创建工作区", "blue", strings.Join(lines, "\n"), buttons)
}

// RenderWorkspaceWorktreeSuccessCard renders the worktree success card.
func (s *RenderService) RenderWorkspaceWorktreeSuccessCard(sessionKey, workspaceID, targetDir string) map[string]any {
	buttons := []feishu.Button{{
		Text:  feishu.MenuBackButtonText,
		Type:  "default",
		Value: cardactions.MenuActionValue{Action: "menu.workspace", SessionKey: sessionKey}.Map(),
	}}
	body := "已从 Worktree 创建工作区 `" + workspaceID + "`\n\ncwd: `" + targetDir + "`"
	return feishu.SimpleStatusCard("工作区已创建", "green", body, buttons)
}

// RenderWorkspaceWorktreeManualHintCard renders the worktree manual takeover hint card.
func (s *RenderService) RenderWorkspaceWorktreeManualHintCard(sessionKey, workspaceID, targetDir, errText string) map[string]any {
	lines := []string{
		"Worktree 已创建，可手动接管。",
		"",
		"目录: `" + firstNonEmpty(strings.TrimSpace(targetDir), "-") + "`",
	}
	if workspaceID = strings.TrimSpace(workspaceID); workspaceID != "" {
		lines = append(lines, "建议 workspace_id: `"+workspaceID+"`")
	}
	lines = append(lines, "", "自动创建或切换工作区失败。Worktree 目录已保留，可稍后通过 `/workspace new` 手动接管。")
	if errText = strings.TrimSpace(errText); errText != "" {
		lines = append(lines, "", "错误: "+errText)
	}
	buttons := []feishu.Button{{
		Text:  feishu.MenuBackButtonText,
		Type:  "default",
		Value: cardactions.MenuActionValue{Action: "menu.workspace", SessionKey: sessionKey}.Map(),
	}}
	return feishu.SimpleStatusCard("Worktree 已创建", "orange", strings.Join(lines, "\n"), buttons)
}

// RenderWorkspaceWorktreeCanceledCard renders the worktree canceled card.
func (s *RenderService) RenderWorkspaceWorktreeCanceledCard(sessionKey string, payload WorktreePayload, plan *WorktreePlan, snapshot CloneProgressSnapshot) map[string]any {
	targetDir := strings.TrimSpace(payload.TargetDir)
	if plan != nil {
		targetDir = firstNonEmpty(strings.TrimSpace(plan.TargetDir), targetDir)
	}
	lines := []string{
		"已取消 Worktree 创建。",
		"",
		"基准工作区: `" + firstNonEmpty(strings.TrimSpace(payload.BaseWorkspaceID), "-") + "`",
		"分支: `" + firstNonEmpty(strings.TrimSpace(payload.BranchName), "-") + "`",
		"workspace_id: `" + firstNonEmpty(strings.TrimSpace(payload.WorkspaceID), "-") + "`",
		"目标目录: `" + firstNonEmpty(targetDir, "-") + "`",
		"",
		"如果目标目录有残留，请清理后重新发起。",
	}
	if len(snapshot.Lines) > 0 {
		lines = append(lines, "", "取消前最后进度:", markdownCodeBlock(strings.Join(snapshot.Lines, "\n")))
	}
	buttons := []feishu.Button{{
		Text:  feishu.MenuBackButtonText,
		Type:  "default",
		Value: cardactions.MenuActionValue{Action: "menu.workspace", SessionKey: sessionKey}.Map(),
	}}
	return feishu.SimpleStatusCard("Worktree 创建已取消", "grey", strings.Join(lines, "\n"), buttons)
}

// RenderWorkspaceMenuCard renders the workspace management menu card.
func (s *RenderService) RenderWorkspaceMenuCard(view appselection.View, sessionKey string) map[string]any {
	currentID := view.CurrentID
	currentLabel := "(未配置)"
	if strings.TrimSpace(currentID) != "" {
		currentLabel = "`" + currentID + "`"
	}
	bodyLines := []string{"当前工作区: " + currentLabel}
	bodyLines = appendWorkspaceSummary(bodyLines, view)
	buttons := make([]feishu.Button, 0, 6)
	workspaces := view.Workspaces
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
		feishu.Button{
			Text: submenuCommandLabel("创建 Worktree", "/workspace new worktree"),
			Type: "default",
			Value: map[string]any{
				"action":      "workspace.worktree",
				"session_key": sessionKey,
			},
		},
	)
	buttons = append(buttons, workspaceConfigButtons(view.ConfigActions, sessionKey)...)
	if view.Group {
		if currentID != "" {
			buttons = append(buttons, feishu.Button{
				Text: "解除本群绑定",
				Type: "default",
				Value: map[string]any{
					"action":      "workspace.binding.unbind",
					"session_key": sessionKey,
				},
			})
		}
	} else {
		buttons = append(buttons, feishu.Button{
			Text: submenuCommandLabel("删除工作区", "/workspace delete"),
			Type: "default",
			Value: map[string]any{
				"action":      "workspace.delete.menu",
				"session_key": sessionKey,
			},
		})
	}
	buttons = append(buttons,
		feishu.Button{
			Text: feishu.MenuBackButtonText,
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
func (s *RenderService) RenderWorkspaceChooseCard(view appselection.View, sessionKey string) map[string]any {
	currentID := view.CurrentID
	sorted := view.RecentWorkspaces

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

// RenderWorkspaceDeleteMenuCard renders the workspace delete menu card.
func (s *RenderService) RenderWorkspaceDeleteMenuCard(view appselection.View, sessionKey string) (map[string]any, error) {
	currentID := view.CurrentID
	workspaces := view.Workspaces
	lines := []string{
		"删除 workspace 只会移除配置，不会删除磁盘目录。",
		"",
		"当前工作区: `" + currentID + "`",
		"当前工作区不可删除，请先切换到其他工作区。",
	}
	deleteOptions := make([]appcards.SelectStaticOption, 0, len(workspaces))
	for _, ws := range view.DeletableWorkspaces {
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
			Text: feishu.MenuBackButtonText,
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
func (s *RenderService) RenderWorkspaceDeleteConfirmCard(sessionKey string, ws domain.Workspace) (map[string]any, error) {
	workspaceID := ws.ID
	body := []string{
		"即将删除工作区配置：`" + workspaceID + "`",
		"",
		"name: `" + firstNonEmpty(strings.TrimSpace(ws.Name), workspaceID) + "`",
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
			Text: feishu.MenuBackButtonText,
			Type: "default",
			Value: map[string]any{
				"action":      "workspace.delete.menu",
				"session_key": sessionKey,
			},
		},
	}
	bodyText := strings.Join(body, "\n")
	bodyText = s.FormatMenuBody("workspace.delete.confirm", bodyText)
	return feishu.SimpleStatusCard("确认删除工作区", "red", bodyText, buttons), nil
}
