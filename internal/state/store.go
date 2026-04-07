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

type Store struct {
	path string
	mu   sync.Mutex
	data Snapshot
}

type Snapshot struct {
	Sessions        map[string]*Session        `json:"sessions"`
	Submissions     map[string]*Submission     `json:"submissions"`
	PendingRequests map[string]*PendingRequest `json:"pending_requests"`
	MessageLinks    map[string]*MessageLink    `json:"message_links"`
	InboundDedup    map[string]int64           `json:"inbound_dedup"`
	Counters        Counters                   `json:"counters"`
}

type Counters struct {
	NextSubmission int64 `json:"next_submission"`
	NextLocalID    int64 `json:"next_local_id"`
}

type Session struct {
	Key                        string               `json:"key"`
	WorkspaceID                string               `json:"workspace_id"`
	ActiveThreadID             string               `json:"active_thread_id"`
	ActiveThreadWorkspaceID    string               `json:"active_thread_workspace_id"`
	ActiveThreadApprovalPolicy string               `json:"active_thread_approval_policy"`
	ActiveThreadSandboxMode    string               `json:"active_thread_sandbox_mode"`
	ActiveThreadName           string               `json:"active_thread_name"`
	ActiveThreadPreview        string               `json:"active_thread_preview"`
	ActiveTurnID               string               `json:"active_turn_id"`
	ActiveSubmissionID         string               `json:"active_submission_id"`
	OwnerUserID                string               `json:"owner_user_id"`
	ChatID                     string               `json:"chat_id"`
	ChatType                   string               `json:"chat_type"`
	RootMessageID              string               `json:"root_message_id"`
	ModelOverride              string               `json:"model_override"`
	Status                     string               `json:"status"`
	Queue                      []string             `json:"queue"`
	StagedImages               []SessionStagedImage `json:"staged_images,omitempty"`
	UpdatedAt                  int64                `json:"updated_at"`
}

type SessionStagedImage struct {
	SourceMessageID string `json:"source_message_id"`
	Name            string `json:"name"`
	LocalPath       string `json:"local_path"`
	CreatedAt       int64  `json:"created_at"`
}

type SubmissionAttachment struct {
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	LocalPath string `json:"local_path"`
}

type Submission struct {
	ID               string                 `json:"id"`
	SessionKey       string                 `json:"session_key"`
	WorkspaceID      string                 `json:"workspace_id"`
	ThreadID         string                 `json:"thread_id"`
	TurnID           string                 `json:"turn_id"`
	UserID           string                 `json:"user_id"`
	UserName         string                 `json:"user_name"`
	ChatID           string                 `json:"chat_id"`
	ChatName         string                 `json:"chat_name"`
	TriggerMessageID string                 `json:"trigger_message_id"`
	SourceMessageIDs []string               `json:"source_message_ids,omitempty"`
	StatusCardID     string                 `json:"status_card_id"`
	InputText        string                 `json:"input_text"`
	Attachments      []SubmissionAttachment `json:"attachments,omitempty"`
	Status           string                 `json:"status"`
	OutputText       string                 `json:"output_text"`
	SummaryText      string                 `json:"summary_text"`
	CommandText      string                 `json:"command_text"`
	PlanText         string                 `json:"plan_text"`
	FinalMessageIDs  []string               `json:"final_message_ids,omitempty"`
	Finalized        bool                   `json:"finalized"`
	CreatedAt        int64                  `json:"created_at"`
	UpdatedAt        int64                  `json:"updated_at"`
}

type PendingRequest struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"`
	SessionKey  string `json:"session_key"`
	ThreadID    string `json:"thread_id"`
	TurnID      string `json:"turn_id"`
	ItemID      string `json:"item_id"`
	OwnerUserID string `json:"owner_user_id"`
	FeishuMsgID string `json:"feishu_msg_id"`
	PayloadJSON string `json:"payload_json"`
	Status      string `json:"status"`
	CreatedAt   int64  `json:"created_at"`
	ExpiresAt   int64  `json:"expires_at"`
}

type MessageLink struct {
	MessageID    string `json:"message_id"`
	Kind         string `json:"kind"`
	SessionKey   string `json:"session_key,omitempty"`
	SubmissionID string `json:"submission_id,omitempty"`
	RequestID    string `json:"request_id,omitempty"`
	ThreadID     string `json:"thread_id,omitempty"`
	TurnID       string `json:"turn_id,omitempty"`
	CreatedAt    int64  `json:"created_at"`
}

func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	s := &Store{
		path: path,
		data: Snapshot{
			Sessions:        map[string]*Session{},
			Submissions:     map[string]*Submission{},
			PendingRequests: map[string]*PendingRequest{},
			MessageLinks:    map[string]*MessageLink{},
			InboundDedup:    map[string]int64{},
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
		return s, nil
	}
	if err := json.Unmarshal(b, &s.data); err != nil {
		return nil, err
	}
	if s.data.Sessions == nil {
		s.data.Sessions = map[string]*Session{}
	}
	if s.data.Submissions == nil {
		s.data.Submissions = map[string]*Submission{}
	}
	if s.data.PendingRequests == nil {
		s.data.PendingRequests = map[string]*PendingRequest{}
	}
	if s.data.MessageLinks == nil {
		s.data.MessageLinks = map[string]*MessageLink{}
	}
	if s.data.InboundDedup == nil {
		s.data.InboundDedup = map[string]int64{}
	}
	if s.data.Counters.NextSubmission <= 0 {
		s.data.Counters.NextSubmission = 1
	}
	if s.data.Counters.NextLocalID <= 0 {
		s.data.Counters.NextLocalID = 1
	}
	return s, nil
}

