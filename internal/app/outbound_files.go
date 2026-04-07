package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

type outboundReplyFile struct {
	Path string `json:"path"`
	Name string `json:"name"`
	Size int64  `json:"size"`
}

type outboundFilesPayload struct {
	SubmissionID     string              `json:"submission_id"`
	TriggerMessageID string              `json:"trigger_message_id"`
	Files            []outboundReplyFile `json:"files"`
}

func (a *App) sendOutboundFilesConfirmation(ctx context.Context, sub *state.Submission, outputText string) error {
	if sub == nil {
		return nil
	}
	workspace := config.FindWorkspace(a.cfg, sub.WorkspaceID)
	if workspace == nil {
		return fmt.Errorf("workspace %q not found", sub.WorkspaceID)
	}
	files := collectReplyFileInfos(outputText, workspace.Cwd)
	if len(files) == 0 {
		return nil
	}
	requestID := fmt.Sprintf("outbound-files-%s", sub.ID)
	payload := outboundFilesPayload{
		SubmissionID:     sub.ID,
		TriggerMessageID: sub.TriggerMessageID,
		Files:            files,
	}
	card := a.feishu.SimpleStatusCard("检测到可回传文件", "orange", renderOutboundFilesBody(files), []feishu.Button{
		{Text: "发送文件", Type: "primary", Value: map[string]any{"action": "outbound_files.send", "request_id": requestID}},
		{Text: "取消", Type: "default", Value: map[string]any{"action": "outbound_files.cancel", "request_id": requestID}},
	})
	msgID, err := a.feishu.SendCard(ctx, sub.ChatID, card)
	if err != nil {
		return err
	}
	a.recordMessageLink(msgID, "outbound_files_card", sub, requestID)
	return a.store.UpsertPending(&state.PendingRequest{
		ID:          requestID,
		Kind:        "outbound_files",
		SessionKey:  sub.SessionKey,
		ThreadID:    sub.ThreadID,
		TurnID:      sub.TurnID,
		OwnerUserID: sub.UserID,
		FeishuMsgID: msgID,
		PayloadJSON: mustJSON(payload),
		Status:      "pending",
		CreatedAt:   time.Now().Unix(),
		ExpiresAt:   time.Now().Add(30 * time.Minute).Unix(),
	})
}

func (a *App) completeOutboundFilesAction(action *feishu.CardAction, actionName string) (*callback.CardActionTriggerResponse, error) {
	requestID, _ := action.ActionValue["request_id"].(string)
	pending := a.store.PendingByID(requestID)
	if pending == nil || pending.Kind != "outbound_files" {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "文件回传请求已过期"}}, nil
	}
	if pending.OwnerUserID != "" && pending.OwnerUserID != action.UserID {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "你没有权限处理这个回传请求"}}, nil
	}
	if pending.Status != "pending" {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "文件回传请求已处理"}}, nil
	}

	var payload outboundFilesPayload
	if err := json.Unmarshal([]byte(pending.PayloadJSON), &payload); err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: "文件回传数据损坏"}}, nil
	}
	if actionName == "outbound_files.cancel" {
		_ = a.store.UpdatePending(requestID, func(req *state.PendingRequest) { req.Status = "resolved" })
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "success", Content: "已取消文件回传"},
			Card:  rawCard(a.feishu.SimpleStatusCard("文件回传已取消", "grey", renderOutboundFilesBody(payload.Files), nil)),
		}, nil
	}

	sub := a.store.GetSubmission(payload.SubmissionID)
	if sub == nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: "找不到对应任务"}}, nil
	}
	inThread := false
	sess := a.store.GetSession(pending.SessionKey)
	if sess != nil && sess.ChatType == "group" {
		inThread = a.cfg.Feishu.ReplyInThread
	}

	var failures []string
	for _, file := range payload.Files {
		if err := a.feishu.ReplyLocalFile(context.Background(), payload.TriggerMessageID, file.Path, inThread); err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", file.Name, err))
		}
	}
	_ = a.store.UpdatePending(requestID, func(req *state.PendingRequest) { req.Status = "resolved" })
	if len(failures) > 0 {
		body := renderOutboundFilesBody(payload.Files) + "\n\n发送失败:\n" + strings.Join(limitFailures(failures), "\n")
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "warning", Content: "部分文件回传失败"},
			Card:  rawCard(a.feishu.SimpleStatusCard("文件回传部分失败", "red", body, nil)),
		}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "文件已回传"},
		Card:  rawCard(a.feishu.SimpleStatusCard("文件已回传", "green", renderOutboundFilesBody(payload.Files), nil)),
	}, nil
}

func collectReplyFileInfos(outputText, workspaceCwd string) []outboundReplyFile {
	paths := collectReplyFiles(outputText, workspaceCwd)
	files := make([]outboundReplyFile, 0, len(paths))
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		files = append(files, outboundReplyFile{
			Path: path,
			Name: filepath.Base(path),
			Size: info.Size(),
		})
	}
	return files
}

func renderOutboundFilesBody(files []outboundReplyFile) string {
	if len(files) == 0 {
		return "没有可回传文件。"
	}
	lines := []string{"检测到以下文件可回传到飞书："}
	for _, file := range files {
		lines = append(lines, fmt.Sprintf("- `%s` (%s)", file.Name, humanFileSize(file.Size)))
	}
	return strings.Join(lines, "\n")
}

func humanFileSize(size int64) string {
	units := []string{"B", "KB", "MB", "GB"}
	value := float64(size)
	unit := units[0]
	for i := 1; i < len(units) && value >= 1024; i++ {
		value /= 1024
		unit = units[i]
	}
	if unit == "B" {
		return fmt.Sprintf("%d %s", size, unit)
	}
	return fmt.Sprintf("%.1f %s", value, unit)
}

func limitFailures(failures []string) []string {
	if len(failures) <= 3 {
		return failures
	}
	return append(failures[:3], "...")
}

func rawCard(card map[string]any) *callback.Card {
	return &callback.Card{Type: "raw", Data: card}
}
