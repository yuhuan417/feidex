package app

import (
	"strings"

	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

func (s threadActionService) completeMenuThread(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return s.app.completeMenuCommand(action, sessionKey, primaryConversationSlash(s.app.configuredBackend()), "menu.root")
}

func (s threadActionService) completeMenuNew(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return s.app.completeMenuCommand(action, sessionKey, primaryConversationSlash(s.app.configuredBackend())+" new", "menu.thread")
}

func (s threadActionService) completeMenuInterrupt(action *feishu.CardAction, sessionKey, targetTurnID string) (*callback.CardActionTriggerResponse, error) {
	if strings.TrimSpace(targetTurnID) != "" {
		if sess := s.app.appState().session(sessionKey); sess != nil && strings.TrimSpace(sess.ActiveTurnID) != "" && strings.TrimSpace(sess.ActiveTurnID) != strings.TrimSpace(targetTurnID) {
			return &callback.CardActionTriggerResponse{
				Toast: &callback.Toast{Type: "warning", Content: "这个任务已经结束或已切换到其他任务"},
			}, nil
		}
	}
	if actions := s.app.backendActions(); actions != nil {
		return actions.completeMenuInterrupt(s.app, action, sessionKey, targetTurnID)
	}
	return s.app.completeMenuCommand(action, sessionKey, "/stop", actionStringValue(action, "parent_action"))
}

func (s threadActionService) completeThreadSandboxMenu(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return s.app.completeMenuCommand(action, sessionKey, "/thread sandbox", "menu.thread")
}

func (s threadActionService) completeThreadPolicyMenu(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return s.app.completeMenuCommand(action, sessionKey, "/thread policy", "menu.thread")
}

func (s threadActionService) completeClaudeSessionPermissionMenu(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return s.app.completeMenuCommand(action, sessionKey, "/session permissions", "menu.thread")
}

func (s threadActionService) completeThreadSandboxSet(action *feishu.CardAction, sessionKey, threadID, sandboxMode string) (*callback.CardActionTriggerResponse, error) {
	appState := s.app.appState()
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
	sess := appState.session(sessionKey)
	if sess == nil || strings.TrimSpace(sess.ActiveThreadID) == "" || strings.TrimSpace(sess.ActiveThreadID) != strings.TrimSpace(threadID) {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "当前 thread 已失效"}}, nil
	}
	sess.ActiveThreadSandboxMode = sandboxMode
	if err := appState.saveSession(sess); err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: err.Error()}}, nil
	}
	card, err := s.app.renderThreadSandboxMenuCard(sessionKey)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已更新 thread sandbox"},
		Card:  rawCard(card),
	}, nil
}

func (s threadActionService) completeThreadPolicySet(action *feishu.CardAction, sessionKey, threadID, approvalPolicy string) (*callback.CardActionTriggerResponse, error) {
	appState := s.app.appState()
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
	sess := appState.session(sessionKey)
	if sess == nil || strings.TrimSpace(sess.ActiveThreadID) == "" || strings.TrimSpace(sess.ActiveThreadID) != strings.TrimSpace(threadID) {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "当前 thread 已失效"}}, nil
	}
	sess.ActiveThreadApprovalPolicy = approvalPolicy
	if err := appState.saveSession(sess); err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: err.Error()}}, nil
	}
	card, err := s.app.renderThreadPolicyMenuCard(sessionKey)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已更新 thread policy"},
		Card:  rawCard(card),
	}, nil
}

