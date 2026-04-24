package app

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

func (s workspaceConfigService) showWorkspaceDeleteMenu(msg *feishu.InboundMessage) error {
	card, err := newWorkspaceConfigService(s.app).renderWorkspaceDeleteMenuCard(makeSessionKey(s.app, msg))
	if err != nil {
		return err
	}
	_, err = s.app.feishu.ReplyCard(context.Background(), msg.MessageID, card, replyInThreadEnabled(s.app, msg.ChatType))
	return err
}

func (s workspaceConfigService) renderWorkspaceDeleteMenuCard(sessionKey string) (map[string]any, error) {
	currentID := defaultWorkspaceID(s.app)
	if sess := s.app.appState().session(sessionKey); sess != nil && strings.TrimSpace(sess.WorkspaceID) != "" {
		currentID = strings.TrimSpace(sess.WorkspaceID)
	}
	lines := []string{
		"删除 workspace 只会移除配置，不会删除磁盘目录。",
		"",
		"当前工作区: `" + currentID + "`",
		"当前工作区不可删除，请先切换到其他工作区。",
	}
	deleteOptions := make([]selectStaticOption, 0, len(s.app.cfg.Workspaces))
	for _, ws := range s.app.cfg.Workspaces {
		if strings.TrimSpace(ws.ID) == "" || ws.ID == currentID {
			continue
		}
		label := ws.ID
		if name := strings.TrimSpace(ws.Name); name != "" && name != ws.ID {
			label = name + " · " + ws.ID
		}
		label += " · " + strings.TrimSpace(ws.Cwd)
		deleteOptions = append(deleteOptions, selectStaticOption{
			Text:  label,
			Value: ws.ID,
		})
	}
	if len(deleteOptions) == 0 {
		lines = append(lines, "", "当前没有可删除的其他工作区。")
	}
	card := newMarkdownBodyCard("删除工作区", "orange")
	appendMarkdownBodyCardElement(card, map[string]any{
		"tag":     "markdown",
		"content": menuCardBody("workspace.delete.menu", strings.Join(lines, "\n")),
	})
	if len(deleteOptions) > 0 {
		appendMarkdownBodyCardElement(card, buildSelectStaticElement(
			"workspace_delete_select",
			"选择要删除的 workspace",
			map[string]any{"action": "workspace.delete.prompt", "session_key": sessionKey},
			deleteOptions,
			"",
		))
	}
	for _, row := range buildMarkdownBodyCardActionElements([]feishu.Button{
		{
			Text: commandLabel("返回工作区", "/workspace"),
			Type: "default",
			Value: map[string]any{
				"action":      "menu.workspace",
				"session_key": sessionKey,
			},
		},
	}) {
		appendMarkdownBodyCardElement(card, row)
	}
	return card, nil
}

func (s workspaceConfigService) renderWorkspaceDeleteConfirmCard(sessionKey, workspaceID string) (map[string]any, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	ws := config.FindWorkspace(s.app.cfg, workspaceID)
	if ws == nil {
		return nil, fmt.Errorf("workspace %q 不存在", workspaceID)
	}
	body := []string{
		"即将删除工作区配置：`" + workspaceID + "`",
		"",
		"name: `" + firstNonEmpty(strings.TrimSpace(ws.Name), workspaceID) + "`",
		"cwd: `" + strings.TrimSpace(ws.Cwd) + "`",
		"",
		"这只会删除配置项，不会删除磁盘目录。",
		"其他空闲 session 如果还引用这个 workspace，会自动切到剩余 workspace 并清空 thread 绑定。",
	}
	buttons := []feishu.Button{
		{
			Text: "确认删除",
			Type: "primary",
			Value: map[string]any{
				"action":       "workspace.delete.confirm",
				"session_key":  sessionKey,
				"workspace_id": workspaceID,
			},
		},
		{
			Text: "返回删除菜单",
			Type: "default",
			Value: map[string]any{
				"action":      "workspace.delete.menu",
				"session_key": sessionKey,
			},
		},
	}
	return s.app.feishu.SimpleStatusCard("确认删除工作区", "red", menuCardBody("workspace.delete.confirm", strings.Join(body, "\n")), buttons), nil
}

func (s workspaceActionService) completeWorkspaceDeleteMenu(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	card, err := newWorkspaceConfigService(s.app).renderWorkspaceDeleteMenuCard(sessionKey)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "info", Content: "请选择要删除的工作区"},
		Card:  rawCard(card),
	}, nil
}

