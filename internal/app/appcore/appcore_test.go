package appcore

import "testing"

func TestCanonicalSessionKeyDropsGroupRoot(t *testing.T) {
	tests := []struct {
		name       string
		frontendID string
		input      string
		want       string
	}{
		{
			name:       "legacy group root without frontend",
			frontendID: "frontend-a",
			input:      "feishu:group:chat-1:root:root-1",
			want:       "feishu:frontend:frontend-a:group:chat-1",
		},
		{
			name:       "frontend group root",
			frontendID: "frontend-b",
			input:      "feishu:frontend:frontend-a:group:chat-1:root:root-1",
			want:       "feishu:frontend:frontend-a:group:chat-1",
		},
		{
			name:       "rootless group",
			frontendID: "frontend-a",
			input:      "feishu:group:chat-1",
			want:       "feishu:frontend:frontend-a:group:chat-1",
		},
		{
			name:       "p2p preserves user",
			frontendID: "frontend-a",
			input:      "feishu:p2p:chat-1:user-1",
			want:       "feishu:frontend:frontend-a:p2p:chat-1:user-1",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CanonicalSessionKey(tt.frontendID, tt.input); got != tt.want {
				t.Fatalf("CanonicalSessionKey() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestReplyInThreadEnabledUsesMainConversation(t *testing.T) {
	for _, chatType := range []string{"", "p2p", "group", " group "} {
		if ReplyInThreadEnabled(nil, chatType) {
			t.Fatalf("ReplyInThreadEnabled(nil, %q) = true, want false", chatType)
		}
	}
}
