package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

const currentSnapshotVersion = 10

type Store struct {
	path    string
	mu      sync.Mutex
	data    Snapshot
	runtime runtimeState
}

type Snapshot struct {
	Version                   int                                   `json:"version"`
	Sessions                  map[string]*storedSession             `json:"sessions"`
	AgentBindings             map[string]*AgentBinding              `json:"agent_bindings,omitempty"`
	GroupPrimaries            map[string]*GroupPrimary              `json:"group_primaries,omitempty"`
	FrontendCardNotifications map[string][]FrontendCardNotification `json:"frontend_card_notifications,omitempty"`
}

type runtimeState struct {
	Sessions        map[string]*Session
	Submissions     map[string]*Submission
	PendingRequests map[string]*PendingRequest
	MessageLinks    map[string]*MessageLink
	Counters        Counters
}

type Counters struct {
	NextSubmission int64 `json:"next_submission"`
	NextLocalID    int64 `json:"next_local_id"`
}

type storedSession struct {
	Key                           string                          `json:"key"`
	BindingID                     string                          `json:"binding_id,omitempty"`
	WorkspaceID                   string                          `json:"workspace_id"`
	ActiveThreadID                string                          `json:"active_thread_id"`
	ActiveThreadWorkspaceID       string                          `json:"active_thread_workspace_id"`
	ActiveThreadApprovalPolicy    string                          `json:"active_thread_approval_policy"`
	ActiveThreadSandboxMode       string                          `json:"active_thread_sandbox_mode"`
	ActiveThreadMultiAgentMode    string                          `json:"active_thread_multi_agent_mode,omitempty"`
	ActiveClaudePermissionMode    string                          `json:"active_claude_permission_mode,omitempty"`
	ActiveThreadServiceTier       string                          `json:"active_thread_service_tier,omitempty"`
	ActiveThreadCollaborationMode *SessionCollaborationMode       `json:"active_thread_collaboration_mode,omitempty"`
	ActiveThreadName              string                          `json:"active_thread_name"`
	ActiveThreadPreview           string                          `json:"active_thread_preview"`
	BackendThreads                map[string]SessionBackendThread `json:"backend_threads,omitempty"`
	OwnerUserID                   string                          `json:"owner_user_id"`
	ModelOverride                 string                          `json:"model_override"`
	RecentWorkspaceIDs            []string                        `json:"recent_workspace_ids,omitempty"`
	UpdatedAt                     int64                           `json:"updated_at"`
}

type FrontendCardNotification struct {
	Kind        string `json:"kind,omitempty"`
	CollapseKey string `json:"collapse_key,omitempty"`
	Title       string `json:"title"`
	Color       string `json:"color,omitempty"`
	Body        string `json:"body"`
	CreatedAt   int64  `json:"created_at,omitempty"`
}

// AgentBinding maps a local frontend/bot to one logical chat project.
// WorkspaceID and the optional model settings refer to this local instance.
type AgentBinding struct {
	ID                      string                      `json:"id"`
	FrontendID              string                      `json:"frontend_id"`
	ChatID                  string                      `json:"chat_id"`
	ChatType                string                      `json:"chat_type"`
	Component               string                      `json:"component"`
	WorkspaceID             string                      `json:"workspace_id"`
	ModelOverride           string                      `json:"model_override,omitempty"`
	ReasoningEffortOverride string                      `json:"reasoning_effort_override,omitempty"`
	ServiceTierOverride     string                      `json:"service_tier_override,omitempty"`
	SandboxModeOverride     string                      `json:"sandbox_mode_override,omitempty"`
	ApprovalPolicyOverride  string                      `json:"approval_policy_override,omitempty"`
	MultiAgentModeOverride  string                      `json:"multi_agent_mode_override,omitempty"`
	ClaudePermissionMode    string                      `json:"claude_permission_mode,omitempty"`
	Primary                 bool                        `json:"primary,omitempty"`
	PendingMessage          *AgentBindingPendingMessage `json:"pending_message,omitempty"`
	Status                  string                      `json:"status"`
	CreatedAt               int64                       `json:"created_at"`
	UpdatedAt               int64                       `json:"updated_at"`
}

// GroupPrimary stores whether one local frontend/bot is the primary handler
// for unmentioned messages in one Feishu group. It is intentionally separate
// from AgentBinding, which only owns local workspace/runtime configuration.
type GroupPrimary struct {
	ID         string `json:"id"`
	FrontendID string `json:"frontend_id"`
	ChatID     string `json:"chat_id"`
	ChatType   string `json:"chat_type"`
	Primary    bool   `json:"primary"`
	CreatedAt  int64  `json:"created_at"`
	UpdatedAt  int64  `json:"updated_at"`
}

// AgentBindingPendingMessage stores one inbound group message while a binding
// is waiting for a local workspace. It is replayed after binding activation.
type AgentBindingPendingMessage struct {
	SessionKey             string                          `json:"session_key,omitempty"`
	MessageID              string                          `json:"message_id"`
	ChatID                 string                          `json:"chat_id"`
	ChatType               string                          `json:"chat_type"`
	UserID                 string                          `json:"user_id"`
	UserName               string                          `json:"user_name,omitempty"`
	ChatName               string                          `json:"chat_name,omitempty"`
	Text                   string                          `json:"text,omitempty"`
	RootMessageID          string                          `json:"root_message_id,omitempty"`
	ParentMessageID        string                          `json:"parent_message_id,omitempty"`
	ThreadID               string                          `json:"thread_id,omitempty"`
	Attachments            []AgentBindingPendingAttachment `json:"attachments,omitempty"`
	MergeForwardMessageIDs []string                        `json:"merge_forward_message_ids,omitempty"`
	ExpandedMergeForward   bool                            `json:"expanded_merge_forward,omitempty"`
	MentionedOpenIDs       []string                        `json:"mentioned_open_ids,omitempty"`
	MentionedEveryone      bool                            `json:"mentioned_everyone,omitempty"`
	MentionedSelf          bool                            `json:"mentioned_self,omitempty"`
	CreatedAt              int64                           `json:"created_at,omitempty"`
	StoredAt               int64                           `json:"stored_at,omitempty"`
}

type AgentBindingPendingAttachment struct {
	Kind            string `json:"kind,omitempty"`
	ResourceKey     string `json:"resource_key,omitempty"`
	SourceMessageID string `json:"source_message_id,omitempty"`
}

type SessionBackendThread struct {
	ThreadID             string                    `json:"thread_id,omitempty"`
	WorkspaceID          string                    `json:"workspace_id,omitempty"`
	ApprovalPolicy       string                    `json:"approval_policy,omitempty"`
	SandboxMode          string                    `json:"sandbox_mode,omitempty"`
	MultiAgentMode       string                    `json:"multi_agent_mode,omitempty"`
	ClaudePermissionMode string                    `json:"claude_permission_mode,omitempty"`
	ServiceTier          string                    `json:"service_tier,omitempty"`
	CollaborationMode    *SessionCollaborationMode `json:"collaboration_mode,omitempty"`
	Name                 string                    `json:"name,omitempty"`
	Preview              string                    `json:"preview,omitempty"`
}

