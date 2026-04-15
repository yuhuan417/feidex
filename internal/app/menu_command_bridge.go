package app

import (
	"context"
	"strings"
	"time"

	"feidex/internal/feishu"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

type commandCaptureClient struct {
	base           feishuClient
	replyMessageID string
	text           string
	card           map[string]any
}

func (c *commandCaptureClient) SetHandlers(onMessage func(*feishu.InboundMessage), onCardAction func(*feishu.CardAction) (*callback.CardActionTriggerResponse, error), onBotMenu func(*feishu.BotMenuClick), onRecall func(*feishu.MessageRecall), onReaction func(*feishu.MessageReaction)) {
	c.base.SetHandlers(onMessage, onCardAction, onBotMenu, onRecall, onReaction)
}

func (c *commandCaptureClient) Start(ctx context.Context) error {
	return c.base.Start(ctx)
}

func (c *commandCaptureClient) Stop() {
	c.base.Stop()
}

func (c *commandCaptureClient) ConfigureMarkdownPreview(statePath, processCWD string) {
	c.base.ConfigureMarkdownPreview(statePath, processCWD)
}

func (c *commandCaptureClient) RewriteMarkdownPreview(ctx context.Context, req feishu.MarkdownPreviewRequest) (string, error) {
	return c.base.RewriteMarkdownPreview(ctx, req)
}

func (c *commandCaptureClient) CleanupArtifactsBefore(ctx context.Context, cutoff time.Time) (feishu.PreviewDriveCleanupResult, error) {
	return c.base.CleanupArtifactsBefore(ctx, cutoff)
}

func (c *commandCaptureClient) AddReaction(ctx context.Context, messageID, emojiType string) error {
	return c.base.AddReaction(ctx, messageID, emojiType)
}

func (c *commandCaptureClient) RemoveReaction(ctx context.Context, messageID, emojiType string) error {
	return c.base.RemoveReaction(ctx, messageID, emojiType)
}

func (c *commandCaptureClient) ReplyText(_ context.Context, _ string, text string, _ bool) error {
	c.text = strings.TrimSpace(text)
	c.card = nil
	return nil
}

func (c *commandCaptureClient) ReplyTextWithID(_ context.Context, _ string, text string, _ bool) (string, error) {
	c.text = strings.TrimSpace(text)
	c.card = nil
	return c.replyMessageID, nil
}

func (c *commandCaptureClient) SendText(_ context.Context, _ string, text string) error {
	c.text = strings.TrimSpace(text)
	c.card = nil
	return nil
}

func (c *commandCaptureClient) ReplyCard(_ context.Context, _ string, card map[string]any, _ bool) (string, error) {
	c.card = card
	c.text = ""
	return c.replyMessageID, nil
}

func (c *commandCaptureClient) SendCard(_ context.Context, _ string, card map[string]any) (string, error) {
	c.card = card
	c.text = ""
	return c.replyMessageID, nil
}

func (c *commandCaptureClient) PatchCard(_ context.Context, _ string, card map[string]any) error {
	c.card = card
	c.text = ""
	return nil
}

func (c *commandCaptureClient) DownloadMessageResource(ctx context.Context, messageID string, attachment feishu.Attachment, targetDir string) (string, string, error) {
	return c.base.DownloadMessageResource(ctx, messageID, attachment, targetDir)
}

func (c *commandCaptureClient) ShareLocalFile(ctx context.Context, req feishu.SharedFileRequest) (feishu.SharedFileResult, error) {
	return c.base.ShareLocalFile(ctx, req)
}

func (c *commandCaptureClient) ResolveMergeForward(ctx context.Context, messageID string, messageIDs []string) (string, []feishu.Attachment, error) {
	return c.base.ResolveMergeForward(ctx, messageID, messageIDs)
}

func (c *commandCaptureClient) SimpleStatusCard(title, color, body string, buttons []feishu.Button) map[string]any {
	return c.base.SimpleStatusCard(title, color, body, buttons)
}

func parseSessionKeyMeta(sessionKey string) (chatType, chatID, rootMessageID, userID string) {
	parts := strings.Split(strings.TrimSpace(sessionKey), ":")
	if len(parts) < 4 || parts[0] != "feishu" {
		return "", "", "", ""
	}
	switch parts[1] {
	case "group":
		if len(parts) >= 5 && parts[3] == "root" {
			return "group", parts[2], parts[4], ""
		}
	case "p2p":
		if len(parts) >= 4 {
			return "p2p", parts[2], "", parts[3]
		}
	}
	return "", "", "", ""
}

func (a *App) commandMessageFromAction(action *feishu.CardAction, sessionKey, rawCommand string) *feishu.InboundMessage {
	msg := &feishu.InboundMessage{
		SessionKey: strings.TrimSpace(sessionKey),
		MessageID:  strings.TrimSpace(action.MessageID),
		ChatID:     strings.TrimSpace(action.ChatID),
		UserID:     strings.TrimSpace(action.UserID),
		Text:       strings.TrimSpace(rawCommand),
	}
	chatType, chatID, rootMessageID, sessionUserID := parseSessionKeyMeta(sessionKey)
	if msg.ChatType == "" {
		msg.ChatType = chatType
	}
	if msg.ChatID == "" {
		msg.ChatID = chatID
	}
	if msg.RootMessageID == "" {
		msg.RootMessageID = rootMessageID
	}
	if msg.UserID == "" {
		msg.UserID = sessionUserID
	}
	if sess := a.appState().session(sessionKey); sess != nil {
		msg.ChatID = firstNonEmpty(msg.ChatID, strings.TrimSpace(sess.ChatID))
		msg.ChatType = firstNonEmpty(msg.ChatType, strings.TrimSpace(sess.ChatType))
		msg.UserID = firstNonEmpty(msg.UserID, strings.TrimSpace(sess.OwnerUserID))
	}
	if msg.ChatType == "group" && strings.TrimSpace(msg.RootMessageID) == "" {
		msg.RootMessageID = firstNonEmpty(rootMessageID, msg.MessageID)
	}
	return msg
}

func (a *App) runCommandFromCardAction(action *feishu.CardAction, sessionKey, rawCommand string) (string, map[string]any, error) {
	if action == nil {
		return "", nil, nil
	}
	capture := &commandCaptureClient{
		base:           a.feishu,
		replyMessageID: strings.TrimSpace(action.MessageID),
	}
	shadow := *a
	shadow.feishu = capture
	msg := a.commandMessageFromAction(action, sessionKey, rawCommand)
	if err := shadow.handleCommand(msg, rawCommand); err != nil {
		return "", nil, err
	}
	return strings.TrimSpace(capture.text), capture.card, nil
}

func (a *App) completeMenuCommand(action *feishu.CardAction, sessionKey, rawCommand, parentAction string) (*callback.CardActionTriggerResponse, error) {
	parentAction = firstNonEmpty(actionStringValue(action, "parent_action"), strings.TrimSpace(parentAction))
	text, card, err := a.runCommandFromCardAction(action, sessionKey, rawCommand)
	if err != nil {
		resp := &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "warning", Content: err.Error()},
		}
		if fallback, ok := a.renderMenuCommandFallback(parentAction, sessionKey); ok {
			resp.Card = rawCard(fallback)
		}
		return resp, nil
	}
	if card != nil {
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "info", Content: "已执行 " + rawCommand},
			Card:  rawCard(card),
		}, nil
	}
	resp := &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: firstNonEmpty(text, "已执行 "+rawCommand)},
	}
	if fallback, ok := a.renderMenuCommandFallback(parentAction, sessionKey); ok {
		resp.Card = rawCard(fallback)
	}
	return resp, nil
}

func (a *App) renderMenuCommandFallback(actionName, sessionKey string) (map[string]any, bool) {
	if a == nil || a.cfg == nil || len(a.cfg.Workspaces) == 0 {
		return nil, false
	}
	return a.renderMenuNodeCard(actionName, sessionKey)
}
