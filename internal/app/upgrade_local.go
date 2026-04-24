package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

func (s appUpgradeService) commandUpgradeLocalPick(msg *feishu.InboundMessage) error {
	if msg == nil {
		return nil
	}
	sessionKey, _, ws := newWorkspaceConfigService(s.app).currentWorkspaceForMessage(msg)
	requestID, payload, err := newAppUpgradeService(s.app).createUpgradeLocalPickerRequest(sessionKey, ws, msg.UserID, "")
	if err != nil {
		return err
	}
	card, err := newWorkspaceRenderService(s.app).renderPathPickerCard(requestID, payload)
	if err != nil {
		return err
	}
	msgID, err := s.app.feishu.ReplyCard(context.Background(), msg.MessageID, card, s.app.replyInThreadEnabled(msg.ChatType))
	if err != nil {
		return err
	}
	return s.app.appState().updatePending(requestID, func(req *state.PendingRequest) {
		req.FeishuMsgID = msgID
	})
}

func (s appUpgradeService) commandUpgradeLocalPath(msg *feishu.InboundMessage, rawPath string) error {
	if msg == nil {
		return nil
	}
	sessionKey, _, ws := newWorkspaceConfigService(s.app).currentWorkspaceForMessage(msg)
	selectedPath, err := resolveUpgradeLocalSourcePath(ws, rawPath)
	if err != nil {
		return err
	}
	requestID, payload, err := newAppUpgradeService(s.app).createLocalUpgradeRequest(sessionKey, msg.UserID, "", selectedPath)
	if err != nil {
		return err
	}
	card := newAppUpgradeService(s.app).renderUpgradeConfirmCard("升级确认", sessionKey, requestID, payload, upgradeLocalConfirmLines(payload.BinaryPath))
	msgID, err := s.app.feishu.ReplyCard(context.Background(), msg.MessageID, card, s.app.replyInThreadEnabled(msg.ChatType))
	if err != nil {
		return err
	}
	return s.app.appState().updatePending(requestID, func(req *state.PendingRequest) {
		req.FeishuMsgID = msgID
	})
}

func resolveUpgradeLocalSourcePath(ws *config.Workspace, rawPath string) (string, error) {
	root, err := resolvePathPickerRoot(ws)
	if err != nil {
		return "", err
	}
	resolved, err := resolvePathPickerPath(root, rawPath)
	if err != nil {
		return "", fmt.Errorf("解析本地 Binary 路径失败: %w", err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("读取本地 Binary 失败: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("本地 Binary 必须是普通文件")
	}
	return resolved, nil
}

func (s appUpgradeService) createUpgradeLocalPickerRequest(sessionKey string, ws *config.Workspace, ownerUserID, feishuMsgID string) (string, pathPickerPayload, error) {
	if _, _, err := newAppUpgradeService(s.app).validateUpgradeRuntime(); err != nil {
		return "", pathPickerPayload{}, err
	}
	appState := s.app.appState()
	payload, err := s.app.newDownloadPathPickerPayload(ws)
	if err != nil {
		return "", pathPickerPayload{}, err
	}
	requestID, err := appState.nextLocalID("upgrade-local")
	if err != nil {
		return "", pathPickerPayload{}, err
	}
	if err := appState.savePending(&state.PendingRequest{
		ID:          requestID,
		Kind:        upgradeLocalBinaryPendingKind,
		SessionKey:  sessionKey,
		OwnerUserID: ownerUserID,
		FeishuMsgID: feishuMsgID,
		PayloadJSON: mustJSON(payload),
		Status:      "pending",
		CreatedAt:   time.Now().Unix(),
		ExpiresAt:   time.Now().Add(10 * time.Minute).Unix(),
	}); err != nil {
		return "", pathPickerPayload{}, err
	}
	return requestID, payload, nil
}

func (s appUpgradeService) createLocalUpgradeRequest(sessionKey, ownerUserID, feishuMsgID, selectedPath string) (string, upgradePendingPayload, error) {
	exePath, _, err := newAppUpgradeService(s.app).validateUpgradeRuntime()
	if err != nil {
		return "", upgradePendingPayload{}, err
	}
	appState := s.app.appState()
	requestID, err := appState.nextLocalID("upgrade")
	if err != nil {
		return "", upgradePendingPayload{}, err
	}
	stagedPath, sha256Hex, sizeBytes, err := newAppUpgradeService(s.app).stageLocalUpgradeArtifact(requestID, selectedPath)
	if err != nil {
		return "", upgradePendingPayload{}, err
	}
	payload := upgradePendingPayload{
		CurrentVersion: currentVersion(),
		TargetVersion:  filepath.Base(selectedPath),
		BinaryPath:     exePath,
		SourcePath:     stagedPath,
		SourceKind:     "local_file",
		SourceName:     filepath.Base(selectedPath),
		SourceSize:     sizeBytes,
		ExpectedSHA256: sha256Hex,
	}
	if err := appState.savePending(&state.PendingRequest{
		ID:          requestID,
		Kind:        "upgrade_release",
		SessionKey:  sessionKey,
		OwnerUserID: ownerUserID,
		FeishuMsgID: feishuMsgID,
		PayloadJSON: mustJSON(payload),
		Status:      "pending",
		CreatedAt:   time.Now().Unix(),
		ExpiresAt:   time.Now().Add(30 * time.Minute).Unix(),
	}); err != nil {
		return "", upgradePendingPayload{}, err
	}
	return requestID, payload, nil
}

func (s appUpgradeService) completeUpgradeLocalPick(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	sessionKey := actionSessionKey(action)
	appState := s.app.appState()
	wsID := s.app.defaultWorkspaceID()
	if sess := appState.session(sessionKey); sess != nil && strings.TrimSpace(sess.WorkspaceID) != "" {
		wsID = sess.WorkspaceID
	}
	ws := config.FindWorkspace(s.app.cfg, wsID)
	requestID, payload, err := newAppUpgradeService(s.app).createUpgradeLocalPickerRequest(sessionKey, ws, action.UserID, action.MessageID)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	card, err := newWorkspaceRenderService(s.app).renderPathPickerCard(requestID, payload)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "info", Content: "请选择本地 Binary"},
		Card:  rawCard(card),
	}, nil
}

