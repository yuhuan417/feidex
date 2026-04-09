package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"feidex/internal/codexrpc"
	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

func (a *App) dispatchCardAction(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	if action == nil {
		return &callback.CardActionTriggerResponse{}, nil
	}
	name, _ := action.ActionValue["action"].(string)
	if strings.TrimSpace(name) == "" {
		if alt := strings.TrimSpace(action.Name); alt != "" {
			if strings.HasPrefix(alt, "turn.item.toggle:") {
				name = "turn.item.toggle"
			} else {
				name = alt
			}
		}
	}
	switch name {
	case "menu.root":
		sessionKey, _ := action.ActionValue["session_key"].(string)
		return a.completeMenuRoot(action, sessionKey)
	case "menu.group.session":
		sessionKey, _ := action.ActionValue["session_key"].(string)
		return a.completeMenuGroupSession(action, sessionKey)
	case "menu.group.context":
		sessionKey, _ := action.ActionValue["session_key"].(string)
		return a.completeMenuGroupContext(action, sessionKey)
	case "menu.group.model":
		sessionKey, _ := action.ActionValue["session_key"].(string)
		return a.completeMenuGroupModel(action, sessionKey)
	case "menu.group.system":
		sessionKey, _ := action.ActionValue["session_key"].(string)
		return a.completeMenuGroupSystem(action, sessionKey)
	case "menu.new":
		sessionKey, _ := action.ActionValue["session_key"].(string)
		return a.completeMenuNew(action, sessionKey)
	case "menu.quiet":
		sessionKey, _ := action.ActionValue["session_key"].(string)
		return a.completeMenuQuiet(action, sessionKey)
	case "menu.compact":
		sessionKey, _ := action.ActionValue["session_key"].(string)
		return a.completeMenuCompact(action, sessionKey)
	case "menu.usage":
		sessionKey, _ := action.ActionValue["session_key"].(string)
		return a.completeMenuUsage(action, sessionKey)
	case "menu.model":
		sessionKey, _ := action.ActionValue["session_key"].(string)
		return a.completeMenuModel(action, sessionKey)
	case "menu.reasoning":
		sessionKey, _ := action.ActionValue["session_key"].(string)
		return a.completeMenuReasoning(action, sessionKey)
	case "menu.fast":
		sessionKey, _ := action.ActionValue["session_key"].(string)
		return a.completeMenuFast(action, sessionKey)
	case "menu.status":
		sessionKey, _ := action.ActionValue["session_key"].(string)
		return a.completeMenuStatus(action, sessionKey)
	case "menu.help":
		sessionKey, _ := action.ActionValue["session_key"].(string)
		return a.completeMenuHelp(action, sessionKey)
	case "menu.history":
		sessionKey, _ := action.ActionValue["session_key"].(string)
		return a.completeMenuHistory(action, sessionKey)
	case "menu.upgrade":
		return a.completeMenuUpgrade(action)
	case "quiet.set":
		enabled, _ := action.ActionValue["enabled"].(bool)
		return a.completeQuietSet(action, enabled)
	case "service_tier.set":
		sessionKey, _ := action.ActionValue["session_key"].(string)
		threadID, _ := action.ActionValue["thread_id"].(string)
		serviceTier, _ := action.ActionValue["service_tier"].(string)
		return a.completeServiceTierSet(action, sessionKey, threadID, serviceTier)
	case "model.config.set_model":
		modelID, _ := action.ActionValue["model_id"].(string)
		return a.completeGlobalModelSet(action, modelID)
	case "model.config.set_effort":
		reasoningEffort, _ := action.ActionValue["reasoning_effort"].(string)
		return a.completeGlobalReasoningEffortSet(action, reasoningEffort)
	case "menu.threads":
		sessionKey, _ := action.ActionValue["session_key"].(string)
		return a.completeMenuThreads(action, sessionKey)
	case "menu.interrupt":
		sessionKey, _ := action.ActionValue["session_key"].(string)
		turnID, _ := action.ActionValue["turn_id"].(string)
		return a.completeMenuInterrupt(action, sessionKey, turnID)
	case "menu.workspace":
		sessionKey, _ := action.ActionValue["session_key"].(string)
		return a.completeMenuWorkspace(action, sessionKey)
	case "workspace.use":
		sessionKey, _ := action.ActionValue["session_key"].(string)
		workspaceID, _ := action.ActionValue["workspace_id"].(string)
		return a.completeWorkspaceUse(action, sessionKey, workspaceID)
	case "workspace.new":
		sessionKey, _ := action.ActionValue["session_key"].(string)
		return a.completeWorkspaceNew(action, sessionKey)
	case "workspace.sandbox.menu":
		sessionKey, _ := action.ActionValue["session_key"].(string)
		return a.completeWorkspaceSandboxMenu(action, sessionKey)
	case "workspace.policy.menu":
		sessionKey, _ := action.ActionValue["session_key"].(string)
		return a.completeWorkspacePolicyMenu(action, sessionKey)
	case "workspace.sandbox.set":
		sessionKey, _ := action.ActionValue["session_key"].(string)
		workspaceID, _ := action.ActionValue["workspace_id"].(string)
		sandboxMode, _ := action.ActionValue["sandbox_mode"].(string)
		return a.completeWorkspaceSandboxSet(action, sessionKey, workspaceID, sandboxMode)
	case "workspace.policy.set":
		sessionKey, _ := action.ActionValue["session_key"].(string)
		workspaceID, _ := action.ActionValue["workspace_id"].(string)
		approvalPolicy, _ := action.ActionValue["approval_policy"].(string)
		return a.completeWorkspacePolicySet(action, sessionKey, workspaceID, approvalPolicy)
	case "thread.sandbox.menu":
		sessionKey, _ := action.ActionValue["session_key"].(string)
		return a.completeThreadSandboxMenu(action, sessionKey)
	case "thread.policy.menu":
		sessionKey, _ := action.ActionValue["session_key"].(string)
		return a.completeThreadPolicyMenu(action, sessionKey)
	case "thread.sandbox.set":
		sessionKey, _ := action.ActionValue["session_key"].(string)
		threadID, _ := action.ActionValue["thread_id"].(string)
		sandboxMode, _ := action.ActionValue["sandbox_mode"].(string)
		return a.completeThreadSandboxSet(action, sessionKey, threadID, sandboxMode)
	case "thread.policy.set":
		sessionKey, _ := action.ActionValue["session_key"].(string)
		threadID, _ := action.ActionValue["thread_id"].(string)
		approvalPolicy, _ := action.ActionValue["approval_policy"].(string)
		return a.completeThreadPolicySet(action, sessionKey, threadID, approvalPolicy)
	case "thread.resume":
		sessionKey, _ := action.ActionValue["session_key"].(string)
		threadID, _ := action.ActionValue["thread_id"].(string)
		return a.completeThreadResume(action, sessionKey, threadID)
	case "history.page":
		sessionKey, _ := action.ActionValue["session_key"].(string)
		page, _ := action.ActionValue["page"].(float64)
		return a.completeHistoryPage(action, sessionKey, int(page))
	case "history.detail":
		sessionKey, _ := action.ActionValue["session_key"].(string)
		index, _ := action.ActionValue["index"].(float64)
		return a.completeHistoryDetail(action, sessionKey, int(index))
	case "turn.append":
		sessionKey, _ := action.ActionValue["session_key"].(string)
		turnID, _ := action.ActionValue["turn_id"].(string)
		itemID, _ := action.ActionValue["item_id"].(string)
		return a.completeTurnAppend(action, sessionKey, turnID, itemID)
	case "turn.item.toggle":
		return a.completeTurnItemToggle(action)
	case "user_input.answer":
		return a.completeUserInputAnswer(action)
	case "upgrade.confirm", "upgrade.cancel":
		return a.completeUpgradeAction(action, name)
	case "approval.command.accept", "approval.command.accept_session", "approval.command.decline", "approval.command.cancel",
		"approval.file.accept", "approval.file.accept_session", "approval.file.decline", "approval.file.cancel",
		"approval.permissions.accept_turn", "approval.permissions.accept_session":
		return a.completeApprovalAction(action, name)
	case "pending_form.cancel":
		return a.completePendingFormCancel(action)
	case "elicitation_url.accept", "elicitation_url.decline", "elicitation_url.cancel":
		return a.completeElicitationURLAction(action, name)
	default:
		slog.Warn("unknown feishu card action",
			"name", name,
			"raw_name", action.Name,
			"message_id", action.MessageID,
			"chat_id", action.ChatID,
			"user_id", action.UserID,
			"action_value", fmt.Sprintf("%v", action.ActionValue),
			"form_value", fmt.Sprintf("%v", action.FormValue),
		)
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "warning", Content: "未知操作"},
		}, nil
	}
}

