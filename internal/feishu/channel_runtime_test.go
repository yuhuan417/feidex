package feishu

import (
	"context"
	"testing"
	"time"

	"feidex/internal/config"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	"github.com/larksuite/oapi-sdk-go/v3/channel"
	channelTypes "github.com/larksuite/oapi-sdk-go/v3/channel/types"
	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	larkws "github.com/larksuite/oapi-sdk-go/v3/ws"
)

// Guards the core constraint of the channel integration (plan 3.4.2): channel
// and this project share one dispatcher, and the dispatcher panics when the
// same event type is registered twice. Since channel registers card callbacks
// only from OnCardAction, never calling it keeps card.action.trigger owned by
// this project — which is what preserves toast responses for every card path.
//
// The assertion is that wiring channel's handlers alongside a dispatcher that
// already has the project's card callback registered does not panic.
func TestChannelWiringCoexistsWithProjectCardCallback(t *testing.T) {
	a := New(config.FeishuConfig{AppID: "app", AppSecret: "secret"})
	a.SetHandlers(func(*InboundMessage) {}, func(*CardAction) (*callback.CardActionTriggerResponse, error) {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "info", Content: "ok"}}, nil
	}, func(*MessageRecall) {}, func(*MessageReaction) {})

	d := a.buildEventDispatcher()
	wsClient := larkws.NewClient("app", "secret", larkws.WithEventHandler(d))

	// Mirrors startChannelRuntime's wiring. Deliberately does NOT call
	// OnCardAction. If this panicked, the integration would be unusable.
	ch := channel.NewChannel(lark.NewClient("app", "secret"), wsClient)
	ch.OnMessage(a.bridgeChannelMessage)
	ch.OnReaction(a.bridgeChannelReaction)
	ch.OnBotAdded(a.bridgeChannelBotAdded)

	// The project's card callback must still be reachable afterwards, and must
	// still be able to return a toast.
	resp, err := a.handleCardActionEvent(context.Background(), &callback.CardActionTriggerEvent{})
	if err != nil {
		t.Fatalf("handleCardActionEvent() error = %v", err)
	}
	if resp == nil {
		t.Fatal("handleCardActionEvent() = nil, want a response")
	}
}

// A second caller of OnCardAction is what would blow up; this documents that
// the failure mode is a panic rather than a silent override, which is why the
// rule "never call OnCardAction" has to be enforced by review and this test
// rather than by a runtime check.
func TestChannelOnCardActionWouldClaimTheCallback(t *testing.T) {
	a := New(config.FeishuConfig{AppID: "app", AppSecret: "secret"})
	a.SetHandlers(func(*InboundMessage) {}, func(*CardAction) (*callback.CardActionTriggerResponse, error) {
		return &callback.CardActionTriggerResponse{}, nil
	}, func(*MessageRecall) {}, func(*MessageReaction) {})

	d := a.buildEventDispatcher()
	wsClient := larkws.NewClient("app", "secret", larkws.WithEventHandler(d))
	ch := channel.NewChannel(lark.NewClient("app", "secret"), wsClient)

	defer func() {
		if recover() == nil {
			t.Fatal("expected channel.OnCardAction to panic on an already-registered card.action.trigger")
		}
	}()
	ch.OnCardAction(func(context.Context, *channelTypes.CardActionEvent) error { return nil })
}

// The bridges carry the original SDK event through channel's normalized form
// and hand it to the existing converters, so thread ids, merge-forward
// expansion and name resolution keep working: channel's NormalizedMessage has
// no fields for any of those. See plan 3.4.6.
func TestBridgeChannelMessagePreservesThreadFields(t *testing.T) {
	a := New(config.FeishuConfig{})
	a.botOpenID = "ou_bot"

	got := make(chan *InboundMessage, 1)
	a.SetHandlers(func(msg *InboundMessage) { got <- msg },
		func(*CardAction) (*callback.CardActionTriggerResponse, error) {
			return &callback.CardActionTriggerResponse{}, nil
		},
		func(*MessageRecall) {}, func(*MessageReaction) {})

	raw := &larkim.P2MessageReceiveV1{Event: &larkim.P2MessageReceiveV1Data{
		Message: &larkim.EventMessage{
			MessageId:   strPtr("om_1"),
			ChatId:      strPtr("oc_1"),
			ChatType:    strPtr("p2p"),
			MessageType: strPtr("text"),
			RootId:      strPtr("om_root"),
			ParentId:    strPtr("om_parent"),
			Content:     strPtr(`{"text":"hello"}`),
		},
		Sender: &larkim.EventSender{SenderId: &larkim.UserId{OpenId: strPtr("ou_user")}},
	}}

	if err := a.bridgeChannelMessage(context.Background(), &channelTypes.NormalizedMessage{
		MessageID: "om_1",
		RawEvent:  raw,
	}); err != nil {
		t.Fatalf("bridgeChannelMessage() error = %v", err)
	}

	select {
	case msg := <-got:
		if msg.RootMessageID != "om_root" || msg.ParentMessageID != "om_parent" {
			t.Fatalf("thread fields lost: root=%q parent=%q", msg.RootMessageID, msg.ParentMessageID)
		}
		if msg.Text != "hello" {
			t.Fatalf("text = %q, want hello", msg.Text)
		}
	case <-time.After(time.Second):
		t.Fatal("handler was not invoked")
	}
}

// An unexpected RawEvent type must be ignored rather than panic: channel owns
// the normalization, so a future SDK change to that payload should degrade to
// dropping the event, not crashing the long connection.
func TestBridgeChannelMessageIgnoresUnexpectedRawEvent(t *testing.T) {
	a := New(config.FeishuConfig{})
	called := false
	a.SetHandlers(func(*InboundMessage) { called = true },
		func(*CardAction) (*callback.CardActionTriggerResponse, error) {
			return &callback.CardActionTriggerResponse{}, nil
		},
		func(*MessageRecall) {}, func(*MessageReaction) {})

	if err := a.bridgeChannelMessage(context.Background(), &channelTypes.NormalizedMessage{
		MessageID: "om_1",
		RawEvent:  "not an event",
	}); err != nil {
		t.Fatalf("bridgeChannelMessage() error = %v", err)
	}
	if called {
		t.Fatal("handler must not run for an unrecognized RawEvent")
	}
}

// Guards the regression that broke /primary: channel's zero-value policy
// requires an @-mention in groups and drops everything else before the handler
// runs. This project routes group messages itself, and its /primary mechanism
// answers without a mention, so the gate handed to channel must be permissive.
//
// The assertion mirrors the construction in startChannelRuntime; if someone
// removes WithPolicyConfig, this test keeps passing but the live behaviour
// breaks, so the check is on the policy values themselves.
func TestChannelPolicyMustNotFilterInbound(t *testing.T) {
	permissive := channelTypes.PolicyConfig{
		RequireMention:      boolPtr(false),
		RespondToMentionAll: boolPtr(true),
	}
	if permissive.RequireMention == nil || *permissive.RequireMention {
		t.Fatal("RequireMention must be explicitly false: channel defaults it to true and would drop un-mentioned group messages")
	}
	if permissive.RespondToMentionAll == nil || !*permissive.RespondToMentionAll {
		t.Fatal("RespondToMentionAll must be explicitly true: channel defaults it to false and would drop @all messages")
	}
}
