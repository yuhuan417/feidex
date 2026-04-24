package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"feidex/internal/feishu"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

func (s workspaceActionService) completePathPickerAction(action *feishu.CardAction, actionName string) (*callback.CardActionTriggerResponse, error) {
	appState := appState(s.app)
	requestID, _ := action.ActionValue["request_id"].(string)
	pending := appState.pending(requestID)
	if pending == nil || (pending.Kind != pathPickerKind && pending.Kind != "workspace_new" && pending.Kind != "workspace_clone" && pending.Kind != downloadFilePendingKind && pending.Kind != upgradeLocalBinaryPendingKind) {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "路径选择请求已过期"}}, nil
	}
	if pending.OwnerUserID != "" && pending.OwnerUserID != action.UserID {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "你没有权限处理这个路径选择请求"}}, nil
	}
	var payload pathPickerPayload
	var workspacePayload workspaceNewPayload
	var clonePayload workspaceClonePayload
	if pending.Kind == "workspace_new" {
		workspacePayload = workspaceNewPayloadFromPending(pending)
		if workspacePayload.Picker == nil {
			return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "目录选择状态已失效"}}, nil
		}
		payload = *workspacePayload.Picker
	} else if pending.Kind == "workspace_clone" {
		clonePayload = workspaceClonePayloadFromPending(pending)
		if clonePayload.Picker == nil {
			return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "父目录选择状态已失效"}}, nil
		}
		payload = *clonePayload.Picker
	} else if err := json.Unmarshal([]byte(pending.PayloadJSON), &payload); err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "路径选择状态损坏"}}, nil
	}

	switch actionName {
	case "path_picker.cancel":
		if pending.Kind == "workspace_new" {
			workspacePayload.Picker = nil
			_ = appState.updatePending(requestID, func(req *state.PendingRequest) { req.PayloadJSON = mustJSON(workspacePayload) })
			return &callback.CardActionTriggerResponse{
				Toast: &callback.Toast{Type: "success", Content: "已返回工作区创建"},
				Card:  rawCard(newWorkspaceRenderService(s.app).renderWorkspaceNewCard(pending.SessionKey, requestID, workspacePayload)),
			}, nil
		}
		if pending.Kind == "workspace_clone" {
			clonePayload.Picker = nil
			_ = appState.updatePending(requestID, func(req *state.PendingRequest) { req.PayloadJSON = mustJSON(clonePayload) })
			return &callback.CardActionTriggerResponse{
				Toast: &callback.Toast{Type: "success", Content: "已返回从仓库创建"},
				Card:  rawCard(newWorkspaceRenderService(s.app).renderWorkspaceCloneCard(pending.SessionKey, requestID, clonePayload)),
			}, nil
		}
		_ = appState.updatePending(requestID, func(req *state.PendingRequest) { req.Status = "resolved" })
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "success", Content: "已取消路径选择"},
			Card:  rawCard(s.app.feishu.SimpleStatusCard("路径选择已取消", "grey", "本次路径选择已取消。", nil)),
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
			workspacePayload = updateWorkspaceNewSuggestedID(workspacePayload, selectedPath)
			workspacePayload.Picker = nil
			_ = appState.updatePending(requestID, func(req *state.PendingRequest) { req.PayloadJSON = mustJSON(workspacePayload) })
			return &callback.CardActionTriggerResponse{
				Toast: &callback.Toast{Type: "success", Content: "已选择目录"},
				Card:  rawCard(newWorkspaceRenderService(s.app).renderWorkspaceNewCard(pending.SessionKey, requestID, workspacePayload)),
			}, nil
		}
		if pending.Kind == "workspace_clone" {
			clonePayload.SelectedParentDir = selectedPath
			clonePayload.Picker = nil
			_ = appState.updatePending(requestID, func(req *state.PendingRequest) { req.PayloadJSON = mustJSON(clonePayload) })
			return &callback.CardActionTriggerResponse{
				Toast: &callback.Toast{Type: "success", Content: "已选择父目录"},
				Card:  rawCard(newWorkspaceRenderService(s.app).renderWorkspaceCloneCard(pending.SessionKey, requestID, clonePayload)),
			}, nil
		}
		if pending.Kind == downloadFilePendingKind {
			payload.SelectedPath = selectedPath
			_ = appState.updatePending(requestID, func(req *state.PendingRequest) { req.PayloadJSON = mustJSON(payload) })
			return completeDownloadFileConfirm(s.app, action, pending, payload, selectedPath)
		}
		if pending.Kind == upgradeLocalBinaryPendingKind {
			payload.SelectedPath = selectedPath
			_ = appState.updatePending(requestID, func(req *state.PendingRequest) { req.PayloadJSON = mustJSON(payload) })
			return newAppUpgradeService(s.app).completeUpgradeLocalBinaryConfirm(action, pending, payload, selectedPath)
		}
		_ = appState.updatePending(requestID, func(req *state.PendingRequest) {
			req.Status = "resolved"
			req.PayloadJSON = mustJSON(payload)
		})
		body := "已选择路径：\n`" + selectedPath + "`"
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "success", Content: "已确认路径"},
			Card:  rawCard(s.app.feishu.SimpleStatusCard("路径已确认", "green", body, nil)),
		}, nil
	default:
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "未知路径选择操作"}}, nil
	}

	if pending.Kind == "workspace_new" {
		workspacePayload.Picker = &payload
		_ = appState.updatePending(requestID, func(req *state.PendingRequest) { req.PayloadJSON = mustJSON(workspacePayload) })
	} else if pending.Kind == "workspace_clone" {
		clonePayload.Picker = &payload
		_ = appState.updatePending(requestID, func(req *state.PendingRequest) { req.PayloadJSON = mustJSON(clonePayload) })
	} else {
		_ = appState.updatePending(requestID, func(req *state.PendingRequest) { req.PayloadJSON = mustJSON(payload) })
	}
	card, err := newWorkspaceRenderService(s.app).renderPathPickerCard(requestID, payload)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "info", Content: "路径选择器已更新"},
		Card:  rawCard(card),
	}, nil
}
