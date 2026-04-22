package app

import (
	"strings"

	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

func (a *App) completeMenuThread(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return a.completeMenuCommand(action, sessionKey, primaryConversationSlash(a.configuredBackend()), "menu.root")
}

func (a *App) completeMenuNew(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return a.completeMenuCommand(action, sessionKey, primaryConversationSlash(a.configuredBackend())+" new", "menu.thread")
}

func (a *App) completeMenuInterrupt(action *feishu.CardAction, sessionKey, targetTurnID string) (*callback.CardActionTriggerResponse, error) {
	if strings.TrimSpace(targetTurnID) != "" {
		if sess := a.appState().session(sessionKey); sess != nil && strings.TrimSpace(sess.ActiveTurnID) != "" && strings.TrimSpace(sess.ActiveTurnID) != strings.TrimSpace(targetTurnID) {
			return &callback.CardActionTriggerResponse{
				Toast: &callback.Toast{Type: "warning", Content: "这个任务已经结束或已切换到其他任务"},
			}, nil
		}
	}
	return a.completeMenuCommand(action, sessionKey, "/stop", actionStringValue(action, "parent_action"))
}

func (a *App) completeThreadSandboxMenu(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return a.completeMenuCommand(action, sessionKey, "/thread sandbox", "menu.thread")
}

func (a *App) completeThreadPolicyMenu(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return a.completeMenuCommand(action, sessionKey, "/thread policy", "menu.thread")
}

func (a *App) completeClaudeSessionPermissionMenu(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return a.completeMenuCommand(action, sessionKey, "/session permissions", "menu.thread")
}

func (a *App) completeThreadSandboxSet(action *feishu.CardAction, sessionKey, threadID, sandboxMode string) (*callback.CardActionTriggerResponse, error) {
	appState := a.appState()
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
	card, err := a.renderThreadSandboxMenuCard(sessionKey)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已更新 thread sandbox"},
		Card:  rawCard(card),
	}, nil
}

func (a *App) completeThreadPolicySet(action *feishu.CardAction, sessionKey, threadID, approvalPolicy string) (*callback.CardActionTriggerResponse, error) {
	appState := a.appState()
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
	card, err := a.renderThreadPolicyMenuCard(sessionKey)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已更新 thread policy"},
		Card:  rawCard(card),
	}, nil
}

func (a *App) completeThreadResume(action *feishu.CardAction, sessionKey, threadID string) (*callback.CardActionTriggerResponse, error) {
	appState := a.appState()
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
		sess.WorkspaceID = a.defaultWorkspaceID()
	}
	ws := config.FindWorkspace(a.cfg, sess.WorkspaceID)
	if ws == nil {
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "error", Content: "workspace not found"},
		}, nil
	}
	selectedName, _ := action.ActionValue["thread_name"].(string)
	selectedPreview, _ := action.ActionValue["thread_preview"].(string)
	selectedCWD, _ := action.ActionValue["thread_cwd"].(string)
	if _, err := a.conversationBackend().resumeSelectedThread(sessionKey, sess, ws, threadResumeSelection{
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
	card, err := a.renderThreadsCard(sessionKey, includeAll)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "success", Content: "已恢复" + primaryConversationNoun(a.configuredBackend())}}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已恢复" + primaryConversationNoun(a.configuredBackend())},
		Card:  rawCard(card),
	}, nil
}