type SessionCollaborationMode struct {
	Mode                  string  `json:"mode"`
	Model                 string  `json:"model"`
	ReasoningEffort       string  `json:"reasoning_effort,omitempty"`
	DeveloperInstructions *string `json:"developer_instructions"`
}

type Session struct {
	Key                           string                          `json:"key"`
	BindingID                     string                          `json:"binding_id,omitempty"`
	WorkspaceID                   string                          `json:"workspace_id"`
	ActiveThreadID                string                          `json:"active_thread_id"`
	ActiveThreadWorkspaceID       string                          `json:"active_thread_workspace_id"`
	ActiveThreadApprovalPolicy    string                          `json:"active_thread_approval_policy"`
	ActiveThreadSandboxMode       string                          `json:"active_thread_sandbox_mode"`
	ActiveThreadMultiAgentMode    string                          `json:"active_thread_multi_agent_mode,omitempty"`
	ActiveClaudePermissionMode    string                          `json:"active_claude_permission_mode,omitempty"`
	ActiveThreadServiceTier       string                          `json:"active_thread_service_tier,omitempty"`
	ActiveThreadCollaborationMode *SessionCollaborationMode       `json:"active_thread_collaboration_mode,omitempty"`
	ActiveThreadName              string                          `json:"active_thread_name"`
	ActiveThreadPreview           string                          `json:"active_thread_preview"`
	BackendThreads                map[string]SessionBackendThread `json:"backend_threads,omitempty"`
	ActiveTurnID                  string                          `json:"active_turn_id"`
	ActiveSubmissionID            string                          `json:"active_submission_id"`
	OwnerUserID                   string                          `json:"owner_user_id"`
	ChatID                        string                          `json:"chat_id"`
	ChatType                      string                          `json:"chat_type"`
	RootMessageID                 string                          `json:"root_message_id"`
	ModelOverride                 string                          `json:"model_override"`
	Status                        string                          `json:"status"`
	Queue                         []string                        `json:"queue"`
	ActiveOperations              []SessionActiveOperation        `json:"active_operations,omitempty"`
	StagedImages                  []SessionStagedImage            `json:"staged_images,omitempty"`
	RecentWorkspaceIDs            []string                        `json:"recent_workspace_ids,omitempty"`
	UpdatedAt                     int64                           `json:"updated_at"`
}

type SessionActiveOperation struct {
	Kind         string `json:"kind,omitempty"`
	SubmissionID string `json:"submission_id,omitempty"`
	ThreadID     string `json:"thread_id,omitempty"`
	TurnID       string `json:"turn_id,omitempty"`
	StartedAt    int64  `json:"started_at,omitempty"`
}

type SessionStagedImage struct {
	SourceMessageID string `json:"source_message_id"`
	RootMessageID   string `json:"root_message_id,omitempty"`
	Name            string `json:"name"`
	LocalPath       string `json:"local_path"`
	CreatedAt       int64  `json:"created_at"`
}

type SubmissionAttachment struct {
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	LocalPath string `json:"local_path"`
}

type SubmissionSkill struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

type Submission struct {
	ID                   string                 `json:"id"`
	SessionKey           string                 `json:"session_key"`
	BindingID            string                 `json:"binding_id,omitempty"`
	WorkspaceID          string                 `json:"workspace_id"`
	ThreadID             string                 `json:"thread_id"`
	TurnID               string                 `json:"turn_id"`
	UserID               string                 `json:"user_id"`
	ChatID               string                 `json:"chat_id"`
	TriggerMessageID     string                 `json:"trigger_message_id"`
	SourceMessageIDs     []string               `json:"source_message_ids,omitempty"`
	SourceRootMessageIDs []string               `json:"source_root_message_ids,omitempty"`
	InputText            string                 `json:"input_text"`
	Skills               []SubmissionSkill      `json:"skills,omitempty"`
	Attachments          []SubmissionAttachment `json:"attachments,omitempty"`
	Kind                 string                 `json:"kind,omitempty"`
	ReviewTargetType     string                 `json:"review_target_type,omitempty"`
	ReviewBranch         string                 `json:"review_branch,omitempty"`
	ReviewCommitSHA      string                 `json:"review_commit_sha,omitempty"`
	ReviewCommitTitle    string                 `json:"review_commit_title,omitempty"`
	ReviewInstructions   string                 `json:"review_instructions,omitempty"`
	Status               string                 `json:"status"`
	WaitedInQueue        bool                   `json:"waited_in_queue,omitempty"`
	StartNoticeSent      bool                   `json:"start_notice_sent,omitempty"`
	Finalized            bool                   `json:"finalized"`
	CreatedAt            int64                  `json:"created_at"`
	UpdatedAt            int64                  `json:"updated_at"`
}

type PendingRequest struct {
	FrontendID   string `json:"frontend_id,omitempty"`
	ID           string `json:"id"`
	RequestIDRaw string `json:"request_id_raw,omitempty"`
	Backend      string `json:"backend,omitempty"`
	Kind         string `json:"kind"`
	SessionKey   string `json:"session_key"`
	ThreadID     string `json:"thread_id"`
	TurnID       string `json:"turn_id"`
	ItemID       string `json:"item_id"`
	OwnerUserID  string `json:"owner_user_id"`
	FeishuMsgID  string `json:"feishu_msg_id"`
	PayloadJSON  string `json:"payload_json"`
	Status       string `json:"status"`
	CreatedAt    int64  `json:"created_at"`
	ExpiresAt    int64  `json:"expires_at"`
}

type MessageLink struct {
	FrontendID   string `json:"frontend_id,omitempty"`
	Backend      string `json:"backend,omitempty"`
	MessageID    string `json:"message_id"`
	SessionKey   string `json:"session_key,omitempty"`
	SubmissionID string `json:"submission_id,omitempty"`
	ThreadID     string `json:"thread_id,omitempty"`
	TurnID       string `json:"turn_id,omitempty"`
}

func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	s := &Store{
		path: path,
		data: Snapshot{
			Version:                   currentSnapshotVersion,
			Sessions:                  map[string]*storedSession{},
			AgentBindings:             map[string]*AgentBinding{},
			GroupPrimaries:            map[string]*GroupPrimary{},
			FrontendCardNotifications: map[string][]FrontendCardNotification{},
		},
		runtime: runtimeState{
			Sessions:        map[string]*Session{},
			Submissions:     map[string]*Submission{},
			PendingRequests: map[string]*PendingRequest{},
			MessageLinks:    map[string]*MessageLink{},
			Counters:        Counters{NextSubmission: 1, NextLocalID: 1},
		},
	}
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return s, s.saveLocked()
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(b) == 0 {
		return s, s.saveLocked()
	}
	var loaded Snapshot
	if err := json.Unmarshal(b, &loaded); err != nil {
		return nil, err
	}
	s.data = loaded
	if s.data.Sessions == nil {
		s.data.Sessions = map[string]*storedSession{}
	}
	if s.data.FrontendCardNotifications == nil {
		s.data.FrontendCardNotifications = map[string][]FrontendCardNotification{}
	}
	rewrite := s.data.Version != currentSnapshotVersion
	s.data.Version = currentSnapshotVersion
	normalizedBindings := normalizeAgentBindings(s.data.AgentBindings)
	if normalizedBindings == nil {
		normalizedBindings = map[string]*AgentBinding{}
	}
	if !agentBindingsEqual(s.data.AgentBindings, normalizedBindings) {
		rewrite = true
	}
	s.data.AgentBindings = normalizedBindings
	normalizedPrimaries := normalizeGroupPrimaries(s.data.GroupPrimaries)
	if normalizedPrimaries == nil {
		normalizedPrimaries = map[string]*GroupPrimary{}
	}
	if s.data.GroupPrimaries == nil {
		migrateGroupPrimariesFromBindings(normalizedPrimaries, normalizedBindings)
	}
	if !groupPrimariesEqual(s.data.GroupPrimaries, normalizedPrimaries) {
		rewrite = true
	}
	s.data.GroupPrimaries = normalizedPrimaries
	for key, sess := range s.data.Sessions {
		persisted := normalizeStoredSession(sess)
		if !storedSessionsEqual(sess, persisted) {
			rewrite = true
		}
		s.data.Sessions[key] = persisted
		s.runtime.Sessions[key] = sessionFromStored(persisted)
	}
	if rewrite {
		if err := s.saveLocked(); err != nil {
			return nil, err
		}
	}
	return s, nil
}

