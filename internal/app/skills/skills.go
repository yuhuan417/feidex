package skills

import (
	"fmt"
	"sort"
	"strings"
	"unicode"

	"feidex/internal/app/appcore"
	appcards "feidex/internal/app/cards"
	"feidex/internal/codexrpc"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

// PrefixMode represents the result of parsing a skill prefix.
type PrefixMode int

const (
	PrefixNone PrefixMode = iota
	PrefixInvalid
	PrefixCandidate
)

// ParsedPrefix is the result of parsing a leading $skill prefix.
type ParsedPrefix struct {
	Mode PrefixMode
	Name string
	Body string
}

// SubmissionSkillResolution describes how a submission's skill was resolved.
type SubmissionSkillResolution struct {
	InputText          string
	Skills             []state.SubmissionSkill
	ConsumePending     bool
	PendingReplacement *state.SubmissionSkill
}

// SortForDisplay sorts skills for display: enabled first, then by scope, then by name.
func SortForDisplay(skills []codexrpc.SkillMetadata) []codexrpc.SkillMetadata {
	sorted := append([]codexrpc.SkillMetadata(nil), skills...)
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
func DisplayName(skill codexrpc.SkillMetadata) string {
	if skill.Interface != nil && strings.TrimSpace(skill.Interface.DisplayName) != "" {
		return strings.TrimSpace(skill.Interface.DisplayName)
	}
	return strings.TrimSpace(skill.Name)
}

// OptionText returns the display text for a skill in a select dropdown.
func OptionText(skill codexrpc.SkillMetadata) string {
	label := DisplayName(skill)
	if skill.Name != "" && skill.Name != label {
		label += " (" + skill.Name + ")"
	}
	label += " [" + appcore.FirstNonEmpty(strings.TrimSpace(skill.Scope), "unknown") + "]"
	if !skill.Enabled {
		label = "[disabled] " + label
	}
	return label
}

// FindByPath finds a skill by path or name in the given list.
func FindByPath(skills []codexrpc.SkillMetadata, selectedValue string) (codexrpc.SkillMetadata, bool) {
	selectedValue = strings.TrimSpace(selectedValue)
	for _, skill := range skills {
		if strings.TrimSpace(skill.Path) == selectedValue || strings.TrimSpace(skill.Name) == selectedValue {
			return skill, true
		}
	}
	return codexrpc.SkillMetadata{}, false
}

// FindEnabledByName finds an enabled skill by name.
func FindEnabledByName(skills []codexrpc.SkillMetadata, name string) (state.SubmissionSkill, bool) {
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

// PendingConfirmationText returns the confirmation text when a skill is selected.
func PendingConfirmationText(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "(unknown skill)"
	}
	return "已选择 `$" + name + "`，请直接继续发送需求。下一条非命令消息会自动带上它。"
}

// ParseLeadingPrefix parses a leading $skill-name prefix from input text.
func ParseLeadingPrefix(text string) ParsedPrefix {
	raw := strings.TrimSpace(text)
	if raw == "" || raw[0] != '$' {
		return ParsedPrefix{Mode: PrefixNone}
	}
	rest := raw[1:]
	if rest == "" {
		return ParsedPrefix{Mode: PrefixInvalid}
	}
	skillName := rest
	body := ""
	if idx := strings.IndexFunc(rest, unicode.IsSpace); idx >= 0 {
		skillName = rest[:idx]
		body = strings.TrimLeftFunc(rest[idx:], unicode.IsSpace)
	}
	if !ValidPrefixName(skillName) {
		return ParsedPrefix{Mode: PrefixInvalid}
	}
	return ParsedPrefix{
		Mode: PrefixCandidate,
		Name: strings.TrimSpace(skillName),
		Body: body,
	}
}

// ValidPrefixName reports whether name is a valid skill prefix name.
func ValidPrefixName(name string) bool {
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

// BuildCardParams contains the parameters for BuildCard.
type BuildCardParams struct {
	Entry       codexrpc.SkillsListEntry
	HasPending  bool
	Pending     state.SubmissionSkill
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
		"当前 cwd: `" + appcore.FirstNonEmpty(strings.TrimSpace(p.Entry.Cwd), "-") + "`",
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
			lines = append(lines, "- "+appcore.FirstNonEmpty(strings.TrimSpace(item.Path), "(unknown path)")+": "+appcore.FirstNonEmpty(strings.TrimSpace(item.Message), "(unknown error)"))
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
		initialOption = appcore.FirstNonEmpty(strings.TrimSpace(p.Pending.Path), strings.TrimSpace(p.Pending.Name))
	}
	if len(sorted) > 0 {
		options := make([]appcards.SelectStaticOption, 0, len(sorted))
		for _, skill := range sorted {
			options = append(options, appcards.SelectStaticOption{
				Text:  OptionText(skill),
				Value: appcore.FirstNonEmpty(strings.TrimSpace(skill.Path), strings.TrimSpace(skill.Name)),
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
		backLabel = "返回上一级"
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
