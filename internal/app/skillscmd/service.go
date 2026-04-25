// Package skillscmd provides the skills command service extracted from the
// app god package. It handles skill listing, selection, and pending skill
// tracking.
package skillscmd

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	appcommandmatch "feidex/internal/app/commandmatch"
	appskills "feidex/internal/app/skills"
	"feidex/internal/codexrpc"
	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

// ---------------------------------------------------------------------------
// Exported types
// ---------------------------------------------------------------------------

// SubmissionSkillResolution describes how a submission's skill was resolved.
type SubmissionSkillResolution = appskills.SubmissionSkillResolution

// PendingSkillTracker tracks per-session pending skills.
type PendingSkillTracker struct {
	mu     sync.Mutex
	Skills map[string]state.SubmissionSkill
}

// NewPendingSkillTracker creates a new PendingSkillTracker.
func NewPendingSkillTracker() *PendingSkillTracker {
	return &PendingSkillTracker{Skills: map[string]state.SubmissionSkill{}}
}

// Service provides skills listing, selection, and pending skill tracking.
// Callback function fields are injected by the app-layer adapter.
type Service struct {
	// FeishuClient returns the Feishu bot client.
	FeishuClient func() FeishuClient
	// RequireCodexClient returns the Codex RPC client or an error.
	RequireCodexClient func() (CodexClient, error)
	// AppStateSession returns the session for the given key.
	AppStateSession func(sessionKey string) *state.Session
	// DefaultWorkspaceID returns the default workspace ID.
	DefaultWorkspaceID func() string
	// FindWorkspace looks up a workspace by ID from the config.
	FindWorkspace func(workspaceID string) *config.Workspace
	// FormatMenuBody formats card body with breadcrumb navigation.
	FormatMenuBody func(action, body string) string
	// CommandLabel formats a command label with its slash command.
	CommandLabel func(label, slash string) string
	// GetPendingSkillTracker returns the pending skill tracker.
	GetPendingSkillTracker func() *PendingSkillTracker
	// MakeSessionKey builds a session key from an inbound message.
	MakeSessionKey func(msg *feishu.InboundMessage) string
	// ReplyInThreadEnabled reports whether reply-in-thread is enabled.
	ReplyInThreadEnabled func(chatType string) bool
}

// NewService creates a new Service.
func NewService() *Service {
	return &Service{}
}

// FeishuClient is the narrow interface for the Feishu bot client methods
// used by the skills service.
type FeishuClient interface {
	ReplyCard(ctx context.Context, messageID string, card map[string]any, inThread bool) (string, error)
	ReplyText(ctx context.Context, messageID string, text string, inThread bool) error
}

// CodexClient is the narrow interface for the Codex RPC client methods
// used by the skills service.
type CodexClient interface {
	Call(ctx context.Context, method string, params any, result any) error
}

const (
	skillConfigReloadArg = "reload"
)

// MatchSkillsCommand reports whether fields match the /skills command.
func MatchSkillsCommand(fields []string) bool {
	return appcommandmatch.ExactOrSingleArgCommand(fields, skillConfigReloadArg)
}

// rawCard wraps a card map for CardActionTriggerResponse.
func rawCard(card map[string]any) *callback.Card {
	return &callback.Card{Type: "raw", Data: card}
}

// ---------------------------------------------------------------------------
// Command handler
// ---------------------------------------------------------------------------

// CommandSkills handles the /skills command.
func (s *Service) CommandSkills(msg *feishu.InboundMessage, args []string) error {
	forceReload := false
	switch len(args) {
	case 0:
	case 1:
		if strings.TrimSpace(args[0]) != skillConfigReloadArg {
			return fmt.Errorf("usage: /skills | /skills reload")
		}
		forceReload = true
	default:
		return fmt.Errorf("usage: /skills | /skills reload")
	}
	card, err := s.RenderSkillsCard(s.MakeSessionKey(msg), forceReload)
	if err != nil {
		return err
	}
	_, err = s.FeishuClient().ReplyCard(context.Background(), msg.MessageID, card, s.ReplyInThreadEnabled(msg.ChatType))
	return err
}

// ---------------------------------------------------------------------------
// Workspace helpers
// ---------------------------------------------------------------------------