func (a *App) completeMenuRoot(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "info", Content: "已返回命令菜单"},
		Card:  rawCard(a.renderCommandMenuCard(sessionKey)),
	}, nil
}

func (a *App) completeMenuGroupSession(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "info", Content: "已打开会话行为"},
		Card:  rawCard(a.renderSessionMenuCard(sessionKey)),
	}, nil
}

func (a *App) completeMenuGroupContext(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "info", Content: "已打开会话管理"},
		Card:  rawCard(a.renderContextMenuCard(sessionKey)),
	}, nil
}

func (a *App) completeMenuGroupModel(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "info", Content: "已打开模型能力"},
		Card:  rawCard(a.renderModelMenuCard(sessionKey)),
	}, nil
}

func (a *App) completeMenuGroupSystem(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "info", Content: "已打开服务管理"},
		Card:  rawCard(a.renderSystemMenuCard(sessionKey)),
	}, nil
}

func (a *App) renderMenuNodeCard(actionName, sessionKey string) (map[string]any, bool) {
	switch strings.TrimSpace(actionName) {
	case "menu.root":
		return a.renderCommandMenuCard(sessionKey), true
	case "menu.group.session":
		return a.renderSessionMenuCard(sessionKey), true
	case "menu.group.context":
		return a.renderContextMenuCard(sessionKey), true
	case "menu.group.model":
		return a.renderModelMenuCard(sessionKey), true
	case "menu.group.system":
		return a.renderSystemMenuCard(sessionKey), true
	default:
		return nil, false
	}
}

