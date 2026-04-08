package feishu

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"feidex/internal/config"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"
	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
	larkapplication "github.com/larksuite/oapi-sdk-go/v3/service/application/v6"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	larkws "github.com/larksuite/oapi-sdk-go/v3/ws"
)

type InboundMessage struct {
	MessageID       string
	ChatID          string
	ChatType        string
	UserID          string
	UserName        string
	ChatName        string
	Text            string
	RootMessageID   string
	ParentMessageID string
	ThreadID        string
	Attachments     []Attachment
	CreatedAt       int64
}

type MessageRecall struct {
	MessageID string
	ChatID    string
}

type MessageReaction struct {
	MessageID string
	ChatID    string
	UserID    string
	EmojiType string
}

type Attachment struct {
	Kind        string
	ResourceKey string
}

type CardAction struct {
	ActionValue map[string]any
	FormValue   map[string]any
	UserID      string
	ChatID      string
	MessageID   string
	Name        string
	InputValue  string
	Options     []string
	Checked     bool
}

type BotMenuClick struct {
	UserID   string
	UserName string
	Command  string
}

type Adapter struct {
	cfg        config.FeishuConfig
	client     *lark.Client
	wsClient   *larkws.Client
	botOpenID  string
	cancel     context.CancelFunc
	allowSet   map[string]struct{}
	allowAll   bool
	startOnce  sync.Once
	seenMu     sync.Mutex
	seen       map[string]time.Time
	reactionMu sync.Mutex
	reactions  map[string]string

	onMessage    func(*InboundMessage)
	onCardAction func(*CardAction) (*callback.CardActionTriggerResponse, error)
	onBotMenu    func(*BotMenuClick)
	onRecall     func(*MessageRecall)
	onReaction   func(*MessageReaction)

	previewMu         sync.Mutex
	previewStatePath  string
	previewProcessCWD string
	previewer         *DriveMarkdownPreviewer
}

func New(cfg config.FeishuConfig) *Adapter {
	allowSet := map[string]struct{}{}
	allowAll := len(cfg.AllowFrom) == 0
	for _, v := range cfg.AllowFrom {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if v == "*" {
			allowAll = true
		}
		allowSet[v] = struct{}{}
	}
	return &Adapter{
		cfg:       cfg,
		client:    lark.NewClient(cfg.AppID, cfg.AppSecret),
		allowSet:  allowSet,
		allowAll:  allowAll,
		seen:      map[string]time.Time{},
		reactions: map[string]string{},
	}
}

func (a *Adapter) SetHandlers(onMessage func(*InboundMessage), onCardAction func(*CardAction) (*callback.CardActionTriggerResponse, error), onBotMenu func(*BotMenuClick), onRecall func(*MessageRecall), onReaction func(*MessageReaction)) {
	a.onMessage = onMessage
	a.onCardAction = onCardAction
	a.onBotMenu = onBotMenu
	a.onRecall = onRecall
	a.onReaction = onReaction
}

func (a *Adapter) Start(ctx context.Context) error {
	var err error
	a.startOnce.Do(func() {
		a.botOpenID = a.fetchBotOpenID()
		dispatcher := dispatcher.NewEventDispatcher("", "").
			OnP2MessageReceiveV1(func(ctx context.Context, event *larkim.P2MessageReceiveV1) error {
				if a.onMessage != nil {
					if msg := a.convertMessage(event); msg != nil {
						go a.onMessage(msg)
					}
				}
				return nil
			}).
			OnP2MessageRecalledV1(func(ctx context.Context, event *larkim.P2MessageRecalledV1) error {
				if a.onRecall != nil {
					if recall := a.convertMessageRecall(event); recall != nil {
						go a.onRecall(recall)
					}
				}
				return nil
			}).
			OnP2MessageReactionCreatedV1(func(ctx context.Context, event *larkim.P2MessageReactionCreatedV1) error {
				if a.onReaction != nil {
					if reaction := a.convertMessageReaction(event); reaction != nil {
						go a.onReaction(reaction)
					}
				}
				return nil
			}).
			OnP2MessageReactionDeletedV1(func(ctx context.Context, event *larkim.P2MessageReactionDeletedV1) error {
				// We don't currently need user reaction-deleted semantics, but
				// registering a handler prevents the SDK from logging it as an
				// unhandled callback event.
				return nil
			}).
			OnP2CardActionTrigger(func(ctx context.Context, event *callback.CardActionTriggerEvent) (*callback.CardActionTriggerResponse, error) {
				if a.onCardAction == nil {
					return &callback.CardActionTriggerResponse{}, nil
				}
				cardAction := &CardAction{
					ActionValue: map[string]any{},
					FormValue:   map[string]any{},
				}
				if event.Event != nil {
					if event.Event.Action != nil {
						cardAction.ActionValue = event.Event.Action.Value
						cardAction.FormValue = event.Event.Action.FormValue
						cardAction.Name = event.Event.Action.Name
						cardAction.InputValue = event.Event.Action.InputValue
						cardAction.Options = append([]string(nil), event.Event.Action.Options...)
						cardAction.Checked = event.Event.Action.Checked
					}
					if event.Event.Operator != nil {
						cardAction.UserID = event.Event.Operator.OpenID
					}
					if event.Event.Context != nil {
						cardAction.ChatID = event.Event.Context.OpenChatID
						cardAction.MessageID = event.Event.Context.OpenMessageID
					}
				}
				slog.Debug("feishu card action",
					"name", cardAction.Name,
					"message_id", cardAction.MessageID,
					"chat_id", cardAction.ChatID,
					"user_id", cardAction.UserID,
					"action_value", fmt.Sprintf("%v", cardAction.ActionValue),
					"form_value", fmt.Sprintf("%v", cardAction.FormValue),
				)
				return a.onCardAction(cardAction)
			}).
			OnP2BotMenuV6(func(ctx context.Context, event *larkapplication.P2BotMenuV6) error {
				if a.onBotMenu == nil || event == nil || event.Event == nil || event.Event.EventKey == nil {
					return nil
				}
				userID := ""
				if event.Event.Operator != nil && event.Event.Operator.OperatorId != nil && event.Event.Operator.OperatorId.OpenId != nil {
					userID = *event.Event.Operator.OperatorId.OpenId
				}
				if !a.allowed(userID) {
					return nil
				}
				cmd := *event.Event.EventKey
				if !strings.HasPrefix(cmd, "/") {
					cmd = "/" + cmd
				}
				go a.onBotMenu(&BotMenuClick{UserID: userID, Command: cmd})
				return nil
			})
		a.wsClient = larkws.NewClient(a.cfg.AppID, a.cfg.AppSecret,
			larkws.WithEventHandler(dispatcher),
			larkws.WithLogLevel(larkcore.LogLevelInfo),
		)
		var runCtx context.Context
		runCtx, a.cancel = context.WithCancel(ctx)
		go func() {
			_ = a.wsClient.Start(runCtx)
		}()
	})
	return err
}

