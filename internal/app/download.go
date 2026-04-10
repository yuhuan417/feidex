package app

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

const downloadFilePendingKind = "download_file"

func (a *App) commandDownload(msg *feishu.InboundMessage, args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("usage: /download")
	}
	if msg == nil {
		return nil
	}
	sessionKey, _, ws := a.currentWorkspaceForMessage(msg)
	payload, err := a.newDownloadPathPickerPayload(ws)
	if err != nil {
		return err
	}
	requestID, err := a.store.NextLocalID("download")
	if err != nil {
		return err
	}
	card, err := a.renderPathPickerCard(requestID, payload)
	if err != nil {
		return err
	}
	msgID, err := a.feishu.ReplyCard(context.Background(), msg.MessageID, card, msg.ChatType == "group" && a.cfg.Feishu.ReplyInThread)
	if err != nil {
		return err
	}
	return a.store.UpsertPending(&state.PendingRequest{
		ID:          requestID,
		Kind:        downloadFilePendingKind,
		SessionKey:  sessionKey,
		OwnerUserID: msg.UserID,
		FeishuMsgID: msgID,
		PayloadJSON: mustJSON(payload),
		Status:      "pending",
		CreatedAt:   time.Now().Unix(),
		ExpiresAt:   time.Now().Add(10 * time.Minute).Unix(),
	})
}

func (a *App) completeMenuDownload(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	wsID := a.defaultWorkspaceID()
	if sess := a.store.GetSession(sessionKey); sess != nil && strings.TrimSpace(sess.WorkspaceID) != "" {
		wsID = sess.WorkspaceID
	}
	ws := config.FindWorkspace(a.cfg, wsID)
	payload, err := a.newDownloadPathPickerPayload(ws)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	requestID, err := a.store.NextLocalID("download")
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: err.Error()}}, nil
	}
	_ = a.store.UpsertPending(&state.PendingRequest{
		ID:          requestID,
		Kind:        downloadFilePendingKind,
		SessionKey:  sessionKey,
		OwnerUserID: action.UserID,
		FeishuMsgID: action.MessageID,
		PayloadJSON: mustJSON(payload),
		Status:      "pending",
		CreatedAt:   time.Now().Unix(),
		ExpiresAt:   time.Now().Add(10 * time.Minute).Unix(),
	})
	card, err := a.renderPathPickerCard(requestID, payload)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "info", Content: "请选择要下载的文件"},
		Card:  rawCard(card),
	}, nil
}

func (a *App) newDownloadPathPickerPayload(ws *config.Workspace) (pathPickerPayload, error) {
	root, err := resolvePathPickerRoot(ws)
	if err != nil {
		return pathPickerPayload{}, err
	}
	return pathPickerPayload{
		Mode:        pathPickerModeFile,
		Style:       pathPickerStyleDropdown,
		RootPath:    root,
		CurrentPath: root,
	}, nil
}

func (a *App) completeDownloadFileConfirm(action *feishu.CardAction, pending *state.PendingRequest, payload pathPickerPayload, selectedPath string) (*callback.CardActionTriggerResponse, error) {
	if pending == nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "下载请求已过期"}}, nil
	}
	sess := a.store.GetSession(pending.SessionKey)
	chatID := strings.TrimSpace(action.ChatID)
	userID := strings.TrimSpace(action.UserID)
	workspaceCWD := strings.TrimSpace(payload.RootPath)
	if sess != nil {
		chatID = firstNonEmpty(chatID, sess.ChatID)
		workspaceID := firstNonEmpty(strings.TrimSpace(sess.WorkspaceID), a.defaultWorkspaceID())
		if ws := config.FindWorkspace(a.cfg, workspaceID); ws != nil {
			workspaceCWD = firstNonEmpty(workspaceCWD, strings.TrimSpace(ws.Cwd))
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	result, err := a.feishu.ShareLocalFile(ctx, feishu.SharedFileRequest{
		LocalPath: selectedPath,
		ChatID:    chatID,
		UserID:    firstNonEmpty(userID, pending.OwnerUserID),
	})
	if err != nil {
		card, renderErr := a.renderPathPickerCard(pending.ID, payload)
		if renderErr != nil {
			return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
		}
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "warning", Content: err.Error()},
			Card:  rawCard(card),
		}, nil
	}
	_ = a.store.UpdatePending(pending.ID, func(req *state.PendingRequest) {
		req.Status = "resolved"
		req.PayloadJSON = mustJSON(payload)
	})
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已生成下载链接"},
		Card:  rawCard(a.renderDownloadReadyCard(selectedPath, workspaceCWD, result)),
	}, nil
}

func (a *App) renderDownloadReadyCard(selectedPath, workspaceCWD string, result feishu.SharedFileResult) map[string]any {
	displayPath := renderDownloadDisplayPath(selectedPath, workspaceCWD)
	lines := []string{
		"已生成文件下载链接（飞书云盘中转）。",
		"",
		"文件: `" + firstNonEmpty(strings.TrimSpace(result.FileName), filepath.Base(selectedPath)) + "`",
		"路径: `" + displayPath + "`",
	}
	if result.SizeBytes > 0 {
		lines = append(lines, "大小: `"+formatDownloadSize(result.SizeBytes)+"`")
	}
	if url := strings.TrimSpace(result.URL); url != "" {
		lines = append(lines, "", "[点击下载]("+url+")", url)
	}
	return a.feishu.SimpleStatusCard("文件下载", "green", strings.Join(lines, "\n"), nil)
}

func renderDownloadDisplayPath(selectedPath, workspaceCWD string) string {
	selectedPath = strings.TrimSpace(selectedPath)
	workspaceCWD = strings.TrimSpace(workspaceCWD)
	if selectedPath == "" {
		return "-"
	}
	if workspaceCWD != "" {
		if rel, err := filepath.Rel(workspaceCWD, selectedPath); err == nil && strings.TrimSpace(rel) != "" && !strings.HasPrefix(rel, "..") {
			return filepath.Clean(rel)
		}
	}
	return selectedPath
}

func formatDownloadSize(size int64) string {
	if size < 1024 {
		return fmt.Sprintf("%d B", size)
	}
	units := []string{"KB", "MB", "GB", "TB"}
	value := float64(size)
	unit := "B"
	for _, next := range units {
		value /= 1024
		unit = next
		if value < 1024 {
			break
		}
	}
	if unit == "KB" {
		return fmt.Sprintf("%.1f %s", value, unit)
	}
	return fmt.Sprintf("%.2f %s", value, unit)
}
