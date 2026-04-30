package upgradecmd

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

	apppathpick "feidex/internal/app/pathpick"
	appworkspace "feidex/internal/app/workspace"
	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

type PathPickerPayload = appworkspace.PathPickerPayload

func NewUpgradeLocalPickerPayload(ws *config.Workspace) (PathPickerPayload, error) {
	root, err := apppathpick.ResolvePathPickerRoot(ws)
	if err != nil {
		return PathPickerPayload{}, err
	}
	return PathPickerPayload{
		Mode:        appworkspace.PathPickerModeFile,
		Style:       appworkspace.PathPickerStyleDropdown,
		RootPath:    root,
		CurrentPath: root,
	}, nil
}

func ResolveUpgradeLocalSourcePath(ws *config.Workspace, rawPath string) (string, error) {
	root, err := apppathpick.ResolvePathPickerRoot(ws)
	if err != nil {
		return "", err
	}
	resolved, err := apppathpick.ResolvePathPickerPath(root, rawPath)
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

func (s UpgradeService) CommandUpgradeLocalPick(msg *feishu.InboundMessage) error {
	if msg == nil {
		return nil
	}
	sessionKey, ws := s.app.UpgradeCurrentWorkspace(msg)
	requestID, payload, err := s.CreateUpgradeLocalPickerRequest(sessionKey, ws, msg.UserID, "")
	if err != nil {
		return err
	}
	card, err := s.app.UpgradeRenderPathPickerCard(requestID, payload)
	if err != nil {
		return err
	}
	msgID, err := s.app.UpgradeFeishu().ReplyCard(context.Background(), msg.MessageID, card, s.app.ReplyInThreadEnabled(msg.ChatType))
	if err != nil {
		return err
	}
	return s.app.UpgradeState().UpdatePending(requestID, func(req *state.PendingRequest) {
		req.FeishuMsgID = msgID
	})
}

func (s UpgradeService) CommandUpgradeLocalPath(msg *feishu.InboundMessage, rawPath string) error {
	if msg == nil {
		return nil
	}
	sessionKey, ws := s.app.UpgradeCurrentWorkspace(msg)
	selectedPath, err := ResolveUpgradeLocalSourcePath(ws, rawPath)
	if err != nil {
		return err
	}
	requestID, payload, err := s.CreateLocalUpgradeRequest(sessionKey, msg.UserID, "", selectedPath)
	if err != nil {
		return err
	}
	card := s.RenderUpgradeConfirmCard("升级确认", sessionKey, requestID, payload, s.UpgradeLocalConfirmLines(payload.BinaryPath))
	msgID, err := s.app.UpgradeFeishu().ReplyCard(context.Background(), msg.MessageID, card, s.app.ReplyInThreadEnabled(msg.ChatType))
	if err != nil {
		return err
	}
	return s.app.UpgradeState().UpdatePending(requestID, func(req *state.PendingRequest) {
		req.FeishuMsgID = msgID
	})
}

func (s UpgradeService) CreateUpgradeLocalPickerRequest(sessionKey string, ws *config.Workspace, ownerUserID, feishuMsgID string) (string, PathPickerPayload, error) {
	if _, _, err := s.ValidateUpgradeRuntime(); err != nil {
		return "", PathPickerPayload{}, err
	}
	payload, err := NewUpgradeLocalPickerPayload(ws)
	if err != nil {
		return "", PathPickerPayload{}, err
	}
	appState := s.app.UpgradeState()
	requestID, err := appState.NextLocalID("upgrade-local")
	if err != nil {
		return "", PathPickerPayload{}, err
	}
	if err := appState.SavePending(&state.PendingRequest{
		ID:          requestID,
		Kind:        UpgradeLocalBinaryPendingKind,
		SessionKey:  sessionKey,
		OwnerUserID: ownerUserID,
		FeishuMsgID: feishuMsgID,
		PayloadJSON: mustJSON(payload),
		Status:      state.PendingRequestStatusPending.String(),
		CreatedAt:   time.Now().Unix(),
		ExpiresAt:   time.Now().Add(10 * time.Minute).Unix(),
	}); err != nil {
		return "", PathPickerPayload{}, err
	}
	return requestID, payload, nil
}

func (s UpgradeService) CreateLocalUpgradeRequest(sessionKey, ownerUserID, feishuMsgID, selectedPath string) (string, UpgradePendingPayload, error) {
	exePath, _, err := s.ValidateUpgradeRuntime()
	if err != nil {
		return "", UpgradePendingPayload{}, err
	}
	appState := s.app.UpgradeState()
	requestID, err := appState.NextLocalID("upgrade")
	if err != nil {
		return "", UpgradePendingPayload{}, err
	}
	stagedPath, sha256Hex, sizeBytes, err := s.StageLocalUpgradeArtifact(requestID, selectedPath)
	if err != nil {
		return "", UpgradePendingPayload{}, err
	}
	payload := UpgradePendingPayload{
		CurrentVersion: s.deps.CurrentVersion(),
		TargetVersion:  filepath.Base(selectedPath),
		BinaryPath:     exePath,
		SourcePath:     stagedPath,
		SourceKind:     "local_file",
		SourceName:     filepath.Base(selectedPath),
		SourceSize:     sizeBytes,
		ExpectedSHA256: sha256Hex,
	}
	if err := appState.SavePending(&state.PendingRequest{
		ID:          requestID,
		Kind:        "upgrade_release",
		SessionKey:  sessionKey,
		OwnerUserID: ownerUserID,
		FeishuMsgID: feishuMsgID,
		PayloadJSON: mustJSON(payload),
		Status:      state.PendingRequestStatusPending.String(),
		CreatedAt:   time.Now().Unix(),
		ExpiresAt:   time.Now().Add(30 * time.Minute).Unix(),
	}); err != nil {
		return "", UpgradePendingPayload{}, err
	}
	return requestID, payload, nil
}

func (s UpgradeService) CompleteUpgradeLocalPick(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	sessionKey := actionSessionKey(action)
	ws := s.app.UpgradeWorkspaceForSession(sessionKey)
	requestID, payload, err := s.CreateUpgradeLocalPickerRequest(sessionKey, ws, action.UserID, action.MessageID)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	card, err := s.app.UpgradeRenderPathPickerCard(requestID, payload)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "info", Content: "请选择本地 Binary"},
		Card:  rawCard(card),
	}, nil
}