func (a *App) completeMenuNew(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	parentAction, _ := action.ActionValue["parent_action"].(string)
	sess := a.store.GetSession(sessionKey)
	if sess == nil {
		sess = &state.Session{Key: sessionKey, OwnerUserID: action.UserID, ChatID: action.ChatID}
	}
	if sessionHasActiveWork(sess) {
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "warning", Content: "当前任务仍在运行，请先等待结束或中断"},
		}, nil
	}
	discarded := a.discardSessionPendingInputs(sessionKey)
	sess = a.store.GetSession(sessionKey)
	if sess == nil {
		sess = &state.Session{Key: sessionKey, OwnerUserID: action.UserID, ChatID: action.ChatID}
	}
	clearSessionThreadContext(sess)
	a.clearSessionLiveThread(sessionKey)
	sess.ActiveTurnID = ""
	sess.ActiveSubmissionID = ""
	sess.Status = "idle"
	sess.Queue = nil
	sess.StagedImages = nil
	_ = a.store.UpsertSession(sess)
	content := "已切换到新会话"
	if discarded > 0 {
		content = fmt.Sprintf("已切换到新会话，并丢弃 %d 条排队或暂存输入", discarded)
	}
	if parentAction == "menu.threads" {
		card, err := a.renderThreadsCard(sessionKey, false)
		if err == nil {
			return &callback.CardActionTriggerResponse{
				Toast: &callback.Toast{Type: "success", Content: content},
				Card:  rawCard(card),
			}, nil
		}
	}
	if card, ok := a.renderMenuNodeCard(parentAction, sessionKey); ok {
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "success", Content: content},
			Card:  rawCard(card),
		}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: content},
	}, nil
}

func (a *App) completeMenuCompact(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	parentAction := "menu.group.context"
	if action != nil {
		if value, ok := action.ActionValue["parent_action"].(string); ok && strings.TrimSpace(value) != "" {
			parentAction = value
		}
	}
	if _, err := a.startThreadCompaction(sessionKey); err != nil {
		if card, ok := a.renderMenuNodeCard(parentAction, sessionKey); ok {
			return &callback.CardActionTriggerResponse{
				Toast: &callback.Toast{Type: "warning", Content: err.Error()},
				Card:  rawCard(card),
			}, nil
		}
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	if card, ok := a.renderMenuNodeCard(parentAction, sessionKey); ok {
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "success", Content: "已请求压缩当前线程上下文"},
			Card:  rawCard(card),
		}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已请求压缩当前线程上下文"},
	}, nil
}

func (a *App) completeGlobalModelSet(action *feishu.CardAction, modelID string) (*callback.CardActionTriggerResponse, error) {
	sessionKey, _ := action.ActionValue["session_key"].(string)
	menuAction, _ := action.ActionValue["menu_action"].(string)
	if strings.TrimSpace(menuAction) == "" {
		menuAction = "menu.model"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	result, err := a.fetchModelList(ctx)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: err.Error()}}, nil
	}
	if err := a.updateGlobalModelConfig(func(c *config.CodexConfig) {
		c.Model = strings.TrimSpace(modelID)
	}, result); err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: err.Error()}}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已更新全局模型"},
		Card:  rawCard(a.renderModelConfigCard(result, sessionKey, menuAction)),
	}, nil
}