func (a *Adapter) Stop() {
	if a.cancel != nil {
		a.cancel()
	}
}

func (a *Adapter) ConfigureMarkdownPreview(statePath, processCWD string) {
	a.previewMu.Lock()
	defer a.previewMu.Unlock()
	a.previewStatePath = strings.TrimSpace(statePath)
	a.previewProcessCWD = strings.TrimSpace(processCWD)
	a.previewer = nil
}

func logWithLevel(level slog.Level, message string, attrs ...any) {
	switch {
	case level <= slog.LevelDebug:
		slog.Debug(message, attrs...)
	case level <= slog.LevelInfo:
		slog.Info(message, attrs...)
	case level <= slog.LevelWarn:
		slog.Warn(message, attrs...)
	default:
		slog.Error(message, attrs...)
	}
}

func logFeishuFailure(message string, err error, code int, apiMsg string, attrs ...any) {
	level := slog.LevelDebug
	if isFeishuAuthOrPermissionFailure(err, apiMsg) {
		level = slog.LevelWarn
	}
	if err != nil {
		attrs = append(attrs, "error", err)
	} else {
		attrs = append(attrs, "code", code, "msg", apiMsg)
	}
	logWithLevel(level, message, attrs...)
}

func isFeishuAuthOrPermissionFailure(err error, apiMsg string) bool {
	text := strings.ToLower(strings.TrimSpace(apiMsg))
	if err != nil {
		if text != "" {
			text += " "
		}
		text += strings.ToLower(err.Error())
	}
	if text == "" {
		return false
	}
	keywords := []string{
		"unauthorized",
		"forbidden",
		"permission",
		"no permission",
		"scope",
		"access denied",
		"auth",
		"token",
		"credential",
		"tenant_access_token",
		"app_access_token",
	}
	for _, keyword := range keywords {
		if strings.Contains(text, keyword) {
			return true
		}
	}
	return false
}

func (a *Adapter) AddReaction(ctx context.Context, messageID, emojiType string) error {
	messageID = strings.TrimSpace(messageID)
	emojiType = strings.TrimSpace(emojiType)
	if messageID == "" || emojiType == "" {
		return nil
	}
	key := reactionKey(messageID, emojiType)
	a.reactionMu.Lock()
	if a.reactions[key] != "" {
		a.reactionMu.Unlock()
		return nil
	}
	a.reactionMu.Unlock()
	resp, err := a.client.Im.MessageReaction.Create(ctx, larkim.NewCreateMessageReactionReqBuilder().
		MessageId(messageID).
		Body(larkim.NewCreateMessageReactionReqBodyBuilder().
			ReactionType(larkim.NewEmojiBuilder().EmojiType(emojiType).Build()).
			Build()).
		Build())
	if err != nil {
		logFeishuFailure("feishu reaction failed", err, 0, "", "op", "add", "message_id", messageID, "emoji_type", emojiType)
		return err
	}
	if !resp.Success() {
		logFeishuFailure("feishu reaction failed", nil, resp.Code, resp.Msg, "op", "add", "message_id", messageID, "emoji_type", emojiType)
		return fmt.Errorf("feishu add reaction failed code=%d msg=%s", resp.Code, resp.Msg)
	}
	reactionID := ""
	if resp.Data != nil && resp.Data.ReactionId != nil {
		reactionID = *resp.Data.ReactionId
	}
	a.reactionMu.Lock()
	a.reactions[key] = reactionID
	a.reactionMu.Unlock()
	return nil
}