// GetAgentBinding returns a binding by id without frontend filtering.
func (s *Store) GetAgentBinding(id string) *AgentBinding {
	return s.GetScopedAgentBinding("", id)
}

// GetScopedAgentBinding returns a binding by id when it belongs to frontendID.
// An empty frontendID disables the scope check for store-level callers.
func (s *Store) GetScopedAgentBinding(frontendID, id string) *AgentBinding {
	frontendID = strings.TrimSpace(frontendID)
	id = strings.TrimSpace(id)
	if id == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	binding, ok := s.data.AgentBindings[id]
	if !ok || binding == nil {
		return nil
	}
	if frontendID != "" && binding.FrontendID != frontendID {
		return nil
	}
	return cloneAgentBinding(binding)
}

// AllAgentBindings returns deep copies of all persisted bindings.
func (s *Store) AllAgentBindings() []*AgentBinding {
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneAgentBindings(s.data.AgentBindings)
}

// AgentBindingsByFrontend returns bindings owned by one local frontend.
func (s *Store) AgentBindingsByFrontend(frontendID string) []*AgentBinding {
	frontendID = strings.TrimSpace(frontendID)
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneAgentBindingsMatching(s.data.AgentBindings, func(binding *AgentBinding) bool {
		return binding != nil && binding.FrontendID == frontendID
	})
}

// AgentBindingsByChat returns bindings for one frontend and logical chat.
func (s *Store) AgentBindingsByChat(frontendID, chatType, chatID string) []*AgentBinding {
	frontendID = strings.TrimSpace(frontendID)
	chatType = strings.ToLower(strings.TrimSpace(chatType))
	chatID = strings.TrimSpace(chatID)
	if chatID == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneAgentBindingsMatching(s.data.AgentBindings, func(binding *AgentBinding) bool {
		if binding == nil || binding.FrontendID != frontendID || binding.ChatID != chatID {
			return false
		}
		return chatType == "" || binding.ChatType == chatType
	})
}

// UpsertAgentBinding persists a local binding.
func (s *Store) UpsertAgentBinding(binding *AgentBinding) error {
	if binding == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := cloneAgentBinding(binding)
	if cp == nil || cp.ID == "" {
		return nil
	}
	normalizeAgentBindingValues(cp)
	now := time.Now().Unix()
	if previous := s.data.AgentBindings[cp.ID]; previous != nil && cp.CreatedAt == 0 {
		cp.CreatedAt = previous.CreatedAt
	}
	for id, previous := range s.data.AgentBindings {
		if id == cp.ID || previous == nil {
			continue
		}
		if previous.FrontendID == cp.FrontendID && previous.ChatType == cp.ChatType && previous.ChatID == cp.ChatID {
			return fmt.Errorf("agent binding already exists for frontend %q chat %q: %s", cp.FrontendID, cp.ChatID, id)
		}
	}
	if cp.CreatedAt == 0 {
		cp.CreatedAt = now
	}
	cp.UpdatedAt = now
	if s.data.AgentBindings == nil {
		s.data.AgentBindings = map[string]*AgentBinding{}
	}
	s.data.AgentBindings[cp.ID] = cp
	return s.saveLocked()
}

// UpsertScopedAgentBinding persists a binding owned by frontendID. A blank
// binding frontend is filled from the scope; a different frontend is rejected.
func (s *Store) UpsertScopedAgentBinding(frontendID string, binding *AgentBinding) error {
	if binding == nil {
		return nil
	}
	frontendID = strings.TrimSpace(frontendID)
	cp := cloneAgentBinding(binding)
	if cp == nil {
		return nil
	}
	if strings.TrimSpace(cp.FrontendID) == "" {
		cp.FrontendID = frontendID
	}
	if frontendID != "" && strings.TrimSpace(cp.FrontendID) != frontendID {
		return fmt.Errorf("agent binding frontend %q does not match scope %q", cp.FrontendID, frontendID)
	}
	return s.UpsertAgentBinding(cp)
}

// DeleteAgentBinding deletes a persisted binding by id.
func (s *Store) DeleteAgentBinding(id string) error {
	return s.DeleteScopedAgentBinding("", id)
}

// DeleteScopedAgentBinding deletes a binding only when it belongs to frontendID.
func (s *Store) DeleteScopedAgentBinding(frontendID, id string) error {
	frontendID = strings.TrimSpace(frontendID)
	id = strings.TrimSpace(id)
	if id == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	binding, ok := s.data.AgentBindings[id]
	if !ok || binding == nil || (frontendID != "" && binding.FrontendID != frontendID) {
		return nil
	}
	delete(s.data.AgentBindings, id)
	return s.saveLocked()
}

// GetGroupPrimary returns a group primary record by id without frontend filtering.
func (s *Store) GetGroupPrimary(id string) *GroupPrimary {
	return s.GetScopedGroupPrimary("", id)
}

// GetScopedGroupPrimary returns a group primary record by id when it belongs to frontendID.
func (s *Store) GetScopedGroupPrimary(frontendID, id string) *GroupPrimary {
	frontendID = strings.TrimSpace(frontendID)
	id = strings.TrimSpace(id)
	if id == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	primary, ok := s.data.GroupPrimaries[id]
	if !ok || primary == nil || (frontendID != "" && primary.FrontendID != frontendID) {
		return nil
	}
	return cloneGroupPrimary(primary)
}

// AllGroupPrimaries returns deep copies of all persisted group primary records.
func (s *Store) AllGroupPrimaries() []*GroupPrimary {
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneGroupPrimaries(s.data.GroupPrimaries)
}

// GroupPrimariesByChat returns primary records for one frontend and logical chat.
func (s *Store) GroupPrimariesByChat(frontendID, chatType, chatID string) []*GroupPrimary {
	frontendID = strings.TrimSpace(frontendID)
	chatType = strings.ToLower(strings.TrimSpace(chatType))
	chatID = strings.TrimSpace(chatID)
	if chatID == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneGroupPrimariesMatching(s.data.GroupPrimaries, func(primary *GroupPrimary) bool {
		if primary == nil || primary.FrontendID != frontendID || primary.ChatID != chatID {
			return false
		}
		return chatType == "" || primary.ChatType == chatType
	})
}