func (a *App) completeGlobalReasoningEffortSet(action *feishu.CardAction, reasoningEffort string) (*callback.CardActionTriggerResponse, error) {
	sessionKey, _ := action.ActionValue["session_key"].(string)
	menuAction, _ := action.ActionValue["menu_action"].(string)
	if strings.TrimSpace(menuAction) == "" {
		menuAction = "menu.model"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	result, err := a.fetchModelList(ctx)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: err.Error()}}, nil
	}
	selectedModel, _ := effectiveConfiguredModelAndEffort(a.cfg, result)
	if strings.TrimSpace(reasoningEffort) != "" && !modelSupportsEffort(selectedModel, reasoningEffort) {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "当前模型不支持这个推理强度"}}, nil
	}
	if err := a.updateGlobalModelConfig(func(c *config.CodexConfig) {
		c.ReasoningEffort = strings.TrimSpace(reasoningEffort)
	}, result); err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: err.Error()}}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已更新全局推理强度"},
		Card:  rawCard(a.renderModelConfigCard(result, sessionKey, menuAction)),
	}, nil
}

func (a *App) completeMenuQuiet(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "info", Content: "已打开 quiet 配置"},
		Card:  rawCard(a.renderQuietModeMenuCard(sessionKey)),
	}, nil
}

func (a *App) completeMenuUsage(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "info", Content: "已打开 token usage"},
		Card:  rawCard(a.renderUsageCard(sessionKey)),
	}, nil
}

func (a *App) completeQuietSet(action *feishu.CardAction, enabled bool) (*callback.CardActionTriggerResponse, error) {
	sessionKey, _ := action.ActionValue["session_key"].(string)
	if err := a.updateQuietMode(enabled); err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: err.Error()}}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已更新 quiet 开关"},
		Card:  rawCard(a.renderQuietModeMenuCard(sessionKey)),
	}, nil
}

func (a *App) completeMenuThreads(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	card, err := a.renderThreadsCard(sessionKey, false)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "info", Content: "已打开线程列表"},
		Card:  rawCard(card),
	}, nil
}

func (a *App) completeMenuModel(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	result, err := a.fetchModelList(ctx)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "info", Content: "已打开模型配置"},
		Card:  rawCard(a.renderModelConfigCard(result, sessionKey, "menu.model")),
	}, nil
}

func (a *App) completeMenuReasoning(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	result, err := a.fetchModelList(ctx)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "info", Content: "已打开推理强度配置"},
		Card:  rawCard(a.renderModelConfigCard(result, sessionKey, "menu.reasoning")),
	}, nil
}

func (a *App) completeMenuStatus(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "info", Content: "已打开状态面板"},
		Card:  rawCard(a.renderStatusCard(sessionKey)),
	}, nil
}

func (a *App) completeMenuHelp(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "info", Content: "已打开帮助说明"},
		Card:  rawCard(a.renderHelpCard(sessionKey)),
	}, nil
}

func (a *App) completeMenuHistory(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	card, err := a.renderHistoryCard(sessionKey, 0)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "info", Content: "已打开历史记录"},
		Card:  rawCard(card),
	}, nil
}

func (a *App) completeHistoryPage(action *feishu.CardAction, sessionKey string, page int) (*callback.CardActionTriggerResponse, error) {
	card, err := a.renderHistoryCard(sessionKey, page)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	return &callback.CardActionTriggerResponse{
		Card: rawCard(card),
	}, nil
}

func (a *App) completeHistoryDetail(action *feishu.CardAction, sessionKey string, index int) (*callback.CardActionTriggerResponse, error) {
	card, err := a.renderHistoryDetailCard(sessionKey, index)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	return &callback.CardActionTriggerResponse{
		Card: rawCard(card),
	}, nil
}

func (a *App) completeMenuFast(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "info", Content: "已打开 service tier 配置"},
		Card:  rawCard(a.renderServiceTierMenuCard(sessionKey)),
	}, nil
}

func (a *App) completeServiceTierSet(action *feishu.CardAction, sessionKey, threadID, serviceTier string) (*callback.CardActionTriggerResponse, error) {
	if _, err := a.setThreadServiceTier(sessionKey, threadID, serviceTier); err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已更新 service tier"},
		Card:  rawCard(a.renderServiceTierMenuCard(sessionKey)),
	}, nil
}

func (a *App) completeMenuUpgrade(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	sessionKey, _ := action.ActionValue["session_key"].(string)
	card, err := a.renderUpgradeCard(sessionKey, action.UserID)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "info", Content: "已打开升级面板"},
		Card:  rawCard(card),
	}, nil
}

