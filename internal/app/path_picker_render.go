package app

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	appworkspace "feidex/internal/app/workspace"
	"feidex/internal/config"
	"feidex/internal/feishu"
)

const (
	pathPickerKind          = appworkspace.PathPickerKind
	pathPickerModeDirectory = appworkspace.PathPickerModeDirectory
	pathPickerModeFile      = appworkspace.PathPickerModeFile
	pathPickerStyleDropdown = appworkspace.PathPickerStyleDropdown
)

type pathPickerPayload = appworkspace.PathPickerPayload
type pathPickerEntry = appworkspace.PathPickerEntry

func normalizePathPickerMode(mode string) string {
	switch strings.TrimSpace(mode) {
	case pathPickerModeFile:
		return pathPickerModeFile
	default:
		return pathPickerModeDirectory
	}
}

func normalizePathPickerStyle(style string) string {
	return pathPickerStyleDropdown
}

func resolvePathPickerRoot(ws *config.Workspace) (string, error) {
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

func resolvePathPickerPath(rootPath, candidate string) (string, error) {
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
	if !pathPickerWithinRoot(rootPath, real) {
		return "", fmt.Errorf("path %q is outside workspace root", real)
	}
	return real, nil
}

func pathPickerWithinRoot(rootPath, candidate string) bool {
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

func (s workspaceRenderService) renderPathPickerCard(requestID string, payload pathPickerPayload) (map[string]any, error) {
	payload.Mode = normalizePathPickerMode(payload.Mode)
	payload.Style = normalizePathPickerStyle(payload.Style)
	currentPath, err := resolvePathPickerPath(payload.RootPath, payload.CurrentPath)
	if err != nil {
		return nil, err
	}
	payload.CurrentPath = currentPath
	if strings.TrimSpace(payload.SelectedPath) != "" {
		selectedPath, err := resolvePathPickerPath(payload.RootPath, payload.SelectedPath)
		if err == nil {
			payload.SelectedPath = selectedPath
		} else {
			payload.SelectedPath = ""
		}
	}

	entries, total, hiddenFiles, _, err := listPathPickerEntries(payload)
	if err != nil {
		return nil, err
	}
	title := "路径选择器"
	if payload.Mode == pathPickerModeDirectory {
		title += " · 目录"
	} else {
		title += " · 文件"
	}
	card := newMarkdownBodyCard(title, "blue")
	lines := []string{
		"浏览根目录: `" + payload.RootPath + "`",
		"当前目录: `" + payload.CurrentPath + "`",
	}
	if strings.TrimSpace(payload.SelectedPath) != "" {
		lines = append(lines, "已选择: `"+payload.SelectedPath+"`")
	}
	lines = append(lines, fmt.Sprintf("当前目录条目: `%d`", total))
	if payload.Mode == pathPickerModeDirectory && hiddenFiles > 0 {
		lines = append(lines, fmt.Sprintf("已隐藏文件: `%d`", hiddenFiles))
	}
	appendMarkdownBodyCardElement(card, map[string]any{
		"tag":     "markdown",
		"content": strings.Join(lines, "\n"),
	})

	appendMarkdownBodyCardElement(card, buildPathPickerDropdownElement(requestID, payload, entries))
	if len(entries) == 0 {
		appendMarkdownBodyCardElement(card, map[string]any{
			"tag":     "markdown",
			"content": "当前目录下没有可显示的条目。",
		})
	}
	appendMarkdownBodyCardElement(card, buildPathPickerFooterElement(requestID, payload))
	return card, nil
}

func buildPathPickerDropdownElement(requestID string, payload pathPickerPayload, entries []pathPickerEntry) map[string]any {
	placeholder := "选择条目"
	if payload.Mode == pathPickerModeDirectory {
		placeholder = "选择子目录并进入"
	}
	options := make([]map[string]any, 0, len(entries))
	initialOption := ""
	if filepath.Clean(payload.CurrentPath) != filepath.Clean(payload.RootPath) {
		parentPath := filepath.Dir(payload.CurrentPath)
		options = append(options, map[string]any{
			"text":  map[string]any{"tag": "plain_text", "content": "../"},
			"value": encodePathPickerOption(pathPickerEntry{Name: "..", Path: parentPath, IsDir: true}),
		})
	}
	for _, entry := range entries {
		value := encodePathPickerOption(entry)
		options = append(options, map[string]any{
			"text":  map[string]any{"tag": "plain_text", "content": renderPathPickerEntryLabel(entry)},
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

func buildPathPickerFooterElement(requestID string, payload pathPickerPayload) map[string]any {
	buttons := []feishu.Button{
		{
			Text: "上一级",
			Type: "default",
			Value: map[string]any{
				"action":     "path_picker.up",
				"request_id": requestID,
			},
		},
	}
	confirmType := "default"
	if payload.Mode == pathPickerModeDirectory || strings.TrimSpace(payload.SelectedPath) != "" {
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
	return buildMarkdownBodyCardActionElement(buttons)
}

func listPathPickerEntries(payload pathPickerPayload) ([]pathPickerEntry, int, int, int, error) {
	items, err := os.ReadDir(payload.CurrentPath)
	if err != nil {
		return nil, 0, 0, 0, err
	}
	entries := make([]pathPickerEntry, 0, len(items))
	hiddenFiles := 0
	for _, item := range items {
		name := strings.TrimSpace(item.Name())
		if name == "" {
			continue
		}
		entry := pathPickerEntry{
			Name:  name,
			Path:  filepath.Join(payload.CurrentPath, name),
			IsDir: item.IsDir(),
		}
		if payload.Mode == pathPickerModeDirectory && !entry.IsDir {
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

func renderPathPickerEntryLabel(entry pathPickerEntry) string {
	if entry.IsDir {
		return entry.Name + "/"
	}
	return entry.Name
}

func encodePathPickerOption(entry pathPickerEntry) string {
	prefix := "file|"
	if entry.IsDir {
		prefix = "dir|"
	}
	return prefix + entry.Path
}

func decodePathPickerOption(raw string) (path string, isDir bool, ok bool) {
	switch {
	case strings.HasPrefix(raw, "dir|"):
		return strings.TrimSpace(strings.TrimPrefix(raw, "dir|")), true, true
	case strings.HasPrefix(raw, "file|"):
		return strings.TrimSpace(strings.TrimPrefix(raw, "file|")), false, true
	default:
		return "", false, false
	}
}