// CurrentWorkspaceForSessionKey returns the workspace for the given session key.
func (s *Service) CurrentWorkspaceForSessionKey(sessionKey string) (*config.Workspace, error) {
	if s.FindWorkspace == nil {
		return nil, fmt.Errorf("当前没有可用工作区")
	}
	workspaceID := s.DefaultWorkspaceID()
	if sess := s.AppStateSession(sessionKey); sess != nil && strings.TrimSpace(sess.WorkspaceID) != "" {
		workspaceID = strings.TrimSpace(sess.WorkspaceID)
	}
	ws := s.FindWorkspace(workspaceID)
	if ws == nil {
		return nil, fmt.Errorf("workspace %q not found", workspaceID)
	}
	return ws, nil
}

// WorkspaceByID returns the workspace for the given workspace ID.
func (s *Service) WorkspaceByID(workspaceID string) (*config.Workspace, error) {
	if s.FindWorkspace == nil {
		return nil, fmt.Errorf("当前没有可用工作区")
	}
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		workspaceID = s.DefaultWorkspaceID()
	}
	ws := s.FindWorkspace(workspaceID)
	if ws == nil {
		return nil, fmt.Errorf("workspace %q not found", workspaceID)
	}
	return ws, nil
}

// ---------------------------------------------------------------------------
// Skill fetching
// ---------------------------------------------------------------------------

// FetchSkillsForCWD fetches skills for the given working directory.
func (s *Service) FetchSkillsForCWD(ctx context.Context, cwd string, forceReload bool) (codexrpc.SkillsListEntry, error) {
	var result codexrpc.SkillsListResult
	client, err := s.RequireCodexClient()
	if err != nil {
		return codexrpc.SkillsListEntry{}, err
	}
	params := map[string]any{
		"forceReload": forceReload,
	}
	if strings.TrimSpace(cwd) != "" {
		params["cwds"] = []string{strings.TrimSpace(cwd)}
	}
	if err := client.Call(ctx, "skills/list", params, &result); err != nil {
		return codexrpc.SkillsListEntry{}, err
	}
	for _, entry := range result.Data {
		if strings.TrimSpace(entry.Cwd) == strings.TrimSpace(cwd) {
			return entry, nil
		}
	}
	if len(result.Data) > 0 {
		return result.Data[0], nil
	}
	return codexrpc.SkillsListEntry{Cwd: strings.TrimSpace(cwd)}, nil
}

// FetchSkillsForSessionKey fetches skills for the workspace associated with
// the given session key.
func (s *Service) FetchSkillsForSessionKey(ctx context.Context, sessionKey string, forceReload bool) (codexrpc.SkillsListEntry, error) {
	ws, err := s.CurrentWorkspaceForSessionKey(sessionKey)
	if err != nil {
		return codexrpc.SkillsListEntry{}, err
	}
	return s.FetchSkillsForCWD(ctx, ws.Cwd, forceReload)
}

// FetchSkillsForWorkspaceID fetches skills for the given workspace ID.
func (s *Service) FetchSkillsForWorkspaceID(ctx context.Context, workspaceID string, forceReload bool) (codexrpc.SkillsListEntry, error) {
	ws, err := s.WorkspaceByID(workspaceID)
	if err != nil {
		return codexrpc.SkillsListEntry{}, err
	}
	return s.FetchSkillsForCWD(ctx, ws.Cwd, forceReload)
}

// ---------------------------------------------------------------------------
// Card rendering
// ---------------------------------------------------------------------------

// RenderSkillsCard renders the skills list card for the given session.
func (s *Service) RenderSkillsCard(sessionKey string, forceReload bool) (map[string]any, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	entry, err := s.FetchSkillsForSessionKey(ctx, sessionKey, forceReload)
	if err != nil {
		return nil, err
	}
	pending, hasPending := s.SessionPendingSkill(sessionKey)
	card := appskills.BuildCard(appskills.BuildCardParams{
		Entry:       entry,
		HasPending:  hasPending,
		Pending:     pending,
		SessionKey:  sessionKey,
		FormatBody:  func(body string) string { return s.FormatMenuBody("menu.skills", body) },
		ReloadLabel: s.CommandLabel("刷新", "/skills reload"),
		BackLabel:   "返回上一级",
	})
	return card, nil
}

// ---------------------------------------------------------------------------
// Card action handlers
// ---------------------------------------------------------------------------

// CompleteSkillsReload handles the skills.reload card action.
func (s *Service) CompleteSkillsReload(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	card, err := s.RenderSkillsCard(sessionKey, true)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已刷新 skill 列表"},
		Card:  rawCard(card),
	}, nil
}