func (a *App) completeMenuInterrupt(action *feishu.CardAction, sessionKey, targetTurnID string) (*callback.CardActionTriggerResponse, error) {
	parentAction := ""
	if action != nil {
		parentAction, _ = action.ActionValue["parent_action"].(string)
	}
	sess := a.store.GetSession(sessionKey)
	discarded := a.discardSessionPendingInputs(sessionKey)
	sess = a.store.GetSession(sessionKey)
	if sess == nil || sess.ActiveTurnID == "" {
		if discarded > 0 {
			if card, ok := a.renderMenuNodeCard(parentAction, sessionKey); ok {
				return &callback.CardActionTriggerResponse{
					Toast: &callback.Toast{Type: "success", Content: fmt.Sprintf("已清空 %d 条排队或暂存输入", discarded)},
					Card:  rawCard(card),
				}, nil
			}
			return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "success", Content: fmt.Sprintf("已清空 %d 条排队或暂存输入", discarded)}}, nil
		}
		if card, ok := a.renderMenuNodeCard(parentAction, sessionKey); ok {
			return &callback.CardActionTriggerResponse{
				Toast: &callback.Toast{Type: "warning", Content: "当前没有运行中的任务"},
				Card:  rawCard(card),
			}, nil
		}
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "当前没有运行中的任务"}}, nil
	}
	if strings.TrimSpace(targetTurnID) != "" && sess.ActiveTurnID != targetTurnID {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "这个任务已经结束或已切换到其他任务"}}, nil
	}
	go func() {
		_ = a.codex.Call(context.Background(), "turn/interrupt", map[string]any{
			"threadId": sess.ActiveThreadID,
			"turnId":   sess.ActiveTurnID,
		}, nil)
	}()
	content := "已请求中断"
	if discarded > 0 {
		content = fmt.Sprintf("已请求中断，并清空 %d 条排队或暂存输入", discarded)
	}
	if card, ok := a.renderMenuNodeCard(parentAction, sessionKey); ok {
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "success", Content: content},
			Card:  rawCard(card),
		}, nil
	}
	return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "success", Content: content}}, nil
}

func (a *App) completeMenuWorkspace(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "info", Content: "已切换到工作区菜单"},
		Card:  rawCard(a.renderWorkspaceMenuCard(sessionKey)),
	}, nil
}

func (a *App) completeTurnAppend(action *feishu.CardAction, sessionKey, targetTurnID, itemID string) (*callback.CardActionTriggerResponse, error) {
	sess := a.store.GetSession(sessionKey)
	if sess == nil || sess.ActiveTurnID == "" || sess.ActiveThreadID == "" {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "当前没有可追加的任务"}}, nil
	}
	if strings.TrimSpace(targetTurnID) != "" && sess.ActiveTurnID != targetTurnID {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "这个任务已经结束或已切换到其他任务"}}, nil
	}
	a.resolvePendingTurnAppendRequests(sessionKey, action.UserID)
	requestID, err := a.store.NextLocalID("turn-append")
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: err.Error()}}, nil
	}
	body := "请直接发送要追加到当前任务的文本。\n\n下一条非命令消息会作为补充输入提交到当前 turn。"
	card := a.feishu.SimpleStatusCard("补充当前任务", "orange", body, []feishu.Button{
		{Text: "取消", Type: "default", Value: map[string]any{"action": "pending_form.cancel", "request_id": requestID}},
	})
	msgID, err := a.feishu.SendCard(context.Background(), action.ChatID, card)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: err.Error()}}, nil
	}
	_ = a.store.UpsertPending(&state.PendingRequest{
		ID:          requestID,
		Kind:        "turn_append",
		SessionKey:  sessionKey,
		ThreadID:    sess.ActiveThreadID,
		TurnID:      sess.ActiveTurnID,
		ItemID:      itemID,
		OwnerUserID: action.UserID,
		FeishuMsgID: msgID,
		Status:      "pending",
		CreatedAt:   time.Now().Unix(),
		ExpiresAt:   time.Now().Add(10 * time.Minute).Unix(),
	})
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "请发送要追加的内容"},
	}, nil
}

