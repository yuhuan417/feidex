package skill

import (
	skillcatalog "feidex/internal/domain/skill"
	domainsubmission "feidex/internal/domain/submission"
	"strings"
	"unicode"
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
	Skills             []domainsubmission.SubmissionSkill
	ConsumePending     bool
	PendingReplacement *domainsubmission.SubmissionSkill
}

// FindEnabledByName finds an enabled skill by name.
func FindEnabledByName(skills []skillcatalog.SkillMetadata, name string) (domainsubmission.SubmissionSkill, bool) {
	name = strings.TrimSpace(name)
	for _, skill := range skills {
		if !skill.Enabled || strings.TrimSpace(skill.Name) != name {
			continue
		}
		return domainsubmission.SubmissionSkill{
			Name: strings.TrimSpace(skill.Name),
			Path: strings.TrimSpace(skill.Path),
		}, true
	}
	return domainsubmission.SubmissionSkill{}, false
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
