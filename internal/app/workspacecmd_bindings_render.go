package app

import (
	"fmt"
	"path/filepath"
	"strings"

	appcards "feidex/internal/app/cards"
	apppathpick "feidex/internal/app/pathpick"
	appworkspace "feidex/internal/app/workspace"
	appworkspacecmd "feidex/internal/app/workspacecmd"
	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

// renderPathPickerCardDirect implements the full path picker card rendering
// without going through the RenderService callback (which would be recursive).
func renderPathPickerCardDirect(requestID string, payload appworkspace.PathPickerPayload) (map[string]any, error) {
	mode := apppathpick.NormalizePathPickerMode(payload.Mode)
	_ = apppathpick.NormalizePathPickerStyle(payload.Style)
	payload.Mode = mode

	currentPath, err := apppathpick.ResolvePathPickerPath(payload.RootPath, payload.CurrentPath)
	if err != nil {
		return nil, err
	}
	payload.CurrentPath = currentPath
	if strings.TrimSpace(payload.SelectedPath) != "" {
		selectedPath, err := apppathpick.ResolvePathPickerPath(payload.RootPath, payload.SelectedPath)
		if err == nil {
			payload.SelectedPath = selectedPath
		} else {
			payload.SelectedPath = ""
		}
	}

	entries, total, hiddenFiles, _, err := apppathpick.ListPathPickerEntries(payload)
	if err != nil {
		return nil, err
	}

	title := "路径选择器"
	if mode == appworkspace.PathPickerModeDirectory {
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
	if mode == appworkspace.PathPickerModeDirectory && hiddenFiles > 0 {
		lines = append(lines, fmt.Sprintf("已隐藏文件: `%d`", hiddenFiles))
	}
	appcards.AppendMarkdownBodyCardElement(card, map[string]any{
		"tag":     "markdown",
		"content": strings.Join(lines, "\n"),
	})

	placeholder := "选择条目"
	if mode == appworkspace.PathPickerModeDirectory {
		placeholder = "选择子目录并进入"
	}
	options := make([]map[string]any, 0, len(entries))
	initialOption := ""
	if filepath.Clean(payload.CurrentPath) != filepath.Clean(payload.RootPath) {
		parentPath := filepath.Dir(payload.CurrentPath)
		options = append(options, map[string]any{
			"text":  map[string]any{"tag": "plain_text", "content": "../"},
			"value": apppathpick.EncodeOption(appworkspace.PathPickerEntry{Name: "..", Path: parentPath, IsDir: true}),
		})
	}
	for _, entry := range entries {
		value := apppathpick.EncodeOption(entry)
		options = append(options, map[string]any{
			"text":  map[string]any{"tag": "plain_text", "content": apppathpick.RenderEntryLabel(entry)},
			"value": value,
		})
		if filepath.Clean(payload.SelectedPath) == filepath.Clean(entry.Path) {
			initialOption = value
		}
	}
	dropdown := map[string]any{
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
		dropdown["initial_option"] = initialOption
	}
	appcards.AppendMarkdownBodyCardElement(card, dropdown)

	if len(entries) == 0 {
		appcards.AppendMarkdownBodyCardElement(card, map[string]any{
			"tag":     "markdown",
			"content": "当前目录下没有可显示的条目。",
		})
	}

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
	if mode == appworkspace.PathPickerModeDirectory || strings.TrimSpace(payload.SelectedPath) != "" {
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
	appcards.AppendMarkdownBodyCardElement(card, appcards.BuildMarkdownBodyCardActionElement(buttons))

	return card, nil
}

func newWorkspaceRenderServiceInner(a *App) *appworkspacecmd.RenderService {
	bcfg := newBackendConfigurationService(a)
	return appworkspacecmd.NewRenderService(appworkspacecmd.RenderDeps{
		App: a,
		State: appworkspacecmd.StateDeps{
			GetSession: func(key string) *state.Session { return a.State().Session(key) },
		},
		Backend: appworkspacecmd.BackendConfigDeps{
			BackendWorkspaceSummaryLines:  bcfg.appendBackendWorkspaceSummaryLines,
			BackendWorkspaceConfigButtons: bcfg.backendWorkspaceConfigButtons,
		},
		Formatting: appworkspacecmd.FormattingDeps{
			FormatMenuBody: menuCardBody,
		},
		PathPicker: appworkspacecmd.PathPickerDeps{
			RenderPathPickerCard: func(requestID string, payload appworkspacecmd.PathPickerPayload) (map[string]any, error) {
				return renderPathPickerCardDirect(requestID, payload)
			},
		},
		Management: appworkspacecmd.RenderManagementDeps{
			DefaultWorkspaceCloneRoot: func(*config.Workspace) string { return "/" },
			DefaultWorkspaceCloneParent: func(ws *config.Workspace) string {
				if ws != nil && strings.TrimSpace(ws.Cwd) != "" {
					return filepath.Dir(strings.TrimSpace(ws.Cwd))
				}
				if cp := strings.TrimSpace(a.ConfigPath()); cp != "" {
					return filepath.Dir(cp)
				}
				return "."
			},
		},
	})
}
