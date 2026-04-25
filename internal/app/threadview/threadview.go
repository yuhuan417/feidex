// Package threadview provides pure thread-list formatting and filtering
// functions extracted from the app package.
package threadview

import (
	"path/filepath"
	"strings"

	"feidex/internal/app/apputil"
	"feidex/internal/codexrpc"
)

func truncate(s string, n int) string { return apputil.Truncate(s, n) }

func RenderThreadSettingValue(override, fallback string) string {
	override = strings.TrimSpace(override)
	fallback = strings.TrimSpace(fallback)
	if override != "" {
		return "`" + override + "`"
	}
	if fallback != "" {
		return "`" + fallback + "` (follow workspace)"
	}
	return "-"
}

func CurrentThreadLabel(activeThreadName, activeThreadPreview, activeThreadID string) string {
	if strings.TrimSpace(activeThreadName) != "" {
		return truncate(activeThreadName, 32)
	}
	if strings.TrimSpace(activeThreadPreview) != "" {
		return truncate(activeThreadPreview, 32)
	}
	if strings.TrimSpace(activeThreadID) != "" {
		return truncate(activeThreadID, 32)
	}
	return "-"
}

func RenderThreadButtonLabel(name, preview, id string) string {
	switch {
	case strings.TrimSpace(name) != "":
		return truncate(name, 18)
	case strings.TrimSpace(preview) != "":
		return truncate(preview, 18)
	default:
		return truncate(id, 18)
	}
}

func RenderThreadListEntry(name, preview, id string) string {
	base := RenderThreadListEntryBase(name, preview, id)
	shortID := ShortThreadID(id)
	if shortID == "" {
		return base
	}
	return truncate(base, 38) + " [" + shortID + "]"
}

func RenderThreadListEntryBase(name, preview, id string) string {
	switch {
	case strings.TrimSpace(name) != "" && strings.TrimSpace(preview) != "":
		return truncate(name, 18) + " | " + truncate(preview, 36)
	case strings.TrimSpace(name) != "":
		return truncate(name, 48)
	case strings.TrimSpace(preview) != "":
		return truncate(preview, 48)
	default:
		return truncate(id, 48)
	}
}

func ShortThreadID(id string) string {
	id = strings.TrimSpace(id)
	if len(id) <= 8 {
		return id
	}
	return id[:8]
}

func FilterThreadsByWorkspaceCWD(items []codexrpc.ThreadListEntry, workspaceCWD string) []codexrpc.ThreadListEntry {
	workspaceCWD = strings.TrimSpace(workspaceCWD)
	if workspaceCWD == "" || len(items) == 0 {
		return items
	}
	filtered := make([]codexrpc.ThreadListEntry, 0, len(items))
	for _, item := range items {
		if SameWorkspaceCWD(item.Cwd, workspaceCWD) {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func SameWorkspaceCWD(a, b string) bool {
	a = strings.TrimSpace(a)
	b = strings.TrimSpace(b)
	if a == "" || b == "" {
		return false
	}
	return filepath.Clean(a) == filepath.Clean(b)
}
