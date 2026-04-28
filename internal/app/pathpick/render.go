package pathpick

import (
	"fmt"
	"path/filepath"
	"strings"

	appcards "feidex/internal/app/cards"
)

// RenderCard renders the Feishu card for a path picker payload.
func RenderCard(requestID string, payload Payload) (map[string]any, error) {
	payload.Mode = NormalizePathPickerMode(payload.Mode)
	payload.Style = NormalizePathPickerStyle(payload.Style)

	currentPath, err := ResolvePathPickerPath(payload.RootPath, payload.CurrentPath)
	if err != nil {
		return nil, err
	}
	payload.CurrentPath = currentPath
	if strings.TrimSpace(payload.SelectedPath) != "" {
		selectedPath, err := ResolvePathPickerPath(payload.RootPath, payload.SelectedPath)
		if err == nil {
			payload.SelectedPath = selectedPath
		} else {
			payload.SelectedPath = ""
		}
	}

	entries, total, hiddenFiles, _, err := ListPathPickerEntries(payload)
	if err != nil {
		return nil, err
	}

	title := "路径选择器"
	if payload.Mode == ModeDirectory {
		title += " · 目录"
	} else {
		title += " · 文件"
	}
	card := appcards.NewMarkdownBodyCard(title, "blue")
	lines := []string{
		"浏览根目录: `" + payload.RootPath + "`",
		"当前目录: `" + payload.CurrentPath + "`",
	}
	if strings.TrimSpace(payload.SelectedPath) != "" {
		lines = append(lines, "已选择: `"+payload.SelectedPath+"`")
	}
	lines = append(lines, fmt.Sprintf("当前目录条目: `%d`", total))
	if payload.Mode == ModeDirectory && hiddenFiles > 0 {
		lines = append(lines, fmt.Sprintf("已隐藏文件: `%d`", hiddenFiles))
	}
	appcards.AppendMarkdownBodyCardElement(card, map[string]any{
		"tag":     "markdown",
		"content": strings.Join(lines, "\n"),
	})
	appcards.AppendMarkdownBodyCardElement(card, BuildDropdownElement(requestID, payload, entries))
	if len(entries) == 0 {
		appcards.AppendMarkdownBodyCardElement(card, map[string]any{
			"tag":     "markdown",
			"content": "当前目录下没有可显示的条目。",
		})
	}
	appcards.AppendMarkdownBodyCardElement(card, BuildFooterElement(requestID, payload))
	return card, nil
}

// SelectedPathForConfirm resolves the path that should be confirmed for the
// current picker state.
func SelectedPathForConfirm(payload Payload) (string, error) {
	selectedPath := payload.SelectedPath
	if payload.Mode == ModeDirectory {
		selectedPath = payload.CurrentPath
	}
	return ResolvePathPickerPath(payload.RootPath, selectedPath)
}

// ParentPath returns the parent directory for the current picker path.
func ParentPath(payload Payload) string {
	if filepath.Clean(payload.CurrentPath) == filepath.Clean(payload.RootPath) {
		return payload.CurrentPath
	}
	return filepath.Dir(payload.CurrentPath)
}