// UpsertGroupPrimary persists a local frontend's primary status for a group.
func (s *Store) UpsertGroupPrimary(primary *GroupPrimary) error {
	if primary == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := cloneGroupPrimary(primary)
	if cp == nil || cp.ID == "" {
		return nil
	}
	normalizeGroupPrimaryValues(cp)
	now := time.Now().Unix()
	if previous := s.data.GroupPrimaries[cp.ID]; previous != nil && cp.CreatedAt == 0 {
		cp.CreatedAt = previous.CreatedAt
	}
	for id, previous := range s.data.GroupPrimaries {
		if id == cp.ID || previous == nil {
			continue
		}
		if previous.FrontendID == cp.FrontendID && previous.ChatType == cp.ChatType && previous.ChatID == cp.ChatID {
			return fmt.Errorf("group primary already exists for frontend %q chat %q: %s", cp.FrontendID, cp.ChatID, id)
		}
	}
	if cp.CreatedAt == 0 {
		cp.CreatedAt = now
	}
	cp.UpdatedAt = now
	if s.data.GroupPrimaries == nil {
		s.data.GroupPrimaries = map[string]*GroupPrimary{}
	}
	s.data.GroupPrimaries[cp.ID] = cp
	return s.saveLocked()
}

// UpsertScopedGroupPrimary persists a group primary record owned by frontendID.
func (s *Store) UpsertScopedGroupPrimary(frontendID string, primary *GroupPrimary) error {
	if primary == nil {
		return nil
	}
	frontendID = strings.TrimSpace(frontendID)
	cp := cloneGroupPrimary(primary)
	if cp == nil {
		return nil
	}
	if strings.TrimSpace(cp.FrontendID) == "" {
		cp.FrontendID = frontendID
	}
	if frontendID != "" && strings.TrimSpace(cp.FrontendID) != frontendID {
		return fmt.Errorf("group primary frontend %q does not match scope %q", cp.FrontendID, frontendID)
	}
	return s.UpsertGroupPrimary(cp)
}

func (s *Store) GetSession(key string) *Session {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sess, ok := s.runtime.Sessions[key]; ok {
		return cloneSession(sess)
	}
	return nil
}

func (s *Store) UpsertSession(sess *Session) error {
	if sess == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := cloneSession(sess)
	if cp == nil {
		return nil
	}
	normalizeSessionValues(cp)
	cp.UpdatedAt = time.Now().Unix()
	s.runtime.Sessions[sess.Key] = cp
	s.syncPersistentSessionLocked(cp)
	return s.saveLocked()
}

func (s *Store) UpdateSession(key string, mutate func(*Session)) (*Session, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, os.ErrNotExist
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.runtime.Sessions[key]
	if !ok {
		return nil, os.ErrNotExist
	}
	if mutate != nil {
		mutate(sess)
	}
	normalizeSessionValues(sess)
	sess.UpdatedAt = time.Now().Unix()
	s.syncPersistentSessionLocked(sess)
	if err := s.saveLocked(); err != nil {
		return nil, err
	}
	return cloneSession(sess), nil
}

func (s *Store) CreateSubmission(sub *Submission) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := strings.TrimSpace(sub.ID)
	if id == "" {
		id = "sub-" + formatID(s.runtime.Counters.NextSubmission)
		s.runtime.Counters.NextSubmission++
	}
	now := time.Now().Unix()
	cp := cloneSubmission(sub)
	if cp == nil {
		return "", nil
	}
	cp.ID = id
	cp.CreatedAt = now
	cp.UpdatedAt = now
	normalizeSubmissionValues(cp)
	s.runtime.Submissions[id] = cp
	return id, nil
}

func (s *Store) GetSubmission(id string) *Submission {
	s.mu.Lock()
	defer s.mu.Unlock()
	sub, ok := s.runtime.Submissions[id]
	if !ok {
		return nil
	}
	return cloneSubmission(sub)
}

func (s *Store) UpdateSubmission(id string, mutate func(*Submission)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	sub, ok := s.runtime.Submissions[id]
	if !ok {
		return os.ErrNotExist
	}
	mutate(sub)
	normalizeSubmissionValues(sub)
	sub.UpdatedAt = time.Now().Unix()
	return nil
}

func (s *Store) DeleteSubmission(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.runtime.Submissions, strings.TrimSpace(id))
}

func (s *Store) QueueSubmission(sessionKey, submissionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess := s.ensureSessionLocked(sessionKey)
	if !slices.Contains(sess.Queue, submissionID) {
		sess.Queue = append(sess.Queue, submissionID)
	}
	sess.UpdatedAt = time.Now().Unix()
	slog.Debug("store queue submission",
		"session_key", sessionKey,
		"submission_id", submissionID,
		"queue_len", len(sess.Queue),
		"queue", sess.Queue,
		"active_turn_id", sess.ActiveTurnID,
		"active_submission_id", sess.ActiveSubmissionID,
	)
	s.syncPersistentSessionLocked(sess)
	return s.saveLocked()
}

func (s *Store) DequeueSubmission(sessionKey string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess := s.ensureSessionLocked(sessionKey)
	if len(sess.Queue) == 0 {
		return "", nil
	}
	next := sess.Queue[0]
	sess.Queue = append([]string(nil), sess.Queue[1:]...)
	sess.UpdatedAt = time.Now().Unix()
	slog.Debug("store dequeue submission",
		"session_key", sessionKey,
		"submission_id", next,
		"queue_len", len(sess.Queue),
		"queue", sess.Queue,
		"active_turn_id", sess.ActiveTurnID,
		"active_submission_id", sess.ActiveSubmissionID,
	)
	s.syncPersistentSessionLocked(sess)
	return next, s.saveLocked()
}

func (s *Store) PendingByID(id string) *PendingRequest {
	return s.PendingByScopedID("", id)
}

func (s *Store) PendingByScopedID(frontendID, id string) *PendingRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	req, ok := s.runtime.PendingRequests[pendingStoreKey(frontendID, id)]
	if !ok {
		return nil
	}
	cp := *req
	return &cp
}

func (s *Store) UpsertPending(req *PendingRequest) error {
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *req
	normalizePendingRequestValues(&cp)
	s.runtime.PendingRequests[pendingStoreKey(cp.FrontendID, cp.ID)] = &cp
	return nil
}

func (s *Store) UpdatePending(id string, mutate func(*PendingRequest)) error {
	return s.UpdateScopedPending("", id, mutate)
}

func (s *Store) UpdateScopedPending(frontendID, id string, mutate func(*PendingRequest)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	req, ok := s.runtime.PendingRequests[pendingStoreKey(frontendID, id)]
	if !ok {
		return os.ErrNotExist
	}
	mutate(req)
	normalizePendingRequestValues(req)
	return nil
}

func (s *Store) DeletePending(id string) {
	s.DeleteScopedPending("", id)
}

func (s *Store) DeleteScopedPending(frontendID, id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.runtime.PendingRequests, pendingStoreKey(frontendID, id))
}

