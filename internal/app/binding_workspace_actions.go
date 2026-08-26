package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	appworkspacecmd "feidex/internal/app/workspacecmd"
	"feidex/internal/feishu"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

func (s bindingService) isGroupWorkspacePending(action *feishu.CardAction, kind string) bool {
	if action == nil || s.app == nil || s.app.State() == nil {
		return false
	}
	requestID := actionStringValue(action, "request_id")
	if requestID == "" {
		return false
	}
	pending := s.app.State().Pending(requestID)
	if pending == nil || strings.TrimSpace(pending.Kind) != strings.TrimSpace(kind) {
		return false
	}
	return groupBindingSessionScopeActive(s.app, pending.SessionKey)
}

func (s bindingService) completeBindingWorkspaceSettingMenu(action *feishu.CardAction, sessionKey, fieldName string) (*callback.CardActionTriggerResponse, error) {
	msg := commandMessageFromAction(s.app, action, sessionKey, "/workspace "+fieldName)
	binding, err := s.ensureBindingForMessage(msg)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	card, err := s.renderBindingWorkspaceSettingCard(sessionKey, binding, fieldName)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "info", Content: "已打开工作区配置"},
		Card:  rawCard(card),
	}, nil
}

func (s bindingService) renderBindingWorkspaceSettingCard(sessionKey string, binding *state.AgentBinding, fieldName string) (map[string]any, error) {
	fieldName = normalizeBindingWorkspaceSettingName(fieldName)
	if binding == nil {
		binding = bindingForSessionKey(s.app, sessionKey)
	}
	if binding == nil {
		return nil, fmt.Errorf("当前群内工作区配置未初始化")
	}
	setting, err := bindingWorkspaceSetting(fieldName, s.app)
	if err != nil {
		return nil, err
	}
	currentValue := setting.current(binding)
	workspaceID := strings.TrimSpace(binding.WorkspaceID)
	if workspaceID == "" {
		workspaceID = "(未配置)"
	}
	body := strings.Join([]string{
		"配置当前 Bot 在本群的 workspace " + setting.Name + " 覆盖。",
		"",
		"当前工作区: `" + workspaceID + "`",
		"当前覆盖: " + renderOptionalBacktick(currentValue),
	}, "\n")
	buttons := make([]feishu.Button, 0, len(setting.Options)+2)
	followType := "default"
	followLabel := "跟随工作区默认"
	if currentValue == "" {
		followType = "primary"
		followLabel = "当前 · " + followLabel
	}
	buttons = append(buttons, feishu.Button{
		Text: followLabel,
		Type: followType,
		Value: map[string]any{
			"action":         setting.SetAction,
			"session_key":    sessionKey,
			setting.ValueKey: "default",
		},
	})
	for _, opt := range setting.Options {
		value := strings.TrimSpace(opt.Value)
		if value == "" {
			continue
		}
		label := strings.TrimSpace(opt.Label)
		if label == "" {
			label = value
		}
		buttonType := "default"
		if value == currentValue {
			buttonType = "primary"
			label = "当前 · " + label
		}
		buttons = append(buttons, feishu.Button{
			Text: label,
			Type: buttonType,
			Value: map[string]any{
				"action":         setting.SetAction,
				"session_key":    sessionKey,
				setting.ValueKey: value,
			},
		})
	}
	buttons = append(buttons, groupBindingBackButton(sessionKey))
	return s.app.feishu.SimpleStatusCard(setting.Title, "blue", menuCardBody(setting.MenuAction, body), buttons), nil
}

type bindingWorkspaceSettingSpec struct {
	Name       string
	Title      string
	MenuAction string
	SetAction  string
	ValueKey   string
	Options    []appworkspacecmd.SettingOption
	current    func(*state.AgentBinding) string
}

