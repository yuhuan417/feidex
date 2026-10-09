package backend

import (
	"testing"

	"feidex/internal/config"
	"feidex/internal/domain/conversation"
)

// TestEffectiveClaudePermissionModeWalksTheChain pins what "default" means at
// each layer: follow the layer above. Only the global setting is a posture, so
// a workspace keeps tracking it and a session keeps tracking its workspace
// until someone pins an explicit mode.
func TestEffectiveClaudePermissionModeWalksTheChain(t *testing.T) {
	for _, tt := range []struct {
		name    string
		session string
		ws      string
		global  string
		want    string
	}{
		{"everything defers", "", "", "bypassPermissions", "bypassPermissions"},
		{"unset global is unattended", "", "", "", "bypassPermissions"},
		{"legacy default global is unattended", "", "", "default", "bypassPermissions"},
		{"workspace follows global", "", "default", "acceptEdits", "acceptEdits"},
		{"session follows workspace", "default", "plan", "bypassPermissions", "plan"},
		{"session follows global when workspace defers", "default", "", "acceptEdits", "acceptEdits"},
		{"workspace pins over global", "", "acceptEdits", "bypassPermissions", "acceptEdits"},
		{"session pins over workspace", "plan", "acceptEdits", "bypassPermissions", "plan"},
		{"empty session follows a pinned workspace", "", "plan", "bypassPermissions", "plan"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			sess := &conversation.Session{ActiveClaudePermissionMode: tt.session}
			ws := &config.Workspace{ID: "w", ClaudePermissionMode: tt.ws}
			got := effectiveClaudePermissionMode(sess, ws, config.ClaudeConfig{PermissionMode: tt.global})
			if got != tt.want {
				t.Fatalf("effectiveClaudePermissionMode(session=%q, workspace=%q, global=%q) = %q, want %q",
					tt.session, tt.ws, tt.global, got, tt.want)
			}
		})
	}
}
