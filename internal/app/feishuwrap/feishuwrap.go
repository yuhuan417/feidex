// Package feishuwrap provides Feishu client wrappers for command capture
// and permission-issue notification. Extracted from the app god package.
package feishuwrap

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"feidex/internal/app/appcore"
	"feidex/internal/app/apputil"
	"feidex/internal/feishu"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

const PermissionNotifyDedupWindow = 30 * time.Second

// NotifyTarget identifies a Feishu message or chat for permission notifications.
type NotifyTarget struct {
	ChatID    string
	MessageID string
	InThread  bool
	UserID    string
}

// CommandCaptureClient captures Feishu replies instead of sending them.
type CommandCaptureClient struct {
	Base           appcore.FeishuClient
	ReplyMessageID string
	Text           string
	Card           map[string]any
}

// CommandCaptureFeishuClient is the interface for capturing command output.
type CommandCaptureFeishuClient interface {
	CaptureCommandOutput(replyMessageID string, fn func() error) (string, map[string]any, error)
}

func (c *CommandCaptureClient) SetHandlers(onMessage func(*feishu.InboundMessage), onCardAction func(*feishu.CardAction) (*callback.CardActionTriggerResponse, error), onBotMenu func(*feishu.BotMenuClick), onRecall func(*feishu.MessageRecall), onReaction func(*feishu.MessageReaction)) {
	c.Base.SetHandlers(onMessage, onCardAction, onBotMenu, onRecall, onReaction)
}

func (c *CommandCaptureClient) SetGroupMessagePolicy(policy feishu.GroupMessagePolicy) {
	if configurable, ok := c.Base.(interface {
		SetGroupMessagePolicy(feishu.GroupMessagePolicy)
	}); ok {
		configurable.SetGroupMessagePolicy(policy)
	}
}

func (c *CommandCaptureClient) Start(ctx context.Context) error {
	return c.Base.Start(ctx)
}

func (c *CommandCaptureClient) Stop() {
	c.Base.Stop()
}

func (c *CommandCaptureClient) ConfigureLocalFileLinks(statePath, processCWD string) {
	c.Base.ConfigureLocalFileLinks(statePath, processCWD)
}

func (c *CommandCaptureClient) RewriteLocalFileLinks(ctx context.Context, req feishu.LocalFileLinkRewriteRequest) (string, error) {
	return c.Base.RewriteLocalFileLinks(ctx, req)
}

func (c *CommandCaptureClient) CleanupArtifactsBefore(ctx context.Context, cutoff time.Time) (feishu.PreviewDriveCleanupResult, error) {
	return c.Base.CleanupArtifactsBefore(ctx, cutoff)
}

func (c *CommandCaptureClient) AddReaction(ctx context.Context, messageID, emojiType string) error {
	return c.Base.AddReaction(ctx, messageID, emojiType)
}

func (c *CommandCaptureClient) RemoveReaction(ctx context.Context, messageID, emojiType string) error {
	return c.Base.RemoveReaction(ctx, messageID, emojiType)
}

func (c *CommandCaptureClient) ReplyText(_ context.Context, _ string, text string, _ bool) error {
	c.Text = strings.TrimSpace(text)
	c.Card = nil
	return nil
}

func (c *CommandCaptureClient) ReplyTextWithID(_ context.Context, _ string, text string, _ bool) (string, error) {
	c.Text = strings.TrimSpace(text)
	c.Card = nil
	return c.ReplyMessageID, nil
}

func (c *CommandCaptureClient) SendText(_ context.Context, _ string, text string) error {
	c.Text = strings.TrimSpace(text)
	c.Card = nil
	return nil
}

func (c *CommandCaptureClient) ReplyCard(_ context.Context, _ string, card map[string]any, _ bool) (string, error) {
	c.Card = card
	c.Text = ""
	return c.ReplyMessageID, nil
}

func (c *CommandCaptureClient) SendCard(_ context.Context, _ string, card map[string]any) (string, error) {
	c.Card = card
	c.Text = ""
	return c.ReplyMessageID, nil
}

