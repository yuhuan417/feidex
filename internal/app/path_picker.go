package app

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

const (
	pathPickerKind          = "path_picker"
	pathPickerModeDirectory = "directory"
	pathPickerModeFile      = "file"
	pathPickerStyleDropdown = "dropdown"
)

type pathPickerPayload struct {
	Mode         string `json:"mode"`
	Style        string `json:"style"`
	RootPath     string `json:"root_path"`
	CurrentPath  string `json:"current_path"`
	SelectedPath string `json:"selected_path,omitempty"`
}

type pathPickerEntry struct {
	Name  string
	Path  string
	IsDir bool
}

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

func (a *App) renderPathPickerCard(requestID string, payload pathPickerPayload) (map[string]any, error) {
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

func (a *App) completePathPickerAction(action *feishu.CardAction, actionName string) (*callback.CardActionTriggerResponse, error) {
	requestID, _ := action.ActionValue["request_id"].(string)
	pending := a.store.PendingByID(requestID)
	if pending == nil || (pending.Kind != pathPickerKind && pending.Kind != "workspace_new" && pending.Kind != downloadFilePendingKind) {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "路径选择请求已过期"}}, nil
	}
	if pending.OwnerUserID != "" && pending.OwnerUserID != action.UserID {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "你没有权限处理这个路径选择请求"}}, nil
	}
	var payload pathPickerPayload
	var workspacePayload workspaceNewPayload
	if pending.Kind == "workspace_new" {
		workspacePayload = workspaceNewPayloadFromPending(pending)
		if workspacePayload.Picker == nil {
			return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "目录选择状态已失效"}}, nil
		}
		payload = *workspacePayload.Picker
	} else if err := json.Unmarshal([]byte(pending.PayloadJSON), &payload); err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "路径选择状态损坏"}}, nil
	}

	switch actionName {
	case "path_picker.cancel":
		if pending.Kind == "workspace_new" {
			workspacePayload.Picker = nil
			_ = a.store.UpdatePending(requestID, func(req *state.PendingRequest) { req.PayloadJSON = mustJSON(workspacePayload) })
			return &callback.CardActionTriggerResponse{
				Toast: &callback.Toast{Type: "success", Content: "已返回工作区创建"},
				Card:  rawCard(a.renderWorkspaceNewCard(pending.SessionKey, requestID, workspacePayload)),
			}, nil
		}
		_ = a.store.UpdatePending(requestID, func(req *state.PendingRequest) { req.Status = "resolved" })
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "success", Content: "已取消路径选择"},
			Card:  rawCard(a.feishu.SimpleStatusCard("路径选择已取消", "grey", "本次路径选择已取消。", nil)),
		}, nil
	case "path_picker.up":
		if filepath.Clean(payload.CurrentPath) != filepath.Clean(payload.RootPath) {
			payload.CurrentPath = filepath.Dir(payload.CurrentPath)
		}
		payload.SelectedPath = ""
	case "path_picker.open":
		nextPath, _ := action.ActionValue["path"].(string)
		resolved, err := resolvePathPickerPath(payload.RootPath, nextPath)
		if err != nil {
			return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "目录不可访问"}}, nil
		}
		info, err := os.Stat(resolved)
		if err != nil || !info.IsDir() {
			return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "只能进入目录"}}, nil
		}
		payload.CurrentPath = resolved
		payload.SelectedPath = ""
	case "path_picker.select":
		nextPath, _ := action.ActionValue["path"].(string)
		resolved, err := resolvePathPickerPath(payload.RootPath, nextPath)
		if err != nil {
			return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "文件不可访问"}}, nil
		}
		info, err := os.Stat(resolved)
		if err != nil || info.IsDir() {
			return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "只能选择文件"}}, nil
		}
		payload.SelectedPath = resolved
	case "path_picker.dropdown":
		nextPath, isDir, ok := decodePathPickerOption(action.Option)
		if !ok {
			return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "未收到有效选项"}}, nil
		}
		resolved, err := resolvePathPickerPath(payload.RootPath, nextPath)
		if err != nil {
			return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "路径不可访问"}}, nil
		}
		if isDir {
			payload.CurrentPath = resolved
			payload.SelectedPath = ""
		} else {
			payload.SelectedPath = resolved
		}
	case "path_picker.confirm":
		selectedPath := payload.SelectedPath
		if payload.Mode == pathPickerModeDirectory {
			selectedPath = payload.CurrentPath
		}
		selectedPath, err := resolvePathPickerPath(payload.RootPath, selectedPath)
		if err != nil || strings.TrimSpace(selectedPath) == "" {
			return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "请先选择路径"}}, nil
		}
		info, err := os.Stat(selectedPath)
		if err != nil {
			return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "所选路径不可访问"}}, nil
		}
		if payload.Mode == pathPickerModeDirectory && !info.IsDir() {
			return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "当前模式只能确认目录"}}, nil
		}
		if payload.Mode == pathPickerModeFile && info.IsDir() {
			return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "当前模式只能确认文件"}}, nil
		}
		if pending.Kind == "workspace_new" {
			workspacePayload.SelectedCWD = selectedPath
			workspacePayload.Picker = nil
			_ = a.store.UpdatePending(requestID, func(req *state.PendingRequest) { req.PayloadJSON = mustJSON(workspacePayload) })
			return &callback.CardActionTriggerResponse{
				Toast: &callback.Toast{Type: "success", Content: "已选择目录"},
				Card:  rawCard(a.renderWorkspaceNewCard(pending.SessionKey, requestID, workspacePayload)),
			}, nil
		}
		if pending.Kind == downloadFilePendingKind {
			payload.SelectedPath = selectedPath
			_ = a.store.UpdatePending(requestID, func(req *state.PendingRequest) { req.PayloadJSON = mustJSON(payload) })
			return a.completeDownloadFileConfirm(action, pending, payload, selectedPath)
		}
		_ = a.store.UpdatePending(requestID, func(req *state.PendingRequest) {
			req.Status = "resolved"
			req.PayloadJSON = mustJSON(payload)
		})
		body := "已选择路径：\n`" + selectedPath + "`"
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "success", Content: "已确认路径"},
			Card:  rawCard(a.feishu.SimpleStatusCard("路径已确认", "green", body, nil)),
		}, nil
	default:
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "未知路径选择操作"}}, nil
	}

	if pending.Kind == "workspace_new" {
		workspacePayload.Picker = &payload
		_ = a.store.UpdatePending(requestID, func(req *state.PendingRequest) { req.PayloadJSON = mustJSON(workspacePayload) })
	} else {
		_ = a.store.UpdatePending(requestID, func(req *state.PendingRequest) { req.PayloadJSON = mustJSON(payload) })
	}
	card, err := a.renderPathPickerCard(requestID, payload)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "info", Content: "路径选择器已更新"},
		Card:  rawCard(card),
	}, nil
}