func (a *Adapter) RemoveReaction(ctx context.Context, messageID, emojiType string) error {
	messageID = strings.TrimSpace(messageID)
	emojiType = strings.TrimSpace(emojiType)
	if messageID == "" || emojiType == "" {
		return nil
	}
	key := reactionKey(messageID, emojiType)
	a.reactionMu.Lock()
	reactionID := a.reactions[key]
	a.reactionMu.Unlock()
	if reactionID == "" {
		return nil
	}
	resp, err := a.client.Im.MessageReaction.Delete(ctx, larkim.NewDeleteMessageReactionReqBuilder().
		MessageId(messageID).
		ReactionId(reactionID).
		Build())
	if err != nil {
		logFeishuFailure("feishu reaction failed", err, 0, "", "op", "remove", "message_id", messageID, "emoji_type", emojiType)
		return err
	}
	if !resp.Success() {
		logFeishuFailure("feishu reaction failed", nil, resp.Code, resp.Msg, "op", "remove", "message_id", messageID, "emoji_type", emojiType)
		return fmt.Errorf("feishu remove reaction failed code=%d msg=%s", resp.Code, resp.Msg)
	}
	a.reactionMu.Lock()
	delete(a.reactions, key)
	a.reactionMu.Unlock()
	return nil
}

func (a *Adapter) ReplyText(ctx context.Context, messageID, text string, inThread bool) error {
	content, _ := json.Marshal(map[string]string{"text": text})
	slog.Debug("feishu outbound", "op", "reply", "msg_type", "text", "reply_to", messageID, "in_thread", inThread, "preview", truncateForLog(text, 160), "text_len", len(text))
	_, err := a.replyMessageDetailed(ctx, messageID, "text", string(content), inThread)
	if err != nil {
		logFeishuFailure("feishu outbound failed", err, 0, "", "op", "reply", "msg_type", "text", "reply_to", messageID)
	}
	return err
}

func (a *Adapter) ReplyTextWithID(ctx context.Context, messageID, text string, inThread bool) (string, error) {
	content, _ := json.Marshal(map[string]string{"text": text})
	slog.Debug("feishu outbound", "op", "reply", "msg_type", "text", "reply_to", messageID, "in_thread", inThread, "preview", truncateForLog(text, 160), "text_len", len(text))
	id, err := a.replyMessageDetailed(ctx, messageID, "text", string(content), inThread)
	if err != nil {
		logFeishuFailure("feishu outbound failed", err, 0, "", "op", "reply", "msg_type", "text", "reply_to", messageID)
		return "", err
	}
	slog.Debug("feishu outbound sent", "op", "reply", "msg_type", "text", "reply_to", messageID, "message_id", id)
	return id, nil
}

func (a *Adapter) SendText(ctx context.Context, chatID, text string) error {
	content, _ := json.Marshal(map[string]string{"text": text})
	slog.Debug("feishu outbound", "op", "send", "msg_type", "text", "chat_id", chatID, "preview", truncateForLog(text, 160), "text_len", len(text))
	req := larkim.NewCreateMessageReqBuilder().
		ReceiveIdType(larkim.ReceiveIdTypeChatId).
		Body(larkim.NewCreateMessageReqBodyBuilder().
			ReceiveId(chatID).
			MsgType("text").
			Content(string(content)).
			Build()).
		Build()
	resp, err := a.client.Im.Message.Create(ctx, req)
	if err != nil {
		logFeishuFailure("feishu outbound failed", err, 0, "", "op", "send", "msg_type", "text", "chat_id", chatID)
		return err
	}
	if !resp.Success() {
		logFeishuFailure("feishu outbound failed", nil, resp.Code, resp.Msg, "op", "send", "msg_type", "text", "chat_id", chatID)
		return fmt.Errorf("feishu send failed code=%d msg=%s", resp.Code, resp.Msg)
	}
	messageID := ""
	if resp.Data != nil && resp.Data.MessageId != nil {
		messageID = *resp.Data.MessageId
	}
	slog.Debug("feishu outbound sent", "op", "send", "msg_type", "text", "chat_id", chatID, "message_id", messageID)
	return nil
}

func (a *Adapter) ReplyLocalFile(ctx context.Context, messageID, path string, inThread bool) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("path %q is not a regular file", path)
	}
	if info.Size() <= 0 {
		return fmt.Errorf("path %q is empty", path)
	}
	if isSupportedReplyImage(path, info) {
		return a.replyLocalImage(ctx, messageID, path, inThread)
	}
	return a.replyLocalUploadedFile(ctx, messageID, path, info, inThread)
}