func (a *App) completeTurnItemToggle(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	requestID, _ := action.ActionValue["request_id"].(string)
	if strings.TrimSpace(requestID) == "" {
		if parsedID, _, ok := parseTurnItemToggleName(action.Name); ok {
			requestID = parsedID
		}
	}
	pending := a.store.PendingByID(requestID)
	if pending == nil || pending.Kind != "turn_item_card" {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "详情卡已失效"}}, nil
	}
	if pending.OwnerUserID != "" && pending.OwnerUserID != action.UserID {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "你没有权限操作这张卡片"}}, nil
	}
	var payload turnItemCardPayload
	if err := json.Unmarshal([]byte(pending.PayloadJSON), &payload); err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: "详情卡数据损坏"}}, nil
	}
	expanded, _ := action.ActionValue["expanded"].(bool)
	if !expanded {
		if _, parsedExpanded, ok := parseTurnItemToggleName(action.Name); ok {
			expanded = parsedExpanded
		}
	}
	sub := a.store.GetSubmission(payload.SubmissionID)
	includeActions := false
	if sess := a.store.GetSession(payload.SessionKey); sess != nil && sess.ActiveTurnID == payload.TurnID {
		includeActions = true
	}
	card := a.renderTurnItemCard(sub, payload, !expanded, includeActions, requestID)
	return &callback.CardActionTriggerResponse{
		Card: rawCard(card),
	}, nil
}

func parseTurnItemToggleName(name string) (requestID string, expanded bool, ok bool) {
	const prefix = "turn.item.toggle:"
	name = strings.TrimSpace(name)
	if !strings.HasPrefix(name, prefix) {
		return "", false, false
	}
	parts := strings.Split(strings.TrimPrefix(name, prefix), ":")
	if len(parts) != 2 {
		return "", false, false
	}
	requestID = strings.TrimSpace(parts[0])
	state := strings.TrimSpace(parts[1])
	switch state {
	case "expanded":
		return requestID, true, requestID != ""
	case "collapsed":
		return requestID, false, requestID != ""
	default:
		return "", false, false
	}
}

func (a *App) completeWorkspaceUse(action *feishu.CardAction, sessionKey, workspaceID string) (*callback.CardActionTriggerResponse, error) {
	ws := config.FindWorkspace(a.cfg, workspaceID)
	if ws == nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: "工作区不存在"}}, nil
	}
	sess := a.store.GetSession(sessionKey)
	if sess == nil {
		sess = &state.Session{Key: sessionKey, OwnerUserID: action.UserID, ChatID: action.ChatID}
	}
	switchSessionWorkspace(sess, workspaceID)
	_ = a.store.UpsertSession(sess)
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已切换工作区"},
		Card:  rawCard(a.renderWorkspaceMenuCard(sessionKey)),
	}, nil
}

func (a *App) completeWorkspaceNew(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	requestID, err := a.store.NextLocalID("workspace")
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: err.Error()}}, nil
	}
	_ = a.store.UpsertPending(&state.PendingRequest{
		ID:          requestID,
		Kind:        "workspace_new",
		SessionKey:  sessionKey,
		OwnerUserID: action.UserID,
		FeishuMsgID: action.MessageID,
		Status:      "pending",
		CreatedAt:   time.Now().Unix(),
		ExpiresAt:   time.Now().Add(10 * time.Minute).Unix(),
	})
	card := a.renderWorkspaceNewCard(sessionKey, requestID)
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "info", Content: "请按提示发送工作区信息"},
		Card:  rawCard(card),
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

func (a *App) completeThreadSandboxMenu(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	card, err := a.renderThreadSandboxMenuCard(sessionKey)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "info", Content: "已打开 thread sandbox 配置"},
		Card:  rawCard(card),
	}, nil
}

func (a *App) completeThreadPolicyMenu(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	card, err := a.renderThreadPolicyMenuCard(sessionKey)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "info", Content: "已打开 thread policy 配置"},
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
	ws, err := a.updateWorkspaceDefaults(workspaceID, func(w *config.Workspace) {
		w.SandboxMode = sandboxMode
	})
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: err.Error()}}, nil
	}
	_ = ws
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
	ws, err := a.updateWorkspaceDefaults(workspaceID, func(w *config.Workspace) {
		w.ApprovalPolicy = approvalPolicy
	})
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: err.Error()}}, nil
	}
	_ = ws
	card, renderErr := a.renderWorkspacePolicyMenuCard(sessionKey)
	if renderErr != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: renderErr.Error()}}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已更新 policy"},
		Card:  rawCard(card),
	}, nil
}