func (s *Store) DeletePendingRequests(match func(*PendingRequest) bool) {
	if match == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, req := range s.runtime.PendingRequests {
		if req != nil && match(req) {
			delete(s.runtime.PendingRequests, id)
		}
	}
}

func (s *Store) AllSessions() []*Session {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*Session, 0, len(s.runtime.Sessions))
	for _, sess := range s.runtime.Sessions {
		out = append(out, cloneSession(sess))
	}
	return out
}

func (s *Store) FrontendCardNotifications(frontendID string) []FrontendCardNotification {
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneFrontendCardNotifications(s.data.FrontendCardNotifications[strings.TrimSpace(frontendID)])
}

func (s *Store) AppendFrontendCardNotification(frontendID string, note FrontendCardNotification) error {
	frontendID = strings.TrimSpace(frontendID)
	note, ok := normalizeFrontendCardNotification(note)
	if !ok {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data.FrontendCardNotifications == nil {
		s.data.FrontendCardNotifications = map[string][]FrontendCardNotification{}
	}
	existing := s.data.FrontendCardNotifications[frontendID]
	key := frontendCardNotificationKey(note)
	for i, candidate := range existing {
		if frontendCardNotificationKey(candidate) == key {
			candidate, _ = normalizeFrontendCardNotification(candidate)
			if candidate.Kind == note.Kind &&
				candidate.CollapseKey == note.CollapseKey &&
				candidate.Title == note.Title &&
				candidate.Color == note.Color &&
				candidate.Body == note.Body {
				return nil
			}
			if note.CreatedAt == 0 {
				note.CreatedAt = time.Now().Unix()
			}
			existing[i] = note
			s.data.FrontendCardNotifications[frontendID] = existing
			return s.saveLocked()
		}
	}
	if note.CreatedAt == 0 {
		note.CreatedAt = time.Now().Unix()
	}
	s.data.FrontendCardNotifications[frontendID] = append(existing, note)
	return s.saveLocked()
}

func (s *Store) DrainFrontendCardNotifications(frontendID string) ([]FrontendCardNotification, error) {
	frontendID = strings.TrimSpace(frontendID)
	s.mu.Lock()
	defer s.mu.Unlock()
	notes := cloneFrontendCardNotifications(s.data.FrontendCardNotifications[frontendID])
	if len(notes) == 0 {
		return nil, nil
	}
	delete(s.data.FrontendCardNotifications, frontendID)
	return notes, s.saveLocked()
}

func cloneFrontendCardNotifications(src []FrontendCardNotification) []FrontendCardNotification {
	if len(src) == 0 {
		return nil
	}
	dst := make([]FrontendCardNotification, 0, len(src))
	for _, note := range src {
		if normalized, ok := normalizeFrontendCardNotification(note); ok {
			dst = append(dst, normalized)
		}
	}
	if len(dst) == 0 {
		return nil
	}
	return dst
}

func cloneAgentBinding(binding *AgentBinding) *AgentBinding {
	if binding == nil {
		return nil
	}
	cp := *binding
	cp.PendingMessage = cloneAgentBindingPendingMessage(binding.PendingMessage)
	normalizeAgentBindingValues(&cp)
	return &cp
}

func cloneAgentBindingPendingMessage(msg *AgentBindingPendingMessage) *AgentBindingPendingMessage {
	if msg == nil {
		return nil
	}
	cp := *msg
	cp.Attachments = append([]AgentBindingPendingAttachment(nil), msg.Attachments...)
	cp.MergeForwardMessageIDs = append([]string(nil), msg.MergeForwardMessageIDs...)
	cp.MentionedOpenIDs = append([]string(nil), msg.MentionedOpenIDs...)
	return &cp
}

func cloneAgentBindings(src map[string]*AgentBinding) []*AgentBinding {
	return cloneAgentBindingsMatching(src, func(*AgentBinding) bool { return true })
}

func cloneAgentBindingsMatching(src map[string]*AgentBinding, match func(*AgentBinding) bool) []*AgentBinding {
	if len(src) == 0 {
		return nil
	}
	out := make([]*AgentBinding, 0, len(src))
	for _, binding := range src {
		if binding == nil || (match != nil && !match(binding)) {
			continue
		}
		out = append(out, cloneAgentBinding(binding))
	}
	slices.SortFunc(out, func(a, b *AgentBinding) int {
		return strings.Compare(a.ID, b.ID)
	})
	return out
}

func cloneGroupPrimary(primary *GroupPrimary) *GroupPrimary {
	if primary == nil {
		return nil
	}
	cp := *primary
	normalizeGroupPrimaryValues(&cp)
	return &cp
}

func cloneGroupPrimaries(src map[string]*GroupPrimary) []*GroupPrimary {
	return cloneGroupPrimariesMatching(src, func(*GroupPrimary) bool { return true })
}

func cloneGroupPrimariesMatching(src map[string]*GroupPrimary, match func(*GroupPrimary) bool) []*GroupPrimary {
	if len(src) == 0 {
		return nil
	}
	out := make([]*GroupPrimary, 0, len(src))
	for _, primary := range src {
		if primary == nil || (match != nil && !match(primary)) {
			continue
		}
		out = append(out, cloneGroupPrimary(primary))
	}
	slices.SortFunc(out, func(a, b *GroupPrimary) int {
		return strings.Compare(a.ID, b.ID)
	})
	return out
}

func normalizeAgentBindingValues(binding *AgentBinding) bool {
	if binding == nil {
		return false
	}
	before := *binding
	binding.ID = strings.TrimSpace(binding.ID)
	binding.FrontendID = strings.TrimSpace(binding.FrontendID)
	binding.ChatID = strings.TrimSpace(binding.ChatID)
	binding.ChatType = strings.ToLower(strings.TrimSpace(binding.ChatType))
	binding.Component = strings.ToLower(strings.TrimSpace(binding.Component))
	binding.WorkspaceID = strings.TrimSpace(binding.WorkspaceID)
	binding.ModelOverride = strings.TrimSpace(binding.ModelOverride)
	binding.ReasoningEffortOverride = strings.TrimSpace(binding.ReasoningEffortOverride)
	binding.ServiceTierOverride = normalizeStoredServiceTier(binding.ServiceTierOverride)
	binding.SandboxModeOverride = strings.TrimSpace(binding.SandboxModeOverride)
	binding.ApprovalPolicyOverride = strings.TrimSpace(binding.ApprovalPolicyOverride)
	binding.MultiAgentModeOverride = strings.TrimSpace(binding.MultiAgentModeOverride)
	binding.ClaudePermissionMode = strings.TrimSpace(binding.ClaudePermissionMode)
	binding.PendingMessage = normalizeAgentBindingPendingMessage(binding.PendingMessage)
	binding.Status = NormalizeAgentBindingStatus(binding.Status).String()
	if binding.UpdatedAt == 0 && binding.CreatedAt != 0 {
		binding.UpdatedAt = binding.CreatedAt
	}
	return before != *binding
}

func normalizeGroupPrimaryValues(primary *GroupPrimary) bool {
	if primary == nil {
		return false
	}
	before := *primary
	primary.ID = strings.TrimSpace(primary.ID)
	primary.FrontendID = strings.TrimSpace(primary.FrontendID)
	primary.ChatID = strings.TrimSpace(primary.ChatID)
	primary.ChatType = strings.ToLower(strings.TrimSpace(primary.ChatType))
	if primary.UpdatedAt == 0 && primary.CreatedAt != 0 {
		primary.UpdatedAt = primary.CreatedAt
	}
	return before != *primary
}

func normalizeAgentBindingPendingMessage(msg *AgentBindingPendingMessage) *AgentBindingPendingMessage {
	if msg == nil {
		return nil
	}
	cp := cloneAgentBindingPendingMessage(msg)
	cp.SessionKey = strings.TrimSpace(cp.SessionKey)
	cp.MessageID = strings.TrimSpace(cp.MessageID)
	cp.ChatID = strings.TrimSpace(cp.ChatID)
	cp.ChatType = strings.ToLower(strings.TrimSpace(cp.ChatType))
	cp.UserID = strings.TrimSpace(cp.UserID)
	cp.UserName = strings.TrimSpace(cp.UserName)
	cp.ChatName = strings.TrimSpace(cp.ChatName)
	cp.RootMessageID = strings.TrimSpace(cp.RootMessageID)
	cp.ParentMessageID = strings.TrimSpace(cp.ParentMessageID)
	cp.ThreadID = strings.TrimSpace(cp.ThreadID)
	cp.MergeForwardMessageIDs = normalizeStringSlice(cp.MergeForwardMessageIDs)
	cp.MentionedOpenIDs = normalizeStringSlice(cp.MentionedOpenIDs)
	attachments := make([]AgentBindingPendingAttachment, 0, len(cp.Attachments))
	for _, attachment := range cp.Attachments {
		attachment.Kind = strings.TrimSpace(attachment.Kind)
		attachment.ResourceKey = strings.TrimSpace(attachment.ResourceKey)
		attachment.SourceMessageID = strings.TrimSpace(attachment.SourceMessageID)
		if attachment.Kind == "" && attachment.ResourceKey == "" && attachment.SourceMessageID == "" {
			continue
		}
		attachments = append(attachments, attachment)
	}
	cp.Attachments = attachments
	if cp.MessageID == "" && strings.TrimSpace(cp.Text) == "" && len(cp.Attachments) == 0 && len(cp.MergeForwardMessageIDs) == 0 {
		return nil
	}
	return cp
}

func normalizeStringSlice(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		out = append(out, value)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func normalizeAgentBindings(src map[string]*AgentBinding) map[string]*AgentBinding {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string]*AgentBinding, len(src))
	for key, binding := range src {
		cp := cloneAgentBinding(binding)
		if cp == nil {
			continue
		}
		if cp.ID == "" {
			cp.ID = strings.TrimSpace(key)
		}
		if cp.ID == "" {
			continue
		}
		dst[cp.ID] = cp
	}
	if len(dst) == 0 {
		return nil
	}
	return dst
}

