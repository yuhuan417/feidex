package app

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"feidex/internal/feishu"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

const feishuPermissionNotifyDedupWindow = 30 * time.Second

type feishuNotifyTarget struct {
	ChatID    string
	MessageID string
	InThread  bool
}

type notifyingFeishuClient struct {
	base feishuClient

	mu     sync.Mutex
	recent map[string]time.Time
}

func wrapFeishuClient(base feishuClient) feishuClient {
	if base == nil {
		return nil
	}
	return &notifyingFeishuClient{
		base:   base,
		recent: map[string]time.Time{},
	}
}

func (n *notifyingFeishuClient) SetHandlers(onMessage func(*feishu.InboundMessage), onCardAction func(*feishu.CardAction) (*callback.CardActionTriggerResponse, error), onBotMenu func(*feishu.BotMenuClick), onRecall func(*feishu.MessageRecall), onReaction func(*feishu.MessageReaction)) {
	n.base.SetHandlers(onMessage, onCardAction, onBotMenu, onRecall, onReaction)
}

func (n *notifyingFeishuClient) Start(ctx context.Context) error {
	return n.base.Start(ctx)
}

func (n *notifyingFeishuClient) Stop() {
	n.base.Stop()
}

func (n *notifyingFeishuClient) ConfigureMarkdownPreview(statePath, processCWD string) {
	n.base.ConfigureMarkdownPreview(statePath, processCWD)
}

func (n *notifyingFeishuClient) RewriteMarkdownPreview(ctx context.Context, req feishu.MarkdownPreviewRequest) (string, error) {
	text, err := n.base.RewriteMarkdownPreview(ctx, req)
	if err != nil {
		n.notifyPermissionIssue(feishuNotifyTarget{ChatID: req.ChatID}, err)
	}
	return text, err
}

func (n *notifyingFeishuClient) CleanupArtifactsBefore(ctx context.Context, cutoff time.Time) (feishu.PreviewDriveCleanupResult, error) {
	return n.base.CleanupArtifactsBefore(ctx, cutoff)
}

func (n *notifyingFeishuClient) AddReaction(ctx context.Context, messageID, emoji string) error {
	err := n.base.AddReaction(ctx, messageID, emoji)
	if err != nil {
		n.notifyPermissionIssue(feishuNotifyTarget{MessageID: messageID}, err)
	}
	return err
}

func (n *notifyingFeishuClient) RemoveReaction(ctx context.Context, messageID, emoji string) error {
	err := n.base.RemoveReaction(ctx, messageID, emoji)
	if err != nil {
		n.notifyPermissionIssue(feishuNotifyTarget{MessageID: messageID}, err)
	}
	return err
}

func (n *notifyingFeishuClient) ReplyText(ctx context.Context, messageID, text string, inThread bool) error {
	err := n.base.ReplyText(ctx, messageID, text, inThread)
	if err != nil {
		n.notifyPermissionIssue(feishuNotifyTarget{MessageID: messageID, InThread: inThread}, err)
	}
	return err
}

func (n *notifyingFeishuClient) ReplyTextWithID(ctx context.Context, messageID, text string, inThread bool) (string, error) {
	id, err := n.base.ReplyTextWithID(ctx, messageID, text, inThread)
	if err != nil {
		n.notifyPermissionIssue(feishuNotifyTarget{MessageID: messageID, InThread: inThread}, err)
	}
	return id, err
}

func (n *notifyingFeishuClient) SendText(ctx context.Context, chatID, text string) error {
	err := n.base.SendText(ctx, chatID, text)
	if err != nil {
		n.notifyPermissionIssue(feishuNotifyTarget{ChatID: chatID}, err)
	}
	return err
}

func (n *notifyingFeishuClient) ReplyCard(ctx context.Context, messageID string, card map[string]any, inThread bool) (string, error) {
	id, err := n.base.ReplyCard(ctx, messageID, card, inThread)
	if err != nil {
		n.notifyPermissionIssue(feishuNotifyTarget{MessageID: messageID, InThread: inThread}, err)
	}
	return id, err
}

func (n *notifyingFeishuClient) SendCard(ctx context.Context, chatID string, card map[string]any) (string, error) {
	id, err := n.base.SendCard(ctx, chatID, card)
	if err != nil {
		n.notifyPermissionIssue(feishuNotifyTarget{ChatID: chatID}, err)
	}
	return id, err
}

func (n *notifyingFeishuClient) PatchCard(ctx context.Context, messageID string, card map[string]any) error {
	err := n.base.PatchCard(ctx, messageID, card)
	if err != nil {
		n.notifyPermissionIssue(feishuNotifyTarget{MessageID: messageID}, err)
	}
	return err
}

func (n *notifyingFeishuClient) DownloadMessageResource(ctx context.Context, messageID string, attachment feishu.Attachment, dir string) (string, string, error) {
	path, name, err := n.base.DownloadMessageResource(ctx, messageID, attachment, dir)
	if err != nil {
		n.notifyPermissionIssue(feishuNotifyTarget{MessageID: messageID}, err)
	}
	return path, name, err
}

func (n *notifyingFeishuClient) ResolveMergeForward(ctx context.Context, messageID string, messageIDs []string) (string, []feishu.Attachment, error) {
	text, attachments, err := n.base.ResolveMergeForward(ctx, messageID, messageIDs)
	if err != nil {
		n.notifyPermissionIssue(feishuNotifyTarget{MessageID: messageID}, err)
	}
	return text, attachments, err
}

