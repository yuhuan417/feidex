package workspace

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"feidex/internal/domain/conversation"
	"feidex/internal/domain/identity"
	"feidex/internal/domain/routing"
	domain "feidex/internal/domain/workspace"
	"feidex/internal/textutil"
)

// ViewSource is one detached revision of the current frontend's workspace scope.
type ViewSource struct {
	Backend              string
	ClaudePermissionMode string
	ClaudeBypassEnabled  bool
	Session              *conversation.Session
	Selection            *conversation.Session
	Binding              *routing.AgentBinding
	Profile              *routing.BotProfile
	Group                bool
	Workspaces           []domain.Workspace
	ConfigDirectory      string
}

type ViewRepository interface {
	WorkspaceViewSource(identity.FrontendID, string) ViewSource
}

type View struct {
	Backend              string
	ClaudePermissionMode string
	ClaudeBypassEnabled  bool
	CurrentID            string
	Current              *domain.Workspace
	SettingsWorkspace    *domain.Workspace
	Workspaces           []domain.Workspace
	RecentWorkspaces     []domain.Workspace
	DeletableWorkspaces  []domain.Workspace
	Group                bool
	Unbound              bool
	PendingCount         int
	PendingPreview       string
	ConfigActions        []string
	CloneRoot            string
	CloneParent          string
}

type ViewService struct {
	Frontend   identity.FrontendID
	Repository ViewRepository
}

func (s ViewService) Snapshot(key string) View {
	if s.Repository == nil {
		return View{}
	}
	source := s.Repository.WorkspaceViewSource(s.Frontend, key)
	view := View{Backend: source.Backend, ClaudePermissionMode: source.ClaudePermissionMode, ClaudeBypassEnabled: source.ClaudeBypassEnabled, Workspaces: append([]domain.Workspace(nil), source.Workspaces...), Group: source.Group, CloneRoot: "/", CloneParent: source.ConfigDirectory}
	view.ConfigActions = WorkspaceConfigActions(view.Backend)
	if view.CloneParent == "" {
		view.CloneParent = "."
	}
	if source.Group {
		if source.Binding != nil {
			view.CurrentID = strings.TrimSpace(source.Binding.WorkspaceID)
			view.PendingCount = len(source.Binding.PendingMessages)
			pending := source.Binding.PendingMessage
			if view.PendingCount > 0 {
				pending = source.Binding.PendingMessages[0]
			}
			view.PendingPreview = PendingPreview(pending)
		}
		view.Unbound = view.CurrentID == ""
	} else {
		if source.Selection != nil {
			view.CurrentID = strings.TrimSpace(source.Selection.WorkspaceID)
		}
		if view.CurrentID == "" && source.Session != nil && !strings.EqualFold(source.Session.ChatType, "group") && source.Profile != nil {
			view.CurrentID = strings.TrimSpace(source.Profile.WorkspaceID)
		}
		if view.CurrentID == "" && source.Session != nil {
			view.CurrentID = strings.TrimSpace(source.Session.WorkspaceID)
		}
		if view.CurrentID == "" {
			view.CurrentID = "default"
			if len(view.Workspaces) > 0 {
				view.CurrentID = view.Workspaces[0].ID
			}
		}
	}
	for _, ws := range view.Workspaces {
		if ws.ID == view.CurrentID {
			value := ws
			view.Current = &value
		}
		if !view.Group && strings.TrimSpace(ws.ID) != "" && ws.ID != view.CurrentID {
			view.DeletableWorkspaces = append(view.DeletableWorkspaces, ws)
		}
	}
	view.SettingsWorkspace = view.Current
	if view.SettingsWorkspace == nil && view.Group {
		fallback := source
		fallback.Group, fallback.Binding = false, nil
		id := selectedWorkspace(fallback)
		for _, ws := range view.Workspaces {
			if ws.ID == id {
				value := ws
				view.SettingsWorkspace = &value
				break
			}
		}
	}
	if view.Current != nil && strings.TrimSpace(view.Current.Cwd) != "" {
		view.CloneParent = filepath.Dir(strings.TrimSpace(view.Current.Cwd))
	}
	var recent []string
	if source.Selection != nil {
		recent = source.Selection.RecentWorkspaceIDs
	}
	if len(recent) == 0 && source.Session != nil {
		recent = source.Session.RecentWorkspaceIDs
	}
	view.RecentWorkspaces = SortByRecent(view.Workspaces, recent)
	return view
}

func selectedWorkspace(source ViewSource) string {
	if source.Selection != nil && strings.TrimSpace(source.Selection.WorkspaceID) != "" {
		return strings.TrimSpace(source.Selection.WorkspaceID)
	}
	if source.Session != nil && source.Session.ChatType != "" && !strings.EqualFold(source.Session.ChatType, "group") && source.Profile != nil && strings.TrimSpace(source.Profile.WorkspaceID) != "" {
		return strings.TrimSpace(source.Profile.WorkspaceID)
	}
	if source.Session != nil && strings.TrimSpace(source.Session.WorkspaceID) != "" {
		return strings.TrimSpace(source.Session.WorkspaceID)
	}
	if len(source.Workspaces) > 0 {
		return source.Workspaces[0].ID
	}
	return "default"
}

func PendingPreview(pending *routing.AgentBindingPendingMessage) string {
	if pending == nil {
		return ""
	}
	preview := textutil.Truncate(strings.TrimSpace(pending.Text), 80)
	if preview == "" && len(pending.Attachments) > 0 {
		preview = fmt.Sprintf("%d 个附件", len(pending.Attachments))
	}
	if preview == "" {
		preview = strings.TrimSpace(pending.MessageID)
	}
	return preview
}

func WorkspaceConfigActions(kind string) []string {
	if kind == "claude" {
		return []string{"workspace.permission_mode.menu"}
	}
	return []string{"workspace.sandbox.menu", "workspace.policy.menu", "workspace.multiagent.menu"}
}

func (v View) DeleteTarget(id string) (*domain.Workspace, error) {
	if v.Group {
		return nil, fmt.Errorf("群聊不能移除本机 workspace 配置；请使用 /workspace unbind")
	}
	id = strings.TrimSpace(id)
	for _, ws := range v.Workspaces {
		if ws.ID == id {
			value := ws
			return &value, nil
		}
	}
	return nil, fmt.Errorf("workspace %q 不存在", id)
}

func SortByRecent(workspaces []domain.Workspace, recent []string) []domain.Workspace {
	rank := make(map[string]int, len(recent))
	for i, id := range recent {
		if _, exists := rank[id]; !exists {
			rank[id] = i
		}
	}
	result := append([]domain.Workspace(nil), workspaces...)
	sort.SliceStable(result, func(i, j int) bool {
		ri, iok := rank[result[i].ID]
		rj, jok := rank[result[j].ID]
		if iok && jok {
			return ri < rj
		}
		if iok {
			return true
		}
		if jok {
			return false
		}
		return result[i].ID < result[j].ID
	})
	return result
}