func normalizeGroupPrimaries(src map[string]*GroupPrimary) map[string]*GroupPrimary {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string]*GroupPrimary, len(src))
	for key, primary := range src {
		cp := cloneGroupPrimary(primary)
		if cp == nil {
			continue
		}
		if cp.ID == "" {
			cp.ID = strings.TrimSpace(key)
		}
		if cp.ID == "" {
			continue
		}
		dst[cp.ID] = cp
	}
	if len(dst) == 0 {
		return nil
	}
	return dst
}

func migrateGroupPrimariesFromBindings(dst map[string]*GroupPrimary, bindings map[string]*AgentBinding) {
	if dst == nil || len(bindings) == 0 {
		return
	}
	for _, binding := range bindings {
		if binding == nil || !binding.Primary || strings.TrimSpace(binding.ChatID) == "" {
			continue
		}
		id := strings.Join([]string{
			"primary",
			sanitizeStateIDPart(binding.FrontendID),
			sanitizeStateIDPart(binding.ChatType),
			sanitizeStateIDPart(binding.ChatID),
		}, "_")
		if _, exists := dst[id]; exists {
			continue
		}
		dst[id] = &GroupPrimary{
			ID:         id,
			FrontendID: strings.TrimSpace(binding.FrontendID),
			ChatID:     strings.TrimSpace(binding.ChatID),
			ChatType:   strings.ToLower(strings.TrimSpace(binding.ChatType)),
			Primary:    true,
			CreatedAt:  binding.CreatedAt,
			UpdatedAt:  binding.UpdatedAt,
		}
	}
}

func agentBindingsEqual(a, b map[string]*AgentBinding) bool {
	ab, err := json.Marshal(a)
	if err != nil {
		return false
	}
	bb, err := json.Marshal(b)
	if err != nil {
		return false
	}
	return string(ab) == string(bb)
}

func groupPrimariesEqual(a, b map[string]*GroupPrimary) bool {
	ab, err := json.Marshal(a)
	if err != nil {
		return false
	}
	bb, err := json.Marshal(b)
	if err != nil {
		return false
	}
	return string(ab) == string(bb)
}

func sanitizeStateIDPart(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "default"
	}
	var b strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "default"
	}
	return b.String()
}

func normalizeFrontendCardNotification(note FrontendCardNotification) (FrontendCardNotification, bool) {
	note.Kind = strings.TrimSpace(note.Kind)
	note.CollapseKey = strings.TrimSpace(note.CollapseKey)
	note.Title = strings.TrimSpace(note.Title)
	note.Color = strings.TrimSpace(note.Color)
	note.Body = strings.TrimSpace(note.Body)
	if note.Title == "" || note.Body == "" {
		return FrontendCardNotification{}, false
	}
	return note, true
}

func frontendCardNotificationKey(note FrontendCardNotification) string {
	note, ok := normalizeFrontendCardNotification(note)
	if !ok {
		return ""
	}
	if note.CollapseKey != "" {
		return strings.Join([]string{"collapse", note.CollapseKey}, "|")
	}
	return strings.Join([]string{note.Kind, note.Title, note.Color, note.Body}, "|")
}

func cloneSession(sess *Session) *Session {
	if sess == nil {
		return nil
	}
	cp := *sess
	normalizeSessionValues(&cp)
	cp.Queue = append([]string(nil), sess.Queue...)
	cp.ActiveOperations = append([]SessionActiveOperation(nil), sess.ActiveOperations...)
	cp.StagedImages = append([]SessionStagedImage(nil), sess.StagedImages...)
	cp.ActiveThreadCollaborationMode = cloneSessionCollaborationMode(sess.ActiveThreadCollaborationMode)
	cp.BackendThreads = cloneSessionBackendThreads(sess.BackendThreads)
	return &cp
}

