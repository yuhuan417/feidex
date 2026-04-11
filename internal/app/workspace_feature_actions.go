package app

import (
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

func (a *App) completeMenuWorkspace(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "info", Content: "已打开 workspace"},
		Card:  rawCard(a.renderWorkspaceMenuCard(sessionKey)),
	}, nil
}

func (a *App) completeWorkspaceUse(action *feishu.CardAction, sessionKey, workspaceID string) (*callback.CardActionTriggerResponse, error) {
	appState := a.appState()
	ws := config.FindWorkspace(a.cfg, workspaceID)
	if ws == nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: "工作区不存在"}}, nil
	}
	sess := appState.session(sessionKey)
	if sess == nil {
		sess = &state.Session{Key: sessionKey, OwnerUserID: action.UserID, ChatID: action.ChatID}
	}
	switchSessionWorkspace(sess, workspaceID)
	_ = appState.saveSession(sess)
	toast := "已切换工作区"
	if !sessionHasInFlightSubmission(sess) {
		binding, err := a.ensureWorkspaceThreadBinding(sessionKey, sess, ws)
		if err != nil {
			slog.Warn("workspace action thread binding failed",
				"session_key", sessionKey,
				"workspace_id", workspaceID,
				"cwd", ws.Cwd,
				"error", err,
			)
			toast = "已切换工作区，自动绑定 thread 失败"
		} else if binding.Resumed {
			toast = "已切换工作区，并恢复最近线程"
		} else {
			toast = "已切换工作区，并创建新线程"
		}
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: toast},
		Card:  rawCard(a.renderWorkspaceMenuCard(sessionKey)),
	}, nil
}

func (a *App) completeWorkspaceNew(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	appState := a.appState()
	requestID, err := appState.nextLocalID("workspace")
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: err.Error()}}, nil
	}
	wsID := a.defaultWorkspaceID()
	if sess := appState.session(sessionKey); sess != nil && strings.TrimSpace(sess.WorkspaceID) != "" {
		wsID = sess.WorkspaceID
	}
	ws := config.FindWorkspace(a.cfg, wsID)
	payload := workspaceNewPayload{
		RootPath: a.defaultWorkspaceNewRoot(ws),
		SelectedCWD: firstNonEmpty(func() string {
			if ws == nil {
				return ""
			}
			return strings.TrimSpace(ws.Cwd)
		}(), "/"),
	}
	_ = appState.savePending(&state.PendingRequest{
		ID:          requestID,
		Kind:        "workspace_new",
		SessionKey:  sessionKey,
		OwnerUserID: action.UserID,
		FeishuMsgID: action.MessageID,
		PayloadJSON: mustJSON(payload),
		Status:      "pending",
		CreatedAt:   time.Now().Unix(),
		ExpiresAt:   time.Now().Add(10 * time.Minute).Unix(),
	})
	card := a.renderWorkspaceNewCard(sessionKey, requestID, payload)
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "info", Content: "请填写工作区信息"},
		Card:  rawCard(card),
	}, nil
}

func (a *App) completeWorkspaceNewPickDir(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	appState := a.appState()
	requestID, _ := action.ActionValue["request_id"].(string)
	pending := appState.pending(requestID)
	if pending == nil || pending.Kind != "workspace_new" {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "工作区创建请求已过期"}}, nil
	}
	if pending.OwnerUserID != "" && pending.OwnerUserID != action.UserID {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "你没有权限处理这个工作区请求"}}, nil
	}
	payload := mergeWorkspaceNewFormValues(workspaceNewPayloadFromPending(pending), action.FormValue)
	currentPath := firstNonEmpty(strings.TrimSpace(payload.SelectedCWD), "/")
	payload.Picker = &pathPickerPayload{
		Mode:        pathPickerModeDirectory,
		Style:       pathPickerStyleDropdown,
		RootPath:    firstNonEmpty(strings.TrimSpace(payload.RootPath), "/"),
		CurrentPath: currentPath,
	}
	_ = appState.updatePending(requestID, func(req *state.PendingRequest) { req.PayloadJSON = mustJSON(payload) })
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "info", Content: "已打开目录选择"},
		Card:  rawCard(a.renderWorkspaceNewCard(pending.SessionKey, requestID, payload)),
	}, nil
}

