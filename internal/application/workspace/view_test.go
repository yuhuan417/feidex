package workspace

import (
	"reflect"
	"testing"

	"feidex/internal/domain/conversation"
	"feidex/internal/domain/identity"
	"feidex/internal/domain/routing"
	domain "feidex/internal/domain/workspace"
)

type viewSourceFixture struct{ source ViewSource }

func (r viewSourceFixture) WorkspaceViewSource(identity.FrontendID, string) ViewSource {
	return r.source
}

func TestWorkspaceViewKeepsGroupBindingSeparateFromPrivateDefaults(t *testing.T) {
	source := ViewSource{Backend: "codex", Group: true,
		Workspaces: []domain.Workspace{{ID: "machine", Cwd: "/machine/repo"}, {ID: "group", Cwd: "/group/repo"}},
		Session:    &conversation.Session{ChatType: "group", WorkspaceID: "machine", ActiveTurnID: "turn-running", Queue: []string{"queued"}},
		Selection:  &conversation.Session{WorkspaceID: "machine"}, Profile: &routing.BotProfile{WorkspaceID: "machine"},
		Binding: &routing.AgentBinding{WorkspaceID: "group", PendingMessages: []*routing.AgentBindingPendingMessage{{Text: "next"}, {Text: "later"}}},
	}
	before := conversation.CloneSession(source.Session)
	view := (ViewService{Repository: viewSourceFixture{source}}).Snapshot("session")
	if view.CurrentID != "group" || view.CloneParent != "/group" || view.PendingPreview != "next" || view.PendingCount != 2 {
		t.Fatalf("group view: %+v", view)
	}
	if len(view.DeletableWorkspaces) != 0 {
		t.Fatalf("group exposes machine deletion: %+v", view.DeletableWorkspaces)
	}
	if _, err := view.DeleteTarget("machine"); err == nil {
		t.Fatal("group deletion accepted")
	}
	if !reflect.DeepEqual(source.Session, before) {
		t.Fatalf("view changed active session: %+v", source.Session)
	}
	view.Workspaces[0].ID = "changed"
	view.Current.Cwd = "changed"
	if source.Workspaces[0].ID != "machine" || source.Workspaces[1].Cwd != "/group/repo" {
		t.Fatal("view aliases configuration")
	}

	source.Binding.WorkspaceID = ""
	view = (ViewService{Repository: viewSourceFixture{source}}).Snapshot("session")
	if view.CurrentID != "" || view.Current != nil || !view.Unbound {
		t.Fatalf("unbound group inherited private workspace: %+v", view)
	}
}

func TestWorkspaceViewPrivateSelectionAndRecentOrder(t *testing.T) {
	source := ViewSource{Backend: "codex", Workspaces: []domain.Workspace{{ID: "first"}, {ID: "profile"}, {ID: "selected"}},
		Session: &conversation.Session{ChatType: "p2p", WorkspaceID: "first", RecentWorkspaceIDs: []string{"first"}},
		Profile: &routing.BotProfile{WorkspaceID: "profile"}, Selection: &conversation.Session{WorkspaceID: "selected", RecentWorkspaceIDs: []string{"profile", "selected", "profile"}},
	}
	service := ViewService{Repository: viewSourceFixture{source}}
	view := service.Snapshot("session")
	if view.CurrentID != "selected" || view.RecentWorkspaces[0].ID != "profile" || view.RecentWorkspaces[1].ID != "selected" {
		t.Fatalf("selection/recent view: %+v", view)
	}
	if len(view.DeletableWorkspaces) != 2 {
		t.Fatalf("delete options: %+v", view.DeletableWorkspaces)
	}
	source.Selection = nil
	view = (ViewService{Repository: viewSourceFixture{source}}).Snapshot("session")
	if view.CurrentID != "profile" {
		t.Fatalf("profile selection = %q", view.CurrentID)
	}
	source.Profile = nil
	view = (ViewService{Repository: viewSourceFixture{source}}).Snapshot("session")
	if view.CurrentID != "first" {
		t.Fatalf("session selection = %q", view.CurrentID)
	}
}

func TestWorkspaceSettingsViewCapabilitiesAndClaudeBypass(t *testing.T) {
	source := ViewSource{Backend: "claude", ClaudePermissionMode: "acceptEdits", Workspaces: []domain.Workspace{{ID: "repo"}}}
	service := ViewService{Repository: viewSourceFixture{source}}
	if _, err := service.Settings("session", SettingSandbox); err == nil {
		t.Fatal("Claude sandbox accepted")
	}
	view, err := service.Settings("session", SettingPermission)
	if err != nil || view.EffectiveValue != "acceptEdits" || !view.Inherited || len(view.Options) != 2 {
		t.Fatalf("Claude settings: %+v, %v", view, err)
	}
	source.ClaudeBypassEnabled = true
	view, err = (ViewService{Repository: viewSourceFixture{source}}).Settings("session", SettingPermission)
	if err != nil || len(view.Options) != 3 || view.Options[2].Value != "bypassPermissions" {
		t.Fatalf("bypass settings: %+v, %v", view, err)
	}
	source.Backend = "codex"
	if _, err := (ViewService{Repository: viewSourceFixture{source}}).Settings("session", SettingPermission); err == nil {
		t.Fatal("Codex permissions accepted")
	}
	for _, kind := range []string{"", "unsupported"} {
		source.Backend = kind
		for _, setting := range []Setting{SettingPermission, SettingSandbox, SettingPolicy, SettingMultiAgent} {
			if _, err := (ViewService{Repository: viewSourceFixture{source}}).Settings("session", setting); err == nil {
				t.Fatalf("backend %q exposes %q", kind, setting)
			}
		}
	}
}
