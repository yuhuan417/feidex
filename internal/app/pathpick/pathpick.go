// Package pathpick provides pure formatting, validation, and rendering
// functions for the path picker UI in Feishu cards.
package pathpick

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	appworkspace "feidex/internal/app/workspace"
	appcards "feidex/internal/app/cards"
	"feidex/internal/config"
	"feidex/internal/feishu"
)

// Re-export workspace constants and types for convenience.
const (
	Kind          = appworkspace.PathPickerKind
	ModeDirectory = appworkspace.PathPickerModeDirectory
	ModeFile      = appworkspace.PathPickerModeFile
	StyleDropdown = appworkspace.PathPickerStyleDropdown
)

// Payload and Entry are type aliases for the workspace types.
type Payload = appworkspace.PathPickerPayload
type Entry = appworkspace.PathPickerEntry

// NormalizePathPickerMode normalizes a mode string to a valid value.
// Defaults to directory mode.
func NormalizePathPickerMode(mode string) string {
	switch strings.TrimSpace(mode) {
	case ModeFile:
		return ModeFile
	default:
		return ModeDirectory
	}
}

// NormalizePathPickerStyle normalizes a style string.
// Always returns the dropdown style.
func NormalizePathPickerStyle(style string) string {
	return StyleDropdown
}

// ResolvePathPickerRoot resolves the workspace root directory from a
// Workspace config. Returns an error if the workspace is nil or its cwd
// is not an accessible directory.
func ResolvePathPickerRoot(ws *config.Workspace) (string, error) {
	if ws == nil {
		return "", fmt.Errorf("current workspace not found")
	}
	root := strings.TrimSpace(ws.Cwd)
	if root == "" {
		return "", fmt.Errorf("workspace %q cwd is empty", ws.ID)
	}
	if !filepath.IsAbs(root) {
		abs, err := filepath.Abs(root)
		if err != nil {
			return "", err
		}
		root = abs
	}
	root = filepath.Clean(root)
	if real, err := filepath.EvalSymlinks(root); err == nil {
		root = filepath.Clean(real)
	}
	info, err := os.Stat(root)
	if err != nil {
		return "", fmt.Errorf("workspace cwd %q is not accessible: %w", root, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("workspace cwd %q is not a directory", root)
	}
	return root, nil
}

// ResolvePathPickerPath resolves a candidate path against the workspace root.
// The resolved path must be within rootPath.
func ResolvePathPickerPath(rootPath, candidate string) (string, error) {
	rootPath = filepath.Clean(strings.TrimSpace(rootPath))
	candidate = strings.TrimSpace(candidate)
	if candidate == "" {
		candidate = rootPath
	}
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(rootPath, candidate)
	}
	candidate = filepath.Clean(candidate)
	real, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return "", err
	}
	real = filepath.Clean(real)
	if !WithinRoot(rootPath, real) {
		return "", fmt.Errorf("path %q is outside workspace root", real)
	}
	return real, nil
}

// WithinRoot checks whether candidate is within rootPath.
func WithinRoot(rootPath, candidate string) bool {
	rootPath = filepath.Clean(strings.TrimSpace(rootPath))
	candidate = filepath.Clean(strings.TrimSpace(candidate))
	if rootPath == "" || candidate == "" {
		return false
	}
	if rootPath == string(filepath.Separator) {
		return filepath.IsAbs(candidate)
	}
	if candidate == rootPath {
		return true
	}
	return strings.HasPrefix(candidate, rootPath+string(filepath.Separator))
}

// ListPathPickerEntries lists directory entries for the path picker,
// filtering by mode and sorting directories first, then alphabetically.
func ListPathPickerEntries(payload Payload) ([]Entry, int, int, int, error) {
	items, err := os.ReadDir(payload.CurrentPath)
	if err != nil {
		return nil, 0, 0, 0, err
	}
	entries := make([]Entry, 0, len(items))
	hiddenFiles := 0
	for _, item := range items {
		name := strings.TrimSpace(item.Name())
		if name == "" {
			continue
		}
		entry := Entry{
			Name:  name,
			Path:  filepath.Join(payload.CurrentPath, name),
			IsDir: item.IsDir(),
		}
		if payload.Mode == ModeDirectory && !entry.IsDir {
			hiddenFiles++
			continue
		}
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].IsDir != entries[j].IsDir {
			return entries[i].IsDir
		}
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})
	return entries, len(entries), hiddenFiles, 0, nil
}