func (s workspaceActionService) completeWorkspaceDeletePrompt(action *feishu.CardAction, sessionKey, workspaceID string) (*callback.CardActionTriggerResponse, error) {
	workspaceID = firstNonEmpty(strings.TrimSpace(workspaceID), strings.TrimSpace(action.Option))
	if err := newWorkspaceConfigService(s.app).validateWorkspaceDeletion(sessionKey, workspaceID); err != nil {
		card, renderErr := newWorkspaceConfigService(s.app).renderWorkspaceDeleteMenuCard(sessionKey)
		if renderErr != nil {
			return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
		}
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "warning", Content: err.Error()},
			Card:  rawCard(card),
		}, nil
	}
	card, err := newWorkspaceConfigService(s.app).renderWorkspaceDeleteConfirmCard(sessionKey, workspaceID)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "warning", Content: "确认后只删除配置，不删除目录"},
		Card:  rawCard(card),
	}, nil
}

func (s workspaceActionService) completeWorkspaceDeleteConfirm(action *feishu.CardAction, sessionKey, workspaceID string) (*callback.CardActionTriggerResponse, error) {
	if err := newWorkspaceConfigService(s.app).deleteWorkspace(sessionKey, workspaceID); err != nil {
		card, renderErr := newWorkspaceConfigService(s.app).renderWorkspaceDeleteMenuCard(sessionKey)
		if renderErr != nil {
			return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
		}
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "warning", Content: err.Error()},
			Card:  rawCard(card),
		}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已删除工作区 " + strings.TrimSpace(workspaceID)},
		Card:  rawCard(newWorkspaceConfigService(s.app).renderWorkspaceMenuCard(sessionKey)),
	}, nil
}

func (s workspaceConfigService) validateWorkspaceDeletion(sessionKey, workspaceID string) error {
	s.app.configMu.RLock()
	defer s.app.configMu.RUnlock()
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return fmt.Errorf("请指定 workspace_id")
	}
	if config.FindWorkspace(s.app.cfg, workspaceID) == nil {
		return fmt.Errorf("workspace %q 不存在", workspaceID)
	}
	if len(s.app.cfg.Workspaces) <= 1 {
		return fmt.Errorf("至少保留一个 workspace")
	}
	currentID := defaultWorkspaceID(s.app)
	if sess := s.app.appState().session(sessionKey); sess != nil && strings.TrimSpace(sess.WorkspaceID) != "" {
		currentID = strings.TrimSpace(sess.WorkspaceID)
	}
	if workspaceID == currentID {
		return fmt.Errorf("不能删除当前 workspace，请先切换到其他 workspace")
	}
	for _, sess := range s.app.appState().sessions() {
		if sess == nil || !sessionHasInFlightSubmission(sess) {
			continue
		}
		if sessionReferencesWorkspace(sess, workspaceID) {
			return fmt.Errorf("workspace %q 仍有运行中的任务，无法删除", workspaceID)
		}
	}
	return nil
}

func (s workspaceConfigService) deleteWorkspace(sessionKey, workspaceID string) error {
	if err := newWorkspaceConfigService(s.app).validateWorkspaceDeletion(sessionKey, workspaceID); err != nil {
		return err
	}
	s.app.configMu.Lock()
	workspaceID = strings.TrimSpace(workspaceID)
	fallbackID := ""
	nextWorkspaces := make([]config.Workspace, 0, len(s.app.cfg.Workspaces)-1)
	for _, ws := range s.app.cfg.Workspaces {
		if ws.ID == workspaceID {
			continue
		}
		if fallbackID == "" {
			fallbackID = ws.ID
		}
		nextWorkspaces = append(nextWorkspaces, ws)
	}
	if fallbackID == "" {
		return fmt.Errorf("至少保留一个 workspace")
	}
	prevWorkspaces := append([]config.Workspace(nil), s.app.cfg.Workspaces...)
	s.app.cfg.Workspaces = nextWorkspaces
	if err := s.app.cfg.Normalize(filepath.Dir(s.app.cfgPath)); err != nil {
		s.app.cfg.Workspaces = prevWorkspaces
		s.app.configMu.Unlock()
		return err
	}
	if err := config.Save(s.app.cfgPath, s.app.cfg); err != nil {
		s.app.cfg.Workspaces = prevWorkspaces
		s.app.configMu.Unlock()
		return err
	}
	s.app.configMu.Unlock()
	appState := s.app.appState()
	for _, sess := range appState.sessions() {
		if sess == nil {
			continue
		}
		updated := false
		if strings.TrimSpace(sess.WorkspaceID) == workspaceID {
			switchSessionWorkspace(sess, fallbackID)
			updated = true
		} else if strings.TrimSpace(sess.ActiveThreadWorkspaceID) == workspaceID {
			clearSessionThreadContext(sess)
			updated = true
		}
		if !updated {
			continue
		}
		s.app.clearSessionLiveThread(sess.Key)
		if err := appState.saveSession(sess); err != nil {
			return err
		}
	}
	return nil
}

func sessionReferencesWorkspace(sess *state.Session, workspaceID string) bool {
	if sess == nil {
		return false
	}
	workspaceID = strings.TrimSpace(workspaceID)
	return strings.TrimSpace(sess.WorkspaceID) == workspaceID || strings.TrimSpace(sess.ActiveThreadWorkspaceID) == workspaceID
}