func (c *CommandCaptureClient) PatchCard(_ context.Context, _ string, card map[string]any) error {
	c.Card = card
	c.Text = ""
	return nil
}

func (c *CommandCaptureClient) ReplyLocalAttachment(ctx context.Context, messageID, path string, inThread bool) error {
	return c.Base.ReplyLocalAttachment(ctx, messageID, path, inThread)
}

func (c *CommandCaptureClient) ReplyLocalImage(ctx context.Context, messageID, path string, inThread bool) error {
	return c.Base.ReplyLocalImage(ctx, messageID, path, inThread)
}

func (c *CommandCaptureClient) ReplyLocalVideo(ctx context.Context, messageID, path string, inThread bool) error {
	return c.Base.ReplyLocalVideo(ctx, messageID, path, inThread)
}

func (c *CommandCaptureClient) DownloadMessageResource(ctx context.Context, messageID string, attachment feishu.Attachment, targetDir string) (string, string, error) {
	return c.Base.DownloadMessageResource(ctx, messageID, attachment, targetDir)
}

func (c *CommandCaptureClient) ShareLocalFile(ctx context.Context, req feishu.SharedFileRequest) (feishu.SharedFileResult, error) {
	return c.Base.ShareLocalFile(ctx, req)
}

func (c *CommandCaptureClient) ResolveMergeForward(ctx context.Context, messageID string, messageIDs []string) (string, []feishu.Attachment, error) {
	return c.Base.ResolveMergeForward(ctx, messageID, messageIDs)
}

func (c *CommandCaptureClient) SimpleStatusCard(title, color, body string, buttons []feishu.Button) map[string]any {
	return c.Base.SimpleStatusCard(title, color, body, buttons)
}

func (c *CommandCaptureClient) UrgentApp(ctx context.Context, messageID, userID string) error {
	return c.Base.UrgentApp(ctx, messageID, userID)
}

func (c *CommandCaptureClient) LookupMessageSenderOpenID(ctx context.Context, messageID string) (string, error) {
	return c.Base.LookupMessageSenderOpenID(ctx, messageID)
}

// NotifyingFeishuClient wraps a FeishuClient to intercept replies for
// command capture and to send permission-issue notifications.
type NotifyingFeishuClient struct {
	Base appcore.FeishuClient

	mu        sync.Mutex
	recent    map[string]time.Time
	captureMu sync.Mutex
	captures  []*CommandCaptureClient
}

// WrapFeishuClient creates a NotifyingFeishuClient wrapping the given base client.
func WrapFeishuClient(base appcore.FeishuClient) appcore.FeishuClient {
	if base == nil {
		return nil
	}
	return &NotifyingFeishuClient{
		Base:   base,
		recent: map[string]time.Time{},
	}
}

func (n *NotifyingFeishuClient) CaptureCommandOutput(replyMessageID string, fn func() error) (string, map[string]any, error) {
	capture := &CommandCaptureClient{ReplyMessageID: strings.TrimSpace(replyMessageID)}
	n.captureMu.Lock()
	n.captures = append(n.captures, capture)
	n.captureMu.Unlock()
	defer func() {
		n.captureMu.Lock()
		defer n.captureMu.Unlock()
		for i := len(n.captures) - 1; i >= 0; i-- {
			if n.captures[i] != capture {
				continue
			}
			n.captures = append(n.captures[:i], n.captures[i+1:]...)
			break
		}
	}()
	var err error
	if fn != nil {
		err = fn()
	}
	n.captureMu.Lock()
	defer n.captureMu.Unlock()
	return strings.TrimSpace(capture.Text), feishu.CloneCapturedCard(capture.Card), err
}

func (n *NotifyingFeishuClient) commandCaptureForMessageLocked(messageID string) *CommandCaptureClient {
	messageID = strings.TrimSpace(messageID)
	for i := len(n.captures) - 1; i >= 0; i-- {
		capture := n.captures[i]
		if capture == nil || strings.TrimSpace(capture.ReplyMessageID) != messageID {
			continue
		}
		return capture
	}
	return nil
}

