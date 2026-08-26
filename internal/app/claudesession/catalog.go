// Package claudesession provides Claude session JSONL parsing, listing,
// and history extraction functions extracted from the app package.
package claudesession

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	appruntime "feidex/internal/app/runtime"
	appworkspace "feidex/internal/app/workspace"
	"feidex/internal/codexrpc"
	"feidex/internal/config"
)

const (
	SessionListLimit           = 8
	sessionMetaReadBytes int64 = 64 * 1024
	ProjectDirNameMax          = 80
	TitleLimit                 = 48
	PreviewLimit               = 72
)

// ListSessions returns the most recent Claude sessions for the given workspace.
func ListSessions(_ string, ws *config.Workspace, includeAll bool) ([]codexrpc.ThreadListEntry, error) {
	if ws == nil {
		return nil, fmt.Errorf("workspace not found")
	}
	dirs, strictWorkspaceFilter, err := SessionProjectDirs(strings.TrimSpace(ws.Cwd), includeAll)
	if err != nil {
		return nil, err
	}
	items, err := ScanSessionEntries(dirs, strings.TrimSpace(ws.Cwd), strictWorkspaceFilter)
	if err != nil {
		return nil, err
	}
	appworkspace.SortThreadsByUpdated(items)
	if len(items) > SessionListLimit {
		items = items[:SessionListLimit]
	}
	return items, nil
}

// FindSessionEntry looks up a single Claude session by ID across all project dirs.
func FindSessionEntry(sessionID string) (*codexrpc.ThreadListEntry, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, nil
	}
	dirs, _, err := SessionProjectDirs("", true)
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
			meta, err := ReadSessionListMeta(filePath)
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