func (a *Adapter) ReplyCard(ctx context.Context, messageID string, card map[string]any, inThread bool) (string, error) {
	contentBytes, _ := json.Marshal(card)
	title, preview, buttonCount := summarizeCardForLog(card)
	slog.Debug("feishu outbound", "op", "reply", "msg_type", "interactive", "reply_to", messageID, "in_thread", inThread, "card_title", title, "card_preview", preview, "button_count", buttonCount, "card_size", len(contentBytes))
	req := larkim.NewReplyMessageReqBuilder().
		MessageId(messageID).
		Body(larkim.NewReplyMessageReqBodyBuilder().
			MsgType("interactive").
			Content(string(contentBytes)).
			ReplyInThread(inThread).
			Build()).
		Build()
	resp, err := a.client.Im.Message.Reply(ctx, req)
	if err != nil {
		logFeishuFailure("feishu outbound failed", err, 0, "", "op", "reply", "msg_type", "interactive", "reply_to", messageID)
		return "", err
	}
	if !resp.Success() {
		logFeishuFailure("feishu outbound failed", nil, resp.Code, resp.Msg, "op", "reply", "msg_type", "interactive", "reply_to", messageID, "card_title", title)
		return "", fmt.Errorf("feishu reply card failed code=%d msg=%s", resp.Code, resp.Msg)
	}
	if resp.Data == nil || resp.Data.MessageId == nil {
		slog.Debug("feishu outbound sent", "op", "reply", "msg_type", "interactive", "reply_to", messageID, "message_id", "")
		return "", nil
	}
	slog.Debug("feishu outbound sent", "op", "reply", "msg_type", "interactive", "reply_to", messageID, "message_id", *resp.Data.MessageId, "card_title", title)
	return *resp.Data.MessageId, nil
}

func (a *Adapter) SendCard(ctx context.Context, chatID string, card map[string]any) (string, error) {
	contentBytes, _ := json.Marshal(card)
	title, preview, buttonCount := summarizeCardForLog(card)
	slog.Debug("feishu outbound", "op", "send", "msg_type", "interactive", "chat_id", chatID, "card_title", title, "card_preview", preview, "button_count", buttonCount, "card_size", len(contentBytes))
	req := larkim.NewCreateMessageReqBuilder().
		ReceiveIdType(larkim.ReceiveIdTypeChatId).
		Body(larkim.NewCreateMessageReqBodyBuilder().
			ReceiveId(chatID).
			MsgType("interactive").
			Content(string(contentBytes)).
			Build()).
		Build()
	resp, err := a.client.Im.Message.Create(ctx, req)
	if err != nil {
		logFeishuFailure("feishu outbound failed", err, 0, "", "op", "send", "msg_type", "interactive", "chat_id", chatID)
		return "", err
	}
	if !resp.Success() {
		logFeishuFailure("feishu outbound failed", nil, resp.Code, resp.Msg, "op", "send", "msg_type", "interactive", "chat_id", chatID, "card_title", title)
		return "", fmt.Errorf("feishu send card failed code=%d msg=%s", resp.Code, resp.Msg)
	}
	if resp.Data == nil || resp.Data.MessageId == nil {
		slog.Debug("feishu outbound sent", "op", "send", "msg_type", "interactive", "chat_id", chatID, "message_id", "", "card_title", title)
		return "", nil
	}
	slog.Debug("feishu outbound sent", "op", "send", "msg_type", "interactive", "chat_id", chatID, "message_id", *resp.Data.MessageId, "card_title", title)
	return *resp.Data.MessageId, nil
}

func (a *Adapter) PatchCard(ctx context.Context, messageID string, card map[string]any) error {
	contentBytes, _ := json.Marshal(card)
	title, preview, buttonCount := summarizeCardForLog(card)
	slog.Debug("feishu outbound", "op", "patch", "msg_type", "interactive", "message_id", messageID, "card_title", title, "card_preview", preview, "button_count", buttonCount, "card_size", len(contentBytes))
	req := larkim.NewPatchMessageReqBuilder().
		MessageId(messageID).
		Body(larkim.NewPatchMessageReqBodyBuilder().
			Content(string(contentBytes)).
			Build()).
		Build()
	resp, err := a.client.Im.Message.Patch(ctx, req)
	if err != nil {
		logFeishuFailure("feishu outbound failed", err, 0, "", "op", "patch", "msg_type", "interactive", "message_id", messageID)
		return err
	}
	if !resp.Success() {
		logFeishuFailure("feishu outbound failed", nil, resp.Code, resp.Msg, "op", "patch", "msg_type", "interactive", "message_id", messageID, "card_title", title)
		return fmt.Errorf("feishu patch failed code=%d msg=%s", resp.Code, resp.Msg)
	}
	slog.Debug("feishu outbound sent", "op", "patch", "msg_type", "interactive", "message_id", messageID, "card_title", title)
	return nil
}