func (n *NotifyingFeishuClient) SetHandlers(onMessage func(*feishu.InboundMessage), onCardAction func(*feishu.CardAction) (*callback.CardActionTriggerResponse, error), onBotMenu func(*feishu.BotMenuClick), onRecall func(*feishu.MessageRecall), onReaction func(*feishu.MessageReaction)) {
	n.Base.SetHandlers(onMessage, onCardAction, onBotMenu, onRecall, onReaction)
}

func (n *NotifyingFeishuClient) SetGroupMessagePolicy(policy feishu.GroupMessagePolicy) {
	if configurable, ok := n.Base.(interface {
		SetGroupMessagePolicy(feishu.GroupMessagePolicy)
	}); ok {
		configurable.SetGroupMessagePolicy(policy)
	}
}

func (n *NotifyingFeishuClient) Start(ctx context.Context) error {
	return n.Base.Start(ctx)
}

func (n *NotifyingFeishuClient) Stop() {
	n.Base.Stop()
}

func (n *NotifyingFeishuClient) ConfigureLocalFileLinks(statePath, processCWD string) {
	n.Base.ConfigureLocalFileLinks(statePath, processCWD)
}

func (n *NotifyingFeishuClient) RewriteLocalFileLinks(ctx context.Context, req feishu.LocalFileLinkRewriteRequest) (string, error) {
	text, err := n.Base.RewriteLocalFileLinks(ctx, req)
	if err != nil {
		n.NotifyPermissionIssue(NotifyTarget{ChatID: req.ChatID, UserID: req.UserID}, err)
	}
	return text, err
}

func (n *NotifyingFeishuClient) CleanupArtifactsBefore(ctx context.Context, cutoff time.Time) (feishu.PreviewDriveCleanupResult, error) {
	return n.Base.CleanupArtifactsBefore(ctx, cutoff)
}

func (n *NotifyingFeishuClient) AddReaction(ctx context.Context, messageID, emoji string) error {
	err := n.Base.AddReaction(ctx, messageID, emoji)
	if err != nil {
		n.NotifyPermissionIssue(NotifyTarget{MessageID: messageID}, err)
	}
	return err
}

func (n *NotifyingFeishuClient) RemoveReaction(ctx context.Context, messageID, emoji string) error {
	err := n.Base.RemoveReaction(ctx, messageID, emoji)
	if err != nil {
		n.NotifyPermissionIssue(NotifyTarget{MessageID: messageID}, err)
	}
	return err
}

func (n *NotifyingFeishuClient) ReplyText(ctx context.Context, messageID, text string, inThread bool) error {
	n.captureMu.Lock()
	if capture := n.commandCaptureForMessageLocked(messageID); capture != nil {
		capture.Text = strings.TrimSpace(text)
		capture.Card = nil
		n.captureMu.Unlock()
		return nil
	}
	n.captureMu.Unlock()
	err := n.Base.ReplyText(ctx, messageID, text, inThread)
	if err != nil {
		n.NotifyPermissionIssue(NotifyTarget{MessageID: messageID, InThread: inThread}, err)
	}
	return err
}

func (n *NotifyingFeishuClient) ReplyTextWithID(ctx context.Context, messageID, text string, inThread bool) (string, error) {
	n.captureMu.Lock()
	if capture := n.commandCaptureForMessageLocked(messageID); capture != nil {
		capture.Text = strings.TrimSpace(text)
		capture.Card = nil
		n.captureMu.Unlock()
		return apputil.FirstNonEmpty(strings.TrimSpace(capture.ReplyMessageID), strings.TrimSpace(messageID)), nil
	}
	n.captureMu.Unlock()
	id, err := n.Base.ReplyTextWithID(ctx, messageID, text, inThread)
	if err != nil {
		n.NotifyPermissionIssue(NotifyTarget{MessageID: messageID, InThread: inThread}, err)
	}
	return id, err
}

