package feishuapp

import (
	"context"
	"feidex/internal/textutil"
	"fmt"
	"log/slog"
	"strings"
	"time"

	appworkspacecmd "feidex/internal/adapter/feishu/workspacecmd"
	workspaceapp "feidex/internal/application/workspace"
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
	binding, err := s.app.bindings.RoutingConfiguration.EnsureBinding(msg.ChatType, msg.ChatID)
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
	return s.renderer.SimpleStatusCard(setting.Title, "blue", menuCardBody(setting.MenuAction, body), buttons), nil
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
	if pending == nil || !groupBindingSessionScopeActive(s.app, pending.SessionKey) {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "工作区创建请求已过期"}}, nil
	}
	payload := appworkspacecmd.MergeNewFormValues(appworkspacecmd.NewPayloadFromPending(pending), action.FormValue)
	msg := commandMessageFromAction(s.app, action, pending.SessionKey, "/workspace new")
	binding, err := s.app.bindings.RoutingConfiguration.EnsureBinding(msg.ChatType, msg.ChatID)
	if err != nil {
		return nil, err
	}
	out, err := s.app.bindings.WorkspaceWorkflow.SubmitNew(requestID, action.UserID, payload, workspaceapp.SwitchRequest{Session: s.app.State().Session(pending.SessionKey), Binding: binding})
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}, Card: rawCard(s.app.bindings.WorkspacePresentation.RenderWorkspaceNewCard(pending.SessionKey, requestID, out.Payload))}, nil
	}
	toast := "已创建工作区"
	if out.Existing {
		toast = "已设置当前工作区"
	}
	return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "success", Content: toast}, Card: rawCard(s.app.bindings.WorkspacePresentation.RenderWorkspaceMenuCard(pending.SessionKey))}, nil
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
	mgmt := s.app.bindings.WorkspaceManagement
	parentDir := strings.TrimSpace(payload.SelectedParentDir)
	if parentDir == "" {
		parentDir = mgmt.Deps.Planning.DefaultWorkspaceCloneParent(nil)
	}
	payload.SelectedParentDir = parentDir
	msg := commandMessageFromAction(s.app, action, pending.SessionKey, "/workspace clone")
	binding, err := s.app.bindings.RoutingConfiguration.EnsureBinding(msg.ChatType, msg.ChatID)
	if err != nil {
		return nil, err
	}
	payload, plan, preparation, err := s.app.bindings.WorkspaceWorkflow.PrepareClone(requestID, action.UserID, payload, nil, workspaceapp.SwitchRequest{Session: s.app.State().Session(pending.SessionKey), Binding: binding})
	if err != nil {
		return nil, err
	}
	if plan == nil {
		return s.renderBindingWorkspacePreparation(action, pending, payload, preparation, false)
	}
	ctx, cancel := context.WithCancel(s.app.Context())
	op := appworkspacecmd.NewCloneOperation(cancel)
	messageID := textutil.FirstNonEmpty(strings.TrimSpace(pending.FeishuMsgID), strings.TrimSpace(action.MessageID))
	if err := s.app.bindings.WorkspaceWorkflow.Start(requestID, pending.Kind, action.UserID, messageID, payload); err != nil {
		cancel()
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	mgmt.SetWorkspaceCloneOperation(requestID, op)
	runAsync(s.app, func() {
		s.finishBindingWorkspaceClone(ctx, mgmt, op, requestID, messageID, pending.SessionKey, parentDir, payload, plan, binding)
	})
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "info", Content: "已开始从仓库创建工作区"},
		Card:  rawCard(s.app.bindings.WorkspacePresentation.RenderWorkspaceClonePreparingCard(requestID, payload, parentDir, op.Snapshot())),
	}, nil
}

func (s bindingService) renderBindingWorkspaceCloneFormWarning(requestID string, pending *state.PendingRequest, payload appworkspacecmd.ClonePayload, warning string) (*callback.CardActionTriggerResponse, error) {
	payload.ErrorMessage = warning
	if err := s.app.bindings.Forms.SaveDraft(requestID, payload, state.PendingRequestStatusPending.String(), 10*time.Minute, ""); err != nil {
		return nil, err
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "warning", Content: warning},
		Card:  rawCard(s.app.bindings.WorkspacePresentation.RenderWorkspaceCloneCard(pending.SessionKey, requestID, payload)),
	}, nil
}