func (a *App) completeThreadSandboxSet(action *feishu.CardAction, sessionKey, threadID, sandboxMode string) (*callback.CardActionTriggerResponse, error) {
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
	sess := a.store.GetSession(sessionKey)
	if sess == nil || strings.TrimSpace(sess.ActiveThreadID) == "" || strings.TrimSpace(sess.ActiveThreadID) != strings.TrimSpace(threadID) {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "当前 thread 已失效"}}, nil
	}
	sess.ActiveThreadSandboxMode = sandboxMode
	if err := a.store.UpsertSession(sess); err != nil {
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
	sess := a.store.GetSession(sessionKey)
	if sess == nil || strings.TrimSpace(sess.ActiveThreadID) == "" || strings.TrimSpace(sess.ActiveThreadID) != strings.TrimSpace(threadID) {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "当前 thread 已失效"}}, nil
	}
	sess.ActiveThreadApprovalPolicy = approvalPolicy
	if err := a.store.UpsertSession(sess); err != nil {
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
	sess := a.store.GetSession(sessionKey)
	if sess == nil {
		sess = &state.Session{Key: sessionKey, OwnerUserID: action.UserID, ChatID: action.ChatID}
	}
	if sess.ActiveTurnID != "" {
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
	selectedName, _ := action.ActionValue["thread_name"].(string)
	selectedPreview, _ := action.ActionValue["thread_preview"].(string)
	selectedCWD, _ := action.ActionValue["thread_cwd"].(string)
	workspaceID := sess.WorkspaceID
	if strings.TrimSpace(workspaceID) == "" {
		workspaceID = a.defaultWorkspaceID()
	}
	if ws := config.FindWorkspace(a.cfg, workspaceID); ws != nil && strings.TrimSpace(selectedCWD) != "" && !sameWorkspaceCWD(selectedCWD, ws.Cwd) {
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "warning", Content: "该线程不属于当前工作区，请先切换 workspace"},
		}, nil
	}
	effectiveModel := configuredGlobalModel(a.cfg)
	params := map[string]any{
		"threadId":               threadID,
		"persistExtendedHistory": true,
	}
	if strings.TrimSpace(effectiveModel) != "" {
		params["model"] = effectiveModel
	}
	slog.Debug("manual thread resume request",
		"session_key", sessionKey,
		"thread_id", threadID,
		"model", effectiveModel,
	)
	var result codexrpc.ThreadStartResult
	if err := a.codex.Call(context.Background(), "thread/resume", params, &result); err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: err.Error()}}, nil
	}
	sess.ActiveThreadApprovalPolicy = ""
	sess.ActiveThreadSandboxMode = ""
	setSessionThreadContext(sess, workspaceID, threadID, firstNonEmpty(selectedName, result.Thread.Name), firstNonEmpty(selectedPreview, result.Thread.Preview))
	a.markSessionThreadLive(sessionKey, threadID)
	sess.ActiveTurnID = ""
	sess.ActiveSubmissionID = ""
	sess.Status = "idle"
	_ = a.store.UpsertSession(sess)
	card, err := a.renderThreadsCard(sessionKey, false)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "success", Content: "已恢复线程"}}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已恢复线程"},
		Card:  rawCard(card),
	}, nil
}

func (a *App) completeApprovalAction(action *feishu.CardAction, actionName string) (*callback.CardActionTriggerResponse, error) {
	requestID, _ := action.ActionValue["request_id"].(string)
	pending := a.store.PendingByID(requestID)
	if pending == nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "审批已过期"}}, nil
	}
	if pending.OwnerUserID != "" && pending.OwnerUserID != action.UserID {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "你没有权限处理这个审批"}}, nil
	}
	var replyPayload any
	switch pending.Kind {
	case "command":
		resp := map[string]any{"decision": "decline"}
		switch actionName {
		case "approval.command.accept":
			resp["decision"] = "accept"
		case "approval.command.accept_session":
			resp["decision"] = "acceptForSession"
		case "approval.command.cancel", "approval.command.decline":
			resp["decision"] = "decline"
		}
		replyPayload = resp
	case "file":
		resp := map[string]any{"decision": "decline"}
		switch actionName {
		case "approval.file.accept":
			resp["decision"] = "accept"
		case "approval.file.accept_session":
			resp["decision"] = "acceptForSession"
		case "approval.file.cancel", "approval.file.decline":
			resp["decision"] = "decline"
		}
		replyPayload = resp
	case "permissions":
		var payload struct {
			Permissions map[string]any `json:"permissions"`
		}
		_ = json.Unmarshal([]byte(pending.PayloadJSON), &payload)
		scope := "turn"
		if actionName == "approval.permissions.accept_session" {
			scope = "session"
		}
		replyPayload = map[string]any{
			"permissions": payload.Permissions,
			"scope":       scope,
		}
	default:
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "不支持的审批类型"}}, nil
	}
	if err := a.codex.Reply(pendingRequestIDRaw(pending), replyPayload); err != nil {
		slog.Error("approval reply to codex failed",
			"request_id", requestID,
			"pending_kind", pending.Kind,
			"action", actionName,
			"user_id", action.UserID,
			"error", err,
		)
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "warning", Content: "审批结果提交失败，请重试"},
		}, nil
	}
	_ = a.store.UpdatePending(requestID, func(req *state.PendingRequest) { req.Status = "resolved" })
	a.resumeSubmissionAfterRequest(pending)
	card := a.renderResolvedApprovalCard(pending, actionName)
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "审批已提交"},
		Card: &callback.Card{
			Type: "raw",
			Data: card,
		},
	}, nil
}

