package app

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	appworkspace "feidex/internal/app/workspace"
	"feidex/internal/codexrpc"
	"feidex/internal/config"
)

const (
	claudeSessionListLimit               = 8
	claudeSessionMetaReadBytes     int64 = 64 * 1024
	claudeSessionProjectDirNameMax       = 80
	claudeSessionTitleLimit              = 48
	claudeSessionPreviewLimit            = 72
)


func listClaudeSessions(_ string, ws *config.Workspace, includeAll bool) ([]codexrpc.ThreadListEntry, error) {
	if ws == nil {
		return nil, fmt.Errorf("workspace not found")
	}
	dirs, strictWorkspaceFilter, err := claudeSessionProjectDirs(strings.TrimSpace(ws.Cwd), includeAll)
	if err != nil {
		return nil, err
	}
	items, err := scanClaudeSessionEntries(dirs, strings.TrimSpace(ws.Cwd), strictWorkspaceFilter)
	if err != nil {
		return nil, err
	}
	appworkspace.SortThreadsByUpdated(items)
	if len(items) > claudeSessionListLimit {
		items = items[:claudeSessionListLimit]
	}
	return items, nil
}

func findClaudeSessionEntry(sessionID string) (*codexrpc.ThreadListEntry, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, nil
	}
	dirs, _, err := claudeSessionProjectDirs("", true)
	if err != nil {
		return nil, err
	}
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".jsonl") {
				continue
			}
			filePath := filepath.Join(dir, entry.Name())
			meta, err := readClaudeSessionListMeta(filePath)
			if err != nil || strings.TrimSpace(meta.ID) != sessionID {
				continue
			}
			item := codexrpc.ThreadListEntry{
				ID:        meta.ID,
				Name:      meta.Title,
				Preview:   meta.Preview,
				UpdatedAt: meta.UpdatedAt.Unix(),
				Cwd:       meta.Cwd,
			}
			return &item, nil
		}
	}
	return nil, nil
}

func scanClaudeSessionEntries(dirs []string, workspaceCwd string, strictWorkspaceFilter bool) ([]codexrpc.ThreadListEntry, error) {
	items := make([]codexrpc.ThreadListEntry, 0, claudeSessionListLimit)
	seen := map[string]struct{}{}
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".jsonl") {
				continue
			}
			meta, err := readClaudeSessionListMeta(filepath.Join(dir, entry.Name()))
			if err != nil || strings.TrimSpace(meta.ID) == "" {
				continue
			}
			if strictWorkspaceFilter && !sameWorkspaceCWD(meta.Cwd, workspaceCwd) {
				continue
			}
			if _, ok := seen[meta.ID]; ok {
				continue
			}
			seen[meta.ID] = struct{}{}
			items = append(items, codexrpc.ThreadListEntry{
				ID:        meta.ID,
				Name:      meta.Title,
				Preview:   meta.Preview,
				UpdatedAt: meta.UpdatedAt.Unix(),
				Cwd:       meta.Cwd,
			})
		}
	}
	return items, nil
}

func claudeSessionProjectDirs(workspaceCwd string, includeAll bool) ([]string, bool, error) {
	projectsDir := claudeProjectsDir()
	if projectsDir == "" {
		return nil, false, nil
	}
	entries, err := os.ReadDir(projectsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	all := make([]string, 0, len(entries))
	specific := ""
	if strings.TrimSpace(workspaceCwd) != "" {
		specific = filepath.Join(projectsDir, sanitizeClaudeProjectDirName(workspaceCwd))
	}
	specificFound := false
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(projectsDir, entry.Name())
		all = append(all, dir)
		if specific != "" && dir == specific {
			specificFound = true
		}
	}
	if includeAll || strings.TrimSpace(workspaceCwd) == "" {
		return all, false, nil
	}
	if specificFound {
		return []string{specific}, false, nil
	}
	return all, true, nil
}

func claudeProjectsDir() string {
	configDir := strings.TrimSpace(os.Getenv("CLAUDE_CONFIG_DIR"))
	if configDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		configDir = filepath.Join(home, ".claude")
	}
	return filepath.Join(configDir, "projects")
}

func sanitizeClaudeProjectDirName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(value))
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	out := b.String()
	if len(out) <= claudeSessionProjectDirNameMax {
		return out
	}
	return out[:claudeSessionProjectDirNameMax] + "-" + simpleClaudeProjectPathHash(value)
}

func simpleClaudeProjectPathHash(value string) string {
	hash := 0
	for _, r := range value {
		hash = ((hash << 5) - hash) + int(r)
	}
	if hash < 0 {
		hash = -hash
	}
	return strconv.FormatInt(int64(hash), 36)
}