func normalizeSessionValues(sess *Session) bool {
	if sess == nil {
		return false
	}
	before := *sess
	normalizedServiceTier := normalizeStoredServiceTier(sess.ActiveThreadServiceTier)
	changed := sess.ActiveThreadServiceTier != normalizedServiceTier
	sess.ActiveThreadServiceTier = normalizedServiceTier
	if chatType, chatID, rootMessageID, ok := sessionContextFromKey(sess.Key); ok {
		if strings.TrimSpace(sess.ChatType) == "" {
			sess.ChatType = chatType
		}
		if strings.TrimSpace(sess.ChatID) == "" {
			sess.ChatID = chatID
		}
		if strings.TrimSpace(sess.RootMessageID) == "" {
			sess.RootMessageID = rootMessageID
		}
	}
	normalizedStatus := NormalizeSessionStatus(sess.Status).String()
	if sess.Status != normalizedStatus {
		changed = true
	}
	sess.Status = normalizedStatus
	normalizedThreads := normalizeSessionBackendThreads(sess.BackendThreads)
	if !sessionBackendThreadsEqual(sess.BackendThreads, normalizedThreads) {
		changed = true
	}
	sess.BackendThreads = normalizedThreads
	normalizedCollaborationMode := normalizeSessionCollaborationMode(sess.ActiveThreadCollaborationMode)
	if !sessionCollaborationModeEqual(sess.ActiveThreadCollaborationMode, normalizedCollaborationMode) {
		changed = true
	}
	sess.ActiveThreadCollaborationMode = normalizedCollaborationMode
	return changed || before.ChatType != sess.ChatType || before.ChatID != sess.ChatID || before.RootMessageID != sess.RootMessageID || before.BindingID != sess.BindingID
}

func normalizeSubmissionValues(sub *Submission) {
	if sub == nil {
		return
	}
	sub.Status = NormalizeSubmissionStatus(sub.Status).String()
}

func normalizePendingRequestValues(req *PendingRequest) {
	if req == nil {
		return
	}
	req.Status = NormalizePendingRequestStatus(req.Status).String()
}

func storedSessionFromSession(sess *Session) *storedSession {
	if sess == nil {
		return nil
	}
	cp := cloneSession(sess)
	if cp == nil {
		return nil
	}
	normalizeSessionValues(cp)
	return &storedSession{
		Key:                           cp.Key,
		BindingID:                     cp.BindingID,
		WorkspaceID:                   cp.WorkspaceID,
		ActiveThreadID:                cp.ActiveThreadID,
		ActiveThreadWorkspaceID:       cp.ActiveThreadWorkspaceID,
		ActiveThreadApprovalPolicy:    cp.ActiveThreadApprovalPolicy,
		ActiveThreadSandboxMode:       cp.ActiveThreadSandboxMode,
		ActiveThreadMultiAgentMode:    cp.ActiveThreadMultiAgentMode,
		ActiveClaudePermissionMode:    cp.ActiveClaudePermissionMode,
		ActiveThreadServiceTier:       cp.ActiveThreadServiceTier,
		ActiveThreadCollaborationMode: cloneSessionCollaborationMode(cp.ActiveThreadCollaborationMode),
		ActiveThreadName:              cp.ActiveThreadName,
		ActiveThreadPreview:           cp.ActiveThreadPreview,
		BackendThreads:                cloneSessionBackendThreads(cp.BackendThreads),
		OwnerUserID:                   cp.OwnerUserID,
		ModelOverride:                 cp.ModelOverride,
		RecentWorkspaceIDs:            cloneStringSlice(cp.RecentWorkspaceIDs),
		UpdatedAt:                     cp.UpdatedAt,
	}
}

func sessionFromStored(sess *storedSession) *Session {
	if sess == nil {
		return nil
	}
	cp := &Session{
		Key:                           sess.Key,
		BindingID:                     strings.TrimSpace(sess.BindingID),
		WorkspaceID:                   sess.WorkspaceID,
		ActiveThreadID:                sess.ActiveThreadID,
		ActiveThreadWorkspaceID:       sess.ActiveThreadWorkspaceID,
		ActiveThreadApprovalPolicy:    sess.ActiveThreadApprovalPolicy,
		ActiveThreadSandboxMode:       sess.ActiveThreadSandboxMode,
		ActiveThreadMultiAgentMode:    sess.ActiveThreadMultiAgentMode,
		ActiveClaudePermissionMode:    sess.ActiveClaudePermissionMode,
		ActiveThreadServiceTier:       sess.ActiveThreadServiceTier,
		ActiveThreadCollaborationMode: cloneSessionCollaborationMode(sess.ActiveThreadCollaborationMode),
		ActiveThreadName:              sess.ActiveThreadName,
		ActiveThreadPreview:           sess.ActiveThreadPreview,
		BackendThreads:                cloneSessionBackendThreads(sess.BackendThreads),
		OwnerUserID:                   sess.OwnerUserID,
		ModelOverride:                 sess.ModelOverride,
		RecentWorkspaceIDs:            cloneStringSlice(sess.RecentWorkspaceIDs),
		Status:                        SessionStatusIdle.String(),
		UpdatedAt:                     sess.UpdatedAt,
	}
	if chatType, chatID, rootMessageID, ok := sessionContextFromKey(sess.Key); ok {
		cp.ChatType = chatType
		cp.ChatID = chatID
		cp.RootMessageID = rootMessageID
	}
	normalizeSessionValues(cp)
	return cp
}

func normalizeStoredSession(sess *storedSession) *storedSession {
	if sess == nil {
		return nil
	}
	cp := *sess
	cp.BindingID = strings.TrimSpace(cp.BindingID)
	cp.ActiveClaudePermissionMode = strings.TrimSpace(cp.ActiveClaudePermissionMode)
	cp.ActiveThreadServiceTier = normalizeStoredServiceTier(cp.ActiveThreadServiceTier)
	cp.ActiveThreadCollaborationMode = normalizeSessionCollaborationMode(cp.ActiveThreadCollaborationMode)
	cp.BackendThreads = normalizeSessionBackendThreads(cp.BackendThreads)
	return &cp
}

func cloneSessionBackendThreads(src map[string]SessionBackendThread) map[string]SessionBackendThread {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string]SessionBackendThread, len(src))
	for key, value := range src {
		dst[key] = normalizeSessionBackendThread(value)
	}
	return dst
}

func cloneStringSlice(src []string) []string {
	if len(src) == 0 {
		return nil
	}
	dst := make([]string, len(src))
	copy(dst, src)
	return dst
}

func cloneSessionCollaborationMode(src *SessionCollaborationMode) *SessionCollaborationMode {
	if src == nil {
		return nil
	}
	cp := *src
	if src.DeveloperInstructions != nil {
		value := *src.DeveloperInstructions
		cp.DeveloperInstructions = &value
	}
	return &cp
}

func normalizeSessionCollaborationMode(mode *SessionCollaborationMode) *SessionCollaborationMode {
	if mode == nil {
		return nil
	}
	cp := *mode
	cp.Mode = strings.TrimSpace(cp.Mode)
	cp.Model = strings.TrimSpace(cp.Model)
	cp.ReasoningEffort = strings.TrimSpace(cp.ReasoningEffort)
	if cp.DeveloperInstructions != nil {
		value := strings.TrimSpace(*cp.DeveloperInstructions)
		cp.DeveloperInstructions = &value
	}
	if cp.Mode == "" || cp.Model == "" {
		return nil
	}
	return &cp
}

func sessionCollaborationModeEqual(a, b *SessionCollaborationMode) bool {
	switch {
	case a == nil && b == nil:
		return true
	case a == nil || b == nil:
		return false
	}
	if a.Mode != b.Mode || a.Model != b.Model || a.ReasoningEffort != b.ReasoningEffort {
		return false
	}
	switch {
	case a.DeveloperInstructions == nil && b.DeveloperInstructions == nil:
		return true
	case a.DeveloperInstructions == nil || b.DeveloperInstructions == nil:
		return false
	default:
		return *a.DeveloperInstructions == *b.DeveloperInstructions
	}
}