func (a *App) completeWorkspaceNewSubmit(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	appState := a.appState()
	requestID, _ := action.ActionValue["request_id"].(string)
	pending := appState.pending(requestID)
	if pending == nil || pending.Kind != "workspace_new" {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "工作区创建请求已过期"}}, nil
	}
	if pending.OwnerUserID != "" && pending.OwnerUserID != action.UserID {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "你没有权限处理这个工作区请求"}}, nil
	}
	payload := mergeWorkspaceNewFormValues(workspaceNewPayloadFromPending(pending), action.FormValue)
	id := strings.TrimSpace(payload.DraftID)
	if id == "" {
		_ = appState.updatePending(requestID, func(req *state.PendingRequest) { req.PayloadJSON = mustJSON(payload) })
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "warning", Content: "请填写 workspace_id"},
			Card:  rawCard(a.renderWorkspaceNewCard(pending.SessionKey, requestID, payload)),
		}, nil
	}
	cwd := strings.TrimSpace(payload.SelectedCWD)
	if cwd == "" {
		_ = appState.updatePending(requestID, func(req *state.PendingRequest) { req.PayloadJSON = mustJSON(payload) })
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "warning", Content: "请先选择目录"},
			Card:  rawCard(a.renderWorkspaceNewCard(pending.SessionKey, requestID, payload)),
		}, nil
	}
	name := strings.TrimSpace(payload.DraftName)
	if name == "" {
		name = id
	}
	sess := appState.session(pending.SessionKey)
	chatID := action.ChatID
	chatType := ""
	if sess != nil {
		chatID = firstNonEmpty(chatID, sess.ChatID)
		chatType = sess.ChatType
	}
	if err := a.createWorkspaceAndSwitch(pending.SessionKey, action.UserID, chatID, chatType, id, name, cwd); err != nil {
		_ = appState.updatePending(requestID, func(req *state.PendingRequest) { req.PayloadJSON = mustJSON(payload) })
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "warning", Content: err.Error()},
			Card:  rawCard(a.renderWorkspaceNewCard(pending.SessionKey, requestID, payload)),
		}, nil
	}
	_ = appState.updatePending(requestID, func(req *state.PendingRequest) {
		req.Status = "resolved"
		req.PayloadJSON = mustJSON(payload)
	})
	body := "已创建并切换到工作区 `" + id + "`\n\ncwd: `" + cwd + "`"
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已创建工作区"},
		Card:  rawCard(a.feishu.SimpleStatusCard("工作区已创建", "green", body, nil)),
	}, nil
}

func (a *App) completeWorkspaceSandboxMenu(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	card, err := a.renderWorkspaceSandboxMenuCard(sessionKey)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "info", Content: "已打开 sandbox 配置"},
		Card:  rawCard(card),
	}, nil
}

func (a *App) completeWorkspacePolicyMenu(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	card, err := a.renderWorkspacePolicyMenuCard(sessionKey)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "info", Content: "已打开 policy 配置"},
		Card:  rawCard(card),
	}, nil
}

func (a *App) updateWorkspaceDefaults(workspaceID string, mutate func(*config.Workspace)) (*config.Workspace, error) {
	ws := config.FindWorkspace(a.cfg, workspaceID)
	if ws == nil {
		return nil, fmt.Errorf("workspace %q not found", workspaceID)
	}
	mutate(ws)
	if err := a.cfg.Normalize(filepath.Dir(a.cfgPath)); err != nil {
		return nil, err
	}
	if err := config.Save(a.cfgPath, a.cfg); err != nil {
		return nil, err
	}
	return config.FindWorkspace(a.cfg, workspaceID), nil
}

func (a *App) completeWorkspaceSandboxSet(action *feishu.CardAction, sessionKey, workspaceID, sandboxMode string) (*callback.CardActionTriggerResponse, error) {
	valid := false
	for _, opt := range workspaceSandboxOptions() {
		if opt.Value == sandboxMode {
			valid = true
			break
		}
	}
	if !valid {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: "不支持的 sandbox"}}, nil
	}
	_, err := a.updateWorkspaceDefaults(workspaceID, func(w *config.Workspace) {
		w.SandboxMode = sandboxMode
	})
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: err.Error()}}, nil
	}
	card, renderErr := a.renderWorkspaceSandboxMenuCard(sessionKey)
	if renderErr != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: renderErr.Error()}}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已更新 sandbox"},
		Card:  rawCard(card),
	}, nil
}

func (a *App) completeWorkspacePolicySet(action *feishu.CardAction, sessionKey, workspaceID, approvalPolicy string) (*callback.CardActionTriggerResponse, error) {
	valid := false
	for _, opt := range workspaceApprovalPolicyOptions() {
		if opt.Value == approvalPolicy {
			valid = true
			break
		}
	}
	if !valid {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: "不支持的 policy"}}, nil
	}
	_, err := a.updateWorkspaceDefaults(workspaceID, func(w *config.Workspace) {
		w.ApprovalPolicy = approvalPolicy
	})
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: err.Error()}}, nil
	}
	card, renderErr := a.renderWorkspacePolicyMenuCard(sessionKey)
	if renderErr != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: renderErr.Error()}}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已更新 policy"},
		Card:  rawCard(card),
	}, nil
}
