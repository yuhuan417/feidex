package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
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
	Counters        Counters                   `json:"counters"`
}

type Counters struct {
	NextSubmission int64 `json:"next_submission"`
}

type Session struct {
	Key                string   `json:"key"`
	WorkspaceID        string   `json:"workspace_id"`
	ActiveThreadID     string   `json:"active_thread_id"`
	ActiveTurnID       string   `json:"active_turn_id"`
	ActiveSubmissionID string   `json:"active_submission_id"`
	OwnerUserID        string   `json:"owner_user_id"`
	ChatID             string   `json:"chat_id"`
	ChatType           string   `json:"chat_type"`
	RootMessageID      string   `json:"root_message_id"`
	ModelOverride      string   `json:"model_override"`
	Status             string   `json:"status"`
	Queue              []string `json:"queue"`
	UpdatedAt          int64    `json:"updated_at"`
}

type Submission struct {
	ID               string `json:"id"`
	SessionKey       string `json:"session_key"`
	WorkspaceID      string `json:"workspace_id"`
	ThreadID         string `json:"thread_id"`
	TurnID           string `json:"turn_id"`
	UserID           string `json:"user_id"`
	UserName         string `json:"user_name"`
	ChatID           string `json:"chat_id"`
	ChatName         string `json:"chat_name"`
	TriggerMessageID string `json:"trigger_message_id"`
	StatusCardID     string `json:"status_card_id"`
	InputText        string `json:"input_text"`
	Status           string `json:"status"`
	OutputText       string `json:"output_text"`
	SummaryText      string `json:"summary_text"`
	CommandText      string `json:"command_text"`
	PlanText         string `json:"plan_text"`
	Finalized        bool   `json:"finalized"`
	CreatedAt        int64  `json:"created_at"`
	UpdatedAt        int64  `json:"updated_at"`
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
			Counters:        Counters{NextSubmission: 1},
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
	if s.data.Counters.NextSubmission <= 0 {
		s.data.Counters.NextSubmission = 1
	}
	return s, nil
}

func (s *Store) GetSession(key string) *Session {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sess, ok := s.data.Sessions[key]; ok {
		cp := *sess
		cp.Queue = append([]string(nil), sess.Queue...)
		return &cp
	}
	return nil
}

func (s *Store) UpsertSession(sess *Session) error {
	if sess == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *sess
	cp.UpdatedAt = time.Now().Unix()
	cp.Queue = append([]string(nil), sess.Queue...)
	s.data.Sessions[sess.Key] = &cp
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
	cp := *sub
	cp.ID = id
	cp.CreatedAt = now
	cp.UpdatedAt = now
	s.data.Submissions[id] = &cp
	return id, s.saveLocked()
}

func (s *Store) GetSubmission(id string) *Submission {
	s.mu.Lock()
	defer s.mu.Unlock()
	sub, ok := s.data.Submissions[id]
	if !ok {
		return nil
	}
	cp := *sub
	return &cp
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
		cp := *sess
		cp.Queue = append([]string(nil), sess.Queue...)
		out = append(out, &cp)
	}
	return out
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