func (n *NotifyingFeishuClient) SendText(ctx context.Context, chatID, text string) error {
	err := n.Base.SendText(ctx, chatID, text)
	if err != nil {
		n.NotifyPermissionIssue(NotifyTarget{ChatID: chatID}, err)
	}
	return err
}

func (n *NotifyingFeishuClient) ReplyCard(ctx context.Context, messageID string, card map[string]any, inThread bool) (string, error) {
	n.captureMu.Lock()
	if capture := n.commandCaptureForMessageLocked(messageID); capture != nil {
		capture.Card = feishu.CloneCapturedCard(card)
		capture.Text = ""
		n.captureMu.Unlock()
		return apputil.FirstNonEmpty(strings.TrimSpace(capture.ReplyMessageID), strings.TrimSpace(messageID)), nil
	}
	n.captureMu.Unlock()
	id, err := n.Base.ReplyCard(ctx, messageID, card, inThread)
	if err != nil {
		n.NotifyPermissionIssue(NotifyTarget{MessageID: messageID, InThread: inThread}, err)
	}
	return id, err
}

func (n *NotifyingFeishuClient) SendCard(ctx context.Context, chatID string, card map[string]any) (string, error) {
	id, err := n.Base.SendCard(ctx, chatID, card)
	if err != nil {
		n.NotifyPermissionIssue(NotifyTarget{ChatID: chatID}, err)
	}
	return id, err
}

func (n *NotifyingFeishuClient) PatchCard(ctx context.Context, messageID string, card map[string]any) error {
	n.captureMu.Lock()
	if capture := n.commandCaptureForMessageLocked(messageID); capture != nil {
		capture.Card = feishu.CloneCapturedCard(card)
		capture.Text = ""
		n.captureMu.Unlock()
		return nil
	}
	n.captureMu.Unlock()
	err := n.Base.PatchCard(ctx, messageID, card)
	if err != nil {
		n.NotifyPermissionIssue(NotifyTarget{MessageID: messageID}, err)
	}
	return err
}

func (n *NotifyingFeishuClient) ReplyLocalAttachment(ctx context.Context, messageID, path string, inThread bool) error {
	err := n.Base.ReplyLocalAttachment(ctx, messageID, path, inThread)
	if err != nil {
		n.NotifyPermissionIssue(NotifyTarget{MessageID: messageID, InThread: inThread}, err)
	}
	return err
}

func (n *NotifyingFeishuClient) ReplyLocalImage(ctx context.Context, messageID, path string, inThread bool) error {
	err := n.Base.ReplyLocalImage(ctx, messageID, path, inThread)
	if err != nil {
		n.NotifyPermissionIssue(NotifyTarget{MessageID: messageID, InThread: inThread}, err)
	}
	return err
}

func (n *NotifyingFeishuClient) ReplyLocalVideo(ctx context.Context, messageID, path string, inThread bool) error {
	err := n.Base.ReplyLocalVideo(ctx, messageID, path, inThread)
	if err != nil {
		n.NotifyPermissionIssue(NotifyTarget{MessageID: messageID, InThread: inThread}, err)
	}
	return err
}

func (n *NotifyingFeishuClient) DownloadMessageResource(ctx context.Context, messageID string, attachment feishu.Attachment, dir string) (string, string, error) {
	path, name, err := n.Base.DownloadMessageResource(ctx, messageID, attachment, dir)
	if err != nil {
		n.NotifyPermissionIssue(NotifyTarget{MessageID: messageID}, err)
	}
	return path, name, err
}

func (n *NotifyingFeishuClient) ResolveMergeForward(ctx context.Context, messageID string, messageIDs []string) (string, []feishu.Attachment, error) {
	text, attachments, err := n.Base.ResolveMergeForward(ctx, messageID, messageIDs)
	if err != nil {
		n.NotifyPermissionIssue(NotifyTarget{MessageID: messageID}, err)
	}
	return text, attachments, err
}

func (n *NotifyingFeishuClient) ShareLocalFile(ctx context.Context, req feishu.SharedFileRequest) (feishu.SharedFileResult, error) {
	result, err := n.Base.ShareLocalFile(ctx, req)
	if err != nil {
		n.NotifyPermissionIssue(NotifyTarget{ChatID: req.ChatID, UserID: req.UserID}, err)
	}
	return result, err
}