func (a *Adapter) replyLocalImage(ctx context.Context, messageID, path string, inThread bool) error {
	body, err := larkim.NewCreateImagePathReqBodyBuilder().
		ImageType("message").
		ImagePath(path).
		Build()
	if err != nil {
		return err
	}
	resp, err := a.client.Im.Image.Create(ctx, larkim.NewCreateImageReqBuilder().
		Body(body).
		Build())
	if err != nil {
		return err
	}
	if !resp.Success() || resp.Data == nil || resp.Data.ImageKey == nil || strings.TrimSpace(*resp.Data.ImageKey) == "" {
		return fmt.Errorf("feishu image upload failed code=%d msg=%s", resp.Code, resp.Msg)
	}
	content, _ := (&larkim.MessageImage{ImageKey: *resp.Data.ImageKey}).String()
	_, err = a.replyMessageDetailed(ctx, messageID, "image", content, inThread)
	return err
}

func (a *Adapter) replyLocalUploadedFile(ctx context.Context, messageID, path string, info fs.FileInfo, inThread bool) error {
	if info.Size() > 30*1024*1024 {
		return fmt.Errorf("path %q exceeds Feishu 30MB file upload limit", path)
	}
	body, err := larkim.NewCreateFilePathReqBodyBuilder().
		FileType(detectUploadFileType(path)).
		FileName(filepath.Base(path)).
		FilePath(path).
		Build()
	if err != nil {
		return err
	}
	resp, err := a.client.Im.File.Create(ctx, larkim.NewCreateFileReqBuilder().
		Body(body).
		Build())
	if err != nil {
		return err
	}
	if !resp.Success() || resp.Data == nil || resp.Data.FileKey == nil || strings.TrimSpace(*resp.Data.FileKey) == "" {
		return fmt.Errorf("feishu file upload failed code=%d msg=%s", resp.Code, resp.Msg)
	}
	content, _ := (&larkim.MessageFile{FileKey: *resp.Data.FileKey}).String()
	_, err = a.replyMessageDetailed(ctx, messageID, "file", content, inThread)
	return err
}

func (a *Adapter) replyMessageDetailed(ctx context.Context, messageID, msgType, content string, inThread bool) (string, error) {
	slog.Debug("feishu outbound raw", "op", "reply", "msg_type", msgType, "reply_to", messageID, "in_thread", inThread, "content_preview", truncateForLog(content, 200), "content_size", len(content))
	req := larkim.NewReplyMessageReqBuilder().
		MessageId(messageID).
		Body(larkim.NewReplyMessageReqBodyBuilder().
			MsgType(msgType).
			Content(content).
			ReplyInThread(inThread).
			Build()).
		Build()
	resp, err := a.client.Im.Message.Reply(ctx, req)
	if err != nil {
		logFeishuFailure("feishu outbound failed", err, 0, "", "op", "reply", "msg_type", msgType, "reply_to", messageID)
		return "", err
	}
	if !resp.Success() {
		logFeishuFailure("feishu outbound failed", nil, resp.Code, resp.Msg, "op", "reply", "msg_type", msgType, "reply_to", messageID)
		return "", fmt.Errorf("feishu reply failed code=%d msg=%s", resp.Code, resp.Msg)
	}
	if resp.Data == nil || resp.Data.MessageId == nil {
		slog.Debug("feishu outbound sent", "op", "reply", "msg_type", msgType, "reply_to", messageID, "message_id", "")
		return "", nil
	}
	slog.Debug("feishu outbound sent", "op", "reply", "msg_type", msgType, "reply_to", messageID, "message_id", *resp.Data.MessageId)
	return *resp.Data.MessageId, nil
}

func (a *Adapter) DownloadMessageResource(ctx context.Context, messageID string, attachment Attachment, dir string) (string, string, error) {
	messageID = strings.TrimSpace(messageID)
	if messageID == "" {
		return "", "", fmt.Errorf("missing message id for message resource download")
	}
	kind := strings.TrimSpace(attachment.Kind)
	if kind != "image" && kind != "file" && kind != "audio" {
		return "", "", fmt.Errorf("unsupported attachment kind %q", attachment.Kind)
	}
	resourceKey := strings.TrimSpace(attachment.ResourceKey)
	if resourceKey == "" {
		return "", "", fmt.Errorf("missing resource key for message resource download")
	}

	req := larkim.NewGetMessageResourceReqBuilder().
		MessageId(messageID).
		FileKey(resourceKey).
		Type(resourceTypeForAttachment(kind)).
		Build()
	resp, err := a.client.Im.MessageResource.Get(ctx, req)
	if err != nil {
		return "", "", err
	}
	if resp == nil || resp.File == nil {
		if resp != nil {
			return "", "", fmt.Errorf("feishu resource download failed code=%d msg=%s", resp.Code, resp.Msg)
		}
		return "", "", fmt.Errorf("feishu resource download returned empty response")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "", err
	}

	fileName := resolveDownloadedFileName(resp, attachment)
	path := uniqueDownloadPath(dir, fileName)
	f, err := os.Create(path)
	if err != nil {
		return "", "", err
	}
	defer f.Close()
	if _, err := io.Copy(f, resp.File); err != nil {
		return "", "", err
	}
	return path, filepath.Base(path), nil
}

