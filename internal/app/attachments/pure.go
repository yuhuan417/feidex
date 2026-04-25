package attachments

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"feidex/internal/pathdisplay"
	"feidex/internal/state"
)

const AttachmentsDirName = ".feidex-attachments"

var (
	MarkdownLinkFullRe = regexp.MustCompile(`\[([^\]]+)\]\(([^)\n]+)\)`)
	LineSuffixRe       = regexp.MustCompile(`^(.*?)(?::\d+(?::\d+)?)?$`)
)

func SessionAttachmentDir(workspaceCwd, sessionKey, messageID string) string {
	return filepath.Join(workspaceCwd, AttachmentsDirName, ShortHash(sessionKey), ShortHash(messageID))
}

func ShortHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:12])
}

func BuildTurnInputs(sub *state.Submission) []map[string]any {
	inputs := make([]map[string]any, 0, len(sub.Skills)+1+len(sub.Attachments))
	for _, skill := range sub.Skills {
		if strings.TrimSpace(skill.Name) == "" || strings.TrimSpace(skill.Path) == "" {
			continue
		}
		inputs = append(inputs, map[string]any{
			"type": "skill",
			"name": skill.Name,
			"path": skill.Path,
		})
	}
	if text := strings.TrimSpace(sub.InputText); text != "" {
		inputs = append(inputs, TextInput(text))
	}
	for _, attachment := range sub.Attachments {
		switch attachment.Kind {
		case "image":
			if strings.TrimSpace(attachment.LocalPath) != "" {
				inputs = append(inputs, map[string]any{
					"type": "localImage",
					"path": attachment.LocalPath,
				})
			}
		case "file":
			if prompt := AttachmentPrompt(attachment); prompt != "" {
				inputs = append(inputs, TextInput(prompt))
			}
		default:
			if prompt := AttachmentPrompt(attachment); prompt != "" {
				inputs = append(inputs, TextInput(prompt))
			}
		}
	}
	return inputs
}

func TextInput(text string) map[string]any {
	return map[string]any{
		"type":          "text",
		"text":          text,
		"text_elements": []any{},
	}
}

func AttachmentPrompt(attachment state.SubmissionAttachment) string {
	path := strings.TrimSpace(attachment.LocalPath)
	if path == "" {
		return ""
	}
	switch attachment.Kind {
	case "file":
		return fmt.Sprintf("User attached file: %s", path)
	case "image":
		return fmt.Sprintf("User attached image: %s", path)
	case "audio":
		return fmt.Sprintf("User attached audio file (not transcribed): %s", path)
	default:
		return fmt.Sprintf("User attached %s: %s", strings.TrimSpace(attachment.Kind), path)
	}
}

func SubmissionInputPreview(sub *state.Submission) string {
	parts := make([]string, 0, len(sub.Skills)+1+len(sub.Attachments))
	for _, skill := range sub.Skills {
		if strings.TrimSpace(skill.Name) == "" {
			continue
		}
		parts = append(parts, "[skill] "+skill.Name)
	}
	if text := strings.TrimSpace(sub.InputText); text != "" {
		parts = append(parts, text)
	}
	for _, attachment := range sub.Attachments {
		parts = append(parts, AttachmentPreview(attachment))
	}
	if len(parts) == 0 {
		return "-"
	}
	return strings.Join(parts, "\n")
}

func AttachmentPreview(attachment state.SubmissionAttachment) string {
	name := strings.TrimSpace(attachment.Name)
	if name == "" {
		name = filepath.Base(attachment.LocalPath)
	}
	if name == "" {
		name = "attachment"
	}
	switch attachment.Kind {
	case "image":
		return "[图片] " + name
	case "file":
		return "[文件] " + name
	case "audio":
		return "[音频] " + name
	default:
		return "[" + strings.TrimSpace(attachment.Kind) + "] " + name
	}
}

func NormalizeReferencedPath(raw, workspaceCwd string) (string, bool) {
	raw = strings.TrimSpace(raw)
	raw = strings.Trim(raw, "\"'")
	raw = strings.TrimSuffix(raw, ")")
	if raw == "" {
		return "", false
	}
	if idx := strings.IndexByte(raw, '#'); idx >= 0 {
		raw = raw[:idx]
	}
	raw = TrimLineReferenceSuffix(raw)
	if raw == "" {
		return "", false
	}
	var path string
	switch {
	case filepath.IsAbs(raw):
		path = filepath.Clean(raw)
		if repaired, ok := RepairMalformedWorkspacePath(path, workspaceCwd); ok {
			path = repaired
		}
	case strings.HasPrefix(raw, "./"), strings.HasPrefix(raw, "../"), strings.Contains(raw, "/"):
		path = filepath.Clean(filepath.Join(workspaceCwd, raw))
	default:
		return "", false
	}
	if !PathWithinWorkspace(path, workspaceCwd) {
		return "", false
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return "", false
	}
	return path, true
}

