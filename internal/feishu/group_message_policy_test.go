package feishu

import (
	"testing"

	"feidex/internal/config"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

func TestConvertMessageUsesGroupPolicyAndPreservesMentions(t *testing.T) {
	a := New(config.FeishuConfig{GroupAtOnly: true})
	a.botOpenID = "bot-self"
	var captured struct {
		chatID, rootID, parentID string
		text                     string
		mentions                 []string
		self, everyone           bool
	}
	a.SetGroupMessagePolicy(func(input GroupMessagePolicyInput) bool {
		captured.chatID = input.ChatID
		captured.rootID = input.RootMessageID
		captured.parentID = input.ParentMessageID
		captured.text = input.Text
		captured.mentions = append([]string(nil), input.MentionedOpenIDs...)
		captured.self = input.MentionedSelf
		captured.everyone = input.MentionedEveryone
		return true
	})

	messageID := "policy-msg"
	chatID := "chat-1"
	chatType := "group"
	messageType := "text"
	rootID := "root-1"
	parentID := "parent-1"
	userID := "user-1"
	otherBotID := "bot-other"
	otherKey := "@other"
	content := `{"text":"@other hello"}`
	msg := a.convertMessage(&larkim.P2MessageReceiveV1{
		Event: &larkim.P2MessageReceiveV1Data{
			Sender: &larkim.EventSender{SenderId: &larkim.UserId{OpenId: &userID}},
			Message: &larkim.EventMessage{
				MessageId:   &messageID,
				ChatId:      &chatID,
				ChatType:    &chatType,
				MessageType: &messageType,
				RootId:      &rootID,
				ParentId:    &parentID,
				Content:     &content,
				Mentions:    []*larkim.MentionEvent{{Key: &otherKey, Id: &larkim.UserId{OpenId: &otherBotID}}},
			},
		},
	})
	if msg == nil {
		t.Fatal("convertMessage() returned nil")
	}
	if captured.chatID != chatID || captured.rootID != rootID || captured.parentID != parentID || captured.text != "@other hello" || captured.self || captured.everyone || len(captured.mentions) != 1 || captured.mentions[0] != otherBotID {
		t.Fatalf("group policy args = %+v", captured)
	}
	if len(msg.MentionedOpenIDs) != 1 || msg.MentionedOpenIDs[0] != otherBotID || msg.MentionedSelf || msg.MentionedEveryone {
		t.Fatalf("inbound mention metadata = %+v", msg)
	}
}

func TestConvertMessageNormalizesDefaultedTopLevelRootForGroupPolicy(t *testing.T) {
	a := New(config.FeishuConfig{GroupAtOnly: true})
	a.botOpenID = "bot-self"
	var capturedRootID string
	a.SetGroupMessagePolicy(func(input GroupMessagePolicyInput) bool {
		capturedRootID = input.RootMessageID
		return input.ChatID == "chat-1" && input.RootMessageID == "" && input.ParentMessageID == "" && !input.MentionedSelf && len(input.MentionedOpenIDs) == 0 && !input.MentionedEveryone
	})

	messageID := "msg-top"
	chatID := "chat-1"
	chatType := "group"
	messageType := "text"
	userID := "user-1"
	content := `{"text":"/menu"}`
	msg := a.convertMessage(&larkim.P2MessageReceiveV1{
		Event: &larkim.P2MessageReceiveV1Data{
			Sender: &larkim.EventSender{SenderId: &larkim.UserId{OpenId: &userID}},
			Message: &larkim.EventMessage{
				MessageId:   &messageID,
				ChatId:      &chatID,
				ChatType:    &chatType,
				MessageType: &messageType,
				RootId:      &messageID,
				Content:     &content,
			},
		},
	})
	if msg == nil {
		t.Fatal("convertMessage() returned nil")
	}
	if capturedRootID != "" {
		t.Fatalf("group policy root id = %q, want normalized empty top-level root", capturedRootID)
	}
	if msg.RootMessageID != messageID || msg.Text != "/menu" {
		t.Fatalf("inbound message = %+v, want original root preserved and slash text", msg)
	}
}