func (s appUpgradeService) completeUpgradeLocalBinaryConfirm(action *feishu.CardAction, pending *state.PendingRequest, payload pathPickerPayload, selectedPath string) (*callback.CardActionTriggerResponse, error) {
	sessionKey := firstNonEmpty(strings.TrimSpace(pending.SessionKey), actionSessionKey(action))
	requestID, upgradePayload, err := newAppUpgradeService(s.app).createLocalUpgradeRequest(sessionKey, pending.OwnerUserID, firstNonEmpty(strings.TrimSpace(pending.FeishuMsgID), strings.TrimSpace(action.MessageID)), selectedPath)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	_ = s.app.appState().updatePending(pending.ID, func(req *state.PendingRequest) { req.Status = "resolved" })
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已选择本地 Binary"},
		Card:  rawCard(newAppUpgradeService(s.app).renderUpgradeConfirmCard("升级确认", sessionKey, requestID, upgradePayload, upgradeLocalConfirmLines(upgradePayload.BinaryPath))),
	}, nil
}

func (s appUpgradeService) stageLocalUpgradeArtifact(requestID, sourcePath string) (string, string, int64, error) {
	sourcePath = strings.TrimSpace(sourcePath)
	if sourcePath == "" {
		return "", "", 0, fmt.Errorf("missing local binary path")
	}
	info, err := os.Stat(sourcePath)
	if err != nil {
		return "", "", 0, fmt.Errorf("读取本地制品失败: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", "", 0, fmt.Errorf("本地制品不是普通文件")
	}
	dir := filepath.Join(s.app.cfg.DataDir, "upgrades", requestID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "", 0, err
	}
	targetPath := filepath.Join(dir, filepath.Base(sourcePath))
	if filepath.Clean(targetPath) == filepath.Clean(sourcePath) {
		targetPath = filepath.Join(dir, "artifact-"+filepath.Base(sourcePath))
	}
	source, err := os.Open(sourcePath)
	if err != nil {
		return "", "", 0, err
	}
	defer source.Close()
	target, err := os.Create(targetPath)
	if err != nil {
		return "", "", 0, err
	}
	defer target.Close()
	hash := sha256.New()
	written, err := io.Copy(io.MultiWriter(target, hash), source)
	if err != nil {
		return "", "", 0, err
	}
	if err := target.Chmod(0o755); err != nil {
		return "", "", 0, err
	}
	return targetPath, hex.EncodeToString(hash.Sum(nil)), written, nil
}

func upgradeLocalConfirmLines(binaryPath string) []string {
	return []string{
		"当前版本: `" + currentVersion() + "`",
		"目标架构: `" + currentGOARCH() + "`",
		"二进制: `" + binaryPath + "`",
	}
}
