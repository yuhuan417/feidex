package app

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"

	"feidex/internal/codexrpc"
	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

const (
	skillConfigReloadArg = "reload"
)

type skillPrefixMode int

const (
	skillPrefixNone skillPrefixMode = iota
	skillPrefixInvalid
	skillPrefixCandidate
)

type parsedSkillPrefix struct {
	Mode skillPrefixMode
	Name string
	Body string
}

type submissionSkillResolution struct {
	InputText          string
	Skills             []state.SubmissionSkill
	ConsumePending     bool
	PendingReplacement *state.SubmissionSkill
}

func matchSkillsCommand(fields []string) bool {
	return exactOrSingleArgCommand(fields, skillConfigReloadArg)
}

func (a *App) commandSkills(msg *feishu.InboundMessage, args []string) error {
	forceReload := false
	switch len(args) {
	case 0:
	case 1:
		if strings.TrimSpace(args[0]) != skillConfigReloadArg {
			return fmt.Errorf("usage: /skills | /skills reload")
		}
		forceReload = true
	default:
		return fmt.Errorf("usage: /skills | /skills reload")
	}
	card, err := a.renderSkillsCard(a.makeSessionKey(msg), forceReload)
	if err != nil {
		return err
	}
	_, err = a.feishu.ReplyCard(context.Background(), msg.MessageID, card, msg.ChatType == "group" && a.cfg.Feishu.ReplyInThread)
	return err
}

func (a *App) currentWorkspaceForSessionKey(sessionKey string) (*config.Workspace, error) {
	if a == nil || a.cfg == nil || len(a.cfg.Workspaces) == 0 {
		return nil, fmt.Errorf("当前没有可用工作区")
	}
	workspaceID := a.defaultWorkspaceID()
	if sess := a.appState().session(sessionKey); sess != nil && strings.TrimSpace(sess.WorkspaceID) != "" {
		workspaceID = strings.TrimSpace(sess.WorkspaceID)
	}
	ws := config.FindWorkspace(a.cfg, workspaceID)
	if ws == nil {
		return nil, fmt.Errorf("workspace %q not found", workspaceID)
	}
	return ws, nil
}

func (a *App) workspaceByID(workspaceID string) (*config.Workspace, error) {
	if a == nil || a.cfg == nil || len(a.cfg.Workspaces) == 0 {
		return nil, fmt.Errorf("当前没有可用工作区")
	}
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		workspaceID = a.defaultWorkspaceID()
	}
	ws := config.FindWorkspace(a.cfg, workspaceID)
	if ws == nil {
		return nil, fmt.Errorf("workspace %q not found", workspaceID)
	}
	return ws, nil
}

func (a *App) fetchSkillsForCWD(ctx context.Context, cwd string, forceReload bool) (codexrpc.SkillsListEntry, error) {
	var result codexrpc.SkillsListResult
	if a == nil || a.codex == nil {
		return codexrpc.SkillsListEntry{}, fmt.Errorf("codex client not initialized")
	}
	params := map[string]any{
		"forceReload": forceReload,
	}
	if strings.TrimSpace(cwd) != "" {
		params["cwds"] = []string{strings.TrimSpace(cwd)}
	}
	if err := a.codex.Call(ctx, "skills/list", params, &result); err != nil {
		return codexrpc.SkillsListEntry{}, err
	}
	for _, entry := range result.Data {
		if strings.TrimSpace(entry.Cwd) == strings.TrimSpace(cwd) {
			return entry, nil
		}
	}
	if len(result.Data) > 0 {
		return result.Data[0], nil
	}
	return codexrpc.SkillsListEntry{Cwd: strings.TrimSpace(cwd)}, nil
}

func (a *App) fetchSkillsForSessionKey(ctx context.Context, sessionKey string, forceReload bool) (codexrpc.SkillsListEntry, error) {
	ws, err := a.currentWorkspaceForSessionKey(sessionKey)
	if err != nil {
		return codexrpc.SkillsListEntry{}, err
	}
	return a.fetchSkillsForCWD(ctx, ws.Cwd, forceReload)
}