func (a *Adapter) SimpleStatusCard(title, color, body string, buttons []Button) map[string]any {
	card := map[string]any{
		"config": map[string]any{
			"wide_screen_mode": true,
			"update_multi":     true,
		},
		"header": map[string]any{
			"title": map[string]any{
				"tag":     "plain_text",
				"content": title,
			},
			"template": color,
		},
		"elements": []map[string]any{
			{"tag": "markdown", "content": body},
		},
	}
	if len(buttons) > 0 {
		actions := make([]map[string]any, 0, len(buttons))
		for _, btn := range buttons {
			action := map[string]any{
				"tag":   "button",
				"type":  btn.Type,
				"text":  map[string]any{"tag": "plain_text", "content": btn.Text},
				"value": btn.Value,
			}
			if strings.TrimSpace(btn.Name) != "" {
				action["name"] = btn.Name
			}
			actions = append(actions, action)
		}
		card["elements"] = append(card["elements"].([]map[string]any), map[string]any{
			"tag":     "action",
			"actions": actions,
		})
	}
	return card
}

type Button struct {
	Text  string
	Type  string
	Name  string
	Value map[string]any
}

func (a *Adapter) convertMessage(event *larkim.P2MessageReceiveV1) *InboundMessage {
	if event == nil || event.Event == nil || event.Event.Message == nil || event.Event.Sender == nil {
		return nil
	}
	msg := event.Event.Message
	sender := event.Event.Sender
	if msg.MessageType == nil {
		return nil
	}
	messageType := strings.TrimSpace(*msg.MessageType)
	userID := ""
	if sender.SenderId != nil && sender.SenderId.OpenId != nil {
		userID = *sender.SenderId.OpenId
	}
	if !a.allowed(userID) {
		return nil
	}
	if msg.ChatType != nil && *msg.ChatType == "group" && a.cfg.GroupAtOnly && a.botOpenID != "" {
		if !mentioned(msg.Mentions, a.botOpenID) && !(a.cfg.RespondToAtEveryone && mentionedEveryone(msg.Mentions)) {
			return nil
		}
	}
	var (
		text        string
		attachments []Attachment
	)
	switch messageType {
	case "text":
		text = stripBotMention(extractText(msg.Content), msg.Mentions, a.botOpenID)
	case "image":
		attachment, ok := extractImageAttachment(msg.Content)
		if !ok {
			slog.Warn("feishu image message missing image key")
			return nil
		}
		attachments = append(attachments, attachment)
	case "file":
		attachment, ok := extractFileAttachment(msg.Content)
		if !ok {
			slog.Warn("feishu file message missing file key")
			return nil
		}
		attachments = append(attachments, attachment)
	case "audio":
		attachment, ok := extractAudioAttachment(msg.Content)
		if !ok {
			slog.Warn("feishu audio message missing file key")
			return nil
		}
		attachments = append(attachments, attachment)
	default:
		return nil
	}
	if strings.TrimSpace(text) == "" && len(attachments) == 0 {
		return nil
	}
	out := &InboundMessage{
		UserID:      userID,
		Text:        text,
		Attachments: attachments,
	}
	if msg.MessageId != nil {
		out.MessageID = *msg.MessageId
	}
	if out.MessageID != "" && a.duplicate(out.MessageID) {
		slog.Debug("feishu duplicate message ignored", "message_id", out.MessageID)
		return nil
	}
	if msg.ChatId != nil {
		out.ChatID = *msg.ChatId
	}
	if msg.ChatType != nil {
		out.ChatType = *msg.ChatType
	}
	if msg.RootId != nil {
		out.RootMessageID = *msg.RootId
	}
	if msg.ParentId != nil {
		out.ParentMessageID = *msg.ParentId
	}
	if out.RootMessageID == "" && out.MessageID != "" && out.ChatType == "group" {
		out.RootMessageID = out.MessageID
	}
	if msg.ThreadId != nil {
		out.ThreadID = *msg.ThreadId
	}
	if msg.CreateTime != nil {
		out.CreatedAt = parseUnixMillis(*msg.CreateTime)
	}
	return out
}

func (a *Adapter) convertMessageRecall(event *larkim.P2MessageRecalledV1) *MessageRecall {
	if event == nil || event.Event == nil || event.Event.MessageId == nil {
		return nil
	}
	messageID := strings.TrimSpace(*event.Event.MessageId)
	if messageID == "" {
		return nil
	}
	recall := &MessageRecall{MessageID: messageID}
	if event.Event.ChatId != nil {
		recall.ChatID = strings.TrimSpace(*event.Event.ChatId)
	}
	return recall
}

func (a *Adapter) convertMessageReaction(event *larkim.P2MessageReactionCreatedV1) *MessageReaction {
	if event == nil || event.Event == nil || event.Event.MessageId == nil || event.Event.ReactionType == nil {
		return nil
	}
	if event.Event.OperatorType != nil && strings.TrimSpace(*event.Event.OperatorType) != "" && !strings.EqualFold(strings.TrimSpace(*event.Event.OperatorType), "user") {
		return nil
	}
	messageID := strings.TrimSpace(*event.Event.MessageId)
	emojiType := ""
	if event.Event.ReactionType.EmojiType != nil {
		emojiType = strings.TrimSpace(*event.Event.ReactionType.EmojiType)
	}
	userID := parseReactionUserID(event.Event.UserId)
	if messageID == "" || emojiType == "" {
		return nil
	}
	reaction := &MessageReaction{
		MessageID: messageID,
		UserID:    userID,
		EmojiType: emojiType,
	}
	return reaction
}

