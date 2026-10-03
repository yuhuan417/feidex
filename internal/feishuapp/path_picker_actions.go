package feishuapp

import (
	appupgradecmd "feidex/internal/adapter/feishu/upgradecmd"

	"encoding/json"
	"os"
	"strings"

	appdebugviewcmd "feidex/internal/adapter/feishu/debugviewcmd"
	pickercards "feidex/internal/adapter/feishu/pathpicker"
	appworkspacecmd "feidex/internal/adapter/feishu/workspacecmd"
	apppathpick "feidex/internal/adapter/filesystem/pathpicker"
	pickerapp "feidex/internal/application/pathpicker"
	appworkspace "feidex/internal/application/workspace"
	"feidex/internal/feishu"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

func completePathPickerAction(a *App, action *feishu.CardAction, actionName string) (*callback.CardActionTriggerResponse, error) {
	appState := a.State()
	requestID, _ := action.ActionValue["request_id"].(string)
	pending := appState.Pending(requestID)
	if pending == nil || (pending.Kind != appworkspacecmd.PathPickerKind && pending.Kind != "workspace_new" && pending.Kind != "workspace_clone" && pending.Kind != appdebugviewcmd.DownloadFilePendingKind && pending.Kind != appupgradecmd.UpgradeLocalBinaryPendingKind) {
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
			if err := a.bindings.Forms.SaveDraft(requestID, workspacePayload, "", 0, ""); err != nil {
				return nil, err
			}
			return &callback.CardActionTriggerResponse{
				Toast: &callback.Toast{Type: "success", Content: "已返回工作区创建"},
				Card:  rawCard(a.bindings.WorkspacePresentation.RenderWorkspaceNewCard(pending.SessionKey, requestID, workspacePayload)),
			}, nil
		}
		if pending.Kind == "workspace_clone" {
			clonePayload.Picker = nil
			if err := a.bindings.Forms.SaveDraft(requestID, clonePayload, "", 0, ""); err != nil {
				return nil, err
			}
			return &callback.CardActionTriggerResponse{
				Toast: &callback.Toast{Type: "success", Content: "已返回从仓库创建"},
				Card:  rawCard(a.bindings.WorkspacePresentation.RenderWorkspaceCloneCard(pending.SessionKey, requestID, clonePayload)),
			}, nil
		}
		if err := a.bindings.Forms.SaveDraft(requestID, nil, state.PendingRequestStatusResolved.String(), 0, ""); err != nil {
			return nil, err
		}
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "success", Content: "已取消路径选择"},
			Card:  rawCard(a.feishu.SimpleStatusCard("路径选择已取消", "grey", "本次路径选择已取消。", nil)),
		}, nil
	case "path_picker.up":
		payload.CurrentPath = pickerapp.ParentPath(payload)
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
		nextPath, isDir, ok := pickercards.DecodeOption(action.Option)
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
		selectedPath, err := a.bindings.PathPicker.SelectedPathForConfirm(payload)
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
			if err := a.bindings.Forms.SaveDraft(requestID, workspacePayload, "", 0, ""); err != nil {
				return nil, err
			}
			return &callback.CardActionTriggerResponse{
				Toast: &callback.Toast{Type: "success", Content: "已选择目录"},
				Card:  rawCard(a.bindings.WorkspacePresentation.RenderWorkspaceNewCard(pending.SessionKey, requestID, workspacePayload)),
			}, nil
		}
		if pending.Kind == "workspace_clone" {
			clonePayload.SelectedParentDir = selectedPath
			clonePayload = a.bindings.WorkspacePlanning.DefaultCloneWorktreePayload(clonePayload, selectedPath)
			clonePayload.Picker = nil
			if err := a.bindings.Forms.SaveDraft(requestID, clonePayload, "", 0, ""); err != nil {
				return nil, err
			}
			return &callback.CardActionTriggerResponse{
				Toast: &callback.Toast{Type: "success", Content: "已选择父目录"},
				Card:  rawCard(a.bindings.WorkspacePresentation.RenderWorkspaceCloneCard(pending.SessionKey, requestID, clonePayload)),
			}, nil
		}
		if pending.Kind == appdebugviewcmd.DownloadFilePendingKind {
			payload.SelectedPath = selectedPath
			if err := a.bindings.Forms.SaveDraft(requestID, payload, "", 0, ""); err != nil {
				return nil, err
			}
			return appdebugviewcmd.CompleteDownloadFileConfirm(newDebugViewAppAdapter(a), action, pending, payload, selectedPath)
		}
		if pending.Kind == appupgradecmd.UpgradeLocalBinaryPendingKind {
			payload.SelectedPath = selectedPath
			if err := a.bindings.Forms.SaveDraft(requestID, payload, "", 0, ""); err != nil {
				return nil, err
			}
			return a.bindings.Upgrades.CompleteUpgradeLocalBinaryConfirm(action, pending, payload, selectedPath)
		}
		if err := a.bindings.Forms.SaveDraft(requestID, payload, state.PendingRequestStatusResolved.String(), 0, ""); err != nil {
			return nil, err
		}
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
		if err := a.bindings.Forms.SaveDraft(requestID, workspacePayload, "", 0, ""); err != nil {
			return nil, err
		}
	} else if pending.Kind == "workspace_clone" {
		clonePayload.Picker = &payload
		if err := a.bindings.Forms.SaveDraft(requestID, clonePayload, "", 0, ""); err != nil {
			return nil, err
		}
	} else {
		if err := a.bindings.Forms.SaveDraft(requestID, payload, "", 0, ""); err != nil {
			return nil, err
		}
	}
	card, err := a.bindings.WorkspacePresentation.RenderPathPickerCard(requestID, payload)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "info", Content: "路径选择器已更新"},
		Card:  rawCard(card),
	}, nil
}