func (n *NotifyingFeishuClient) SimpleStatusCard(title, color, body string, buttons []feishu.Button) map[string]any {
	return n.Base.SimpleStatusCard(title, color, body, buttons)
}

func (n *NotifyingFeishuClient) UrgentApp(ctx context.Context, messageID, userID string) error {
	return n.Base.UrgentApp(ctx, messageID, userID)
}

func (n *NotifyingFeishuClient) LookupMessageSenderOpenID(ctx context.Context, messageID string) (string, error) {
	return n.Base.LookupMessageSenderOpenID(ctx, messageID)
}

func (n *NotifyingFeishuClient) NotifyPermissionIssue(target NotifyTarget, err error) {
	if n == nil || n.Base == nil {
		return
	}
	issue, ok := feishu.PermissionIssueFromError(err)
	if !ok || issue == nil {
		return
	}
	target.ChatID = strings.TrimSpace(target.ChatID)
	target.MessageID = strings.TrimSpace(target.MessageID)
	target.UserID = strings.TrimSpace(target.UserID)
	if target.ChatID == "" && target.MessageID == "" {
		return
	}
	key := n.PermissionIssueKey(target, issue)
	if !n.shouldSendPermissionIssue(key) {
		return
	}
	body := feishu.RenderPermissionIssueBody(issue)
	if body == "" {
		return
	}
	card := n.Base.SimpleStatusCard("飞书权限错误", "red", body, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var (
		sentMessageID string
		sendErr       error
	)
	switch {
	case target.MessageID != "":
		sentMessageID, sendErr = n.Base.ReplyCard(ctx, target.MessageID, card, target.InThread)
	case target.ChatID != "":
		sentMessageID, sendErr = n.Base.SendCard(ctx, target.ChatID, card)
	}
	if sendErr != nil {
		slog.Warn("send feishu permission diagnostic failed",
			"message_id", target.MessageID,
			"chat_id", target.ChatID,
			"error", sendErr,
		)
		return
	}
	if sentMessageID == "" {
		return
	}
	urgentUserID := n.resolveUrgentUserID(ctx, target)
	if urgentUserID == "" {
		return
	}
	if urgentErr := n.Base.UrgentApp(ctx, sentMessageID, urgentUserID); urgentErr != nil {
		slog.Warn("feishu permission urgent_app failed",
			"message_id", sentMessageID,
			"user_id", urgentUserID,
			"error", urgentErr,
		)
	}
}

func (n *NotifyingFeishuClient) resolveUrgentUserID(ctx context.Context, target NotifyTarget) string {
	if n == nil || n.Base == nil {
		return ""
	}
	if userID := strings.TrimSpace(target.UserID); userID != "" {
		return userID
	}
	messageID := strings.TrimSpace(target.MessageID)
	if messageID == "" {
		return ""
	}
	userID, err := n.Base.LookupMessageSenderOpenID(ctx, messageID)
	if err != nil {
		slog.Warn("lookup feishu permission urgent user failed",
			"message_id", messageID,
			"error", err,
		)
		return ""
	}
	return strings.TrimSpace(userID)
}

func (n *NotifyingFeishuClient) PermissionIssueKey(target NotifyTarget, issue *feishu.PermissionIssue) string {
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

func (n *NotifyingFeishuClient) shouldSendPermissionIssue(key string) bool {
	if strings.TrimSpace(key) == "" {
		return false
	}
	now := time.Now()
	cutoff := now.Add(-PermissionNotifyDedupWindow)
	n.mu.Lock()
	defer n.mu.Unlock()
	for existingKey, ts := range n.recent {
		if ts.Before(cutoff) {
			delete(n.recent, existingKey)
		}
	}
	if ts, ok := n.recent[key]; ok && now.Sub(ts) < PermissionNotifyDedupWindow {
		return false
	}
	n.recent[key] = now
	return true
}
