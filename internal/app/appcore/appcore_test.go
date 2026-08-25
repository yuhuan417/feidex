package appcore

import "testing"

func TestReplyInThreadEnabledUsesMainConversation(t *testing.T) {
	for _, chatType := range []string{"", "p2p", "group", " group "} {
		if ReplyInThreadEnabled(nil, chatType) {
			t.Fatalf("ReplyInThreadEnabled(nil, %q) = true, want false", chatType)
		}
	}
}