func normalizeSessionBackendThread(thread SessionBackendThread) SessionBackendThread {
	thread.ThreadID = strings.TrimSpace(thread.ThreadID)
	thread.WorkspaceID = strings.TrimSpace(thread.WorkspaceID)
	thread.ApprovalPolicy = strings.TrimSpace(thread.ApprovalPolicy)
	thread.SandboxMode = strings.TrimSpace(thread.SandboxMode)
	thread.MultiAgentMode = strings.TrimSpace(thread.MultiAgentMode)
	thread.ClaudePermissionMode = strings.TrimSpace(thread.ClaudePermissionMode)
	thread.ServiceTier = normalizeStoredServiceTier(thread.ServiceTier)
	thread.CollaborationMode = normalizeSessionCollaborationMode(thread.CollaborationMode)
	thread.Name = strings.TrimSpace(thread.Name)
	thread.Preview = strings.TrimSpace(thread.Preview)
	return thread
}

func normalizeSessionBackendThreads(src map[string]SessionBackendThread) map[string]SessionBackendThread {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string]SessionBackendThread, len(src))
	for key, value := range src {
		key = strings.ToLower(strings.TrimSpace(key))
		if key == "" {
			continue
		}
		normalized := normalizeSessionBackendThread(value)
		if normalized == (SessionBackendThread{}) {
			continue
		}
		dst[key] = normalized
	}
	if len(dst) == 0 {
		return nil
	}
	return dst
}

func sessionBackendThreadsEqual(a, b map[string]SessionBackendThread) bool {
	if len(a) != len(b) {
		return false
	}
	for key, value := range a {
		if b[key] != value {
			return false
		}
	}
	return true
}

func storedSessionsEqual(a, b *storedSession) bool {
	ab, err := json.Marshal(a)
	if err != nil {
		return false
	}
	bb, err := json.Marshal(b)
	if err != nil {
		return false
	}
	return string(ab) == string(bb)
}

func sessionContextFromKey(key string) (chatType, chatID, rootMessageID string, ok bool) {
	key = strings.TrimSpace(key)
	parts := strings.Split(key, ":")
	if len(parts) < 4 || parts[0] != "feishu" {
		return "", "", "", false
	}
	offset := 1
	if len(parts) >= 6 && parts[1] == "frontend" {
		offset = 3
	}
	switch parts[offset] {
	case "group":
		if len(parts) <= offset+3 || strings.TrimSpace(parts[offset+1]) == "" {
			return "", "", "", false
		}
		switch parts[offset+2] {
		case "root":
			return "group", strings.TrimSpace(parts[offset+1]), strings.TrimSpace(parts[offset+3]), true
		default:
			return "", "", "", false
		}
	case "p2p":
		if len(parts) <= offset+2 || strings.TrimSpace(parts[offset+1]) == "" {
			return "", "", "", false
		}
		return "p2p", strings.TrimSpace(parts[offset+1]), "", true
	default:
		return "", "", "", false
	}
}

func normalizeStoredServiceTier(value string) string {
	if strings.EqualFold(strings.TrimSpace(value), "fast") {
		return "fast"
	}
	return ""
}

func cloneSubmission(sub *Submission) *Submission {
	if sub == nil {
		return nil
	}
	cp := *sub
	cp.SourceMessageIDs = append([]string(nil), sub.SourceMessageIDs...)
	cp.SourceRootMessageIDs = append([]string(nil), sub.SourceRootMessageIDs...)
	cp.Skills = append([]SubmissionSkill(nil), sub.Skills...)
	cp.Attachments = append([]SubmissionAttachment(nil), sub.Attachments...)
	return &cp
}

func (s *Store) AllPendingRequests() []*PendingRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*PendingRequest, 0, len(s.runtime.PendingRequests))
	for _, req := range s.runtime.PendingRequests {
		cp := *req
		out = append(out, &cp)
	}
	return out
}

func (s *Store) NextLocalID(prefix string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := strings.TrimSpace(prefix)
	if id == "" {
		id = "local"
	}
	value := fmt.Sprintf("%s-%s", id, formatID(s.runtime.Counters.NextLocalID))
	s.runtime.Counters.NextLocalID++
	return value, nil
}

func (s *Store) UpsertMessageLink(link *MessageLink) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if link == nil || strings.TrimSpace(link.MessageID) == "" {
		return nil
	}
	cp := *link
	s.runtime.MessageLinks[messageLinkStoreKey(cp.FrontendID, cp.MessageID)] = &cp
	return nil
}

func (s *Store) GetMessageLink(messageID string) *MessageLink {
	return s.GetScopedMessageLink("", messageID)
}

func (s *Store) GetScopedMessageLink(frontendID, messageID string) *MessageLink {
	s.mu.Lock()
	defer s.mu.Unlock()
	messageID = strings.TrimSpace(messageID)
	if messageID == "" {
		return nil
	}
	if link, ok := s.runtime.MessageLinks[messageLinkStoreKey(frontendID, messageID)]; ok {
		cp := *link
		return &cp
	}
	return nil
}

func (s *Store) DeleteMessageLinks(match func(*MessageLink) bool) {
	if match == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, link := range s.runtime.MessageLinks {
		if link != nil && match(link) {
			delete(s.runtime.MessageLinks, id)
		}
	}
}

func (s *Store) ensureSessionLocked(key string) *Session {
	if sess, ok := s.runtime.Sessions[key]; ok {
		return sess
	}
	if persisted, ok := s.data.Sessions[key]; ok {
		sess := sessionFromStored(persisted)
		s.runtime.Sessions[key] = sess
		return sess
	}
	sess := &Session{Key: key, Status: SessionStatusIdle.String(), UpdatedAt: time.Now().Unix()}
	s.runtime.Sessions[key] = sess
	s.syncPersistentSessionLocked(sess)
	return sess
}

func (s *Store) syncPersistentSessionLocked(sess *Session) {
	if sess == nil || strings.TrimSpace(sess.Key) == "" {
		return
	}
	persisted := storedSessionFromSession(sess)
	if persisted == nil {
		return
	}
	s.data.Sessions[sess.Key] = persisted
}

func (s *Store) saveLocked() error {
	s.data.Version = currentSnapshotVersion
	tmp := s.path + ".tmp"
	b, err := json.MarshalIndent(&s.data, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func formatID(v int64) string {
	return time.Now().UTC().Format("20060102T150405") + "-" + strconvFormat(v)
}

func strconvFormat(v int64) string {
	return fmt.Sprintf("%06d", v)
}

func pendingStoreKey(frontendID, id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return ""
	}
	frontendID = strings.TrimSpace(frontendID)
	if frontendID == "" {
		return id
	}
	return "frontend:" + frontendID + ":pending:" + id
}

func messageLinkStoreKey(frontendID, messageID string) string {
	messageID = strings.TrimSpace(messageID)
	if messageID == "" {
		return ""
	}
	frontendID = strings.TrimSpace(frontendID)
	if frontendID == "" {
		return messageID
	}
	return "frontend:" + frontendID + ":message:" + messageID
}
