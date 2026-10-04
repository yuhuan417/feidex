package feishuapp

import (
	"encoding/json"
	"os"
	"strings"

	appdebugviewcmd "feidex/internal/adapter/feishu/debugviewcmd"
	pickercards "feidex/internal/adapter/feishu/pathpicker"
	appupgradecmd "feidex/internal/adapter/feishu/upgradecmd"
	"feidex/internal/adapter/feishu/workspace"
	appworkspacecmd "feidex/internal/adapter/feishu/workspacecmd"
	apppathpick "feidex/internal/adapter/filesystem/pathpicker"
	appstate "feidex/internal/adapter/storage/json/scoped"
	"feidex/internal/application/interaction"
	pickerapp "feidex/internal/application/pathpicker"
	appworkspace "feidex/internal/application/workspace"
	"feidex/internal/feishu"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

type pathPickerActionService struct {
	state            *appstate.Store
	forms            *interaction.FormService
	picker           pickerapp.Service
	planning         *appworkspace.PlanningService
	workspaceCards   *workspace.Presentation
	upgrades         appupgradecmd.UpgradeService
	debug            appdebugviewcmd.DebugService
	simpleStatusCard func(string, string, string, []feishu.Button) map[string]any
}

type PathPickerActionInputs struct {
	State            *appstate.Store
	Forms            *interaction.FormService
	Picker           pickerapp.Service
	Planning         *appworkspace.PlanningService
	WorkspaceCards   *workspace.Presentation
	Upgrades         appupgradecmd.UpgradeService
	Debug            appdebugviewcmd.DebugService
	SimpleStatusCard func(string, string, string, []feishu.Button) map[string]any
}

func pathPickerActionHandlers(inputs PathPickerActionInputs) map[string]cardActionPortHandler {
	service := pathPickerActionService{
		state: inputs.State, forms: inputs.Forms, picker: inputs.Picker,
		planning: inputs.Planning, workspaceCards: inputs.WorkspaceCards,
		upgrades: inputs.Upgrades, debug: inputs.Debug,
		simpleStatusCard: inputs.SimpleStatusCard,
	}
	complete := func(action *feishu.CardAction, name string) (*callback.CardActionTriggerResponse, error) {
		return completePathPickerActionWithService(service, action, name)
	}
	return map[string]cardActionPortHandler{
		"path_picker.dropdown": func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return complete(action, "path_picker.dropdown")
		},
		"path_picker.up": func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return complete(action, "path_picker.up")
		},
		"path_picker.open": func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return complete(action, "path_picker.open")
		},
		"path_picker.select": func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return complete(action, "path_picker.select")
		},
		"path_picker.confirm": func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return complete(action, "path_picker.confirm")
		},
		"path_picker.cancel": func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return complete(action, "path_picker.cancel")
		},
	}
}

func completePathPickerActionWithService(service pathPickerActionService, action *feishu.CardAction, actionName string) (*callback.CardActionTriggerResponse, error) {
	appState := service.state
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
			if err := service.forms.SaveDraft(requestID, workspacePayload, "", 0, ""); err != nil {
				return nil, err
			}
			return &callback.CardActionTriggerResponse{
				Toast: &callback.Toast{Type: "success", Content: "已返回工作区创建"},
				Card:  rawCard(service.workspaceCards.RenderWorkspaceNewCard(pending.SessionKey, requestID, workspacePayload)),
			}, nil
		}
		if pending.Kind == "workspace_clone" {
			clonePayload.Picker = nil
			if err := service.forms.SaveDraft(requestID, clonePayload, "", 0, ""); err != nil {
				return nil, err
			}
			return &callback.CardActionTriggerResponse{
				Toast: &callback.Toast{Type: "success", Content: "已返回从仓库创建"},
				Card:  rawCard(service.workspaceCards.RenderWorkspaceCloneCard(pending.SessionKey, requestID, clonePayload)),
			}, nil
		}
		if err := service.forms.SaveDraft(requestID, nil, state.PendingRequestStatusResolved.String(), 0, ""); err != nil {
			return nil, err
		}
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "success", Content: "已取消路径选择"},
			Card:  rawCard(service.simpleStatusCard("路径选择已取消", "grey", "本次路径选择已取消。", nil)),
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
		selectedPath, err := service.picker.SelectedPathForConfirm(payload)
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
			if err := service.forms.SaveDraft(requestID, workspacePayload, "", 0, ""); err != nil {
				return nil, err
			}
			return &callback.CardActionTriggerResponse{
				Toast: &callback.Toast{Type: "success", Content: "已选择目录"},
				Card:  rawCard(service.workspaceCards.RenderWorkspaceNewCard(pending.SessionKey, requestID, workspacePayload)),
			}, nil
		}
		if pending.Kind == "workspace_clone" {
			clonePayload.SelectedParentDir = selectedPath
			clonePayload = service.planning.DefaultCloneWorktreePayload(clonePayload, selectedPath)
			clonePayload.Picker = nil
			if err := service.forms.SaveDraft(requestID, clonePayload, "", 0, ""); err != nil {
				return nil, err
			}
			return &callback.CardActionTriggerResponse{
				Toast: &callback.Toast{Type: "success", Content: "已选择父目录"},
				Card:  rawCard(service.workspaceCards.RenderWorkspaceCloneCard(pending.SessionKey, requestID, clonePayload)),
			}, nil
		}
		if pending.Kind == appdebugviewcmd.DownloadFilePendingKind {
			payload.SelectedPath = selectedPath
			if err := service.forms.SaveDraft(requestID, payload, "", 0, ""); err != nil {
				return nil, err
			}
			return service.debug.CompleteDownloadFileConfirm(action, pending, payload, selectedPath)
		}
		if pending.Kind == appupgradecmd.UpgradeLocalBinaryPendingKind {
			payload.SelectedPath = selectedPath
			if err := service.forms.SaveDraft(requestID, payload, "", 0, ""); err != nil {
				return nil, err
			}
			return service.upgrades.CompleteUpgradeLocalBinaryConfirm(action, pending, payload, selectedPath)
		}
		if err := service.forms.SaveDraft(requestID, payload, state.PendingRequestStatusResolved.String(), 0, ""); err != nil {
			return nil, err
		}
		body := "已选择路径：\n`" + selectedPath + "`"
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "success", Content: "已确认路径"},
			Card:  rawCard(service.simpleStatusCard("路径已确认", "green", body, nil)),
		}, nil
	default:
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "未知路径选择操作"}}, nil
	}

	if pending.Kind == "workspace_new" {
		workspacePayload.Picker = &payload
		if err := service.forms.SaveDraft(requestID, workspacePayload, "", 0, ""); err != nil {
			return nil, err
		}
	} else if pending.Kind == "workspace_clone" {
		clonePayload.Picker = &payload
		if err := service.forms.SaveDraft(requestID, clonePayload, "", 0, ""); err != nil {
			return nil, err
		}
	} else {
		if err := service.forms.SaveDraft(requestID, payload, "", 0, ""); err != nil {
			return nil, err
		}
	}
	card, err := service.workspaceCards.RenderPathPickerCard(requestID, payload)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "info", Content: "路径选择器已更新"},
		Card:  rawCard(card),
	}, nil
}