func (s *Store) GetSession(key string) *Session {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sess, ok := s.data.Sessions[key]; ok {
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
	s.data.Sessions[sess.Key] = cp
	return s.saveLocked()
}

func (s *Store) CreateSubmission(sub *Submission) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := sub.ID
	if id == "" {
		id = "sub-" + formatID(s.data.Counters.NextSubmission)
		s.data.Counters.NextSubmission++
	}
	now := time.Now().Unix()
	cp := cloneSubmission(sub)
	if cp == nil {
		return "", nil
	}
	cp.ID = id
	cp.CreatedAt = now
	cp.UpdatedAt = now
	s.data.Submissions[id] = cp
	return id, s.saveLocked()
}

func (s *Store) GetSubmission(id string) *Submission {
	s.mu.Lock()
	defer s.mu.Unlock()
	sub, ok := s.data.Submissions[id]
	if !ok {
		return nil
	}
	return cloneSubmission(sub)
}

func (s *Store) UpdateSubmission(id string, mutate func(*Submission)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	sub, ok := s.data.Submissions[id]
	if !ok {
		return os.ErrNotExist
	}
	mutate(sub)
	sub.UpdatedAt = time.Now().Unix()
	return s.saveLocked()
}

func (s *Store) QueueSubmission(sessionKey, submissionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess := s.ensureSessionLocked(sessionKey)
	if !slices.Contains(sess.Queue, submissionID) {
		sess.Queue = append(sess.Queue, submissionID)
	}
	sess.UpdatedAt = time.Now().Unix()
	slog.Info("store queue submission",
		"session_key", sessionKey,
		"submission_id", submissionID,
		"queue_len", len(sess.Queue),
		"queue", sess.Queue,
		"active_turn_id", sess.ActiveTurnID,
		"active_submission_id", sess.ActiveSubmissionID,
	)
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
	slog.Info("store dequeue submission",
		"session_key", sessionKey,
		"submission_id", next,
		"queue_len", len(sess.Queue),
		"queue", sess.Queue,
		"active_turn_id", sess.ActiveTurnID,
		"active_submission_id", sess.ActiveSubmissionID,
	)
	return next, s.saveLocked()
}

func (s *Store) PendingByID(id string) *PendingRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	req, ok := s.data.PendingRequests[id]
	if !ok {
		return nil
	}
	cp := *req
	return &cp
}

func (s *Store) UpsertPending(req *PendingRequest) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *req
	s.data.PendingRequests[req.ID] = &cp
	return s.saveLocked()
}

func (s *Store) UpdatePending(id string, mutate func(*PendingRequest)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	req, ok := s.data.PendingRequests[id]
	if !ok {
		return os.ErrNotExist
	}
	mutate(req)
	return s.saveLocked()
}

func (s *Store) AllSessions() []*Session {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*Session, 0, len(s.data.Sessions))
	for _, sess := range s.data.Sessions {
		out = append(out, cloneSession(sess))
	}
	return out
}

func cloneSession(sess *Session) *Session {
	if sess == nil {
		return nil
	}
	cp := *sess
	cp.Queue = append([]string(nil), sess.Queue...)
	cp.StagedImages = append([]SessionStagedImage(nil), sess.StagedImages...)
	return &cp
}

func cloneSubmission(sub *Submission) *Submission {
	if sub == nil {
		return nil
	}
	cp := *sub
	cp.SourceMessageIDs = append([]string(nil), sub.SourceMessageIDs...)
	cp.Attachments = append([]SubmissionAttachment(nil), sub.Attachments...)
	cp.FinalMessageIDs = append([]string(nil), sub.FinalMessageIDs...)
	return &cp
}

func (s *Store) AllPendingRequests() []*PendingRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*PendingRequest, 0, len(s.data.PendingRequests))
	for _, req := range s.data.PendingRequests {
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
	value := fmt.Sprintf("%s-%s", id, formatID(s.data.Counters.NextLocalID))
	s.data.Counters.NextLocalID++
	return value, s.saveLocked()
}

func (s *Store) UpsertMessageLink(link *MessageLink) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if link == nil || link.MessageID == "" {
		return nil
	}
	cp := *link
	if cp.CreatedAt == 0 {
		cp.CreatedAt = time.Now().Unix()
	}
	s.data.MessageLinks[cp.MessageID] = &cp
	return s.saveLocked()
}

func (s *Store) MarkInboundSeen(messageID string, seenAt int64) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if strings.TrimSpace(messageID) == "" {
		return false, nil
	}
	if _, exists := s.data.InboundDedup[messageID]; exists {
		return true, nil
	}
	if seenAt == 0 {
		seenAt = time.Now().Unix()
	}
	s.data.InboundDedup[messageID] = seenAt
	return false, s.saveLocked()
}

func (s *Store) CleanupInboundSeen(before int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	changed := false
	for id, ts := range s.data.InboundDedup {
		if ts < before {
			delete(s.data.InboundDedup, id)
			changed = true
		}
	}
	if !changed {
		return nil
	}
	return s.saveLocked()
}

func (s *Store) ensureSessionLocked(key string) *Session {
	if sess, ok := s.data.Sessions[key]; ok {
		return sess
	}
	sess := &Session{Key: key, Status: "idle", UpdatedAt: time.Now().Unix()}
	s.data.Sessions[key] = sess
	return sess
}

func (s *Store) saveLocked() error {
	tmp := s.path + ".tmp"
	b, err := json.MarshalIndent(&s.data, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
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
