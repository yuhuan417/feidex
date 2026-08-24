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
		self, any, everyone      bool
	}
	a.SetGroupMessagePolicy(func(chatID, rootID, parentID string, self, any, everyone bool) bool {
		captured.chatID = chatID
		captured.rootID = rootID
		captured.parentID = parentID
		captured.self = self
		captured.any = any
		captured.everyone = everyone
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
	if captured.chatID != chatID || captured.rootID != rootID || captured.parentID != parentID || captured.self || !captured.any || captured.everyone {
		t.Fatalf("group policy args = %+v", captured)
	}
	if len(msg.MentionedOpenIDs) != 1 || msg.MentionedOpenIDs[0] != otherBotID || msg.MentionedSelf || msg.MentionedEveryone {
		t.Fatalf("inbound mention metadata = %+v", msg)
	}
}