func (s bindingService) finishBindingWorkspaceClone(ctx context.Context, mgmt *appworkspacecmd.ManagementService, op *appworkspacecmd.CloneOperation, requestID, messageID, sessionKey, parentDir string, payload appworkspacecmd.ClonePayload, plan *appworkspacecmd.ClonePlan, binding *state.AgentBinding) {

	defer mgmt.ClearWorkspaceCloneOperation(requestID)
	out, err := s.app.bindings.WorkspaceWorkflow.FinishClone(ctx, requestID, workspaceapp.SwitchRequest{Session: s.app.State().Session(sessionKey), Binding: binding}, payload, plan, func(line string) {
		snapshot, shouldPatch := op.RecordProgress(line)
		if shouldPatch && strings.TrimSpace(messageID) != "" {
			_ = patchCardEffect(s.app.Context(), s.app, messageID, s.app.bindings.WorkspacePresentation.RenderWorkspaceClonePreparingCard(requestID, payload, parentDir, snapshot))
		}
	})
	if err != nil {
		slog.Warn("workspace clone result save failed", "error", err)
		return
	}
	var card map[string]any
	switch out.Outcome {
	case workspaceapp.CreationCancelled:
		card = s.app.bindings.WorkspacePresentation.RenderWorkspaceCloneCanceledCard(sessionKey, out.Payload, parentDir, op.Snapshot())
	case workspaceapp.CreationTakeover:
		card = s.app.bindings.WorkspacePresentation.RenderWorkspaceCloneManualHintCard(sessionKey, out.WorkspaceID, out.TargetDir, out.Error)
	case workspaceapp.CreationFailed:
		card = s.app.bindings.WorkspacePresentation.RenderWorkspaceCloneCard(sessionKey, requestID, out.Payload)
	default:
		card = s.app.bindings.WorkspacePresentation.RenderWorkspaceCloneSuccessCard(sessionKey, out.WorkspaceID, out.TargetDir)
	}
	if strings.TrimSpace(messageID) != "" {
		_ = patchCardEffect(s.app.Context(), s.app, messageID, card)
	}
}

func (s bindingService) completeBindingWorkspaceWorktreeSubmit(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	requestID := actionStringValue(action, "request_id")
	pending := s.app.State().Pending(requestID)
	if pending == nil || pending.Kind != "workspace_worktree" || !groupBindingSessionScopeActive(s.app, pending.SessionKey) {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "Worktree 创建请求已过期"}}, nil
	}
	if pending.OwnerUserID != "" && pending.OwnerUserID != action.UserID {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "你没有权限处理这个工作区请求"}}, nil
	}
	payload := appworkspacecmd.MergeWorktreeFormValues(appworkspacecmd.WorktreePayloadFromPending(pending), action.FormValue)
	payload.ErrorMessage = ""
	mgmt := s.app.bindings.WorkspaceManagement
	if status := state.NormalizePendingRequestStatus(pending.Status); status == state.PendingRequestStatusProcessing || status == state.PendingRequestStatusCancelling {
		snapshot := appworkspacecmd.CloneProgressSnapshot{State: status.String()}
		if op := mgmt.GetWorkspaceCloneOperation(requestID); op != nil {
			snapshot = op.Snapshot()
		}
		plan, _ := mgmt.Deps.Planning.PrepareWorkspaceWorktree(payload)
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "info", Content: "正在创建 Worktree 工作区"},
			Card:  rawCard(s.app.bindings.WorkspacePresentation.RenderWorkspaceWorktreePreparingCard(requestID, payload, plan, snapshot)),
		}, nil
	}
	msg := commandMessageFromAction(s.app, action, pending.SessionKey, "/workspace new worktree")
	binding, err := s.app.bindings.RoutingConfiguration.EnsureBinding(msg.ChatType, msg.ChatID)
	if err != nil {
		return nil, err
	}
	payload, plan, preparation, err := s.app.bindings.WorkspaceWorkflow.PrepareWorktree(requestID, action.UserID, payload, workspaceapp.SwitchRequest{Session: s.app.State().Session(pending.SessionKey), Binding: binding})
	if err != nil {
		return nil, err
	}
	if plan == nil {
		return s.renderBindingWorkspacePreparation(action, pending, payload, preparation, true)
	}
	payload.BaseWorkspaceID = plan.BaseWorkspaceID
	payload.BranchName = plan.BranchName
	payload.WorkspaceID = plan.WorkspaceID
	payload.DirectoryName = plan.DirectoryName
	payload.TargetDir = plan.TargetDir
	ctx, cancel := context.WithCancel(s.app.Context())
	op := appworkspacecmd.NewCloneOperation(cancel)
	messageID := textutil.FirstNonEmpty(strings.TrimSpace(pending.FeishuMsgID), strings.TrimSpace(action.MessageID))
	if err := s.app.bindings.WorkspaceWorkflow.Start(requestID, pending.Kind, action.UserID, messageID, payload); err != nil {
		cancel()
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	mgmt.SetWorkspaceCloneOperation(requestID, op)
	runAsync(s.app, func() {
		s.finishBindingWorkspaceWorktree(ctx, mgmt, op, requestID, messageID, pending.SessionKey, payload, plan, binding)
	})
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "info", Content: "已开始创建 Worktree 工作区"},
		Card:  rawCard(s.app.bindings.WorkspacePresentation.RenderWorkspaceWorktreePreparingCard(requestID, payload, plan, op.Snapshot())),
	}, nil
}