func (a *App) renderResolvedApprovalCard(pending *state.PendingRequest, action string) map[string]any {
	decision := a.approvalDecisionText(action)
	body := strings.TrimSpace(a.approvalBodyText(pending))
	lines := []string{"处理结果: " + decision}
	if body != "" {
		lines = append(lines, "", body)
	}
	color := "green"
	if decision == "已拒绝" {
		color = "grey"
	}
	return a.feishu.SimpleStatusCard("审批已处理", color, strings.Join(lines, "\n"), nil)
}

func (a *App) approvalBodyText(pending *state.PendingRequest) string {
	if pending == nil {
		return ""
	}
	var payload map[string]any
	if strings.TrimSpace(pending.PayloadJSON) != "" {
		if err := json.Unmarshal([]byte(pending.PayloadJSON), &payload); err == nil {
			if body := strings.TrimSpace(stringValue(payload["body"])); body != "" {
				return body
			}
			if pending.Kind == "command" {
				if request, ok := payload["request"].(map[string]any); ok {
					if body := strings.TrimSpace(renderCommandApprovalBody(request)); body != "" {
						return body
					}
				}
			}
			if pending.Kind == "file" {
				if request, ok := payload["request"].(map[string]any); ok {
					if body := strings.TrimSpace(renderFileApprovalBody(request)); body != "" {
						return body
					}
				}
			}
			if pending.Kind == "permissions" {
				if request, ok := payload["request"].(map[string]any); ok {
					if body := strings.TrimSpace(renderPermissionsApprovalBody(request)); body != "" {
						return body
					}
				}
				if permissions, ok := payload["permissions"]; ok {
					if rendered := strings.TrimSpace(prettyJSON(permissions)); rendered != "" {
						return "权限审批\n" + rendered
					}
				}
			}
		}
	}
	switch pending.Kind {
	case "command":
		return "命令审批"
	case "file":
		return "文件变更审批"
	case "permissions":
		return "权限审批"
	default:
		return ""
	}
}

func (a *App) completeUserInputAnswer(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	requestID, _ := action.ActionValue["request_id"].(string)
	questionID, _ := action.ActionValue["question_id"].(string)
	answer, _ := action.ActionValue["answer"].(string)
	pending := a.store.PendingByID(requestID)
	if pending == nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "请求已过期"}}, nil
	}
	if pending.OwnerUserID != "" && pending.OwnerUserID != action.UserID {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "你没有权限回答这个问题"}}, nil
	}
	payload := map[string]any{
		"answers": map[string]any{
			questionID: map[string]any{
				"answers": []string{answer},
			},
		},
	}
	_ = a.codex.Reply(pendingRequestIDRaw(pending), payload)
	_ = a.store.UpdatePending(requestID, func(req *state.PendingRequest) { req.Status = "resolved" })
	a.resumeSubmissionAfterRequest(pending)
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已提交"},
		Card: &callback.Card{
			Type: "raw",
			Data: a.feishu.SimpleStatusCard("已提交", "green", answer, nil),
		},
	}, nil
}

func (a *App) approvalDecisionText(action string) string {
	switch action {
	case "approval.command.accept", "approval.file.accept":
		return "已允许本次执行"
	case "approval.command.accept_session", "approval.file.accept_session":
		return "已允许本会话执行"
	case "approval.permissions.accept_turn":
		return "已授权本次权限请求"
	case "approval.permissions.accept_session":
		return "已授权本会话权限请求"
	default:
		return "已拒绝"
	}
}

func (a *App) resumeSubmissionAfterRequest(pending *state.PendingRequest) {
	if pending == nil {
		return
	}
	_, sub := a.findSubmissionByTurn(pending.ThreadID, pending.TurnID)
	if sub == nil {
		return
	}
	_ = a.store.UpdateSubmission(sub.ID, func(s *state.Submission) { s.Status = "running" })
	_ = a.refreshStatusCard(sub.ID)
}