func bindingWorkspaceSetting(fieldName string, a *App) (bindingWorkspaceSettingSpec, error) {
	switch normalizeBindingWorkspaceSettingName(fieldName) {
	case "sandbox":
		return bindingWorkspaceSettingSpec{
			Name:       "sandbox",
			Title:      "配置 Sandbox",
			MenuAction: "workspace.sandbox.menu",
			SetAction:  "workspace.sandbox.set",
			ValueKey:   "sandbox_mode",
			Options:    appworkspacecmd.SandboxOptions(),
			current:    func(binding *state.AgentBinding) string { return strings.TrimSpace(binding.SandboxModeOverride) },
		}, nil
	case "policy":
		return bindingWorkspaceSettingSpec{
			Name:       "approval policy",
			Title:      "配置 Policy",
			MenuAction: "workspace.policy.menu",
			SetAction:  "workspace.policy.set",
			ValueKey:   "approval_policy",
			Options:    appworkspacecmd.ApprovalPolicyOptions(),
			current:    func(binding *state.AgentBinding) string { return strings.TrimSpace(binding.ApprovalPolicyOverride) },
		}, nil
	case "multiagent":
		return bindingWorkspaceSettingSpec{
			Name:       "multi-agent mode",
			Title:      "配置 Multi-Agent Mode",
			MenuAction: "workspace.multiagent.menu",
			SetAction:  "workspace.multiagent.set",
			ValueKey:   "multi_agent_mode",
			Options:    appworkspacecmd.MultiAgentModeOptions(),
			current:    func(binding *state.AgentBinding) string { return strings.TrimSpace(binding.MultiAgentModeOverride) },
		}, nil
	case "permissions":
		options := make([]appworkspacecmd.SettingOption, 0, 3)
		for _, opt := range claudePermissionModeOptions(isClaudeBypassPermissionsEnabled(a.cfg)) {
			options = append(options, appworkspacecmd.SettingOption{Value: opt.Value, Label: opt.Label})
		}
		return bindingWorkspaceSettingSpec{
			Name:       "Claude permissions",
			Title:      "配置默认权限",
			MenuAction: "workspace.permission_mode.menu",
			SetAction:  "workspace.permission_mode.set",
			ValueKey:   "mode",
			Options:    options,
			current:    func(binding *state.AgentBinding) string { return strings.TrimSpace(binding.ClaudePermissionMode) },
		}, nil
	default:
		return bindingWorkspaceSettingSpec{}, fmt.Errorf("unsupported workspace setting %q", fieldName)
	}
}

func normalizeBindingWorkspaceSettingName(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "permission":
		return "permissions"
	default:
		return strings.ToLower(strings.TrimSpace(value))
	}
}

func (s bindingService) completeBindingWorkspaceNewSubmit(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	requestID := actionStringValue(action, "request_id")
	pending := s.app.State().Pending(requestID)
	if pending == nil || pending.Kind != "workspace_new" || !groupBindingSessionScopeActive(s.app, pending.SessionKey) {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "工作区创建请求已过期"}}, nil
	}
	if pending.OwnerUserID != "" && pending.OwnerUserID != action.UserID {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "你没有权限处理这个工作区请求"}}, nil
	}
	payload := appworkspacecmd.MergeNewFormValues(appworkspacecmd.NewPayloadFromPending(pending), action.FormValue)
	id := strings.TrimSpace(payload.DraftID)
	if id == "" {
		return s.renderBindingWorkspaceNewFormWarning(requestID, pending, payload, "请填写 workspace_id")
	}
	cwd := strings.TrimSpace(payload.SelectedCWD)
	if cwd == "" {
		return s.renderBindingWorkspaceNewFormWarning(requestID, pending, payload, "请先选择目录")
	}
	name := strings.TrimSpace(payload.DraftName)
	if name == "" {
		name = id
	}
	msg := commandMessageFromAction(s.app, action, pending.SessionKey, "/workspace new")
	binding, err := s.ensureBindingForMessage(msg)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	mgmt := newWorkspaceManagementServiceInner(s.app)
	if existingWS := mgmt.WorkspaceByIDAndCWD(id, cwd); existingWS != nil {
		return s.finishBindingWorkspaceCreated(requestID, pending.SessionKey, pending, payload, binding, existingWS.ID, "已设置当前工作区")
	}
	if _, err := s.createLocalWorkspace(id, name, cwd); err != nil {
		return s.renderBindingWorkspaceNewFormWarning(requestID, pending, payload, err.Error())
	}
	return s.finishBindingWorkspaceCreated(requestID, pending.SessionKey, pending, payload, binding, id, "已创建工作区")
}

