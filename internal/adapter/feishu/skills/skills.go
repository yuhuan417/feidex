package skills

import (
	skillcatalog "feidex/internal/domain/skill"
	domainsubmission "feidex/internal/domain/submission"
	"feidex/internal/textutil"
	"fmt"
	"sort"
	"strings"

	appcards "feidex/internal/adapter/feishu/cards"
	"feidex/internal/feishu"
)

// SortForDisplay sorts skills for display: enabled first, then by scope, then by name.
func SortForDisplay(skills []skillcatalog.SkillMetadata) []skillcatalog.SkillMetadata {
	sorted := append([]skillcatalog.SkillMetadata(nil), skills...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].Enabled != sorted[j].Enabled {
			return sorted[i].Enabled
		}
		if strings.TrimSpace(sorted[i].Scope) != strings.TrimSpace(sorted[j].Scope) {
			return strings.TrimSpace(sorted[i].Scope) < strings.TrimSpace(sorted[j].Scope)
		}
		return strings.ToLower(DisplayName(sorted[i])) < strings.ToLower(DisplayName(sorted[j]))
	})
	return sorted
}

// DisplayName returns the display name for a skill.
func DisplayName(skill skillcatalog.SkillMetadata) string {
	if skill.Interface != nil && strings.TrimSpace(skill.Interface.DisplayName) != "" {
		return strings.TrimSpace(skill.Interface.DisplayName)
	}
	return strings.TrimSpace(skill.Name)
}

// OptionText returns the display text for a skill in a select dropdown.
func OptionText(skill skillcatalog.SkillMetadata) string {
	label := DisplayName(skill)
	if skill.Name != "" && skill.Name != label {
		label += " (" + skill.Name + ")"
	}
	label += " [" + textutil.FirstNonEmpty(strings.TrimSpace(skill.Scope), "unknown") + "]"
	if !skill.Enabled {
		label = "[disabled] " + label
	}
	return label
}

// FindByPath finds a skill by path or name in the given list.
func FindByPath(skills []skillcatalog.SkillMetadata, selectedValue string) (skillcatalog.SkillMetadata, bool) {
	selectedValue = strings.TrimSpace(selectedValue)
	for _, skill := range skills {
		if strings.TrimSpace(skill.Path) == selectedValue || strings.TrimSpace(skill.Name) == selectedValue {
			return skill, true
		}
	}
	return skillcatalog.SkillMetadata{}, false
}

// PendingConfirmationText returns the confirmation text when a skill is selected.
func PendingConfirmationText(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "(unknown skill)"
	}
	return "已选择 `$" + name + "`，请直接继续发送需求。下一条非命令消息会自动带上它。"
}

type BuildCardParams struct {
	Entry       skillcatalog.SkillsListEntry
	HasPending  bool
	Pending     domainsubmission.SubmissionSkill
	SessionKey  string
	FormatBody  func(string) string
	ReloadLabel string
	BackLabel   string
}

// BuildCard builds the skills list card from the given data.
// The formatBody function is applied to the markdown body (typically menuCardBody).
// reloadLabel and backLabel are the formatted button labels.
func BuildCard(p BuildCardParams) map[string]any {
	sorted := SortForDisplay(p.Entry.Skills)
	enabledCount := 0
	disabledCount := 0
	for _, skill := range p.Entry.Skills {
		if skill.Enabled {
			enabledCount++
			continue
		}
		disabledCount++
	}
	lines := []string{
		"当前 cwd: `" + textutil.FirstNonEmpty(strings.TrimSpace(p.Entry.Cwd), "-") + "`",
		fmt.Sprintf("skills: `%d` (enabled `%d`, disabled `%d`)", len(p.Entry.Skills), enabledCount, disabledCount),
	}
	if p.HasPending {
		lines = append(lines, "当前待发送 skill: `$"+p.Pending.Name+"`")
	} else {
		lines = append(lines, "当前待发送 skill: `-`")
	}
	lines = append(lines,
		"",
		"通过下拉选择 skill；下一条非命令消息会自动携带它。",
		"也可以直接发送 `$skill-name 你的需求`。",
	)
	if len(p.Entry.Errors) > 0 {
		lines = append(lines, "", fmt.Sprintf("扫描错误: `%d`", len(p.Entry.Errors)))
		for i, item := range p.Entry.Errors {
			if i >= 3 {
				break
			}
			lines = append(lines, "- "+textutil.FirstNonEmpty(strings.TrimSpace(item.Path), "(unknown path)")+": "+textutil.FirstNonEmpty(strings.TrimSpace(item.Message), "(unknown error)"))
		}
	}

	body := strings.Join(lines, "\n")
	if p.FormatBody != nil {
		body = p.FormatBody(body)
	}

	card := appcards.NewMarkdownBodyCard("技能列表", "blue")
	appcards.AppendMarkdownBodyCardElement(card, map[string]any{
		"tag":     "markdown",
		"content": body,
	})

	initialOption := ""
	if p.HasPending {
		initialOption = textutil.FirstNonEmpty(strings.TrimSpace(p.Pending.Path), strings.TrimSpace(p.Pending.Name))
	}
	if len(sorted) > 0 {
		options := make([]appcards.SelectStaticOption, 0, len(sorted))
		for _, skill := range sorted {
			options = append(options, appcards.SelectStaticOption{
				Text:  OptionText(skill),
				Value: textutil.FirstNonEmpty(strings.TrimSpace(skill.Path), strings.TrimSpace(skill.Name)),
			})
		}
		appcards.AppendMarkdownBodyCardElement(card, appcards.BuildSelectStaticElement(
			"skills_select",
			"选择 skill",
			map[string]any{"action": "skills.select", "session_key": p.SessionKey},
			options,
			initialOption,
		))
	}

	reloadLabel := p.ReloadLabel
	if reloadLabel == "" {
		reloadLabel = "刷新 /skills reload"
	}
	backLabel := p.BackLabel
	if backLabel == "" {
		backLabel = feishu.MenuBackButtonText
	}

	for _, row := range appcards.BuildMarkdownBodyCardActionElements([]feishu.Button{
		{
			Text: reloadLabel,
			Type: "default",
			Value: map[string]any{
				"action":      "skills.reload",
				"session_key": p.SessionKey,
			},
		},
		{
			Text: backLabel,
			Type: "default",
			Value: map[string]any{
				"action":      "menu.tools",
				"session_key": p.SessionKey,
			},
		},
	}) {
		appcards.AppendMarkdownBodyCardElement(card, row)
	}
	return card
}
