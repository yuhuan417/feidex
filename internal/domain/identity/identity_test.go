package identity

import "testing"

func TestChatRefNormalizeAndValid(t *testing.T) {
	chat := (ChatRef{Type: " GROUP ", ID: " chat-1 "}).Normalize()
	if chat.Type != ChatTypeGroup || chat.ID != "chat-1" || !chat.Valid() {
		t.Fatalf("normalized chat = %#v, want valid group chat", chat)
	}
}

func TestChatRefRejectsUnknownType(t *testing.T) {
	if (ChatRef{Type: "channel", ID: "chat-1"}).Valid() {
		t.Fatal("unknown chat type should be invalid")
	}
}