func (s bindingService) renderBindingWorkspaceNewFormWarning(requestID string, pending *state.PendingRequest, payload appworkspacecmd.NewPayload, warning string) (*callback.CardActionTriggerResponse, error) {
	_ = s.app.State().UpdatePending(requestID, func(req *state.PendingRequest) { req.PayloadJSON = mustJSON(payload) })
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "warning", Content: warning},
		Card:  rawCard(newWorkspaceRenderServiceInner(s.app).RenderWorkspaceNewCard(pending.SessionKey, requestID, payload)),
	}, nil
}

func (s bindingService) finishBindingWorkspaceCreated(requestID, sessionKey string, pending *state.PendingRequest, payload appworkspacecmd.NewPayload, binding *state.AgentBinding, workspaceID, toast string) (*callback.CardActionTriggerResponse, error) {
	updated, err := s.activateBindingWorkspace(binding, workspaceID)
	if err != nil {
		return s.renderBindingWorkspaceNewFormWarning(requestID, pending, payload, err.Error())
	}
	_ = s.app.State().UpdatePending(requestID, func(req *state.PendingRequest) {
		req.Status = state.PendingRequestStatusResolved.String()
		req.PayloadJSON = mustJSON(payload)
		req.ExpiresAt = time.Now().Add(30 * time.Minute).Unix()
	})
	s.replayPendingBindingMessageAsync(updated)
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: toast},
		Card:  rawCard(newWorkspaceRenderServiceInner(s.app).RenderWorkspaceMenuCard(sessionKey)),
	}, nil
}

func (s bindingService) completeBindingWorkspaceCloneSubmit(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	requestID := actionStringValue(action, "request_id")
	pending := s.app.State().Pending(requestID)
	if pending == nil || pending.Kind != "workspace_clone" || !groupBindingSessionScopeActive(s.app, pending.SessionKey) {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "工作区克隆请求已过期"}}, nil
	}
	if pending.OwnerUserID != "" && pending.OwnerUserID != action.UserID {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "你没有权限处理这个工作区请求"}}, nil
	}
	payload := appworkspacecmd.MergeCloneFormValues(appworkspacecmd.ClonePayloadFromPending(pending), action.FormValue)
	payload.ErrorMessage = ""
	if strings.TrimSpace(payload.RepoURL) == "" {
		return s.renderBindingWorkspaceCloneFormWarning(requestID, pending, payload, "请填写 git 地址")
	}
	mgmt := newWorkspaceManagementServiceInner(s.app)
	parentDir := strings.TrimSpace(payload.SelectedParentDir)
	if parentDir == "" {
		parentDir = mgmt.DefaultWorkspaceCloneParent(nil)
	}
	payload.SelectedParentDir = parentDir
	plan, err := mgmt.PrepareWorkspaceClone(payload.RepoURL, payload.DraftID, parentDir)
	if err != nil {
		return s.handleBindingWorkspaceClonePrepareError(action, requestID, pending, payload, err)
	}
	msg := commandMessageFromAction(s.app, action, pending.SessionKey, "/workspace clone")
	binding, err := s.ensureBindingForMessage(msg)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	op := appworkspacecmd.NewCloneOperation(cancel)
	mgmt.SetWorkspaceCloneOperation(requestID, op)
	messageID := firstNonEmpty(strings.TrimSpace(pending.FeishuMsgID), strings.TrimSpace(action.MessageID))
	_ = s.app.State().UpdatePending(requestID, func(req *state.PendingRequest) {
		req.Status = state.PendingRequestStatusProcessing.String()
		req.PayloadJSON = mustJSON(payload)
		req.FeishuMsgID = firstNonEmpty(strings.TrimSpace(req.FeishuMsgID), messageID)
		req.ExpiresAt = time.Now().Add(30 * time.Minute).Unix()
	})
	runAsync(s.app, func() {
		s.finishBindingWorkspaceClone(ctx, mgmt, op, requestID, messageID, pending.SessionKey, parentDir, payload, plan, binding)
	})
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "info", Content: "已开始从仓库创建工作区"},
		Card:  rawCard(newWorkspaceRenderServiceInner(s.app).RenderWorkspaceClonePreparingCard(requestID, payload, parentDir, op.Snapshot())),
	}, nil
}