func (a *App) fetchSkillsForWorkspaceID(ctx context.Context, workspaceID string, forceReload bool) (codexrpc.SkillsListEntry, error) {
	ws, err := a.workspaceByID(workspaceID)
	if err != nil {
		return codexrpc.SkillsListEntry{}, err
	}
	return a.fetchSkillsForCWD(ctx, ws.Cwd, forceReload)
}

func sortSkillsForDisplay(skills []codexrpc.SkillMetadata) []codexrpc.SkillMetadata {
	sorted := append([]codexrpc.SkillMetadata(nil), skills...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].Enabled != sorted[j].Enabled {
			return sorted[i].Enabled
		}
		if strings.TrimSpace(sorted[i].Scope) != strings.TrimSpace(sorted[j].Scope) {
			return strings.TrimSpace(sorted[i].Scope) < strings.TrimSpace(sorted[j].Scope)
		}
		return strings.ToLower(skillDisplayName(sorted[i])) < strings.ToLower(skillDisplayName(sorted[j]))
	})
	return sorted
}

func skillDisplayName(skill codexrpc.SkillMetadata) string {
	if skill.Interface != nil && strings.TrimSpace(skill.Interface.DisplayName) != "" {
		return strings.TrimSpace(skill.Interface.DisplayName)
	}
	return strings.TrimSpace(skill.Name)
}

func skillSummaryText(skill codexrpc.SkillMetadata) string {
	for _, candidate := range []string{
		strings.TrimSpace(skill.Description),
		strings.TrimSpace(skill.ShortDescription),
	} {
		if candidate != "" {
			return candidate
		}
	}
	if skill.Interface != nil && strings.TrimSpace(skill.Interface.ShortDescription) != "" {
		return strings.TrimSpace(skill.Interface.ShortDescription)
	}
	return "-"
}

func skillOptionText(skill codexrpc.SkillMetadata) string {
	label := skillDisplayName(skill)
	if skill.Name != "" && skill.Name != label {
		label += " (" + skill.Name + ")"
	}
	label += " [" + firstNonEmpty(strings.TrimSpace(skill.Scope), "unknown") + "]"
	if !skill.Enabled {
		label = "[disabled] " + label
	}
	return label
}

func (a *App) renderSkillsCard(sessionKey string, forceReload bool) (map[string]any, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	entry, err := a.fetchSkillsForSessionKey(ctx, sessionKey, forceReload)
	if err != nil {
		return nil, err
	}
	pending, hasPending := a.sessionPendingSkill(sessionKey)
	skills := sortSkillsForDisplay(entry.Skills)
	enabledCount := 0
	disabledCount := 0
	for _, skill := range entry.Skills {
		if skill.Enabled {
			enabledCount++
			continue
		}
		disabledCount++
	}
	lines := []string{
		"当前 cwd: `" + firstNonEmpty(strings.TrimSpace(entry.Cwd), "-") + "`",
		fmt.Sprintf("skills: `%d` (enabled `%d`, disabled `%d`)", len(entry.Skills), enabledCount, disabledCount),
	}
	if hasPending {
		lines = append(lines, "当前待发送 skill: `$"+pending.Name+"`")
	} else {
		lines = append(lines, "当前待发送 skill: `-`")
	}
	lines = append(lines,
		"",
		"通过下拉选择 skill；下一条非命令消息会自动携带它。",
		"也可以直接发送 `$skill-name 你的需求`。",
	)
	if len(entry.Errors) > 0 {
		lines = append(lines, "", fmt.Sprintf("扫描错误: `%d`", len(entry.Errors)))
		for i, item := range entry.Errors {
			if i >= 3 {
				break
			}
			lines = append(lines, "- "+firstNonEmpty(strings.TrimSpace(item.Path), "(unknown path)")+": "+firstNonEmpty(strings.TrimSpace(item.Message), "(unknown error)"))
		}
	}

	card := newMarkdownBodyCard("技能列表", "blue")
	appendMarkdownBodyCardElement(card, map[string]any{
		"tag":     "markdown",
		"content": menuCardBody("menu.skills", strings.Join(lines, "\n")),
	})

	initialOption := ""
	if hasPending {
		initialOption = firstNonEmpty(strings.TrimSpace(pending.Path), strings.TrimSpace(pending.Name))
	}
	if len(skills) > 0 {
		options := make([]selectStaticOption, 0, len(skills))
		for _, skill := range skills {
			options = append(options, selectStaticOption{
				Text:  skillOptionText(skill),
				Value: firstNonEmpty(strings.TrimSpace(skill.Path), strings.TrimSpace(skill.Name)),
			})
		}
		appendMarkdownBodyCardElement(card, buildSelectStaticElement(
			"skills_select",
			"选择 skill",
			map[string]any{"action": "skills.select", "session_key": sessionKey},
			options,
			initialOption,
		))
	}

	for _, row := range buildMarkdownBodyCardActionElements([]feishu.Button{
		{
			Text: commandLabel("刷新", "/skills reload"),
			Type: "default",
			Value: map[string]any{
				"action":      "skills.reload",
				"session_key": sessionKey,
			},
		},
		{
			Text: "返回上一级",
			Type: "default",
			Value: map[string]any{
				"action":      "menu.tools",
				"session_key": sessionKey,
			},
		},
	}) {
		appendMarkdownBodyCardElement(card, row)
	}
	return card, nil
}

