package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	appworkspace "feidex/internal/app/workspace"
	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

type workspaceManagementService struct {
	app *App
}
func newWorkspaceManagementService(app *App) workspaceManagementService {
	return workspaceManagementService{app: app}
}

type workspaceNewPayload = appworkspace.NewPayload
type workspaceClonePayload = appworkspace.ClonePayload
type workspaceCloneTakeoverError = appworkspace.CloneTakeoverError
type workspaceCloneExistingDirError = appworkspace.CloneExistingDirError
type workspaceCloneExistingWorkspaceError = appworkspace.CloneExistingWorkspaceError
type workspaceCloneProgressSnapshot = appworkspace.CloneProgressSnapshot
type workspaceClonePlan = appworkspace.ClonePlan

func workspaceNewPayloadFromPending(pending *state.PendingRequest) workspaceNewPayload {
	var payload workspaceNewPayload
	if pending != nil && strings.TrimSpace(pending.PayloadJSON) != "" {
		_ = json.Unmarshal([]byte(pending.PayloadJSON), &payload)
	}
	return payload
}

func workspaceClonePayloadFromPending(pending *state.PendingRequest) workspaceClonePayload {
	var payload workspaceClonePayload
	if pending != nil && strings.TrimSpace(pending.PayloadJSON) != "" {
		_ = json.Unmarshal([]byte(pending.PayloadJSON), &payload)
	}
	return payload
}

func (s workspaceManagementService) defaultWorkspaceNewRoot(ws *config.Workspace) string {
	return "/"
}

func (s workspaceManagementService) defaultWorkspaceCloneRoot(ws *config.Workspace) string {
	return "/"
}

func (s workspaceManagementService) beginWorkspaceNew(msg *feishu.InboundMessage) error {
	sessionKey, _, ws := newWorkspaceConfigService(s.app).currentWorkspaceForMessage(msg)
	payload := workspaceNewPayload{
		RootPath: newWorkspaceManagementService(s.app).defaultWorkspaceNewRoot(ws),
		SelectedCWD: firstNonEmpty(func() string {
			if ws == nil {
				return ""
			}
			return strings.TrimSpace(ws.Cwd)
		}(), "/"),
	}
	return newWorkspaceManagementService(s.app).beginWorkspaceNewWithPayload(msg, sessionKey, payload)
}

func (s workspaceManagementService) beginWorkspaceNewWithPayload(msg *feishu.InboundMessage, sessionKey string, payload workspaceNewPayload) error {
	appState := appState(s.app)
	requestID, err := appState.nextLocalID("workspace")
	if err != nil {
		return err
	}
	card := newWorkspaceRenderService(s.app).renderWorkspaceNewCard(sessionKey, requestID, payload)
	msgID, err := s.app.feishu.ReplyCard(context.Background(), msg.MessageID, card, replyInThreadEnabled(s.app, msg.ChatType))
	if err != nil {
		return err
	}
	return appState.savePending(&state.PendingRequest{
		ID:          requestID,
		Kind:        "workspace_new",
		SessionKey:  sessionKey,
		OwnerUserID: msg.UserID,
		FeishuMsgID: msgID,
		PayloadJSON: mustJSON(payload),
		Status:      "pending",
		CreatedAt:   time.Now().Unix(),
		ExpiresAt:   time.Now().Add(10 * time.Minute).Unix(),
	})
}

func (s workspaceManagementService) createWorkspaceNewPending(sessionKey, userID, feishuMsgID string, payload workspaceNewPayload) (string, error) {
	appState := appState(s.app)
	requestID, err := appState.nextLocalID("workspace")
	if err != nil {
		return "", err
	}
	if err := appState.savePending(&state.PendingRequest{
		ID:          requestID,
		Kind:        "workspace_new",
		SessionKey:  sessionKey,
		OwnerUserID: userID,
		FeishuMsgID: strings.TrimSpace(feishuMsgID),
		PayloadJSON: mustJSON(payload),
		Status:      "pending",
		CreatedAt:   time.Now().Unix(),
		ExpiresAt:   time.Now().Add(10 * time.Minute).Unix(),
	}); err != nil {
		return "", err
	}
	return requestID, nil
}


func (s workspaceManagementService) defaultWorkspaceCloneParent(ws *config.Workspace) string {
	if ws != nil && strings.TrimSpace(ws.Cwd) != "" {
		return filepath.Dir(strings.TrimSpace(ws.Cwd))
	}
	if strings.TrimSpace(s.app.cfgPath) != "" {
		return filepath.Dir(strings.TrimSpace(s.app.cfgPath))
	}
	return "."
}

func (s workspaceManagementService) workspaceByCWD(targetDir string) *config.Workspace {
	targetDir = strings.TrimSpace(targetDir)
	if targetDir == "" || s.app == nil || s.app.cfg == nil {
		return nil
	}
	cleanTarget := filepath.Clean(targetDir)
	for i := range s.app.cfg.Workspaces {
		ws := &s.app.cfg.Workspaces[i]
		if filepath.Clean(strings.TrimSpace(ws.Cwd)) == cleanTarget {
			return ws
		}
	}
	return nil
}

