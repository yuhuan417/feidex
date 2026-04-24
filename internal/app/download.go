package app

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	appdelivery "feidex/internal/app/delivery"
	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

const downloadFilePendingKind = "download_file"

func (s conversationWorkflowService) commandDownload(msg *feishu.InboundMessage, args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("usage: /download")
	}
	if msg == nil {
		return nil
	}
	sessionKey, _, ws := newWorkspaceConfigService(s.app).currentWorkspaceForMessage(msg)
	appState := s.app.appState()
	payload, err := s.app.newDownloadPathPickerPayload(ws)
	if err != nil {
		return err
	}
	requestID, err := appState.nextLocalID("download")
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
	return appState.savePending(&state.PendingRequest{
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

func (s conversationWorkflowService) completeMenuDownload(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return s.app.completeMenuCommand(action, sessionKey, "/download", "menu.tools")
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
	if strings.TrimSpace(pending.Status) == "processing" {
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "info", Content: "正在生成下载链接，请稍候"},
			Card:  rawCard(a.renderDownloadPreparingCard(selectedPath, payload.RootPath)),
		}, nil
	}
	appState := a.appState()
	sess := appState.session(pending.SessionKey)
	chatID := strings.TrimSpace(action.ChatID)
	userID := strings.TrimSpace(action.UserID)
	messageID := firstNonEmpty(strings.TrimSpace(pending.FeishuMsgID), strings.TrimSpace(action.MessageID))
	workspaceCWD := strings.TrimSpace(payload.RootPath)
	if sess != nil {
		chatID = firstNonEmpty(chatID, sess.ChatID)
		workspaceID := firstNonEmpty(strings.TrimSpace(sess.WorkspaceID), a.defaultWorkspaceID())
		if ws := config.FindWorkspace(a.cfg, workspaceID); ws != nil {
			workspaceCWD = firstNonEmpty(workspaceCWD, strings.TrimSpace(ws.Cwd))
		}
	}
	_ = appState.updatePending(pending.ID, func(req *state.PendingRequest) {
		req.Status = "processing"
		req.PayloadJSON = mustJSON(payload)
		if strings.TrimSpace(req.FeishuMsgID) == "" {
			req.FeishuMsgID = messageID
		}
	})
	go a.finishDownloadFileShare(pending.ID, messageID, payload, selectedPath, workspaceCWD, feishu.SharedFileRequest{
		LocalPath: selectedPath,
		ChatID:    chatID,
		UserID:    firstNonEmpty(userID, pending.OwnerUserID),
	})
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "info", Content: "正在生成下载链接"},
		Card:  rawCard(a.renderDownloadPreparingCard(selectedPath, workspaceCWD)),
	}, nil
}

func (a *App) finishDownloadFileShare(requestID, messageID string, payload pathPickerPayload, selectedPath, workspaceCWD string, req feishu.SharedFileRequest) {
	appState := a.appState()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	slog.Debug("download share started",
		"request_id", requestID,
		"message_id", messageID,
		"path", selectedPath,
	)
	result, err := a.feishu.ShareLocalFile(ctx, req)
	if err != nil {
		slog.Warn("download share failed",
			"request_id", requestID,
			"message_id", messageID,
			"path", selectedPath,
			"error", err,
		)
		_ = appState.updatePending(requestID, func(p *state.PendingRequest) {
			p.Status = "pending"
			p.PayloadJSON = mustJSON(payload)
		})
		if strings.TrimSpace(messageID) == "" {
			return
		}
		card, renderErr := newWorkspaceRenderService(a).renderPathPickerCard(requestID, payload)
		if renderErr != nil {
			slog.Error("download failure card render failed",
				"request_id", requestID,
				"message_id", messageID,
				"error", renderErr,
			)
			_ = a.feishu.PatchCard(context.Background(), messageID, a.renderDownloadFailedCard(selectedPath, workspaceCWD, err.Error()))
			return
		}
		_ = a.feishu.PatchCard(context.Background(), messageID, card)
		return
	}
	slog.Debug("download share completed",
		"request_id", requestID,
		"message_id", messageID,
		"path", selectedPath,
		"url", result.URL,
	)
	_ = appState.updatePending(requestID, func(p *state.PendingRequest) {
		p.Status = "resolved"
		p.PayloadJSON = mustJSON(payload)
	})
	if strings.TrimSpace(messageID) == "" {
		return
	}
	_ = a.feishu.PatchCard(context.Background(), messageID, a.renderDownloadReadyCard(selectedPath, workspaceCWD, result))
}

func (a *App) renderDownloadPreparingCard(selectedPath, workspaceCWD string) map[string]any {
	displayPath := renderDownloadDisplayPath(selectedPath, workspaceCWD)
	lines := []string{
		"正在生成文件下载链接（飞书云盘中转）。",
		"",
		"文件: `" + filepath.Base(selectedPath) + "`",
		"路径: `" + displayPath + "`",
		"",
		"请稍候，这张卡片会自动刷新。",
	}
	return a.feishu.SimpleStatusCard("文件下载", "blue", strings.Join(lines, "\n"), nil)
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

func (a *App) renderDownloadFailedCard(selectedPath, workspaceCWD, errText string) map[string]any {
	displayPath := renderDownloadDisplayPath(selectedPath, workspaceCWD)
	lines := []string{
		"生成下载链接失败。",
		"",
		"文件: `" + filepath.Base(selectedPath) + "`",
		"路径: `" + displayPath + "`",
	}
	if strings.TrimSpace(errText) != "" {
		lines = append(lines, "", "错误: "+strings.TrimSpace(errText))
	}
	return a.feishu.SimpleStatusCard("文件下载", "orange", strings.Join(lines, "\n"), nil)
}

func renderDownloadDisplayPath(selectedPath, workspaceCWD string) string {
	return appdelivery.RenderDownloadDisplayPath(selectedPath, workspaceCWD)
}

func formatDownloadSize(size int64) string {
	return appdelivery.FormatDownloadSize(size)
}