func (a *Adapter) allowed(userID string) bool {
	if a.allowAll {
		return true
	}
	_, ok := a.allowSet[userID]
	return ok
}

func (a *Adapter) duplicate(messageID string) bool {
	a.seenMu.Lock()
	defer a.seenMu.Unlock()
	now := time.Now()
	for id, ts := range a.seen {
		if now.Sub(ts) > 10*time.Minute {
			delete(a.seen, id)
		}
	}
	if _, ok := a.seen[messageID]; ok {
		return true
	}
	a.seen[messageID] = now
	return false
}

func extractText(raw *string) string {
	if raw == nil {
		return ""
	}
	var body struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(*raw), &body); err != nil {
		return ""
	}
	return body.Text
}

func parseUnixMillis(raw string) int64 {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0
	}
	return v / 1000
}

func extractImageAttachment(raw *string) (Attachment, bool) {
	if raw == nil {
		return Attachment{}, false
	}
	var body larkim.MessageImage
	if err := json.Unmarshal([]byte(*raw), &body); err != nil {
		return Attachment{}, false
	}
	if strings.TrimSpace(body.ImageKey) == "" {
		return Attachment{}, false
	}
	return Attachment{Kind: "image", ResourceKey: strings.TrimSpace(body.ImageKey)}, true
}

func extractFileAttachment(raw *string) (Attachment, bool) {
	if raw == nil {
		return Attachment{}, false
	}
	var body larkim.MessageFile
	if err := json.Unmarshal([]byte(*raw), &body); err != nil {
		return Attachment{}, false
	}
	if strings.TrimSpace(body.FileKey) == "" {
		return Attachment{}, false
	}
	return Attachment{Kind: "file", ResourceKey: strings.TrimSpace(body.FileKey)}, true
}

func extractAudioAttachment(raw *string) (Attachment, bool) {
	if raw == nil {
		return Attachment{}, false
	}
	var body larkim.MessageAudio
	if err := json.Unmarshal([]byte(*raw), &body); err != nil {
		return Attachment{}, false
	}
	if strings.TrimSpace(body.FileKey) == "" {
		return Attachment{}, false
	}
	return Attachment{Kind: "audio", ResourceKey: strings.TrimSpace(body.FileKey)}, true
}

func stripBotMention(text string, mentions []*larkim.MentionEvent, botOpenID string) string {
	for _, mention := range mentions {
		if mention == nil || mention.Key == nil {
			continue
		}
		if mention.Id != nil && mention.Id.OpenId != nil && *mention.Id.OpenId == botOpenID {
			text = strings.ReplaceAll(text, *mention.Key, "")
		}
	}
	return strings.TrimSpace(text)
}

func mentioned(mentions []*larkim.MentionEvent, botOpenID string) bool {
	for _, mention := range mentions {
		if mention == nil || mention.Id == nil || mention.Id.OpenId == nil {
			continue
		}
		if *mention.Id.OpenId == botOpenID {
			return true
		}
	}
	return false
}

func mentionedEveryone(mentions []*larkim.MentionEvent) bool {
	for _, mention := range mentions {
		if mention == nil {
			continue
		}
		if mention.Key != nil {
			key := strings.ToLower(strings.TrimSpace(*mention.Key))
			if key == "@all" || strings.Contains(key, "所有人") || strings.Contains(key, "everyone") {
				return true
			}
		}
		if mention.Name != nil {
			name := strings.ToLower(strings.TrimSpace(*mention.Name))
			if name == "all" || strings.Contains(name, "所有人") || strings.Contains(name, "everyone") {
				return true
			}
		}
	}
	return false
}

func parseReactionUserID(value *larkim.UserId) string {
	if value == nil {
		return ""
	}
	switch {
	case value.OpenId != nil && strings.TrimSpace(*value.OpenId) != "":
		return strings.TrimSpace(*value.OpenId)
	case value.UserId != nil && strings.TrimSpace(*value.UserId) != "":
		return strings.TrimSpace(*value.UserId)
	case value.UnionId != nil && strings.TrimSpace(*value.UnionId) != "":
		return strings.TrimSpace(*value.UnionId)
	default:
		return ""
	}
}

func resolveDownloadedFileName(resp *larkim.GetMessageResourceResp, attachment Attachment) string {
	if resp != nil {
		if fileName := sanitizeDownloadedFileName(resp.FileName); fileName != "" {
			return fileName
		}
	}
	ext := defaultAttachmentExt(attachment.Kind)
	if resp != nil && resp.ApiResp != nil {
		if contentType := strings.TrimSpace(resp.Header.Get("Content-Type")); contentType != "" {
			if exts, err := mime.ExtensionsByType(contentType); err == nil && len(exts) > 0 {
				ext = exts[0]
			}
		}
	}
	return fmt.Sprintf("%s-%s%s", attachment.Kind, sanitizeAttachmentKey(attachment.ResourceKey), ext)
}

