package pathpicker

import (
	appcards "feidex/internal/adapter/feishu/cards"
	domain "feidex/internal/domain/workspace"
	"feidex/internal/feishu"
	"path/filepath"
	"strings"
)

type Payload = domain.PathPickerPayload
type Entry = domain.PathPickerEntry

const ModeDirectory = domain.PathPickerModeDirectory
const ModeFile = domain.PathPickerModeFile
const StyleDropdown = domain.PathPickerStyleDropdown

func RenderEntryLabel(entry Entry) string {
	if entry.IsDir {
		return entry.Name + "/"
	}
	return entry.Name
}

func EncodeOption(entry Entry) string {
	prefix := "file|"
	if entry.IsDir {
		prefix = "dir|"
	}
	return prefix + entry.Path
}

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