// RenderEntryLabel renders an entry label for display in the dropdown.
// Directories get a trailing "/".
func RenderEntryLabel(entry Entry) string {
	if entry.IsDir {
		return entry.Name + "/"
	}
	return entry.Name
}

// EncodeOption encodes a path picker entry as a select option value.
// Prefixes with "dir|" or "file|" depending on entry type.
func EncodeOption(entry Entry) string {
	prefix := "file|"
	if entry.IsDir {
		prefix = "dir|"
	}
	return prefix + entry.Path
}

// DecodeOption decodes a path picker select option value back into
// path and isDir fields.
func DecodeOption(raw string) (path string, isDir bool, ok bool) {
	switch {
	case strings.HasPrefix(raw, "dir|"):
		return strings.TrimSpace(strings.TrimPrefix(raw, "dir|")), true, true
	case strings.HasPrefix(raw, "file|"):
		return strings.TrimSpace(strings.TrimPrefix(raw, "file|")), false, true
	default:
		return "", false, false
	}
}

// BuildDropdownElement builds the Feishu card select_static element for
// the path picker dropdown.
func BuildDropdownElement(requestID string, payload Payload, entries []Entry) map[string]any {
	placeholder := "选择条目"
	if payload.Mode == ModeDirectory {
		placeholder = "选择子目录并进入"
	}
	options := make([]map[string]any, 0, len(entries))
	initialOption := ""
	if filepath.Clean(payload.CurrentPath) != filepath.Clean(payload.RootPath) {
		parentPath := filepath.Dir(payload.CurrentPath)
		options = append(options, map[string]any{
			"text":  map[string]any{"tag": "plain_text", "content": "../"},
			"value": EncodeOption(Entry{Name: "..", Path: parentPath, IsDir: true}),
		})
	}
	for _, entry := range entries {
		value := EncodeOption(entry)
		options = append(options, map[string]any{
			"text":  map[string]any{"tag": "plain_text", "content": RenderEntryLabel(entry)},
			"value": value,
		})
		if filepath.Clean(payload.SelectedPath) == filepath.Clean(entry.Path) {
			initialOption = value
		}
	}
	element := map[string]any{
		"tag":         "select_static",
		"placeholder": map[string]any{"tag": "plain_text", "content": placeholder},
		"options":     options,
		"name":        "path_picker_select",
		"behaviors": []map[string]any{{
			"type": "callback",
			"value": map[string]any{
				"action":     "path_picker.dropdown",
				"request_id": requestID,
			},
		}},
	}
	if initialOption != "" {
		element["initial_option"] = initialOption
	}
	return element
}

// BuildFooterElement builds the footer action element with up/confirm/cancel
// buttons for the path picker card.
func BuildFooterElement(requestID string, payload Payload) map[string]any {
	buttons := []feishu.Button{
		{
			Text: "上级目录",
			Type: "default",
			Value: map[string]any{
				"action":     "path_picker.up",
				"request_id": requestID,
			},
		},
	}
	confirmType := "default"
	if payload.Mode == ModeDirectory || strings.TrimSpace(payload.SelectedPath) != "" {
		confirmType = "primary"
	}
	buttons = append(buttons,
		feishu.Button{
			Text: "确认",
			Type: confirmType,
			Value: map[string]any{
				"action":     "path_picker.confirm",
				"request_id": requestID,
			},
		},
		feishu.Button{
			Text: "取消",
			Type: "default",
			Value: map[string]any{
				"action":     "path_picker.cancel",
				"request_id": requestID,
			},
		},
	)
	return appcards.BuildMarkdownBodyCardActionElement(buttons)
}
