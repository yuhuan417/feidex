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

const currentSnapshotVersion = 6

type Store struct {
	path    string
	mu      sync.Mutex
	data    Snapshot
	runtime runtimeState
}

type Snapshot struct {
	Version                   int                                   `json:"version"`
	Sessions                  map[string]*storedSession             `json:"sessions"`
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
	Key                        string                          `json:"key"`
	WorkspaceID                string                          `json:"workspace_id"`
	ActiveThreadID             string                          `json:"active_thread_id"`
	ActiveThreadWorkspaceID    string                          `json:"active_thread_workspace_id"`
	ActiveThreadApprovalPolicy string                          `json:"active_thread_approval_policy"`
	ActiveThreadSandboxMode    string                          `json:"active_thread_sandbox_mode"`
	ActiveClaudePermissionMode string                          `json:"active_claude_permission_mode,omitempty"`
	ActiveThreadServiceTier    string                          `json:"active_thread_service_tier,omitempty"`
	ActiveThreadName           string                          `json:"active_thread_name"`
	ActiveThreadPreview        string                          `json:"active_thread_preview"`
	BackendThreads             map[string]SessionBackendThread `json:"backend_threads,omitempty"`
	OwnerUserID                string                          `json:"owner_user_id"`
	ModelOverride              string                          `json:"model_override"`
	UpdatedAt                  int64                           `json:"updated_at"`
}

type FrontendCardNotification struct {
	Kind        string `json:"kind,omitempty"`
	CollapseKey string `json:"collapse_key,omitempty"`
	Title       string `json:"title"`
	Color       string `json:"color,omitempty"`
	Body        string `json:"body"`
	CreatedAt   int64  `json:"created_at,omitempty"`
}

type SessionBackendThread struct {
	ThreadID             string `json:"thread_id,omitempty"`
	WorkspaceID          string `json:"workspace_id,omitempty"`
	ApprovalPolicy       string `json:"approval_policy,omitempty"`
	SandboxMode          string `json:"sandbox_mode,omitempty"`
	ClaudePermissionMode string `json:"claude_permission_mode,omitempty"`
	ServiceTier          string `json:"service_tier,omitempty"`
	Name                 string `json:"name,omitempty"`
	Preview              string `json:"preview,omitempty"`
}

type Session struct {
	Key                        string                          `json:"key"`
	WorkspaceID                string                          `json:"workspace_id"`
	ActiveThreadID             string                          `json:"active_thread_id"`
	ActiveThreadWorkspaceID    string                          `json:"active_thread_workspace_id"`
	ActiveThreadApprovalPolicy string                          `json:"active_thread_approval_policy"`
	ActiveThreadSandboxMode    string                          `json:"active_thread_sandbox_mode"`
	ActiveClaudePermissionMode string                          `json:"active_claude_permission_mode,omitempty"`
	ActiveThreadServiceTier    string                          `json:"active_thread_service_tier,omitempty"`
	ActiveThreadName           string                          `json:"active_thread_name"`
	ActiveThreadPreview        string                          `json:"active_thread_preview"`
	BackendThreads             map[string]SessionBackendThread `json:"backend_threads,omitempty"`
	ActiveTurnID               string                          `json:"active_turn_id"`
	ActiveSubmissionID         string                          `json:"active_submission_id"`
	OwnerUserID                string                          `json:"owner_user_id"`
	ChatID                     string                          `json:"chat_id"`
	ChatType                   string                          `json:"chat_type"`
	RootMessageID              string                          `json:"root_message_id"`
	ModelOverride              string                          `json:"model_override"`
	Status                     string                          `json:"status"`
	Queue                      []string                        `json:"queue"`
	ActiveOperations           []SessionActiveOperation        `json:"active_operations,omitempty"`
	StagedImages               []SessionStagedImage            `json:"staged_images,omitempty"`
	UpdatedAt                  int64                           `json:"updated_at"`
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
	if err := json.Unmarshal(b, &s.data); err != nil {
		return nil, err
	}
	if s.data.Sessions == nil {
		s.data.Sessions = map[string]*storedSession{}
	}
	if s.data.FrontendCardNotifications == nil {
		s.data.FrontendCardNotifications = map[string][]FrontendCardNotification{}
	}
	rewrite := s.data.Version != currentSnapshotVersion
	s.data.Version = currentSnapshotVersion
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
	cp.BackendThreads = cloneSessionBackendThreads(sess.BackendThreads)
	return &cp
}

