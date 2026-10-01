package feishu

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	larkws "github.com/larksuite/oapi-sdk-go/v3/ws"
	"github.com/larksuite/oapi-sdk-go/v3/channel"
	channeltypes "github.com/larksuite/oapi-sdk-go/v3/channel/types"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

// wsWriteTimeout pins the outbound write timeout. The SDK defaults to 10s;
// this project has always used 15s, so it is set explicitly rather than
// letting the value drift silently.
const wsWriteTimeout = 15 * time.Second

// validateWSStartup checks credentials and long-connection availability before
// the adapter reports itself started.
//
// It deliberately performs only the endpoint request, not a dial: a failed
// endpoint request means the app id/secret are wrong or the app has no
// long-connection mode enabled, which is a configuration error worth failing
// startup for (service.go stops every app when a frontend fails to start).
// A failed dial is a transient network condition that the SDK reconnects
// through on its own, so it must not take the whole daemon down.
func (a *Adapter) validateWSStartup(ctx context.Context) error {
	endpointResp, err := a.fetchWSEndpoint(ctx)
	if err != nil {
		return err
	}
	logServerWSConfig(endpointResp)
	return nil
}

// logServerWSConfig records the WebSocket tuning the server hands out.
//
// This is a canary. The SDK applies these values without guarding against
// zero (core/client.go applyConfigLocked assigns unconditionally), so a server
// response that omits a field would overwrite the SDK's sane defaults with 0 —
// pingInterval 0 yields a 5s read deadline and constant reconnect churn. The
// previous hand-rolled client guarded each field; we cannot intercept the SDK's
// copy, but the preflight sees the same payload, so logging it makes that
// failure mode visible at startup instead of only in the reconnect logs.
func logServerWSConfig(resp *larkws.EndpointResp) {
	if resp == nil || resp.Data == nil || resp.Data.ClientConfig == nil {
		slog.Warn("feishu websocket endpoint returned no client config; SDK defaults apply")
		return
	}
	conf := resp.Data.ClientConfig
	slog.Info("feishu websocket client config from server",
		"ping_interval_sec", conf.PingInterval,
		"reconnect_interval_sec", conf.ReconnectInterval,
		"reconnect_count", conf.ReconnectCount,
		"reconnect_nonce", conf.ReconnectNonce,
	)
	if conf.PingInterval <= 0 || conf.ReconnectInterval <= 0 {
		slog.Warn("feishu websocket client config has zero values; SDK will overwrite its defaults with zero",
			"ping_interval_sec", conf.PingInterval,
			"reconnect_interval_sec", conf.ReconnectInterval,
		)
	}
}