func (s bindingService) finishBindingWorkspaceWorktree(ctx context.Context, mgmt *appworkspacecmd.ManagementService, op *appworkspacecmd.CloneOperation, requestID, messageID, sessionKey string, payload appworkspacecmd.WorktreePayload, plan *appworkspacecmd.WorktreePlan, binding *state.AgentBinding) {

	defer mgmt.ClearWorkspaceCloneOperation(requestID)
	out, err := s.app.bindings.WorkspaceWorkflow.FinishWorktree(ctx, requestID, workspaceapp.SwitchRequest{Session: s.app.State().Session(sessionKey), Binding: binding}, payload, plan)
	if err != nil {
		slog.Warn("workspace worktree result save failed", "error", err)
		return
	}
	var card map[string]any
	switch out.Outcome {
	case workspaceapp.CreationCancelled:
		card = s.app.bindings.WorkspacePresentation.RenderWorkspaceWorktreeCanceledCard(sessionKey, out.Payload, plan, op.Snapshot())
	case workspaceapp.CreationTakeover:
		card = s.app.bindings.WorkspacePresentation.RenderWorkspaceWorktreeManualHintCard(sessionKey, out.WorkspaceID, out.TargetDir, out.Error)
	case workspaceapp.CreationFailed:
		card = s.app.bindings.WorkspacePresentation.RenderWorkspaceWorktreeCard(sessionKey, requestID, out.Payload)
	default:
		card = s.app.bindings.WorkspacePresentation.RenderWorkspaceWorktreeSuccessCard(sessionKey, out.WorkspaceID, out.TargetDir)
	}
	if strings.TrimSpace(messageID) != "" {
		_ = patchCardEffect(s.app.Context(), s.app, messageID, card)
	}
}

func (s bindingService) renderBindingWorkspacePreparation(action *feishu.CardAction, pending *state.PendingRequest, payload any, preparation workspaceapp.Preparation, worktree bool) (*callback.CardActionTriggerResponse, error) {
	kind, toast := "warning", preparation.Error
	var card map[string]any
	switch preparation.Outcome {
	case workspaceapp.CreationCompleted:
		kind, toast = "success", "已设置当前工作区"
		card = s.app.bindings.WorkspacePresentation.RenderWorkspaceMenuCard(pending.SessionKey)
	case workspaceapp.CreationTakeover:
		kind, toast = "info", "clone 目标目录已存在，已打开预填好的新建工作区"
		if worktree {
			toast = "worktree 目标目录已存在，已打开预填好的新建工作区"
		}
		card = s.app.bindings.WorkspacePresentation.RenderWorkspaceNewCard(pending.SessionKey, preparation.Takeover.ID, preparation.NewPayload)
	default:
		if worktree {
			card = s.app.bindings.WorkspacePresentation.RenderWorkspaceWorktreeCard(pending.SessionKey, pending.ID, payload.(appworkspacecmd.WorktreePayload))
		} else {
			card = s.app.bindings.WorkspacePresentation.RenderWorkspaceCloneCard(pending.SessionKey, pending.ID, payload.(appworkspacecmd.ClonePayload))
		}
	}
	return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: kind, Content: toast}, Card: rawCard(card)}, nil
}
