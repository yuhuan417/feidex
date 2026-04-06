package feishu

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
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
	MessageID     string
	ChatID        string
	ChatType      string
	UserID        string
	UserName      string
	ChatName      string
	Text          string
	RootMessageID string
	ThreadID      string
}

type CardAction struct {
	ActionValue map[string]any
	UserID      string
	ChatID      string
	MessageID   string
}

type BotMenuClick struct {
	UserID   string
	UserName string
	Command  string
}

type Adapter struct {
	cfg       config.FeishuConfig
	client    *lark.Client
	wsClient  *larkws.Client
	botOpenID string
	cancel    context.CancelFunc
	allowSet  map[string]struct{}
	allowAll  bool
	startOnce sync.Once
	seenMu    sync.Mutex
	seen      map[string]time.Time

	onMessage    func(*InboundMessage)
	onCardAction func(*CardAction) (*callback.CardActionTriggerResponse, error)
	onBotMenu    func(*BotMenuClick)
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
		cfg:      cfg,
		client:   lark.NewClient(cfg.AppID, cfg.AppSecret),
		allowSet: allowSet,
		allowAll: allowAll,
		seen:     map[string]time.Time{},
	}
}

func (a *Adapter) SetHandlers(onMessage func(*InboundMessage), onCardAction func(*CardAction) (*callback.CardActionTriggerResponse, error), onBotMenu func(*BotMenuClick)) {
	a.onMessage = onMessage
	a.onCardAction = onCardAction
	a.onBotMenu = onBotMenu
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
			OnP2CardActionTrigger(func(ctx context.Context, event *callback.CardActionTriggerEvent) (*callback.CardActionTriggerResponse, error) {
				if a.onCardAction == nil {
					return &callback.CardActionTriggerResponse{}, nil
				}
				cardAction := &CardAction{
					ActionValue: map[string]any{},
				}
				if event.Event != nil {
					cardAction.ActionValue = event.Event.Action.Value
					if event.Event.Operator != nil {
						cardAction.UserID = event.Event.Operator.OpenID
					}
					if event.Event.Context != nil {
						cardAction.ChatID = event.Event.Context.OpenChatID
						cardAction.MessageID = event.Event.Context.OpenMessageID
					}
				}
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

func (a *Adapter) ReplyText(ctx context.Context, messageID, text string, inThread bool) error {
	content, _ := json.Marshal(map[string]string{"text": text})
	req := larkim.NewReplyMessageReqBuilder().
		MessageId(messageID).
		Body(larkim.NewReplyMessageReqBodyBuilder().
			MsgType("text").
			Content(string(content)).
			ReplyInThread(inThread).
			Build()).
		Build()
	resp, err := a.client.Im.Message.Reply(ctx, req)
	if err != nil {
		return err
	}
	if !resp.Success() {
		return fmt.Errorf("feishu reply failed code=%d msg=%s", resp.Code, resp.Msg)
	}
	return nil
}

func (a *Adapter) SendText(ctx context.Context, chatID, text string) error {
	content, _ := json.Marshal(map[string]string{"text": text})
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
		return err
	}
	if !resp.Success() {
		return fmt.Errorf("feishu send failed code=%d msg=%s", resp.Code, resp.Msg)
	}
	return nil
}

func (a *Adapter) ReplyCard(ctx context.Context, messageID string, card map[string]any, inThread bool) (string, error) {
	contentBytes, _ := json.Marshal(card)
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
		return "", err
	}
	if !resp.Success() {
		return "", fmt.Errorf("feishu reply card failed code=%d msg=%s", resp.Code, resp.Msg)
	}
	if resp.Data == nil || resp.Data.MessageId == nil {
		return "", nil
	}
	return *resp.Data.MessageId, nil
}

func (a *Adapter) SendCard(ctx context.Context, chatID string, card map[string]any) (string, error) {
	contentBytes, _ := json.Marshal(card)
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
		return "", err
	}
	if !resp.Success() {
		return "", fmt.Errorf("feishu send card failed code=%d msg=%s", resp.Code, resp.Msg)
	}
	if resp.Data == nil || resp.Data.MessageId == nil {
		return "", nil
	}
	return *resp.Data.MessageId, nil
}

func (a *Adapter) PatchCard(ctx context.Context, messageID string, card map[string]any) error {
	contentBytes, _ := json.Marshal(card)
	req := larkim.NewPatchMessageReqBuilder().
		MessageId(messageID).
		Body(larkim.NewPatchMessageReqBodyBuilder().
			Content(string(contentBytes)).
			Build()).
		Build()
	resp, err := a.client.Im.Message.Patch(ctx, req)
	if err != nil {
		return err
	}
	if !resp.Success() {
		return fmt.Errorf("feishu patch failed code=%d msg=%s", resp.Code, resp.Msg)
	}
	return nil
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
			actions = append(actions, map[string]any{
				"tag":   "button",
				"type":  btn.Type,
				"text":  map[string]any{"tag": "plain_text", "content": btn.Text},
				"value": btn.Value,
			})
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
	Value map[string]any
}

func (a *Adapter) convertMessage(event *larkim.P2MessageReceiveV1) *InboundMessage {
	if event == nil || event.Event == nil || event.Event.Message == nil || event.Event.Sender == nil {
		return nil
	}
	msg := event.Event.Message
	sender := event.Event.Sender
	if msg.MessageType == nil || *msg.MessageType != "text" {
		return nil
	}
	userID := ""
	if sender.SenderId != nil && sender.SenderId.OpenId != nil {
		userID = *sender.SenderId.OpenId
	}
	if !a.allowed(userID) {
		return nil
	}
	if msg.ChatType != nil && *msg.ChatType == "group" && a.cfg.GroupAtOnly && a.botOpenID != "" {
		if !mentioned(msg.Mentions, a.botOpenID) {
			return nil
		}
	}
	text := extractText(msg.Content)
	text = stripBotMention(text, msg.Mentions, a.botOpenID)
	if strings.TrimSpace(text) == "" {
		return nil
	}
	out := &InboundMessage{
		UserID: userID,
		Text:   text,
	}
	if msg.MessageId != nil {
		out.MessageID = *msg.MessageId
	}
	if out.MessageID != "" && a.duplicate(out.MessageID) {
		slog.Info("feishu duplicate message ignored", "message_id", out.MessageID)
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
	if out.RootMessageID == "" && out.MessageID != "" && out.ChatType == "group" {
		out.RootMessageID = out.MessageID
	}
	if msg.ThreadId != nil {
		out.ThreadID = *msg.ThreadId
	}
	return out
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