func (s workspaceManagementService) workspaceByIDAndCWD(workspaceID, targetDir string) *config.Workspace {
	ws := config.FindWorkspace(s.app.cfg, strings.TrimSpace(workspaceID))
	if ws == nil || !sameWorkspaceCWD(targetDir, ws.Cwd) {
		return nil
	}
	return ws
}

func (s workspaceManagementService) createWorkspaceAndSwitch(sessionKey, userID, chatID, chatType, id, name, cwd string) error {
	appState := appState(s.app)
	s.app.configMu.Lock()
	if config.FindWorkspace(s.app.cfg, id) != nil {
		s.app.configMu.Unlock()
		return fmt.Errorf("workspace %q 已存在", id)
	}
	s.app.cfg.Workspaces = append(s.app.cfg.Workspaces, config.Workspace{
		ID:             id,
		Name:           name,
		Cwd:            cwd,
		Model:          "",
		ApprovalPolicy: "on-request",
		SandboxMode:    "workspace-write",
	})
	if err := s.app.cfg.Normalize(filepath.Dir(s.app.cfgPath)); err != nil {
		s.app.cfg.Workspaces = s.app.cfg.Workspaces[:len(s.app.cfg.Workspaces)-1]
		s.app.configMu.Unlock()
		return err
	}
	if err := config.Save(s.app.cfgPath, s.app.cfg); err != nil {
		s.app.cfg.Workspaces = s.app.cfg.Workspaces[:len(s.app.cfg.Workspaces)-1]
		s.app.configMu.Unlock()
		return err
	}
	ws := config.FindWorkspace(s.app.cfg, id)
	s.app.configMu.Unlock()
	sess := appState.session(sessionKey)
	if sess == nil {
		sess = &state.Session{Key: sessionKey, ChatID: chatID, ChatType: chatType, OwnerUserID: userID}
	}
	switchSessionWorkspace(sess, id)
	if err := appState.saveSession(sess); err != nil {
		return err
	}
	if sessionHasInFlightSubmission(sess) {
		return nil
	}
	if ws == nil {
		return nil
	}
	if _, err := newWorkspaceThreadService(s.app).ensureWorkspaceThreadBinding(sessionKey, sess, ws); err != nil {
		slog.Warn("workspace create thread binding failed",
			"session_key", sessionKey,
			"workspace_id", id,
			"cwd", cwd,
			"error", err,
		)
	}
	return nil
}

func (s pendingInputService) completeWorkspaceNewText(msg *feishu.InboundMessage, pending *state.PendingRequest) error {
	appState := appState(s.app)
	payload := workspaceNewPayloadFromPending(pending)
	parts := strings.Fields(strings.TrimSpace(msg.Text))
	if len(parts) < 1 {
		return fmt.Errorf("格式错误，需发送: workspace_id [name]")
	}
	id := parts[0]
	cwd := strings.TrimSpace(payload.SelectedCWD)
	name := id
	if cwd == "" && len(parts) >= 2 {
		cwd = parts[1]
		if len(parts) > 2 {
			name = strings.Join(parts[2:], " ")
		}
	} else if len(parts) > 1 {
		name = strings.Join(parts[1:], " ")
	}
	if strings.TrimSpace(cwd) == "" {
		return fmt.Errorf("请先选择目录")
	}
	sessionKey := makeSessionKey(s.app, msg)
	if existingWS := newWorkspaceManagementService(s.app).workspaceByIDAndCWD(id, cwd); existingWS != nil {
		payload.DraftID = id
		payload.DraftName = name
		_ = appState.updatePending(pending.ID, func(req *state.PendingRequest) {
			req.Status = "resolved"
			req.PayloadJSON = mustJSON(payload)
			req.ExpiresAt = time.Now().Add(30 * time.Minute).Unix()
		})
		if pending.FeishuMsgID != "" {
			_ = s.app.feishu.PatchCard(context.Background(), pending.FeishuMsgID, newWorkspaceRenderService(s.app).renderWorkspaceSwitchExistingCard(sessionKey, existingWS.ID, existingWS.Cwd, appworkspace.NewExistingWorkspaceNotice()))
		}
		return s.app.feishu.ReplyText(context.Background(), msg.MessageID, "工作区已存在且目录一致，可直接切换到 "+existingWS.ID, replyInThreadEnabled(s.app, msg.ChatType))
	}
	if err := newWorkspaceManagementService(s.app).createWorkspaceAndSwitch(sessionKey, msg.UserID, msg.ChatID, msg.ChatType, id, name, cwd); err != nil {
		return err
	}
	_ = appState.updatePending(pending.ID, func(req *state.PendingRequest) { req.Status = "resolved" })
	if pending.FeishuMsgID != "" {
		_ = s.app.feishu.PatchCard(context.Background(), pending.FeishuMsgID, s.app.feishu.SimpleStatusCard("工作区已创建", "green", "已创建并切换到工作区 `"+id+"`\n\ncwd: `"+cwd+"`", nil))
	}
	return s.app.feishu.ReplyText(context.Background(), msg.MessageID, "已创建并切换到工作区 "+id, replyInThreadEnabled(s.app, msg.ChatType))
}
