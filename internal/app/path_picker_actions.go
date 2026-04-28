package app

import (
	"encoding/json"
	"os"
	"strings"

	appdebugviewcmd "feidex/internal/app/debugviewcmd"
	apppathpick "feidex/internal/app/pathpick"
	appworkspace "feidex/internal/app/workspace"
	appworkspacecmd "feidex/internal/app/workspacecmd"
	"feidex/internal/feishu"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

func completePathPickerAction(a *App, action *feishu.CardAction, actionName string) (*callback.CardActionTriggerResponse, error) {
	appState := a.State()
	requestID, _ := action.ActionValue["request_id"].(string)
	pending := appState.Pending(requestID)
	if pending == nil || (pending.Kind != appworkspacecmd.PathPickerKind && pending.Kind != "workspace_new" && pending.Kind != "workspace_clone" && pending.Kind != appdebugviewcmd.DownloadFilePendingKind && pending.Kind != upgradeLocalBinaryPendingKind) {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "路径选择请求已过期"}}, nil
	}
	if pending.OwnerUserID != "" && pending.OwnerUserID != action.UserID {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "你没有权限处理这个路径选择请求"}}, nil
	}

	var payload appworkspacecmd.PathPickerPayload
	var workspacePayload appworkspacecmd.NewPayload
	var clonePayload appworkspacecmd.ClonePayload
	if pending.Kind == "workspace_new" {
		workspacePayload = appworkspacecmd.NewPayloadFromPending(pending)
		if workspacePayload.Picker == nil {
			return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "目录选择状态已失效"}}, nil
		}
		payload = *workspacePayload.Picker
	} else if pending.Kind == "workspace_clone" {
		clonePayload = appworkspacecmd.ClonePayloadFromPending(pending)
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
			_ = appState.UpdatePending(requestID, func(req *state.PendingRequest) { req.PayloadJSON = mustJSON(workspacePayload) })
			return &callback.CardActionTriggerResponse{
				Toast: &callback.Toast{Type: "success", Content: "已返回工作区创建"},
				Card:  rawCard(newWorkspaceRenderServiceInner(a).RenderWorkspaceNewCard(pending.SessionKey, requestID, workspacePayload)),
			}, nil
		}
		if pending.Kind == "workspace_clone" {
			clonePayload.Picker = nil
			_ = appState.UpdatePending(requestID, func(req *state.PendingRequest) { req.PayloadJSON = mustJSON(clonePayload) })
			return &callback.CardActionTriggerResponse{
				Toast: &callback.Toast{Type: "success", Content: "已返回从仓库创建"},
				Card:  rawCard(newWorkspaceRenderServiceInner(a).RenderWorkspaceCloneCard(pending.SessionKey, requestID, clonePayload)),
			}, nil
		}
		_ = appState.UpdatePending(requestID, func(req *state.PendingRequest) { req.Status = state.PendingRequestStatusResolved.String() })
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "success", Content: "已取消路径选择"},
			Card:  rawCard(a.feishu.SimpleStatusCard("路径选择已取消", "grey", "本次路径选择已取消。", nil)),
		}, nil
	case "path_picker.up":
		payload.CurrentPath = apppathpick.ParentPath(payload)
		payload.SelectedPath = ""
	case "path_picker.open":
		nextPath, _ := action.ActionValue["path"].(string)
		resolved, err := apppathpick.ResolvePathPickerPath(payload.RootPath, nextPath)
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
		resolved, err := apppathpick.ResolvePathPickerPath(payload.RootPath, nextPath)
		if err != nil {
			return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "文件不可访问"}}, nil
		}
		info, err := os.Stat(resolved)
		if err != nil || info.IsDir() {
			return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "只能选择文件"}}, nil
		}
		payload.SelectedPath = resolved
	case "path_picker.dropdown":
		nextPath, isDir, ok := apppathpick.DecodeOption(action.Option)
		if !ok {
			return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "未收到有效选项"}}, nil
		}
		resolved, err := apppathpick.ResolvePathPickerPath(payload.RootPath, nextPath)
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
		selectedPath, err := apppathpick.SelectedPathForConfirm(payload)
		if err != nil || strings.TrimSpace(selectedPath) == "" {
			return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "请先选择路径"}}, nil
		}
		info, err := os.Stat(selectedPath)
		if err != nil {
			return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "所选路径不可访问"}}, nil
		}
		if payload.Mode == appworkspacecmd.PathPickerModeDirectory && !info.IsDir() {
			return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "当前模式只能确认目录"}}, nil
		}
		if payload.Mode == appworkspacecmd.PathPickerModeFile && info.IsDir() {
			return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "当前模式只能确认文件"}}, nil
		}
		if pending.Kind == "workspace_new" {
			workspacePayload.SelectedCWD = selectedPath
			workspacePayload = appworkspace.UpdateNewSuggestedID(workspacePayload, selectedPath)
			workspacePayload.Picker = nil
			_ = appState.UpdatePending(requestID, func(req *state.PendingRequest) { req.PayloadJSON = mustJSON(workspacePayload) })
			return &callback.CardActionTriggerResponse{
				Toast: &callback.Toast{Type: "success", Content: "已选择目录"},
				Card:  rawCard(newWorkspaceRenderServiceInner(a).RenderWorkspaceNewCard(pending.SessionKey, requestID, workspacePayload)),
			}, nil
		}
		if pending.Kind == "workspace_clone" {
			clonePayload.SelectedParentDir = selectedPath
			clonePayload.Picker = nil
			_ = appState.UpdatePending(requestID, func(req *state.PendingRequest) { req.PayloadJSON = mustJSON(clonePayload) })
			return &callback.CardActionTriggerResponse{
				Toast: &callback.Toast{Type: "success", Content: "已选择父目录"},
				Card:  rawCard(newWorkspaceRenderServiceInner(a).RenderWorkspaceCloneCard(pending.SessionKey, requestID, clonePayload)),
			}, nil
		}
		if pending.Kind == appdebugviewcmd.DownloadFilePendingKind {
			payload.SelectedPath = selectedPath
			_ = appState.UpdatePending(requestID, func(req *state.PendingRequest) { req.PayloadJSON = mustJSON(payload) })
			return appdebugviewcmd.CompleteDownloadFileConfirm(newDebugViewAppAdapter(a), action, pending, payload, selectedPath)
		}
		if pending.Kind == upgradeLocalBinaryPendingKind {
			payload.SelectedPath = selectedPath
			_ = appState.UpdatePending(requestID, func(req *state.PendingRequest) { req.PayloadJSON = mustJSON(payload) })
			return newUpgradeServiceInner(a).CompleteUpgradeLocalBinaryConfirm(action, pending, payload, selectedPath)
		}
		_ = appState.UpdatePending(requestID, func(req *state.PendingRequest) {
			req.Status = state.PendingRequestStatusResolved.String()
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
		_ = appState.UpdatePending(requestID, func(req *state.PendingRequest) { req.PayloadJSON = mustJSON(workspacePayload) })
	} else if pending.Kind == "workspace_clone" {
		clonePayload.Picker = &payload
		_ = appState.UpdatePending(requestID, func(req *state.PendingRequest) { req.PayloadJSON = mustJSON(clonePayload) })
	} else {
		_ = appState.UpdatePending(requestID, func(req *state.PendingRequest) { req.PayloadJSON = mustJSON(payload) })
	}
	card, err := newWorkspaceRenderServiceInner(a).RenderPathPickerCard(requestID, payload)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "info", Content: "路径选择器已更新"},
		Card:  rawCard(card),
	}, nil
}