func (a *App) completeSkillsReload(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	card, err := a.renderSkillsCard(sessionKey, true)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已刷新 skill 列表"},
		Card:  rawCard(card),
	}, nil
}

func (a *App) completeSkillsSelect(action *feishu.CardAction, sessionKey, selectedValue string) (*callback.CardActionTriggerResponse, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	entry, err := a.fetchSkillsForSessionKey(ctx, sessionKey, false)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	skill, ok := findSkillByPath(entry.Skills, selectedValue)
	if !ok {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "未找到所选 skill"}}, nil
	}
	if !skill.Enabled {
		card, renderErr := a.renderSkillsCard(sessionKey, false)
		if renderErr != nil {
			return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "该 skill 当前为 disabled"}}, nil
		}
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "warning", Content: "该 skill 当前为 disabled，不能用于下一条消息"},
			Card:  rawCard(card),
		}, nil
	}
	a.setSessionPendingSkill(sessionKey, state.SubmissionSkill{Name: strings.TrimSpace(skill.Name), Path: strings.TrimSpace(skill.Path)})
	card, err := a.renderSkillsCard(sessionKey, false)
	if err != nil {
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "success", Content: skillPendingConfirmationText(skill.Name)},
		}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: skillPendingConfirmationText(skill.Name)},
		Card:  rawCard(card),
	}, nil
}

func findSkillByPath(skills []codexrpc.SkillMetadata, selectedValue string) (codexrpc.SkillMetadata, bool) {
	selectedValue = strings.TrimSpace(selectedValue)
	for _, skill := range skills {
		if strings.TrimSpace(skill.Path) == selectedValue || strings.TrimSpace(skill.Name) == selectedValue {
			return skill, true
		}
	}
	return codexrpc.SkillMetadata{}, false
}

func findEnabledSkillByName(skills []codexrpc.SkillMetadata, name string) (state.SubmissionSkill, bool) {
	name = strings.TrimSpace(name)
	for _, skill := range skills {
		if !skill.Enabled || strings.TrimSpace(skill.Name) != name {
			continue
		}
		return state.SubmissionSkill{
			Name: strings.TrimSpace(skill.Name),
			Path: strings.TrimSpace(skill.Path),
		}, true
	}
	return state.SubmissionSkill{}, false
}

func skillPendingConfirmationText(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "(unknown skill)"
	}
	return "已选择 `$" + name + "`，请直接继续发送需求。下一条非命令消息会自动带上它。"
}