func RepairMalformedWorkspacePath(path, workspaceCwd string) (string, bool) {
	workspaceCwd = filepath.Clean(workspaceCwd)
	path = filepath.Clean(path)
	prefix := workspaceCwd + "."
	if !strings.HasPrefix(path, prefix) {
		return "", false
	}
	rest := strings.TrimPrefix(path, prefix)
	parts := strings.SplitN(rest, string(filepath.Separator), 2)
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return "", false
	}
	candidate := filepath.Join(workspaceCwd, parts[1]+"."+parts[0])
	info, err := os.Stat(candidate)
	if err != nil || !info.Mode().IsRegular() {
		return "", false
	}
	return candidate, true
}

func SanitizeLocalMarkdownLinks(text, workspaceCwd string) string {
	return RewriteMarkdownLinksForCard(text, workspaceCwd, false)
}

func NeutralizeLocalMarkdownLinks(text, workspaceCwd string) string {
	if strings.TrimSpace(text) == "" {
		return text
	}
	return RewriteMarkdownLinksForCard(text, workspaceCwd, true)
}

func RewriteMarkdownLinksForCard(text, workspaceCwd string, keepNonLocalLink bool) string {
	if strings.TrimSpace(text) == "" {
		return text
	}
	return MarkdownLinkFullRe.ReplaceAllStringFunc(text, func(match string) string {
		parts := MarkdownLinkFullRe.FindStringSubmatch(match)
		if len(parts) != 3 {
			return match
		}
		label := strings.TrimSpace(parts[1])
		href := strings.TrimSpace(parts[2])
		if pathText, ok := LocalLinkDisplayTarget(href, workspaceCwd); ok {
			return "`" + pathText + "`"
		}
		if fixed := RecoverFilenameFromMalformedLabel(label); fixed != "" {
			return "`" + fixed + "`"
		}
		if keepNonLocalLink {
			return match
		}
		return "`" + label + "`"
	})
}

func LocalLinkDisplayTarget(rawHref, workspaceCwd string) (string, bool) {
	target := CleanMarkdownLinkTarget(rawHref)
	if target == "" {
		return "", false
	}
	display := pathdisplay.RenderWorkspaceDisplayPath(target, workspaceCwd)
	if _, ok := NormalizeReferencedPath(target, workspaceCwd); ok {
		return display, true
	}
	if !LooksLikeLocalPathTarget(target) {
		return "", false
	}
	return display, true
}

func CleanMarkdownLinkTarget(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.Trim(raw, "\"'")
	raw = strings.TrimSuffix(raw, ")")
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(raw, "<") && strings.HasSuffix(raw, ">") {
		raw = strings.TrimPrefix(strings.TrimSuffix(raw, ">"), "<")
	}
	return strings.TrimSpace(raw)
}

func LooksLikeLocalPathTarget(target string) bool {
	target = strings.TrimSpace(target)
	if target == "" || strings.HasPrefix(target, "#") {
		return false
	}
	parsed, err := url.Parse(target)
	if err == nil && strings.TrimSpace(parsed.Scheme) != "" {
		return strings.EqualFold(parsed.Scheme, "file")
	}
	if filepath.IsAbs(target) || strings.HasPrefix(target, "./") || strings.HasPrefix(target, "../") || strings.HasPrefix(target, "~/") {
		return true
	}
	return strings.Contains(target, "/") || strings.Contains(target, `\`)
}

func RecoverFilenameFromMalformedLabel(label string) string {
	label = strings.TrimSpace(label)
	if !strings.HasPrefix(label, ".") {
		return ""
	}
	trimmed := strings.TrimPrefix(label, ".")
	for i := 1; i < len(trimmed); i++ {
		ext := trimmed[:i]
		name := trimmed[i:]
		if ext == "" || name == "" {
			continue
		}
		if !IsAlphaNum(ext) || !IsFileNameLike(name) {
			continue
		}
		return name + "." + ext
	}
	return ""
}

func IsAlphaNum(s string) bool {
	for _, r := range s {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') {
			return false
		}
	}
	return s != ""
}

func IsFileNameLike(s string) bool {
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '_' || r == '-' || r == '.':
		default:
			return false
		}
	}
	return s != ""
}

func TrimLineReferenceSuffix(path string) string {
	match := LineSuffixRe.FindStringSubmatch(path)
	if len(match) == 2 {
		return match[1]
	}
	return path
}

func PathWithinWorkspace(path, workspaceCwd string) bool {
	workspaceCwd = filepath.Clean(workspaceCwd)
	path = filepath.Clean(path)
	rel, err := filepath.Rel(workspaceCwd, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}