func (s bindingService) handleBindingWorkspaceClonePrepareError(action *feishu.CardAction, requestID string, pending *state.PendingRequest, payload appworkspacecmd.ClonePayload, err error) (*callback.CardActionTriggerResponse, error) {
	var existingWorkspaceErr *appworkspacecmd.CloneExistingWorkspaceError
	if errors.As(err, &existingWorkspaceErr) {
		msg := commandMessageFromAction(s.app, action, pending.SessionKey, "/workspace clone")
		binding, bindErr := s.ensureBindingForMessage(msg)
		if bindErr != nil {
			return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: bindErr.Error()}}, nil
		}
		updated, bindErr := s.activateBindingWorkspace(binding, existingWorkspaceErr.WorkspaceID)
		if bindErr != nil {
			return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: bindErr.Error()}}, nil
		}
		_ = s.app.State().UpdatePending(requestID, func(req *state.PendingRequest) {
			req.Status = state.PendingRequestStatusResolved.String()
			req.PayloadJSON = mustJSON(payload)
			req.ExpiresAt = time.Now().Add(30 * time.Minute).Unix()
		})
		s.replayPendingBindingMessageAsync(updated)
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "success", Content: "已设置当前工作区"}, Card: rawCard(newWorkspaceRenderServiceInner(s.app).RenderWorkspaceMenuCard(pending.SessionKey))}, nil
	}
	var existingDirErr *appworkspacecmd.CloneExistingDirError
	if errors.As(err, &existingDirErr) {
		_ = s.app.State().UpdatePending(requestID, func(req *state.PendingRequest) {
			req.Status = state.PendingRequestStatusResolved.String()
			req.PayloadJSON = mustJSON(payload)
			req.ExpiresAt = time.Now().Add(30 * time.Minute).Unix()
		})
		mgmt := newWorkspaceManagementServiceInner(s.app)
		takeoverPayload := appworkspacecmd.NewTakeoverPayloadWithNotice(existingDirErr.WorkspaceID, existingDirErr.TargetDir, appworkspacecmd.NewTakeoverNotice(existingDirErr.TargetDir))
		newRequestID, createErr := mgmt.CreateWorkspaceNewPending(pending.SessionKey, action.UserID, "", takeoverPayload)
		if createErr != nil {
			return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: createErr.Error()}}, nil
		}
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "info", Content: "clone 目标目录已存在，已打开预填好的新建工作区"},
			Card:  rawCard(newWorkspaceRenderServiceInner(s.app).RenderWorkspaceNewCard(pending.SessionKey, newRequestID, takeoverPayload)),
		}, nil
	}
	return s.renderBindingWorkspaceCloneFormWarning(requestID, pending, payload, err.Error())
}

func (s bindingService) renderBindingWorkspaceCloneFormWarning(requestID string, pending *state.PendingRequest, payload appworkspacecmd.ClonePayload, warning string) (*callback.CardActionTriggerResponse, error) {
	payload.ErrorMessage = warning
	_ = s.app.State().UpdatePending(requestID, func(req *state.PendingRequest) {
		req.Status = state.PendingRequestStatusPending.String()
		req.PayloadJSON = mustJSON(payload)
		req.ExpiresAt = time.Now().Add(10 * time.Minute).Unix()
	})
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "warning", Content: warning},
		Card:  rawCard(newWorkspaceRenderServiceInner(s.app).RenderWorkspaceCloneCard(pending.SessionKey, requestID, payload)),
	}, nil
}

