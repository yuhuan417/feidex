package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"path"
	"path/filepath"
	"strings"
	"time"

	appworkspace "feidex/internal/app/workspace"
	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

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
	appState := s.app.appState()
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
	appState := s.app.appState()
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

func formValueString(values map[string]any, key string) (string, bool) {
	if len(values) == 0 {
		return "", false
	}
	raw, ok := values[key]
	if !ok {
		return "", false
	}
	switch v := raw.(type) {
	case string:
		return strings.TrimSpace(v), true
	default:
		return strings.TrimSpace(fmt.Sprint(v)), true
	}
}

func mergeWorkspaceNewFormValues(payload workspaceNewPayload, values map[string]any) workspaceNewPayload {
	if value, ok := formValueString(values, "workspace_id"); ok {
		if value != "" {
			payload.DraftID = value
			if strings.TrimSpace(payload.AutoDraftID) != value {
				payload.AutoDraftID = ""
			}
		} else if strings.TrimSpace(payload.DraftID) == strings.TrimSpace(payload.AutoDraftID) {
			payload.DraftID = ""
			payload.AutoDraftID = ""
		}
	}
	if value, ok := formValueString(values, "workspace_name"); ok {
		payload.DraftName = value
	}
	return payload
}

func mergeWorkspaceCloneFormValues(payload workspaceClonePayload, values map[string]any) workspaceClonePayload {
	if value, ok := formValueString(values, "repo_url"); ok {
		payload.RepoURL = value
	}
	if value, ok := formValueString(values, "workspace_id"); ok {
		payload.DraftID = value
	}
	return payload
}

func workspaceCloneRepoName(repoURL string) (string, error) {
	repoURL = strings.TrimSpace(repoURL)
	if repoURL == "" {
		return "", fmt.Errorf("git 地址不能为空")
	}
	pathPart := repoURL
	if !strings.Contains(repoURL, "://") {
		if idx := strings.Index(repoURL, ":"); idx > 0 && !strings.Contains(repoURL[:idx], "/") && strings.Contains(repoURL[idx+1:], "/") {
			pathPart = repoURL[idx+1:]
		}
	} else if parsed, err := url.Parse(repoURL); err == nil {
		pathPart = firstNonEmpty(strings.TrimSpace(parsed.Path), strings.TrimSpace(parsed.Opaque))
	}
	base := strings.TrimSpace(strings.TrimSuffix(path.Base(strings.TrimSuffix(pathPart, "/")), ".git"))
	if base == "" || base == "." || base == "/" {
		return "", fmt.Errorf("无法从 git 地址推导仓库名")
	}
	return base, nil
}

func workspaceCloneDefaultID(repoName string) string {
	return workspaceSuggestedID(repoName)
}

func workspaceSuggestedIDFromDir(dir string) string {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return ""
	}
	base := filepath.Base(filepath.Clean(dir))
	if base == "" || base == "." || base == string(filepath.Separator) {
		return ""
	}
	return workspaceSuggestedID(base)
}

func workspaceSuggestedID(raw string) string {
	repoName := strings.ToLower(strings.TrimSpace(raw))
	var out strings.Builder
	lastDash := false
	for _, r := range repoName {
		switch {
		case r >= 'a' && r <= 'z':
			out.WriteRune(r)
			lastDash = false
		case r >= '0' && r <= '9':
			out.WriteRune(r)
			lastDash = false
		case r == '-' || r == '_':
			if out.Len() > 0 && !lastDash {
				out.WriteByte('-')
				lastDash = true
			}
		default:
			if out.Len() > 0 && !lastDash {
				out.WriteByte('-')
				lastDash = true
			}
		}
	}
	return strings.Trim(out.String(), "-")
}

func workspaceNewTakeoverNotice(targetDir string) string {
	targetDir = firstNonEmpty(strings.TrimSpace(targetDir), "-")
	return "clone 目标目录已存在，可直接新建工作区接管。\n\n目录已预填为 `" + targetDir + "`，并已带上建议的 `workspace_id`。"
}

func workspaceNewExistingWorkspaceNotice() string {
	return "该 workspace_id 已存在，并且目录与现有工作区一致。"
}

func updateWorkspaceNewSuggestedID(payload workspaceNewPayload, selectedDir string) workspaceNewPayload {
	nextAuto := workspaceSuggestedIDFromDir(selectedDir)
	currentDraft := strings.TrimSpace(payload.DraftID)
	currentAuto := strings.TrimSpace(payload.AutoDraftID)
	if nextAuto != "" && (currentDraft == "" || currentDraft == currentAuto) {
		payload.DraftID = nextAuto
	}
	payload.AutoDraftID = nextAuto
	return payload
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
	appState := s.app.appState()
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
	appState := s.app.appState()
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
			_ = s.app.feishu.PatchCard(context.Background(), pending.FeishuMsgID, newWorkspaceRenderService(s.app).renderWorkspaceSwitchExistingCard(sessionKey, existingWS.ID, existingWS.Cwd, workspaceNewExistingWorkspaceNotice()))
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
