package pathpicker

import (
	"fmt"
	"strings"

	appcards "feidex/internal/adapter/feishu/cards"
	pickerapp "feidex/internal/application/pathpicker"
)

// RenderCard renders the Feishu card for a path picker payload.
func RenderCard(requestID string, view pickerapp.View) map[string]any {
	payload, entries, total, hiddenFiles := view.Payload, view.Entries, view.Total, view.HiddenFiles
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
	return card
}