func (s UpgradeService) CompleteUpgradeLocalBinaryConfirm(action *feishu.CardAction, pending *state.PendingRequest, payload PathPickerPayload, selectedPath string) (*callback.CardActionTriggerResponse, error) {
	sessionKey := firstNonEmpty(strings.TrimSpace(pending.SessionKey), actionSessionKey(action))
	requestID, upgradePayload, err := s.CreateLocalUpgradeRequest(sessionKey, pending.OwnerUserID, firstNonEmpty(strings.TrimSpace(pending.FeishuMsgID), strings.TrimSpace(action.MessageID)), selectedPath)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	_ = s.app.UpgradeState().UpdatePending(pending.ID, func(req *state.PendingRequest) {
		req.Status = state.PendingRequestStatusResolved.String()
		req.PayloadJSON = mustJSON(payload)
	})
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已选择本地 Binary"},
		Card:  rawCard(s.RenderUpgradeConfirmCard("升级确认", sessionKey, requestID, upgradePayload, s.UpgradeLocalConfirmLines(upgradePayload.BinaryPath))),
	}, nil
}

func (s UpgradeService) StageLocalUpgradeArtifact(requestID, sourcePath string) (string, string, int64, error) {
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
	dir := filepath.Join(s.app.UpgradeDataDir(), "upgrades", requestID)
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

func (s UpgradeService) UpgradeLocalConfirmLines(binaryPath string) []string {
	return []string{
		"当前版本: `" + s.deps.CurrentVersion() + "`",
		"目标架构: `" + s.deps.CurrentGOARCH() + "`",
		"二进制: `" + binaryPath + "`",
	}
}

func actionSessionKey(action *feishu.CardAction) string {
	return actionStringValue(action, "session_key")
}

func actionStringValue(action *feishu.CardAction, key string) string {
	if action == nil {
		return ""
	}
	value, _ := action.ActionValue[key].(string)
	return strings.TrimSpace(value)
}