func readClaudeSessionListMeta(filePath string) (claudeSessionListMeta, error) {
	stat, err := os.Stat(filePath)
	if err != nil {
		return claudeSessionListMeta{}, err
	}
	head, tail, err := readClaudeSessionHeadTail(filePath, stat.Size())
	if err != nil {
		return claudeSessionListMeta{}, err
	}
	meta := claudeSessionListMeta{
		ID:        strings.TrimSuffix(filepath.Base(filePath), filepath.Ext(filePath)),
		UpdatedAt: stat.ModTime(),
	}
	parseClaudeSessionChunkReverse(tail, func(entry map[string]any) bool {
		if meta.Cwd == "" {
			meta.Cwd = strings.TrimSpace(claudeStringValue(entry["cwd"]))
		}
		if meta.Title == "" {
			meta.Title = claudeSessionEntryTitle(entry)
		}
		if meta.Preview == "" {
			meta.Preview = claudeSessionEntryPreview(entry)
		}
		return meta.Cwd != "" && meta.Title != "" && meta.Preview != ""
	})
	parseClaudeSessionChunkForward(head, func(entry map[string]any) bool {
		if meta.Cwd == "" {
			meta.Cwd = strings.TrimSpace(claudeStringValue(entry["cwd"]))
		}
		if meta.Title == "" {
			meta.Title = claudeSessionEntryTitle(entry)
		}
		if meta.Preview == "" {
			meta.Preview = claudeSessionEntryPreview(entry)
		}
		return meta.Cwd != "" && meta.Title != "" && meta.Preview != ""
	})
	meta.Title = normalizeClaudeSessionSnippet(firstNonEmpty(meta.Title, meta.Preview, meta.ID), claudeSessionTitleLimit)
	meta.Preview = normalizeClaudeSessionSnippet(meta.Preview, claudeSessionPreviewLimit)
	if meta.Preview == meta.Title {
		meta.Preview = ""
	}
	return meta, nil
}

func readClaudeSessionHeadTail(filePath string, size int64) (string, string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", "", err
	}
	defer file.Close()

	headSize := size
	if headSize > claudeSessionMetaReadBytes {
		headSize = claudeSessionMetaReadBytes
	}
	head := make([]byte, headSize)
	if headSize > 0 {
		n, err := file.Read(head)
		if err != nil {
			return "", "", err
		}
		head = head[:n]
	}

	tailSize := size
	if tailSize > claudeSessionMetaReadBytes {
		tailSize = claudeSessionMetaReadBytes
	}
	tail := make([]byte, tailSize)
	if tailSize > 0 {
		n, err := file.ReadAt(tail, size-tailSize)
		if err != nil && n == 0 {
			return "", "", err
		}
		tail = tail[:n]
	}
	return string(head), string(tail), nil
}

func parseClaudeSessionChunkForward(chunk string, fn func(map[string]any) bool) {
	lines := strings.Split(chunk, "\n")
	for _, line := range lines {
		entry, ok := parseClaudeSessionLine(line)
		if !ok {
			continue
		}
		if fn(entry) {
			return
		}
	}
}

func parseClaudeSessionChunkReverse(chunk string, fn func(map[string]any) bool) {
	lines := strings.Split(chunk, "\n")
	for idx := len(lines) - 1; idx >= 0; idx-- {
		entry, ok := parseClaudeSessionLine(lines[idx])
		if !ok {
			continue
		}
		if fn(entry) {
			return
		}
	}
}

func parseClaudeSessionLine(line string) (map[string]any, bool) {
	line = strings.TrimSpace(line)
	if line == "" {
		return nil, false
	}
	var entry map[string]any
	if err := json.Unmarshal([]byte(line), &entry); err != nil {
		return nil, false
	}
	return entry, true
}

func claudeSessionEntryTitle(entry map[string]any) string {
	switch strings.TrimSpace(claudeStringValue(entry["type"])) {
	case "custom-title":
		return normalizeClaudeSessionSnippet(claudeStringValue(entry["customTitle"]), claudeSessionTitleLimit)
	case "session-title":
		return normalizeClaudeSessionSnippet(firstNonEmpty(claudeStringValue(entry["title"]), claudeStringValue(entry["name"])), claudeSessionTitleLimit)
	case "ai-title":
		return normalizeClaudeSessionSnippet(claudeStringValue(entry["aiTitle"]), claudeSessionTitleLimit)
	case "summary":
		return normalizeClaudeSessionSnippet(claudeStringValue(entry["summary"]), claudeSessionTitleLimit)
	default:
		return ""
	}
}

func claudeSessionEntryPreview(entry map[string]any) string {
	switch strings.TrimSpace(claudeStringValue(entry["type"])) {
	case "last-prompt":
		return normalizeClaudeSessionSnippet(claudeStringValue(entry["lastPrompt"]), claudeSessionPreviewLimit)
	case "user":
		message, _ := entry["message"].(map[string]any)
		return normalizeClaudeSessionSnippet(claudeMessageText(message["content"]), claudeSessionPreviewLimit)
	default:
		return ""
	}
}

func claudeMessageText(content any) string {
	switch value := content.(type) {
	case string:
		return strings.TrimSpace(value)
	case []any:
		parts := make([]string, 0, len(value))
		for _, blockValue := range value {
			block, _ := blockValue.(map[string]any)
			if strings.TrimSpace(claudeStringValue(block["type"])) != "text" {
				continue
			}
			text := strings.TrimSpace(claudeStringValue(block["text"]))
			if text == "" {
				continue
			}
			parts = append(parts, text)
		}
		return strings.TrimSpace(strings.Join(parts, "\n"))
	default:
		return ""
	}
}

func normalizeClaudeSessionSnippet(value string, limit int) string {
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	if value == "" {
		return ""
	}
	if limit > 0 && len(value) > limit {
		value = strings.TrimSpace(value[:limit]) + "..."
	}
	return value
}

func claudeStringValue(value any) string {
	text, _ := value.(string)
	return text
}