func (a *App) sessionPendingSkill(sessionKey string) (state.SubmissionSkill, bool) {
	if a == nil {
		return state.SubmissionSkill{}, false
	}
	a.skillsMu.Lock()
	defer a.skillsMu.Unlock()
	if a.pendingSkills == nil {
		return state.SubmissionSkill{}, false
	}
	skill, ok := a.pendingSkills[strings.TrimSpace(sessionKey)]
	if !ok || strings.TrimSpace(skill.Name) == "" || strings.TrimSpace(skill.Path) == "" {
		return state.SubmissionSkill{}, false
	}
	return skill, true
}

func (a *App) setSessionPendingSkill(sessionKey string, skill state.SubmissionSkill) {
	if a == nil || strings.TrimSpace(sessionKey) == "" {
		return
	}
	a.skillsMu.Lock()
	defer a.skillsMu.Unlock()
	if a.pendingSkills == nil {
		a.pendingSkills = map[string]state.SubmissionSkill{}
	}
	skill.Name = strings.TrimSpace(skill.Name)
	skill.Path = strings.TrimSpace(skill.Path)
	if skill.Name == "" || skill.Path == "" {
		delete(a.pendingSkills, strings.TrimSpace(sessionKey))
		return
	}
	a.pendingSkills[strings.TrimSpace(sessionKey)] = skill
}

func (a *App) clearSessionPendingSkill(sessionKey string) {
	if a == nil || strings.TrimSpace(sessionKey) == "" {
		return
	}
	a.skillsMu.Lock()
	defer a.skillsMu.Unlock()
	if a.pendingSkills == nil {
		return
	}
	delete(a.pendingSkills, strings.TrimSpace(sessionKey))
}

func parseLeadingSkillPrefix(text string) parsedSkillPrefix {
	raw := strings.TrimSpace(text)
	if raw == "" || raw[0] != '$' {
		return parsedSkillPrefix{Mode: skillPrefixNone}
	}
	rest := raw[1:]
	if rest == "" {
		return parsedSkillPrefix{Mode: skillPrefixInvalid}
	}
	skillName := rest
	body := ""
	if idx := strings.IndexFunc(rest, unicode.IsSpace); idx >= 0 {
		skillName = rest[:idx]
		body = strings.TrimLeftFunc(rest[idx:], unicode.IsSpace)
	}
	if !validSkillPrefixName(skillName) {
		return parsedSkillPrefix{Mode: skillPrefixInvalid}
	}
	return parsedSkillPrefix{
		Mode: skillPrefixCandidate,
		Name: strings.TrimSpace(skillName),
		Body: body,
	}
}

func validSkillPrefixName(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	hasAlphaNum := false
	for _, r := range name {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			hasAlphaNum = true
			continue
		}
		switch r {
		case '-', '_', '.':
			continue
		default:
			return false
		}
	}
	return hasAlphaNum
}

func (a *App) resolveSubmissionSkill(sessionKey, workspaceID, inputText string, attachments []state.SubmissionAttachment) submissionSkillResolution {
	resolution := submissionSkillResolution{
		InputText: strings.TrimSpace(inputText),
	}
	pending, hasPending := a.sessionPendingSkill(sessionKey)
	if hasPending {
		resolution.ConsumePending = true
	}

	parsed := parseLeadingSkillPrefix(inputText)
	switch parsed.Mode {
	case skillPrefixNone:
		if hasPending {
			resolution.Skills = []state.SubmissionSkill{pending}
		}
		return resolution
	case skillPrefixInvalid:
		return resolution
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	entry, err := a.fetchSkillsForWorkspaceID(ctx, workspaceID, false)
	if err != nil {
		return resolution
	}
	skill, ok := findEnabledSkillByName(entry.Skills, parsed.Name)
	if !ok {
		return resolution
	}
	resolution.InputText = strings.TrimSpace(parsed.Body)
	if resolution.InputText == "" && len(attachments) == 0 {
		resolution.PendingReplacement = &skill
		return resolution
	}
	resolution.Skills = []state.SubmissionSkill{skill}
	return resolution
}