func (s threadActionService) completeThreadResume(action *feishu.CardAction, sessionKey, threadID string) (*callback.CardActionTriggerResponse, error) {
	appState := s.app.appState()
	sess := appState.session(sessionKey)
	if sess == nil {
		sess = &state.Session{Key: sessionKey, OwnerUserID: action.UserID, ChatID: action.ChatID}
	}
	if sessionHasInFlightSubmission(sess) {
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "warning", Content: "当前任务仍在运行，请先等待结束或中断"},
		}, nil
	}
	if strings.TrimSpace(sess.OwnerUserID) == "" {
		sess.OwnerUserID = action.UserID
	}
	if strings.TrimSpace(sess.ChatID) == "" {
		sess.ChatID = action.ChatID
	}
	if strings.TrimSpace(sess.WorkspaceID) == "" {
		sess.WorkspaceID = s.app.defaultWorkspaceID()
	}
	ws := config.FindWorkspace(s.app.cfg, sess.WorkspaceID)
	if ws == nil {
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "error", Content: "workspace not found"},
		}, nil
	}
	selectedName, _ := action.ActionValue["thread_name"].(string)
	selectedPreview, _ := action.ActionValue["thread_preview"].(string)
	selectedCWD, _ := action.ActionValue["thread_cwd"].(string)
	if _, err := s.app.conversationBackend().resumeSelectedThread(sessionKey, sess, ws, threadResumeSelection{
		ThreadID: threadID,
		Name:     selectedName,
		Preview:  selectedPreview,
		Cwd:      selectedCWD,
	}); err != nil {
		toastType := "error"
		if isUIWarningError(err) {
			toastType = "warning"
		}
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: toastType, Content: err.Error()}}, nil
	}
	includeAll, _ := action.ActionValue["include_all"].(bool)
	card, err := s.app.renderThreadsCard(sessionKey, includeAll)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "success", Content: "已恢复" + primaryConversationNoun(s.app.configuredBackend())}}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已恢复" + primaryConversationNoun(s.app.configuredBackend())},
		Card:  rawCard(card),
	}, nil
}

func interruptStatusButtons(sessionKey, parentAction, targetTurnID string, includeRetry bool) []feishu.Button {
	buttons := []feishu.Button{}
	if includeRetry {
		value := map[string]any{
			"action":      "menu.interrupt",
			"session_key": sessionKey,
		}
		if strings.TrimSpace(parentAction) != "" {
			value["parent_action"] = strings.TrimSpace(parentAction)
		}
		if strings.TrimSpace(targetTurnID) != "" {
			value["turn_id"] = strings.TrimSpace(targetTurnID)
		}
		buttons = append(buttons, feishu.Button{
			Text:  "重试",
			Type:  "primary",
			Value: value,
		})
	}
	backAction := firstNonEmpty(strings.TrimSpace(parentAction), "menu.tools")
	buttons = append(buttons, feishu.Button{
		Text: "返回上一级",
		Type: "default",
		Value: map[string]any{
			"action":      backAction,
			"session_key": sessionKey,
		},
	})
	return buttons
}

func (a *App) renderInterruptPreparingCard(sessionKey, parentAction string) map[string]any {
	return a.feishu.SimpleStatusCard(
		"中断任务",
		"blue",
		menuCardBody(firstNonEmpty(strings.TrimSpace(parentAction), "menu.tools"), "正在向 Claude 请求中断当前任务，请稍候。\n\n这张卡片会自动刷新。"),
		nil,
	)
}

func (a *App) renderInterruptResultCard(sessionKey, parentAction, text string) map[string]any {
	return a.feishu.SimpleStatusCard(
		"中断任务",
		"green",
		menuCardBody(firstNonEmpty(strings.TrimSpace(parentAction), "menu.tools"), firstNonEmpty(strings.TrimSpace(text), "已请求中断当前任务。")),
		interruptStatusButtons(sessionKey, parentAction, "", false),
	)
}

func (a *App) renderInterruptFailedCard(sessionKey, parentAction, targetTurnID, errText string) map[string]any {
	body := "请求中断当前任务失败。"
	if text := strings.TrimSpace(errText); text != "" {
		body += "\n\n错误: " + text
	}
	return a.feishu.SimpleStatusCard(
		"中断任务",
		"orange",
		menuCardBody(firstNonEmpty(strings.TrimSpace(parentAction), "menu.tools"), body),
		interruptStatusButtons(sessionKey, parentAction, targetTurnID, true),
	)
}