func normalizeSessionValues(sess *Session) bool {
	if sess == nil {
		return false
	}
	normalizedServiceTier := normalizeStoredServiceTier(sess.ActiveThreadServiceTier)
	changed := sess.ActiveThreadServiceTier != normalizedServiceTier
	sess.ActiveThreadServiceTier = normalizedServiceTier
	normalizedThreads := normalizeSessionBackendThreads(sess.BackendThreads)
	if !sessionBackendThreadsEqual(sess.BackendThreads, normalizedThreads) {
		changed = true
	}
	sess.BackendThreads = normalizedThreads
	return changed
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
		Key:                        cp.Key,
		WorkspaceID:                cp.WorkspaceID,
		ActiveThreadID:             cp.ActiveThreadID,
		ActiveThreadWorkspaceID:    cp.ActiveThreadWorkspaceID,
		ActiveThreadApprovalPolicy: cp.ActiveThreadApprovalPolicy,
		ActiveThreadSandboxMode:    cp.ActiveThreadSandboxMode,
		ActiveClaudePermissionMode: cp.ActiveClaudePermissionMode,
		ActiveThreadServiceTier:    cp.ActiveThreadServiceTier,
		ActiveThreadName:           cp.ActiveThreadName,
		ActiveThreadPreview:        cp.ActiveThreadPreview,
		BackendThreads:             cloneSessionBackendThreads(cp.BackendThreads),
		OwnerUserID:                cp.OwnerUserID,
		ModelOverride:              cp.ModelOverride,
		UpdatedAt:                  cp.UpdatedAt,
	}
}

func sessionFromStored(sess *storedSession) *Session {
	if sess == nil {
		return nil
	}
	cp := &Session{
		Key:                        sess.Key,
		WorkspaceID:                sess.WorkspaceID,
		ActiveThreadID:             sess.ActiveThreadID,
		ActiveThreadWorkspaceID:    sess.ActiveThreadWorkspaceID,
		ActiveThreadApprovalPolicy: sess.ActiveThreadApprovalPolicy,
		ActiveThreadSandboxMode:    sess.ActiveThreadSandboxMode,
		ActiveClaudePermissionMode: sess.ActiveClaudePermissionMode,
		ActiveThreadServiceTier:    sess.ActiveThreadServiceTier,
		ActiveThreadName:           sess.ActiveThreadName,
		ActiveThreadPreview:        sess.ActiveThreadPreview,
		BackendThreads:             cloneSessionBackendThreads(sess.BackendThreads),
		OwnerUserID:                sess.OwnerUserID,
		ModelOverride:              sess.ModelOverride,
		Status:                     "idle",
		UpdatedAt:                  sess.UpdatedAt,
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
	cp.ActiveClaudePermissionMode = strings.TrimSpace(cp.ActiveClaudePermissionMode)
	cp.ActiveThreadServiceTier = normalizeStoredServiceTier(cp.ActiveThreadServiceTier)
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

func normalizeSessionBackendThread(thread SessionBackendThread) SessionBackendThread {
	thread.ThreadID = strings.TrimSpace(thread.ThreadID)
	thread.WorkspaceID = strings.TrimSpace(thread.WorkspaceID)
	thread.ApprovalPolicy = strings.TrimSpace(thread.ApprovalPolicy)
	thread.SandboxMode = strings.TrimSpace(thread.SandboxMode)
	thread.ClaudePermissionMode = strings.TrimSpace(thread.ClaudePermissionMode)
	thread.ServiceTier = normalizeStoredServiceTier(thread.ServiceTier)
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
		if len(parts) <= offset+3 || strings.TrimSpace(parts[offset+1]) == "" || parts[offset+2] != "root" {
			return "", "", "", false
		}
		return "group", strings.TrimSpace(parts[offset+1]), strings.TrimSpace(parts[offset+3]), true
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
	sess := &Session{Key: key, Status: "idle", UpdatedAt: time.Now().Unix()}
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