func truncateForLog(text string, limit int) string {
	text = strings.TrimSpace(text)
	if limit <= 0 || len(text) <= limit {
		return text
	}
	return text[:limit] + "..."
}

func summarizeCardForLog(card map[string]any) (title string, preview string, buttonCount int) {
	if card == nil {
		return "", "", 0
	}
	if header, ok := card["header"].(map[string]any); ok {
		if titleObj, ok := header["title"].(map[string]any); ok {
			if content, ok := titleObj["content"].(string); ok {
				title = strings.TrimSpace(content)
			}
		}
	}
	if elements, ok := card["elements"].([]map[string]any); ok {
		for _, elem := range elements {
			tag, _ := elem["tag"].(string)
			switch tag {
			case "markdown":
				if content, ok := elem["content"].(string); ok && strings.TrimSpace(preview) == "" {
					preview = truncateForLog(content, 160)
				}
			case "action":
				if actions, ok := elem["actions"].([]map[string]any); ok {
					buttonCount += len(actions)
				}
			}
		}
	}
	if body, ok := card["body"].(map[string]any); ok {
		if elements, ok := body["elements"].([]map[string]any); ok {
			for _, elem := range elements {
				tag, _ := elem["tag"].(string)
				switch tag {
				case "markdown":
					if content, ok := elem["content"].(string); ok && strings.TrimSpace(preview) == "" {
						preview = truncateForLog(content, 160)
					}
				case "action":
					if actions, ok := elem["actions"].([]map[string]any); ok {
						buttonCount += len(actions)
					}
				}
			}
		}
	}
	return strings.TrimSpace(title), strings.TrimSpace(preview), buttonCount
}

func sanitizeDownloadedFileName(name string) string {
	name = strings.TrimSpace(strings.ReplaceAll(name, "\x00", ""))
	if name == "" {
		return ""
	}
	name = filepath.Base(name)
	if name == "." || name == string(filepath.Separator) {
		return ""
	}
	return name
}

func uniqueDownloadPath(dir, fileName string) string {
	fileName = sanitizeDownloadedFileName(fileName)
	if fileName == "" {
		fileName = "attachment.bin"
	}
	path := filepath.Join(dir, fileName)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return path
	}
	ext := filepath.Ext(fileName)
	base := strings.TrimSuffix(fileName, ext)
	if base == "" {
		base = "attachment"
	}
	for i := 2; ; i++ {
		candidate := filepath.Join(dir, fmt.Sprintf("%s-%d%s", base, i, ext))
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate
		}
	}
}

func defaultAttachmentExt(kind string) string {
	if kind == "image" {
		return ".png"
	}
	if kind == "audio" {
		return ".opus"
	}
	return ".bin"
}

func resourceTypeForAttachment(kind string) string {
	if kind == "image" {
		return "image"
	}
	return "file"
}

func isSupportedReplyImage(path string, info os.FileInfo) bool {
	if info == nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > 10*1024*1024 {
		return false
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".jpg", ".jpeg", ".png", ".webp", ".gif", ".tiff", ".tif", ".bmp", ".ico":
		return true
	default:
		return false
	}
}

func detectUploadFileType(path string) string {
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")
	switch ext {
	case "opus":
		return "opus"
	case "mp4":
		return "mp4"
	case "pdf":
		return "pdf"
	case "doc", "docx":
		return "doc"
	case "xls", "xlsx":
		return "xls"
	case "ppt", "pptx":
		return "ppt"
	default:
		return "stream"
	}
}

func sanitizeAttachmentKey(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return "attachment"
	}
	var b strings.Builder
	b.Grow(len(key))
	for _, r := range key {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "attachment"
	}
	return b.String()
}

func reactionKey(messageID, emojiType string) string {
	return strings.TrimSpace(messageID) + ":" + strings.TrimSpace(emojiType)
}

func (a *Adapter) fetchBotOpenID() string {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	body, _ := json.Marshal(map[string]string{
		"app_id":     a.cfg.AppID,
		"app_secret": a.cfg.AppSecret,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://open.feishu.cn/open-apis/auth/v3/tenant_access_token/internal", strings.NewReader(string(body)))
	if err != nil {
		return ""
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	var tokenResp struct {
		Code              int    `json:"code"`
		TenantAccessToken string `json:"tenant_access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil || tokenResp.Code != 0 || tokenResp.TenantAccessToken == "" {
		return ""
	}
	infoReq, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://open.feishu.cn/open-apis/bot/v3/info", nil)
	if err != nil {
		return ""
	}
	infoReq.Header.Set("Authorization", "Bearer "+tokenResp.TenantAccessToken)
	infoResp, err := http.DefaultClient.Do(infoReq)
	if err != nil {
		return ""
	}
	defer infoResp.Body.Close()
	var info struct {
		Code int `json:"code"`
		Bot  struct {
			OpenID string `json:"open_id"`
		} `json:"bot"`
	}
	if err := json.NewDecoder(infoResp.Body).Decode(&info); err != nil || info.Code != 0 {
		return ""
	}
	return info.Bot.OpenID
}
