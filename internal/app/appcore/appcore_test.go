package appcore

import (
	"sync"
	"testing"

	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

func TestCanonicalSessionKeyUsesFrontendChatIdentity(t *testing.T) {
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
			want:       "feishu:frontend:frontend-a:chat:chat-1",
		},
		{
			name:       "frontend group root",
			frontendID: "frontend-b",
			input:      "feishu:frontend:frontend-a:group:chat-1:root:root-1",
			want:       "feishu:frontend:frontend-a:chat:chat-1",
		},
		{
			name:       "rootless group",
			frontendID: "frontend-a",
			input:      "feishu:group:chat-1",
			want:       "feishu:frontend:frontend-a:chat:chat-1",
		},
		{
			name:       "p2p drops user",
			frontendID: "frontend-a",
			input:      "feishu:p2p:chat-1:user-1",
			want:       "feishu:frontend:frontend-a:chat:chat-1",
		},
		{
			name:       "canonical frontend chat",
			frontendID: "frontend-b",
			input:      "feishu:frontend:frontend-a:chat:chat-1",
			want:       "feishu:frontend:frontend-a:chat:chat-1",
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

type workspaceSelectionTestApp struct {
	frontend string
	store    *state.Store
}

func (a workspaceSelectionTestApp) Config() *config.Config   { return config.Default() }
func (a workspaceSelectionTestApp) ConfigMu() *sync.RWMutex  { return &sync.RWMutex{} }
func (a workspaceSelectionTestApp) Backend() string          { return "codex" }
func (a workspaceSelectionTestApp) FrontendID() string       { return a.frontend }
func (a workspaceSelectionTestApp) FrontendConfigIndex() int { return -1 }
func (a workspaceSelectionTestApp) Store() *state.Store      { return a.store }

func TestGroupWorkspaceSelectionIsConversationScoped(t *testing.T) {
	store, err := state.Open(t.TempDir() + "/state.json")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	a := workspaceSelectionTestApp{frontend: "bot-a", store: store}
	if got := MakeWorkspaceSelectionKey(a, "group", "chat-1", "user-a"); got != MakeWorkspaceSelectionKey(a, "group", "chat-1", "user-b") {
		t.Fatalf("group selection keys differ by user")
	}
	if err := SetWorkspaceSelection(a, "group", "chat-1", "user-a", "ws-a"); err != nil {
		t.Fatalf("SetWorkspaceSelection() error = %v", err)
	}
	msg := &feishu.InboundMessage{ChatType: "group", ChatID: "chat-1", UserID: "user-b"}
	if got := ResolveWorkspaceSelectionForMessage(a, msg, nil); got != "ws-a" {
		t.Fatalf("ResolveWorkspaceSelectionForMessage() = %q, want ws-a", got)
	}
}