func (n *notifyingFeishuClient) ShareLocalFile(ctx context.Context, req feishu.SharedFileRequest) (feishu.SharedFileResult, error) {
	result, err := n.base.ShareLocalFile(ctx, req)
	if err != nil {
		n.notifyPermissionIssue(feishuNotifyTarget{ChatID: req.ChatID}, err)
	}
	return result, err
}

func (n *notifyingFeishuClient) SimpleStatusCard(title, color, body string, buttons []feishu.Button) map[string]any {
	return n.base.SimpleStatusCard(title, color, body, buttons)
}

func (n *notifyingFeishuClient) notifyPermissionIssue(target feishuNotifyTarget, err error) {
	if n == nil || n.base == nil {
		return
	}
	issue, ok := feishu.PermissionIssueFromError(err)
	if !ok || issue == nil {
		return
	}
	target.ChatID = strings.TrimSpace(target.ChatID)
	target.MessageID = strings.TrimSpace(target.MessageID)
	if target.ChatID == "" && target.MessageID == "" {
		return
	}
	key := n.permissionIssueKey(target, issue)
	if !n.shouldSendPermissionIssue(key) {
		return
	}
	body := renderFeishuPermissionIssueBody(issue)
	if body == "" {
		return
	}
	card := n.base.SimpleStatusCard("飞书权限错误", "red", body, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var sendErr error
	switch {
	case target.MessageID != "":
		_, sendErr = n.base.ReplyCard(ctx, target.MessageID, card, target.InThread)
	case target.ChatID != "":
		_, sendErr = n.base.SendCard(ctx, target.ChatID, card)
	}
	if sendErr != nil {
		slog.Warn("send feishu permission diagnostic failed",
			"message_id", target.MessageID,
			"chat_id", target.ChatID,
			"error", sendErr,
		)
	}
}

func (n *notifyingFeishuClient) permissionIssueKey(target feishuNotifyTarget, issue *feishu.PermissionIssue) string {
	if issue == nil {
		return ""
	}
	return strings.Join([]string{
		strings.TrimSpace(target.ChatID),
		strings.TrimSpace(target.MessageID),
		strings.TrimSpace(issue.API),
		fmt.Sprint(issue.Code),
		strings.TrimSpace(issue.Message),
		strings.TrimSpace(issue.LogID),
	}, "|")
}

func (n *notifyingFeishuClient) shouldSendPermissionIssue(key string) bool {
	if strings.TrimSpace(key) == "" {
		return false
	}
	now := time.Now()
	cutoff := now.Add(-feishuPermissionNotifyDedupWindow)
	n.mu.Lock()
	defer n.mu.Unlock()
	for existingKey, ts := range n.recent {
		if ts.Before(cutoff) {
			delete(n.recent, existingKey)
		}
	}
	if ts, ok := n.recent[key]; ok && now.Sub(ts) < feishuPermissionNotifyDedupWindow {
		return false
	}
	n.recent[key] = now
	return true
}

func renderFeishuPermissionIssueBody(issue *feishu.PermissionIssue) string {
	if issue == nil {
		return ""
	}
	lines := []string{"检测到飞书接口权限或鉴权失败。"}
	if api := strings.TrimSpace(issue.API); api != "" {
		lines = append(lines, "接口: `"+api+"`")
	}
	if issue.Code != 0 || strings.TrimSpace(issue.Message) != "" {
		msg := strings.TrimSpace(issue.Message)
		if msg == "" {
			msg = "-"
		}
		lines = append(lines, fmt.Sprintf("返回: code=`%d` msg=`%s`", issue.Code, escapeInlineBackticks(msg)))
	}
	if cause := strings.TrimSpace(issue.Cause); cause != "" && cause != strings.TrimSpace(issue.Message) {
		lines = append(lines, "错误: `"+escapeInlineBackticks(truncate(cause, 300))+"`")
	}
	if logID := strings.TrimSpace(issue.LogID); logID != "" {
		lines = append(lines, "log_id: `"+escapeInlineBackticks(logID)+"`")
	}
	if ts := strings.TrimSpace(issue.Troubleshooter); ts != "" {
		lines = append(lines, "排障链接: "+ts)
	}
	for _, violation := range issue.PermissionViolations {
		item := joinNonEmpty(" | ",
			firstNonEmpty(strings.TrimSpace(violation.Description), ""),
			labelledValue("type", violation.Type),
			labelledValue("subject", violation.Subject),
		)
		if item != "" {
			lines = append(lines, "权限信息: "+item)
		}
	}
	for _, detail := range issue.Details {
		item := joinNonEmpty(" = ", strings.TrimSpace(detail.Key), strings.TrimSpace(detail.Value))
		if item != "" {
			lines = append(lines, "细节: `"+escapeInlineBackticks(truncate(item, 300))+"`")
		}
	}
	for _, violation := range issue.FieldViolations {
		item := joinNonEmpty(" | ",
			labelledValue("field", violation.Field),
			labelledValue("value", violation.Value),
			firstNonEmpty(strings.TrimSpace(violation.Description), ""),
		)
		if item != "" {
			lines = append(lines, "字段校验: "+item)
		}
	}
	for _, help := range issue.Helps {
		item := strings.TrimSpace(help.URL)
		if desc := strings.TrimSpace(help.Description); desc != "" {
			item = joinNonEmpty(" | ", desc, item)
		}
		if item != "" {
			lines = append(lines, "帮助链接: "+item)
		}
	}
	return strings.Join(lines, "\n")
}

func labelledValue(label, value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	return label + "=" + value
}

func joinNonEmpty(sep string, values ...string) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		parts = append(parts, value)
	}
	return strings.Join(parts, sep)
}

func escapeInlineBackticks(value string) string {
	return strings.ReplaceAll(strings.TrimSpace(value), "`", "'")
}