// ScanSessionEntries reads all JSONL session files from the given directories.
func ScanSessionEntries(dirs []string, workspaceCwd string, strictWorkspaceFilter bool) ([]codexrpc.ThreadListEntry, error) {
	items := make([]codexrpc.ThreadListEntry, 0, SessionListLimit)
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
			meta, err := ReadSessionListMeta(filepath.Join(dir, entry.Name()))
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

// SessionProjectDirs returns the Claude project directories relevant to the workspace.
func SessionProjectDirs(workspaceCwd string, includeAll bool) ([]string, bool, error) {
	projectsDir := ProjectsDir()
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
		specific = filepath.Join(projectsDir, SanitizeProjectDirName(workspaceCwd))
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

// ProjectsDir returns the path to the Claude projects directory.
func ProjectsDir() string {
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

// SanitizeProjectDirName converts a path into a safe directory name.
func SanitizeProjectDirName(value string) string {
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
	if len(out) <= ProjectDirNameMax {
		return out
	}
	return out[:ProjectDirNameMax] + "-" + SimpleProjectPathHash(value)
}

// SimpleProjectPathHash computes a simple hash for path deduplication.
func SimpleProjectPathHash(value string) string {
	hash := 0
	for _, r := range value {
		hash = ((hash << 5) - hash) + int(r)
	}
	if hash < 0 {
		hash = -hash
	}
	return strconv.FormatInt(int64(hash), 36)
}

// ReadSessionListMeta reads session metadata from a JSONL file's head and tail.
func ReadSessionListMeta(filePath string) (appruntime.ClaudeSessionListMeta, error) {
	stat, err := os.Stat(filePath)
	if err != nil {
		return appruntime.ClaudeSessionListMeta{}, err
	}
	head, tail, err := ReadSessionHeadTail(filePath, stat.Size())
	if err != nil {
		return appruntime.ClaudeSessionListMeta{}, err
	}
	meta := appruntime.ClaudeSessionListMeta{
		ID:        strings.TrimSuffix(filepath.Base(filePath), filepath.Ext(filePath)),
		UpdatedAt: stat.ModTime(),
	}
	ParseSessionChunkReverse(tail, func(entry map[string]any) bool {
		if meta.Cwd == "" {
			meta.Cwd = strings.TrimSpace(StringValue(entry["cwd"]))
		}
		if meta.Title == "" {
			meta.Title = SessionEntryTitle(entry)
		}
		if meta.Preview == "" {
			meta.Preview = SessionEntryPreview(entry)
		}
		return meta.Cwd != "" && meta.Title != "" && meta.Preview != ""
	})
	ParseSessionChunkForward(head, func(entry map[string]any) bool {
		if meta.Cwd == "" {
			meta.Cwd = strings.TrimSpace(StringValue(entry["cwd"]))
		}
		if meta.Title == "" {
			meta.Title = SessionEntryTitle(entry)
		}
		if meta.Preview == "" {
			meta.Preview = SessionEntryPreview(entry)
		}
		return meta.Cwd != "" && meta.Title != "" && meta.Preview != ""
	})
	meta.Title = NormalizeSnippet(firstNonEmpty(meta.Title, meta.Preview, meta.ID), TitleLimit)
	meta.Preview = NormalizeSnippet(meta.Preview, PreviewLimit)
	if meta.Preview == meta.Title {
		meta.Preview = ""
	}
	return meta, nil
}

// ReadSessionHeadTail reads the first and last chunks of a file.
func ReadSessionHeadTail(filePath string, size int64) (string, string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", "", err
	}
	defer file.Close()

	headSize := size
	if headSize > sessionMetaReadBytes {
		headSize = sessionMetaReadBytes
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
	if tailSize > sessionMetaReadBytes {
		tailSize = sessionMetaReadBytes
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

// ParseSessionChunkForward iterates JSONL lines forward, calling fn for each entry.
func ParseSessionChunkForward(chunk string, fn func(map[string]any) bool) {
	lines := strings.Split(chunk, "\n")
	for _, line := range lines {
		entry, ok := ParseSessionLine(line)
		if !ok {
			continue
		}
		if fn(entry) {
			return
		}
	}
}

// ParseSessionChunkReverse iterates JSONL lines in reverse, calling fn for each entry.
func ParseSessionChunkReverse(chunk string, fn func(map[string]any) bool) {
	lines := strings.Split(chunk, "\n")
	for idx := len(lines) - 1; idx >= 0; idx-- {
		entry, ok := ParseSessionLine(lines[idx])
		if !ok {
			continue
		}
		if fn(entry) {
			return
		}
	}
}

// ParseSessionLine parses a single JSONL line into a map.
func ParseSessionLine(line string) (map[string]any, bool) {
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

// SessionEntryTitle extracts a display title from a session entry.
func SessionEntryTitle(entry map[string]any) string {
	switch strings.TrimSpace(StringValue(entry["type"])) {
	case "custom-title":
		return NormalizeSnippet(StringValue(entry["customTitle"]), TitleLimit)
	case "session-title":
		return NormalizeSnippet(firstNonEmpty(StringValue(entry["title"]), StringValue(entry["name"])), TitleLimit)
	case "ai-title":
		return NormalizeSnippet(StringValue(entry["aiTitle"]), TitleLimit)
	case "summary":
		return NormalizeSnippet(StringValue(entry["summary"]), TitleLimit)
	default:
		return ""
	}
}

// SessionEntryPreview extracts a preview string from a session entry.
func SessionEntryPreview(entry map[string]any) string {
	switch strings.TrimSpace(StringValue(entry["type"])) {
	case "last-prompt":
		return NormalizeSnippet(StringValue(entry["lastPrompt"]), PreviewLimit)
	case "user":
		message, _ := entry["message"].(map[string]any)
		return NormalizeSnippet(MessageText(message["content"]), PreviewLimit)
	default:
		return ""
	}
}

// MessageText extracts text content from a Claude message content field.
func MessageText(content any) string {
	switch value := content.(type) {
	case string:
		return strings.TrimSpace(value)
	case []any:
		parts := make([]string, 0, len(value))
		for _, blockValue := range value {
			block, _ := blockValue.(map[string]any)
			if strings.TrimSpace(StringValue(block["type"])) != "text" {
				continue
			}
			text := strings.TrimSpace(StringValue(block["text"]))
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

// NormalizeSnippet normalizes and truncates a text snippet to the given limit.
func NormalizeSnippet(value string, limit int) string {
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	if value == "" {
		return ""
	}
	if limit > 0 && len(value) > limit {
		value = strings.TrimSpace(value[:limit]) + "..."
	}
	return value
}

// StringValue performs a type assertion to string, returning "" on failure.
func StringValue(value any) string {
	text, _ := value.(string)
	return text
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func sameWorkspaceCWD(a, b string) bool {
	a = strings.TrimSpace(a)
	b = strings.TrimSpace(b)
	if a == "" || b == "" {
		return false
	}
	return filepath.Clean(a) == filepath.Clean(b)
}