// fetchWSEndpoint requests the long-connection endpoint and its client config.
func (a *Adapter) fetchWSEndpoint(ctx context.Context) (*larkws.EndpointResp, error) {
	body, err := json.Marshal(map[string]string{
		"AppID":     a.cfg.AppID,
		"AppSecret": a.cfg.AppSecret,
	})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.cfg.OpenBaseURL()+larkws.GenEndpointUri, bytes.NewBuffer(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("locale", "zh")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("feishu websocket endpoint request failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	var endpointResp larkws.EndpointResp
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&endpointResp); err != nil {
		return nil, err
	}
	switch endpointResp.Code {
	case larkws.OK:
	case larkws.SystemBusy:
		return nil, fmt.Errorf("feishu websocket endpoint busy")
	case larkws.InternalError:
		return nil, fmt.Errorf("feishu websocket endpoint error: %s", strings.TrimSpace(endpointResp.Msg))
	default:
		return nil, fmt.Errorf("feishu websocket auth failed: %s", strings.TrimSpace(endpointResp.Msg))
	}
	if endpointResp.Data == nil || strings.TrimSpace(endpointResp.Data.Url) == "" {
		return nil, fmt.Errorf("feishu websocket endpoint returned empty URL")
	}
	return &endpointResp, nil
}

// startChannelRuntime builds the SDK WebSocket client and the channel that
// receives events over it, wires the handlers this project owns, and starts
// the connection in the background.
//
// Ownership split (see docs/oapi-sdk-v3.12.0-upgrade-plan.md 3.4):
//   - channel owns messages, reactions and bot-added events;
//   - this project owns card callbacks (channel's OnCardAction cannot return a
//     callback response, and 98% of card paths return a toast through it) and
//     message recall (channel does not handle it).
//
// OnCardAction must never be called: it registers card.action.trigger on the
// shared dispatcher, and the dispatcher panics on a duplicate registration.
func (a *Adapter) startChannelRuntime(ctx context.Context) {
	dispatcher := a.buildEventDispatcher()

	wsClient := larkws.NewClient(a.cfg.AppID, a.cfg.AppSecret,
		larkws.WithEventHandler(dispatcher),
		larkws.WithDomain(a.cfg.OpenBaseURL()),
		larkws.WithWriteTimeout(wsWriteTimeout),
	)

	// The policy gate must be fully permissive. channel's zero-value policy
	// requires an @-mention in groups and drops everything else before the
	// handler runs, which would silently disable this project's own group
	// routing — including the /primary mechanism, whose entire point is that
	// the designated bot answers WITHOUT being mentioned. Routing decisions
	// belong to the project (groupMessagePolicy); channel only does intake.
	ch := channel.NewChannel(a.currentClient(), wsClient,
		channeltypes.WithPolicyConfig(channeltypes.PolicyConfig{
			RequireMention:      boolPtr(false),
			RespondToMentionAll: boolPtr(true),
		}),
	)
	ch.OnMessage(a.bridgeChannelMessage)
	ch.OnReaction(a.bridgeChannelReaction)
	ch.OnBotAdded(a.bridgeChannelBotAdded)
	// Without this, a message dropped by the gate leaves no trace anywhere:
	// the handler never runs and nothing is logged.
	ch.OnReject(func(_ context.Context, event *channeltypes.RejectEvent) error {
		if event == nil {
			return nil
		}
		slog.Info("feishu inbound message rejected by channel policy",
			"reason", string(event.Reason),
			"message_id", event.MessageID,
			"chat_id", event.ChatID,
			"sender_id", event.SenderID,
		)
		return nil
	})

	// Lifecycle must be observed through channel, never through
	// wsClient.SetOn*: channel.Start overwrites all five of those callbacks.
	ch.OnError(func(err error) {
		slog.Warn("feishu websocket error", "error", err)
	})
	ch.OnReconnecting(func() {
		slog.Warn("feishu websocket reconnecting")
	})
	ch.OnReconnected(func() {
		slog.Info("feishu websocket reconnected")
	})
	ch.OnDisconnected(func() {
		slog.Warn("feishu websocket disconnected")
	})

	a.channelMu.Lock()
	a.feishuChannel = ch
	a.wsClient = wsClient
	a.channelMu.Unlock()

	// Start blocks until the connection run ends, so it must not run on the
	// caller's goroutine.
	go func() {
		if err := ch.Start(ctx); err != nil && ctx.Err() == nil {
			slog.Warn("feishu websocket run ended", "error", err)
		}
	}()
}

func boolPtr(v bool) *bool { return &v }

// stopChannelRuntime closes the long connection. channel.Stop ignores its
// context and simply closes the underlying WebSocket client, which is a
// terminal operation for that client instance.
func (a *Adapter) stopChannelRuntime() {
	a.channelMu.Lock()
	wsClient := a.wsClient
	ch := a.feishuChannel
	a.channelMu.Unlock()
	if ch != nil {
		_ = ch.Stop(context.Background())
		return
	}
	if wsClient != nil {
		wsClient.Close()
	}
}

// bridgeChannelMessage adapts a channel-normalized message back to this
// project's InboundMessage.
//
// channel replaces the event intake pipeline, not the message model: the
// normalized form carries no thread ids, no merge-forward expansion and no
// sender/chat names. The original event is preserved in RawEvent, so the
// existing converter stays the single place that builds an InboundMessage.
func (a *Adapter) bridgeChannelMessage(_ context.Context, msg *channeltypes.NormalizedMessage) error {
	if a.onMessage == nil || msg == nil {
		return nil
	}
	raw, ok := msg.RawEvent.(*larkim.P2MessageReceiveV1)
	if !ok || raw == nil {
		return nil
	}
	if converted := a.convertMessage(raw); converted != nil {
		go a.onMessage(converted)
	}
	return nil
}

// bridgeChannelReaction adapts a channel reaction event back to this project's
// MessageReaction via the original event.
func (a *Adapter) bridgeChannelReaction(_ context.Context, event *channeltypes.ReactionEvent) error {
	if a.onReaction == nil || event == nil {
		return nil
	}
	raw, ok := event.RawEvent.(*larkim.P2MessageReactionCreatedV1)
	if !ok || raw == nil {
		return nil
	}
	if converted := a.convertMessageReaction(raw); converted != nil {
		go a.onReaction(converted)
	}
	return nil
}

// bridgeChannelBotAdded adapts a channel bot-added event back to this project's
// BotGroupEvent via the original event.
func (a *Adapter) bridgeChannelBotAdded(_ context.Context, event *channeltypes.BotAddedEvent) error {
	if a.onBotAdded == nil || event == nil {
		return nil
	}
	raw, ok := event.RawEvent.(*larkim.P2ChatMemberBotAddedV1)
	if !ok || raw == nil {
		return nil
	}
	if raw.Event == nil || raw.Event.ChatId == nil {
		return nil
	}
	chatName := ""
	if raw.Event.Name != nil {
		chatName = *raw.Event.Name
	}
	go a.onBotAdded(&BotGroupEvent{ChatID: *raw.Event.ChatId, ChatName: chatName})
	return nil
}