// CompleteSkillsSelect handles the skills.select card action.
func (s *Service) CompleteSkillsSelect(action *feishu.CardAction, sessionKey, selectedValue string) (*callback.CardActionTriggerResponse, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	entry, err := s.FetchSkillsForSessionKey(ctx, sessionKey, false)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	skill, ok := appskills.FindByPath(entry.Skills, selectedValue)
	if !ok {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "未找到所选 skill"}}, nil
	}
	if !skill.Enabled {
		card, renderErr := s.RenderSkillsCard(sessionKey, false)
		if renderErr != nil {
			return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "该 skill 当前为 disabled"}}, nil
		}
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "warning", Content: "该 skill 当前为 disabled，不能用于下一条消息"},
			Card:  rawCard(card),
		}, nil
	}
	s.SetSessionPendingSkill(sessionKey, state.SubmissionSkill{Name: strings.TrimSpace(skill.Name), Path: strings.TrimSpace(skill.Path)})
	card, err := s.RenderSkillsCard(sessionKey, false)
	if err != nil {
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "success", Content: appskills.PendingConfirmationText(skill.Name)},
		}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: appskills.PendingConfirmationText(skill.Name)},
		Card:  rawCard(card),
	}, nil
}

// ---------------------------------------------------------------------------
// Pending skill tracker
// ---------------------------------------------------------------------------

// SessionPendingSkill returns the pending skill for the given session key.
func (s *Service) SessionPendingSkill(sessionKey string) (state.SubmissionSkill, bool) {
	tracker := s.GetPendingSkillTracker()
	if tracker == nil {
		return state.SubmissionSkill{}, false
	}
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	skill, ok := tracker.Skills[strings.TrimSpace(sessionKey)]
	if !ok || strings.TrimSpace(skill.Name) == "" || strings.TrimSpace(skill.Path) == "" {
		return state.SubmissionSkill{}, false
	}
	return skill, true
}

// SetSessionPendingSkill sets the pending skill for the given session key.
func (s *Service) SetSessionPendingSkill(sessionKey string, skill state.SubmissionSkill) {
	if strings.TrimSpace(sessionKey) == "" {
		return
	}
	tracker := s.GetPendingSkillTracker()
	if tracker == nil {
		return
	}
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	if tracker.Skills == nil {
		tracker.Skills = map[string]state.SubmissionSkill{}
	}
	skill.Name = strings.TrimSpace(skill.Name)
	skill.Path = strings.TrimSpace(skill.Path)
	if skill.Name == "" || skill.Path == "" {
		delete(tracker.Skills, strings.TrimSpace(sessionKey))
		return
	}
	tracker.Skills[strings.TrimSpace(sessionKey)] = skill
}

// ClearSessionPendingSkill clears the pending skill for the given session key.
func (s *Service) ClearSessionPendingSkill(sessionKey string) {
	if strings.TrimSpace(sessionKey) == "" {
		return
	}
	tracker := s.GetPendingSkillTracker()
	if tracker == nil {
		return
	}
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	delete(tracker.Skills, strings.TrimSpace(sessionKey))
}

// ---------------------------------------------------------------------------
// Submission skill resolution
// ---------------------------------------------------------------------------

// ResolveSubmissionSkill resolves which skill(s) apply to a submission.
func (s *Service) ResolveSubmissionSkill(sessionKey, workspaceID, inputText string, attachments []state.SubmissionAttachment) SubmissionSkillResolution {
	resolution := SubmissionSkillResolution{
		InputText: strings.TrimSpace(inputText),
	}
	pending, hasPending := s.SessionPendingSkill(sessionKey)
	if hasPending {
		resolution.ConsumePending = true
	}

	parsed := appskills.ParseLeadingPrefix(inputText)
	switch parsed.Mode {
	case appskills.PrefixNone:
		if hasPending {
			resolution.Skills = []state.SubmissionSkill{pending}
		}
		return resolution
	case appskills.PrefixInvalid:
		return resolution
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	entry, err := s.FetchSkillsForWorkspaceID(ctx, workspaceID, false)
	if err != nil {
		return resolution
	}
	skill, ok := appskills.FindEnabledByName(entry.Skills, parsed.Name)
	if !ok {
		return resolution
	}
	resolution.InputText = strings.TrimSpace(parsed.Body)
	if resolution.InputText == "" && len(attachments) == 0 {
		resolution.PendingReplacement = &skill
		return resolution
	}
	resolution.Skills = []state.SubmissionSkill{skill}
	return resolution
}
