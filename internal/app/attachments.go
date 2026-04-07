package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

const attachmentsDirName = ".feidex-attachments"

var (
	markdownPathLinkRe = regexp.MustCompile(`\[[^\]]+\]\(([^)\n]+)\)`)
	markdownLinkFullRe = regexp.MustCompile(`\[([^\]]+)\]\(([^)\n]+)\)`)
	inlineCodePathRe   = regexp.MustCompile("`([^`\\n]+)`")
	plainPathTokenRe   = regexp.MustCompile(`(?:^|[\s(])((?:/|(?:\./)|(?:\.\./)|(?:[A-Za-z0-9_.-]+/))[^\s\)` + "`" + `\"']+)`)
	lineSuffixRe       = regexp.MustCompile(`^(.*?)(?::\d+(?::\d+)?)?$`)
)

func (a *App) resolveInboundAttachments(msg *feishu.InboundMessage, workspaceID, sessionKey string) ([]state.SubmissionAttachment, error) {
	if msg == nil || len(msg.Attachments) == 0 {
		return nil, nil
	}
	workspace := config.FindWorkspace(a.cfg, workspaceID)
	if workspace == nil {
		return nil, fmt.Errorf("workspace %q not found", workspaceID)
	}
	if strings.TrimSpace(msg.MessageID) == "" {
		return nil, fmt.Errorf("attachment message is missing message id")
	}

	dir := sessionAttachmentDir(workspace.Cwd, sessionKey, msg.MessageID)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	attachments := make([]state.SubmissionAttachment, 0, len(msg.Attachments))
	for _, attachment := range msg.Attachments {
		path, name, err := a.feishu.DownloadMessageResource(ctx, msg.MessageID, attachment, dir)
		if err != nil {
			return nil, err
		}
		attachments = append(attachments, state.SubmissionAttachment{
			Kind:      attachment.Kind,
			Name:      name,
			LocalPath: path,
		})
	}
	return attachments, nil
}

func sessionAttachmentDir(workspaceCwd, sessionKey, messageID string) string {
	return filepath.Join(workspaceCwd, attachmentsDirName, shortHash(sessionKey), shortHash(messageID))
}

func shortHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:12])
}

func buildTurnInputs(sub *state.Submission) []map[string]any {
	inputs := make([]map[string]any, 0, 1+len(sub.Attachments))
	if text := strings.TrimSpace(sub.InputText); text != "" {
		inputs = append(inputs, textInput(text))
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
			if prompt := attachmentPrompt(attachment); prompt != "" {
				inputs = append(inputs, textInput(prompt))
			}
		default:
			if prompt := attachmentPrompt(attachment); prompt != "" {
				inputs = append(inputs, textInput(prompt))
			}
		}
	}
	return inputs
}

func textInput(text string) map[string]any {
	return map[string]any{
		"type":          "text",
		"text":          text,
		"text_elements": []any{},
	}
}

func attachmentPrompt(attachment state.SubmissionAttachment) string {
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

func submissionInputPreview(sub *state.Submission) string {
	parts := make([]string, 0, 1+len(sub.Attachments))
	if text := strings.TrimSpace(sub.InputText); text != "" {
		parts = append(parts, text)
	}
	for _, attachment := range sub.Attachments {
		parts = append(parts, attachmentPreview(attachment))
	}
	if len(parts) == 0 {
		return "-"
	}
	return strings.Join(parts, "\n")
}

func attachmentPreview(attachment state.SubmissionAttachment) string {
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

func collectReplyFiles(outputText, workspaceCwd string) []string {
	if strings.TrimSpace(outputText) == "" || strings.TrimSpace(workspaceCwd) == "" {
		return nil
	}
	seen := map[string]struct{}{}
	var paths []string
	addMatches := func(matches [][]string) {
		for _, match := range matches {
			if len(match) < 2 {
				continue
			}
			if path, ok := normalizeReferencedPath(match[1], workspaceCwd); ok {
				if _, exists := seen[path]; exists {
					continue
				}
				seen[path] = struct{}{}
				paths = append(paths, path)
			}
		}
	}
	addMatches(markdownPathLinkRe.FindAllStringSubmatch(outputText, -1))
	addMatches(inlineCodePathRe.FindAllStringSubmatch(outputText, -1))
	addMatches(plainPathTokenRe.FindAllStringSubmatch(outputText, -1))
	return paths
}

func normalizeReferencedPath(raw, workspaceCwd string) (string, bool) {
	raw = strings.TrimSpace(raw)
	raw = strings.Trim(raw, "\"'")
	raw = strings.TrimSuffix(raw, ")")
	if raw == "" {
		return "", false
	}
	if idx := strings.IndexByte(raw, '#'); idx >= 0 {
		raw = raw[:idx]
	}
	raw = trimLineReferenceSuffix(raw)
	if raw == "" {
		return "", false
	}
	var path string
	switch {
	case filepath.IsAbs(raw):
		path = filepath.Clean(raw)
		if repaired, ok := repairMalformedWorkspacePath(path, workspaceCwd); ok {
			path = repaired
		}
	case strings.HasPrefix(raw, "./"), strings.HasPrefix(raw, "../"), strings.Contains(raw, "/"):
		path = filepath.Clean(filepath.Join(workspaceCwd, raw))
	default:
		return "", false
	}
	if !pathWithinWorkspace(path, workspaceCwd) {
		return "", false
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return "", false
	}
	return path, true
}

func repairMalformedWorkspacePath(path, workspaceCwd string) (string, bool) {
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

func sanitizeLocalMarkdownLinks(text, workspaceCwd string) string {
	if strings.TrimSpace(text) == "" {
		return text
	}
	return markdownLinkFullRe.ReplaceAllStringFunc(text, func(match string) string {
		parts := markdownLinkFullRe.FindStringSubmatch(match)
		if len(parts) != 3 {
			return match
		}
		label := strings.TrimSpace(parts[1])
		href := strings.TrimSpace(parts[2])
		if path, ok := normalizeReferencedPath(href, workspaceCwd); ok {
			return "`" + filepath.Base(path) + "`"
		}
		if fixed := recoverFilenameFromMalformedLabel(label); fixed != "" {
			return "`" + fixed + "`"
		}
		return "`" + label + "`"
	})
}

func recoverFilenameFromMalformedLabel(label string) string {
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
		if !isAlphaNum(ext) || !isFileNameLike(name) {
			continue
		}
		return name + "." + ext
	}
	return ""
}

func isAlphaNum(s string) bool {
	for _, r := range s {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') {
			return false
		}
	}
	return s != ""
}

func isFileNameLike(s string) bool {
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

func trimLineReferenceSuffix(path string) string {
	match := lineSuffixRe.FindStringSubmatch(path)
	if len(match) == 2 {
		return match[1]
	}
	return path
}

func pathWithinWorkspace(path, workspaceCwd string) bool {
	workspaceCwd = filepath.Clean(workspaceCwd)
	path = filepath.Clean(path)
	rel, err := filepath.Rel(workspaceCwd, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}