func (s bindingService) finishBindingWorkspaceClone(ctx context.Context, mgmt *appworkspacecmd.ManagementService, op *appworkspacecmd.CloneOperation, requestID, messageID, sessionKey, parentDir string, payload appworkspacecmd.ClonePayload, plan *appworkspacecmd.ClonePlan, binding *state.AgentBinding) {
	defer mgmt.ClearWorkspaceCloneOperation(requestID)
	if err := os.MkdirAll(filepath.Dir(plan.TargetDir), 0o755); err != nil {
		s.patchBindingWorkspaceCloneFailure(requestID, messageID, sessionKey, payload, err.Error())
		return
	}
	err := mgmt.GitClone(ctx, strings.TrimSpace(payload.RepoURL), plan.TargetDir, func(line string) {
		snapshot, shouldPatch := op.RecordProgress(line)
		if shouldPatch && strings.TrimSpace(messageID) != "" {
			_ = s.app.feishu.PatchCard(context.Background(), messageID, newWorkspaceRenderServiceInner(s.app).RenderWorkspaceClonePreparingCard(requestID, payload, parentDir, snapshot))
		}
	})
	if err != nil || ctx.Err() != nil {
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		if errors.Is(err, context.Canceled) {
			_ = s.app.State().UpdatePending(requestID, func(req *state.PendingRequest) {
				req.Status = state.PendingRequestStatusResolved.String()
				req.PayloadJSON = mustJSON(payload)
				req.ExpiresAt = time.Now().Add(10 * time.Minute).Unix()
			})
			if strings.TrimSpace(messageID) != "" {
				_ = s.app.feishu.PatchCard(context.Background(), messageID, newWorkspaceRenderServiceInner(s.app).RenderWorkspaceCloneCanceledCard(sessionKey, payload, parentDir, op.Snapshot()))
			}
			return
		}
		s.patchBindingWorkspaceCloneFailure(requestID, messageID, sessionKey, payload, err.Error())
		return
	}
	if _, err := s.createLocalWorkspace(plan.WorkspaceID, plan.WorkspaceID, plan.TargetDir); err != nil {
		_ = s.app.State().UpdatePending(requestID, func(req *state.PendingRequest) {
			req.Status = state.PendingRequestStatusResolved.String()
			req.PayloadJSON = mustJSON(payload)
			req.ExpiresAt = time.Now().Add(30 * time.Minute).Unix()
		})
		if strings.TrimSpace(messageID) != "" {
			_ = s.app.feishu.PatchCard(context.Background(), messageID, newWorkspaceRenderServiceInner(s.app).RenderWorkspaceCloneManualHintCard(sessionKey, plan.WorkspaceID, plan.TargetDir, err.Error()))
		}
		return
	}
	updated, err := s.activateBindingWorkspace(binding, plan.WorkspaceID)
	if err != nil {
		_ = s.app.State().UpdatePending(requestID, func(req *state.PendingRequest) {
			req.Status = state.PendingRequestStatusResolved.String()
			req.PayloadJSON = mustJSON(payload)
			req.ExpiresAt = time.Now().Add(30 * time.Minute).Unix()
		})
		if strings.TrimSpace(messageID) != "" {
			_ = s.app.feishu.PatchCard(context.Background(), messageID, newWorkspaceRenderServiceInner(s.app).RenderWorkspaceCloneManualHintCard(sessionKey, plan.WorkspaceID, plan.TargetDir, err.Error()))
		}
		return
	}
	_ = s.app.State().UpdatePending(requestID, func(req *state.PendingRequest) {
		req.Status = state.PendingRequestStatusResolved.String()
		req.PayloadJSON = mustJSON(payload)
	})
	if strings.TrimSpace(messageID) != "" {
		_ = s.app.feishu.PatchCard(context.Background(), messageID, newWorkspaceRenderServiceInner(s.app).RenderWorkspaceCloneSuccessCard(sessionKey, plan.WorkspaceID, plan.TargetDir))
	}
	s.replayPendingBindingMessageAsync(updated)
}

func (s bindingService) patchBindingWorkspaceCloneFailure(requestID, messageID, sessionKey string, payload appworkspacecmd.ClonePayload, errorMessage string) {
	payload.ErrorMessage = errorMessage
	_ = s.app.State().UpdatePending(requestID, func(req *state.PendingRequest) {
		req.Status = state.PendingRequestStatusPending.String()
		req.PayloadJSON = mustJSON(payload)
		req.ExpiresAt = time.Now().Add(10 * time.Minute).Unix()
	})
	if strings.TrimSpace(messageID) != "" {
		_ = s.app.feishu.PatchCard(context.Background(), messageID, newWorkspaceRenderServiceInner(s.app).RenderWorkspaceCloneCard(sessionKey, requestID, payload))
	}
}
