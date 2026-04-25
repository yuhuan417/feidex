package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	appworkspace "feidex/internal/app/workspace"
	appworkspacecmd "feidex/internal/app/workspacecmd"
	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

// workspaceManagementService wraps appworkspacecmd.ManagementService and
// provides lowercase method names for backward compatibility.
type workspaceManagementService struct {
	inner *appworkspacecmd.ManagementService
}

func newWorkspaceManagementService(app *App) workspaceManagementService {
	return workspaceManagementService{inner: newWorkspaceManagementServiceInner(app)}
}

type workspaceNewPayload = appworkspace.NewPayload
type workspaceClonePayload = appworkspace.ClonePayload
type workspaceCloneTakeoverError = appworkspace.CloneTakeoverError
type workspaceCloneExistingDirError = appworkspace.CloneExistingDirError
type workspaceCloneExistingWorkspaceError = appworkspace.CloneExistingWorkspaceError
type workspaceCloneProgressSnapshot = appworkspace.CloneProgressSnapshot
type workspaceClonePlan = appworkspace.ClonePlan

var workspaceNewPayloadFromPending = appworkspace.NewPayloadFromPending

var workspaceClonePayloadFromPending = appworkspace.ClonePayloadFromPending

func (s workspaceManagementService) beginWorkspaceNew(msg *feishu.InboundMessage) error {
	return s.inner.BeginWorkspaceNew(msg)
}

func (s workspaceManagementService) beginWorkspaceNewWithPayload(msg *feishu.InboundMessage, sessionKey string, payload workspaceNewPayload) error {
	return s.inner.BeginWorkspaceNewWithPayload(msg, sessionKey, payload)
}

func (s workspaceManagementService) createWorkspaceNewPending(sessionKey, userID, feishuMsgID string, payload workspaceNewPayload) (string, error) {
	return s.inner.CreateWorkspaceNewPending(sessionKey, userID, feishuMsgID, payload)
}

func (s workspaceManagementService) workspaceByCWD(targetDir string) *config.Workspace {
	return s.inner.WorkspaceByCWD(targetDir)
}

func (s workspaceManagementService) workspaceByIDAndCWD(workspaceID, targetDir string) *config.Workspace {
	return s.inner.WorkspaceByIDAndCWD(workspaceID, targetDir)
}

func (s workspaceManagementService) createWorkspaceAndSwitch(sessionKey, userID, chatID, chatType, id, name, cwd string) error {
	return s.inner.CreateWorkspaceAndSwitch(sessionKey, userID, chatID, chatType, id, name, cwd)
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
