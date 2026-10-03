package skill

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"feidex/internal/domain/identity"
	catalog "feidex/internal/domain/skill"
	"feidex/internal/domain/submission"
	"feidex/internal/domain/workspace"
)

// WorkspaceSource is a detached configuration/session revision. The use case
// owns the session override and machine-default selection policy.
type WorkspaceSource struct {
	WorkspaceID string
	Workspaces  []workspace.Workspace
}

type WorkspaceRepository interface {
	SkillWorkspaceSource(string) WorkspaceSource
}

type Catalog interface {
	ListSkills(context.Context, string, bool) (catalog.SkillsListEntry, error)
}

type PendingRepository interface {
	Get(string) (submission.SubmissionSkill, bool)
	Set(string, submission.SubmissionSkill)
	Clear(string)
}

type Dependencies struct {
	Frontend   identity.FrontendID
	Context    func() context.Context
	Workspaces WorkspaceRepository
	Catalog    Catalog
	Pending    PendingRepository
}

type Service struct{ Deps Dependencies }

type View struct {
	Entry      catalog.SkillsListEntry
	Pending    submission.SubmissionSkill
	HasPending bool
}

var ErrDisabled = errors.New("该 skill 当前为 disabled")

func (s *Service) validSession(key string) bool {
	frontend, _, _, _, _ := identity.ParseSessionKey(key)
	return frontend == "" || frontend == string(s.Deps.Frontend) || (s.Deps.Frontend == "" && frontend == "default")
}

func (s *Service) Context() context.Context {
	if s.Deps.Context != nil {
		return s.Deps.Context()
	}
	return context.Background()
}

func (s *Service) workspace(key, id string) (*workspace.Workspace, error) {
	if !s.validSession(key) {
		return nil, fmt.Errorf("skill session belongs to another frontend")
	}
	if s.Deps.Workspaces == nil {
		return nil, fmt.Errorf("当前没有可用工作区")
	}
	source := s.Deps.Workspaces.SkillWorkspaceSource(key)
	if key != "" {
		id = strings.TrimSpace(source.WorkspaceID)
	}
	if id = strings.TrimSpace(id); id == "" {
		id = "default"
		if len(source.Workspaces) > 0 {
			id = source.Workspaces[0].ID
		}
	}
	for _, ws := range source.Workspaces {
		if ws.ID == id {
			return &ws, nil
		}
	}
	return nil, fmt.Errorf("workspace %q not found", id)
}

func (s *Service) fetch(ctx context.Context, key, id string, reload bool) (catalog.SkillsListEntry, error) {
	if err := ctx.Err(); err != nil {
		return catalog.SkillsListEntry{}, err
	}
	ws, err := s.workspace(key, id)
	if err != nil {
		return catalog.SkillsListEntry{}, err
	}
	if s.Deps.Catalog == nil {
		return catalog.SkillsListEntry{}, fmt.Errorf("skill catalog is unavailable")
	}
	return s.Deps.Catalog.ListSkills(ctx, ws.Cwd, reload)
}

func (s *Service) Snapshot(ctx context.Context, key string, reload bool) (View, error) {
	entry, err := s.fetch(ctx, key, "", reload)
	if err != nil {
		return View{}, err
	}
	pending, ok := s.SessionPendingSkill(key)
	return View{Entry: entry, Pending: pending, HasPending: ok}, nil
}

// Select refreshes the catalog before validating a selection. Rendering never
// chooses whether a disabled entry may be used in a later submission.
func (s *Service) Select(ctx context.Context, key, value string) (submission.SubmissionSkill, error) {
	entry, err := s.fetch(ctx, key, "", false)
	if err != nil {
		return submission.SubmissionSkill{}, err
	}
	if err := ctx.Err(); err != nil {
		return submission.SubmissionSkill{}, err
	}
	value = strings.TrimSpace(value)
	for _, metadata := range entry.Skills {
		if strings.TrimSpace(metadata.Path) != value && strings.TrimSpace(metadata.Name) != value {
			continue
		}
		if !metadata.Enabled {
			return submission.SubmissionSkill{}, ErrDisabled
		}
		selected := submission.SubmissionSkill{Name: strings.TrimSpace(metadata.Name), Path: strings.TrimSpace(metadata.Path)}
		s.SetSessionPendingSkill(key, selected)
		return selected, nil
	}
	return submission.SubmissionSkill{}, fmt.Errorf("未找到所选 skill")
}

func (s *Service) SessionPendingSkill(key string) (submission.SubmissionSkill, bool) {
	if !s.validSession(key) || s.Deps.Pending == nil {
		return submission.SubmissionSkill{}, false
	}
	return s.Deps.Pending.Get(key)
}

func (s *Service) SetSessionPendingSkill(key string, selected submission.SubmissionSkill) {
	if s.validSession(key) && s.Deps.Pending != nil {
		s.Deps.Pending.Set(key, selected)
	}
}

func (s *Service) ClearSessionPendingSkill(key string) {
	if s.validSession(key) && s.Deps.Pending != nil {
		s.Deps.Pending.Clear(key)
	}
}

// ResolveSubmissionSkill leaves pending state intact until the submission
// owner persists the new submission, then consumes or replaces the selection.
func (s *Service) ResolveSubmissionSkill(key, workspaceID, text string, attachments []submission.SubmissionAttachment) SubmissionSkillResolution {
	result := SubmissionSkillResolution{InputText: strings.TrimSpace(text)}
	pending, hasPending := s.SessionPendingSkill(key)
	result.ConsumePending = hasPending
	parsed := ParseLeadingPrefix(text)
	switch parsed.Mode {
	case PrefixNone:
		if hasPending {
			result.Skills = []submission.SubmissionSkill{pending}
		}
		return result
	case PrefixInvalid:
		return result
	}
	if !s.validSession(key) {
		return result
	}
	ctx, cancel := context.WithTimeout(s.Context(), 20*time.Second)
	defer cancel()
	entry, err := s.fetch(ctx, "", workspaceID, false)
	if err != nil {
		return result
	}
	selected, ok := FindEnabledByName(entry.Skills, parsed.Name)
	if !ok {
		return result
	}
	result.InputText = strings.TrimSpace(parsed.Body)
	if result.InputText == "" && len(attachments) == 0 {
		result.PendingReplacement = &selected
		return result
	}
	result.Skills = []submission.SubmissionSkill{selected}
	return result
}
