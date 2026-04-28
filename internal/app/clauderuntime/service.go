// Package clauderuntime provides the Claude runtime service extracted from the
// app god package. It manages Claude CLI sessions, turns, permissions, and
// interactive tool events.
package clauderuntime

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"feidex/internal/app/apputil"
	appdelivery "feidex/internal/app/delivery"
	apppendingforms "feidex/internal/app/pendingforms"
	appruntime "feidex/internal/app/runtime"
	appturn "feidex/internal/app/turn"
	"feidex/internal/claudecli"
	"feidex/internal/codexrpc"
	"feidex/internal/config"
	"feidex/internal/state"
)

// ---------------------------------------------------------------------------
// App interface — what the service needs from the host application
// ---------------------------------------------------------------------------

// App defines the narrow interface that the Claude runtime service requires
// from the host application. *App satisfies this via its accessor methods.
type App interface {
	// Store returns the state store.
	Store() *state.Store
}

// ---------------------------------------------------------------------------
// Exported state types — mirror the unexported app/ types so the service
// can work with them directly.
// ---------------------------------------------------------------------------

// SessionState holds the runtime state for a single Claude CLI session.
type SessionState struct {
	SessionKey  string
	WorkspaceID string
	Session     *claudecli.Session
	Ctx         context.Context
	Cancel      context.CancelFunc
	StartedAt   time.Time

	Mu                sync.Mutex
	SessionID         string
	ReadyCh           chan struct{}
	ReadyOnce         sync.Once
	CurrentTurnNumber int
	InterruptPending  bool
	Turns             map[int]*TurnState
	LastPlanFilePath  string
}

// TurnState holds the runtime state for a single turn within a session.
type TurnState struct {
	TurnNumber int
	TurnID     string
	Thinking   string

	LastAssistantText        string
	LastTextChunks           []appdelivery.SentReplyChunk
	DeliveredAnyText         bool
	SuppressFailedCompletion bool

	// SteerSubmissionID is set when this turn was created to send a steer
	// message. The steer submission shares this turn's conversation round
	// and is finalized together with the turn.
	SteerSubmissionID string
}

// PendingInteraction represents a pending interactive request (approval,
// question, plan feedback) waiting for user response.
type PendingInteraction struct {
	Kind                     string
	Session                  *SessionState
	Tool                     string
	SessionPermissionUpdates []map[string]any
	RespCh                   chan PendingResponse
}

// PendingResponse is the response sent back through a PendingInteraction channel.
type PendingResponse struct {
	Approval *claudecli.PermissionResponse
	Answers  map[string]string
	Feedback string
	Err      error
}

type LifecycleDeps struct {
	BindClaudeSessionThread func(sessionKey, turnID, threadID string)
	FinishTurn              func(threadID, turnID, status string)
	FinishSteerSubmission   func(submissionID, status string)
	FailClaudeSessionWork   func(sessionKey, threadID string, err error)
	FailBackendActiveWork   func(backend, sessionKey, threadID, message string)
}

type TurnStreamDeps struct {
	RecordTurnError                func(threadID, turnID, message string)
	CompleteTurnItem               func(ctx context.Context, threadID, turnID, itemID string, item map[string]any)
	PrepareTurnStreamQuietBoundary func(turnID string) (reuseMessageID string)
	PrepareTurnStreamQuietUpdate   func(sessionKey string, sub *state.Submission, threadID, itemID string, item map[string]any, workspaceCwd string) appturn.QuietWorkingCardOp
	MarkTurnStreamFinal            func(turnID string)
}

type UsageDeps struct {
	RecordClaudeThreadUsage       func(threadID string, usage claudecli.TurnUsage)
	RecordTurnTokenUsage          func(threadID, turnID string, usage codexrpc.ThreadTokenUsage)
	RecordTurnContextUsagePercent func(turnID string, percent float64)
	TurnFinalFooterLines          func(turnID string, completedAt time.Time) []string
}

type DeliveryDeps struct {
	ExecuteQuietWorkingCardOp func(ctx context.Context, sub *state.Submission, op appturn.QuietWorkingCardOp)
	UpdateOutputSegment       func(ctx context.Context, threadID, turnID, body, reuseMessageID string) ([]appdelivery.SentReplyChunk, bool)
	FinalizeOutputSegment     func(ctx context.Context, threadID, turnID, body string) bool
	SendFinalMessages         func(ctx context.Context, sub *state.Submission, text string, footerLines []string, inThread bool, reuseMessageIDs []string) []appdelivery.SentReplyChunk
	ReplyInThread             func(sub *state.Submission) bool
}

type InteractiveDeps struct {
	SendClaudeApprovalCard      func(kind, requestID, sessionKey string, sub *state.Submission, threadID, turnID, itemID, body string, requestPayload map[string]any, sessionActionLabel string) error
	SendClaudeUserInputCard     func(requestID, sessionKey string, sub *state.Submission, payload apppendingforms.ToolUserInputPayload) error
	SendClaudeUserInputFormCard func(requestID, sessionKey string, sub *state.Submission, payload apppendingforms.ToolUserInputPayload) error
	SendClaudePlanModeCard      func(requestID, sessionKey string, sub *state.Submission, threadID, turnID, body string) error
}

type LookupDeps struct {
	FindSubmissionByTurn func(threadID, turnID string) (string, *state.Submission)
	GetSession           func(sessionKey string) *state.Session
	SessionHasActiveOps  func(sess *state.Session) bool
	NextLocalID          func(prefix string) (string, error)
	WorkspaceCwd         func(workspaceID string) string
}

type PermissionDeps struct {
	EffectivePermissionMode func(sess *state.Session, ws *config.Workspace, cfg config.ClaudeConfig) string
	QuietWorkingCardEnabled func() bool
}

type Deps struct {
	App         App
	Cfg         config.ClaudeConfig
	Lifecycle   LifecycleDeps
	TurnStream  TurnStreamDeps
	Usage       UsageDeps
	Delivery    DeliveryDeps
	Interactive InteractiveDeps
	Lookup      LookupDeps
	Permission  PermissionDeps
}

// Service provides Claude CLI session management. All exported methods
// satisfy the appcore.ClaudeCore interface.
type Service struct {
	App  App
	Cfg  config.ClaudeConfig
	deps Deps

	mu       sync.Mutex
	sessions map[string]*SessionState
	pending  map[string]*PendingInteraction
}

// NewService creates a new Service.
func NewService(deps Deps) *Service {
	return &Service{
		App:      deps.App,
		Cfg:      deps.Cfg,
		deps:     deps,
		sessions: map[string]*SessionState{},
		pending:  map[string]*PendingInteraction{},
	}
}

func (s *Service) BindClaudeSessionThread(sessionKey, turnID, threadID string) {
	if s != nil && s.deps.Lifecycle.BindClaudeSessionThread != nil {
		s.deps.Lifecycle.BindClaudeSessionThread(sessionKey, turnID, threadID)
	}
}

func (s *Service) FinishTurn(threadID, turnID, status string) {
	if s != nil && s.deps.Lifecycle.FinishTurn != nil {
		s.deps.Lifecycle.FinishTurn(threadID, turnID, status)
	}
}

func (s *Service) FinishSteerSubmission(submissionID, status string) {
	if s != nil && s.deps.Lifecycle.FinishSteerSubmission != nil {
		s.deps.Lifecycle.FinishSteerSubmission(submissionID, status)
	}
}

func (s *Service) FailClaudeSessionWork(sessionKey, threadID string, err error) {
	if s != nil && s.deps.Lifecycle.FailClaudeSessionWork != nil {
		s.deps.Lifecycle.FailClaudeSessionWork(sessionKey, threadID, err)
	}
}

func (s *Service) FailBackendActiveWork(backend, sessionKey, threadID, message string) {
	if s != nil && s.deps.Lifecycle.FailBackendActiveWork != nil {
		s.deps.Lifecycle.FailBackendActiveWork(backend, sessionKey, threadID, message)
	}
}

func (s *Service) RecordTurnError(threadID, turnID, message string) {
	if s != nil && s.deps.TurnStream.RecordTurnError != nil {
		s.deps.TurnStream.RecordTurnError(threadID, turnID, message)
	}
}

func (s *Service) CompleteTurnItem(ctx context.Context, threadID, turnID, itemID string, item map[string]any) {
	if s != nil && s.deps.TurnStream.CompleteTurnItem != nil {
		s.deps.TurnStream.CompleteTurnItem(ctx, threadID, turnID, itemID, item)
	}
}

func (s *Service) PrepareTurnStreamQuietBoundary(turnID string) string {
	if s == nil || s.deps.TurnStream.PrepareTurnStreamQuietBoundary == nil {
		return ""
	}
	return s.deps.TurnStream.PrepareTurnStreamQuietBoundary(turnID)
}

func (s *Service) PrepareTurnStreamQuietUpdate(sessionKey string, sub *state.Submission, threadID, itemID string, item map[string]any, workspaceCwd string) appturn.QuietWorkingCardOp {
	if s == nil || s.deps.TurnStream.PrepareTurnStreamQuietUpdate == nil {
		return appturn.QuietWorkingCardOp{}
	}
	return s.deps.TurnStream.PrepareTurnStreamQuietUpdate(sessionKey, sub, threadID, itemID, item, workspaceCwd)
}

func (s *Service) MarkTurnStreamFinal(turnID string) {
	if s != nil && s.deps.TurnStream.MarkTurnStreamFinal != nil {
		s.deps.TurnStream.MarkTurnStreamFinal(turnID)
	}
}

func (s *Service) RecordClaudeThreadUsage(threadID string, usage claudecli.TurnUsage) {
	if s != nil && s.deps.Usage.RecordClaudeThreadUsage != nil {
		s.deps.Usage.RecordClaudeThreadUsage(threadID, usage)
	}
}

func (s *Service) RecordTurnTokenUsage(threadID, turnID string, usage codexrpc.ThreadTokenUsage) {
	if s != nil && s.deps.Usage.RecordTurnTokenUsage != nil {
		s.deps.Usage.RecordTurnTokenUsage(threadID, turnID, usage)
	}
}

func (s *Service) RecordTurnContextUsagePercent(turnID string, percent float64) {
	if s != nil && s.deps.Usage.RecordTurnContextUsagePercent != nil {
		s.deps.Usage.RecordTurnContextUsagePercent(turnID, percent)
	}
}

func (s *Service) TurnFinalFooterLines(turnID string, completedAt time.Time) []string {
	if s == nil || s.deps.Usage.TurnFinalFooterLines == nil {
		return nil
	}
	return s.deps.Usage.TurnFinalFooterLines(turnID, completedAt)
}

func (s *Service) ExecuteQuietWorkingCardOp(ctx context.Context, sub *state.Submission, op appturn.QuietWorkingCardOp) {
	if s != nil && s.deps.Delivery.ExecuteQuietWorkingCardOp != nil {
		s.deps.Delivery.ExecuteQuietWorkingCardOp(ctx, sub, op)
	}
}

func (s *Service) UpdateOutputSegment(ctx context.Context, threadID, turnID, body, reuseMessageID string) ([]appdelivery.SentReplyChunk, bool) {
	if s == nil || s.deps.Delivery.UpdateOutputSegment == nil {
		return nil, false
	}
	return s.deps.Delivery.UpdateOutputSegment(ctx, threadID, turnID, body, reuseMessageID)
}

func (s *Service) FinalizeOutputSegment(ctx context.Context, threadID, turnID, body string) bool {
	if s == nil || s.deps.Delivery.FinalizeOutputSegment == nil {
		return false
	}
	return s.deps.Delivery.FinalizeOutputSegment(ctx, threadID, turnID, body)
}

func (s *Service) SendFinalMessages(ctx context.Context, sub *state.Submission, text string, footerLines []string, inThread bool, reuseMessageIDs []string) []appdelivery.SentReplyChunk {
	if s == nil || s.deps.Delivery.SendFinalMessages == nil {
		return nil
	}
	return s.deps.Delivery.SendFinalMessages(ctx, sub, text, footerLines, inThread, reuseMessageIDs)
}

func (s *Service) ReplyInThread(sub *state.Submission) bool {
	if s == nil || s.deps.Delivery.ReplyInThread == nil {
		return false
	}
	return s.deps.Delivery.ReplyInThread(sub)
}

func (s *Service) SendClaudeApprovalCard(kind, requestID, sessionKey string, sub *state.Submission, threadID, turnID, itemID, body string, requestPayload map[string]any, sessionActionLabel string) error {
	if s == nil || s.deps.Interactive.SendClaudeApprovalCard == nil {
		return fmt.Errorf("approval card delivery unavailable")
	}
	return s.deps.Interactive.SendClaudeApprovalCard(kind, requestID, sessionKey, sub, threadID, turnID, itemID, body, requestPayload, sessionActionLabel)
}

func (s *Service) SendClaudeUserInputCard(requestID, sessionKey string, sub *state.Submission, payload apppendingforms.ToolUserInputPayload) error {
	if s == nil || s.deps.Interactive.SendClaudeUserInputCard == nil {
		return fmt.Errorf("question card delivery unavailable")
	}
	return s.deps.Interactive.SendClaudeUserInputCard(requestID, sessionKey, sub, payload)
}

func (s *Service) SendClaudeUserInputFormCard(requestID, sessionKey string, sub *state.Submission, payload apppendingforms.ToolUserInputPayload) error {
	if s == nil || s.deps.Interactive.SendClaudeUserInputFormCard == nil {
		return fmt.Errorf("question card delivery unavailable")
	}
	return s.deps.Interactive.SendClaudeUserInputFormCard(requestID, sessionKey, sub, payload)
}

func (s *Service) SendClaudePlanModeCard(requestID, sessionKey string, sub *state.Submission, threadID, turnID, body string) error {
	if s == nil || s.deps.Interactive.SendClaudePlanModeCard == nil {
		return fmt.Errorf("plan card delivery unavailable")
	}
	return s.deps.Interactive.SendClaudePlanModeCard(requestID, sessionKey, sub, threadID, turnID, body)
}

func (s *Service) FindSubmissionByTurn(threadID, turnID string) (string, *state.Submission) {
	if s == nil || s.deps.Lookup.FindSubmissionByTurn == nil {
		return "", nil
	}
	return s.deps.Lookup.FindSubmissionByTurn(threadID, turnID)
}

func (s *Service) GetSession(sessionKey string) *state.Session {
	if s == nil || s.deps.Lookup.GetSession == nil {
		return nil
	}
	return s.deps.Lookup.GetSession(sessionKey)
}

func (s *Service) SessionHasActiveOps(sess *state.Session) bool {
	if s == nil || s.deps.Lookup.SessionHasActiveOps == nil {
		return false
	}
	return s.deps.Lookup.SessionHasActiveOps(sess)
}

func (s *Service) NextLocalID(prefix string) (string, error) {
	if s == nil || s.deps.Lookup.NextLocalID == nil {
		return "", nil
	}
	return s.deps.Lookup.NextLocalID(prefix)
}

func (s *Service) WorkspaceCwd(workspaceID string) string {
	if s == nil || s.deps.Lookup.WorkspaceCwd == nil {
		return ""
	}
	return s.deps.Lookup.WorkspaceCwd(workspaceID)
}

func (s *Service) EffectivePermissionMode(sess *state.Session, ws *config.Workspace, cfg config.ClaudeConfig) string {
	if s == nil || s.deps.Permission.EffectivePermissionMode == nil {
		return ""
	}
	return s.deps.Permission.EffectivePermissionMode(sess, ws, cfg)
}

func (s *Service) QuietWorkingCardEnabled() bool {
	if s == nil || s.deps.Permission.QuietWorkingCardEnabled == nil {
		return false
	}
	return s.deps.Permission.QuietWorkingCardEnabled()
}

// ---------------------------------------------------------------------------
// Exported methods — satisfy appcore.ClaudeCore
// ---------------------------------------------------------------------------

// UpdateConfig updates the Claude configuration.
func (s *Service) UpdateConfig(cfg config.ClaudeConfig) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.Cfg = cfg
	s.mu.Unlock()
}

// EnsureSession returns an existing session for the given key or starts a new one.
func (s *Service) EnsureSession(ctx context.Context, sessionKey string, ws *config.Workspace, resumeID, model string) (string, error) {
	if s == nil {
		return "", fmt.Errorf("claude runtime not initialized")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return "", fmt.Errorf("missing session key")
	}
	if ws == nil {
		return "", fmt.Errorf("workspace not found")
	}
	s.mu.Lock()
	runtimeCfg := s.Cfg
	s.mu.Unlock()
	resumeID = strings.TrimSpace(resumeID)
	model = strings.TrimSpace(apputil.FirstNonEmpty(model, strings.TrimSpace(ws.Model), strings.TrimSpace(runtimeCfg.Model)))

	s.mu.Lock()
	current := s.sessions[sessionKey]
	if current != nil && current.WorkspaceID == ws.ID {
		current.Mu.Lock()
		currentID := strings.TrimSpace(current.SessionID)
		current.Mu.Unlock()
		currentStopped := current.Session != nil && current.Session.Stopped()
		currentExitErr := error(nil)
		if current.Session != nil {
			currentExitErr = current.Session.ExitError()
		}
		s.mu.Unlock()
		if currentStopped {
			slog.Warn("discarding stopped Claude session before ensure",
				"session_key", sessionKey,
				"workspace_id", ws.ID,
				"resume_id", resumeID,
				"session_id", currentID,
				"exit_error", currentExitErr,
			)
		} else {
			switch {
			case resumeID == "":
				return currentID, nil
			case resumeID != "" && currentID == resumeID:
				return currentID, nil
			}
		}
	} else {
		s.mu.Unlock()
	}

	_ = s.ResetSession(sessionKey)
	return s.startSession(ctx, sessionKey, ws, runtimeCfg, model, resumeID, false)
}

// ForkSession starts a new session forked from an existing one.
func (s *Service) ForkSession(ctx context.Context, sessionKey string, ws *config.Workspace, sourceSessionID, model string) (string, error) {
	if s == nil {
		return "", fmt.Errorf("claude runtime not initialized")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return "", fmt.Errorf("missing session key")
	}
	if ws == nil {
		return "", fmt.Errorf("workspace not found")
	}
	sourceSessionID = strings.TrimSpace(sourceSessionID)
	if sourceSessionID == "" {
		return "", fmt.Errorf("missing Claude source session id")
	}
	s.mu.Lock()
	runtimeCfg := s.Cfg
	s.mu.Unlock()
	model = strings.TrimSpace(apputil.FirstNonEmpty(model, strings.TrimSpace(ws.Model), strings.TrimSpace(runtimeCfg.Model)))

	_ = s.ResetSession(sessionKey)
	forkedID, err := s.startSession(ctx, sessionKey, ws, runtimeCfg, model, sourceSessionID, true)
	if err != nil {
		return "", err
	}
	if forkedID != "" && forkedID == sourceSessionID {
		_ = s.ResetSession(sessionKey)
		return "", fmt.Errorf("Claude fork did not create a new session")
	}
	return forkedID, nil
}

// StartTurn sends a prompt to the named session and waits for the turn to be ready.
func (s *Service) StartTurn(ctx context.Context, sessionKey, threadID, turnID, prompt string) error {
	state, err := s.sessionState(sessionKey)
	if err != nil {
		return err
	}
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return fmt.Errorf("empty Claude prompt")
	}
	turnNumber, err := state.Session.SendMessage(ctx, prompt)
	if err != nil {
		return err
	}
	state.Mu.Lock()
	turn := state.Turns[turnNumber]
	if turn == nil {
		turn = &TurnState{TurnNumber: turnNumber}
		state.Turns[turnNumber] = turn
	}
	turn.TurnID = strings.TrimSpace(turnID)
	turn.SuppressFailedCompletion = true
	state.Mu.Unlock()
	if err := s.waitForTurnReady(ctx, state, threadID); err != nil {
		return err
	}
	state.Mu.Lock()
	if turn := state.Turns[turnNumber]; turn != nil {
		turn.SuppressFailedCompletion = false
	}
	state.Mu.Unlock()
	return nil
}

// StartSteerTurn sends a steer message into the current conversation without
// creating a separate CLI turn. The message is written to the CLI's stdin and
// processed as part of the current conversation round. steerSubmissionID is
// recorded so that handleTurnComplete can finalize the steer submission when
// the turn finishes.
func (s *Service) StartSteerTurn(ctx context.Context, sessionKey, threadID, turnID, prompt, steerSubmissionID string) error {
	state, err := s.sessionState(sessionKey)
	if err != nil {
		return err
	}
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return fmt.Errorf("empty Claude steer prompt")
	}
	if err := state.Session.SendSteerInput(ctx, prompt); err != nil {
		return err
	}
	// Record the steer submission ID on the current turn so that
	// handleTurnComplete can finalize it together with the turn.
	state.Mu.Lock()
	if turn := state.Turns[state.CurrentTurnNumber]; turn != nil {
		turn.SteerSubmissionID = strings.TrimSpace(steerSubmissionID)
	}
	state.Mu.Unlock()
	return nil
}

// Interrupt interrupts the current turn for the named session.
func (s *Service) Interrupt(ctx context.Context, sessionKey string) error {
	state, err := s.sessionState(sessionKey)
	if err != nil {
		return err
	}
	state.Mu.Lock()
	state.InterruptPending = true
	state.Mu.Unlock()
	return state.Session.Interrupt(ctx)
}

// SetModel hot-applies a model change to the named session.
func (s *Service) SetModel(ctx context.Context, sessionKey, model string) (bool, error) {
	if s == nil {
		return false, fmt.Errorf("claude runtime not initialized")
	}
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return false, fmt.Errorf("missing session key")
	}
	state, err := s.sessionState(sessionKey)
	if err != nil {
		slog.Debug("skip Claude model hot apply; runtime session not initialized",
			"session_key", sessionKey,
			"model", strings.TrimSpace(model),
		)
		return false, nil
	}
	if state.Session == nil {
		slog.Debug("skip Claude model hot apply; session handle missing",
			"session_key", sessionKey,
			"model", strings.TrimSpace(model),
		)
		return false, nil
	}
	return true, state.Session.SetModel(ctx, strings.TrimSpace(model))
}

// SetEffort hot-applies an effort change to the named session.
func (s *Service) SetEffort(ctx context.Context, sessionKey, effort string) (bool, error) {
	if s == nil {
		return false, fmt.Errorf("claude runtime not initialized")
	}
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return false, fmt.Errorf("missing session key")
	}
	state, err := s.sessionState(sessionKey)
	if err != nil {
		slog.Debug("skip Claude effort hot apply; runtime session not initialized",
			"session_key", sessionKey,
			"effort", strings.TrimSpace(effort),
		)
		return false, nil
	}
	if state.Session == nil {
		slog.Debug("skip Claude effort hot apply; session handle missing",
			"session_key", sessionKey,
			"effort", strings.TrimSpace(effort),
		)
		return false, nil
	}
	return true, state.Session.SetEffort(ctx, strings.TrimSpace(effort))
}

// SetPermissionMode hot-applies a permission mode change to the named session.
func (s *Service) SetPermissionMode(ctx context.Context, sessionKey, mode string) error {
	if s == nil {
		return fmt.Errorf("claude runtime not initialized")
	}
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return fmt.Errorf("missing session key")
	}
	state, err := s.sessionState(sessionKey)
	if err != nil {
		slog.Debug("skip Claude permission mode hot apply; runtime session not initialized",
			"session_key", sessionKey,
			"mode", strings.TrimSpace(mode),
		)
		return nil
	}
	if state.Session == nil {
		slog.Debug("skip Claude permission mode hot apply; session handle missing",
			"session_key", sessionKey,
			"mode", strings.TrimSpace(mode),
		)
		return nil
	}
	return state.Session.SetPermissionMode(ctx, PermissionModeValue(mode))
}

// ResetSession stops and removes the named session.
func (s *Service) ResetSession(sessionKey string) error {
	sessionKey = strings.TrimSpace(sessionKey)
	s.mu.Lock()
	state := s.sessions[sessionKey]
	delete(s.sessions, sessionKey)
	for requestID, pending := range s.pending {
		if pending == nil || pending.Session == nil {
			continue
		}
		if strings.TrimSpace(pending.Session.SessionKey) == sessionKey {
			delete(s.pending, requestID)
			select {
			case pending.RespCh <- PendingResponse{Err: errors.New("session reset")}:
			default:
			}
		}
	}
	s.mu.Unlock()
	if state == nil {
		return nil
	}
	state.Cancel()
	return state.Session.Stop()
}

// ResolveApproval resolves a pending permission approval request.
func (s *Service) ResolveApproval(requestID string, resolution appruntime.ClaudeApprovalResolution) error {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return fmt.Errorf("missing request id")
	}
	pending := s.takePending(requestID)
	if pending == nil {
		return fmt.Errorf("approval request %q not found", requestID)
	}
	resp := &claudecli.PermissionResponse{}
	switch strings.TrimSpace(resolution.Behavior) {
	case "allow":
		resp.Behavior = claudecli.PermissionAllow
		resp.UpdatedPermissions = CopyPermissionUpdates(resolution.UpdatedPermissions)
		if strings.TrimSpace(resolution.Scope) == "session" && len(resp.UpdatedPermissions) == 0 {
			resp.UpdatedPermissions = CopyPermissionUpdates(pending.SessionPermissionUpdates)
		}
	default:
		resp.Behavior = claudecli.PermissionDeny
		resp.Message = apputil.FirstNonEmpty(strings.TrimSpace(resolution.Message), "Declined by user")
		resp.Interrupt = resolution.Interrupt
	}
	pending.RespCh <- PendingResponse{Approval: resp}
	return nil
}

// ResolveUserInput resolves a pending user input request.
func (s *Service) ResolveUserInput(requestID string, answers map[string]string) error {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return fmt.Errorf("missing request id")
	}
	pending := s.takePending(requestID)
	if pending == nil {
		return fmt.Errorf("user input request %q not found", requestID)
	}
	pending.RespCh <- PendingResponse{Answers: answers}
	return nil
}

// ResolvePlanFeedback resolves a pending plan feedback request.
func (s *Service) ResolvePlanFeedback(requestID, feedback string) error {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return fmt.Errorf("missing request id")
	}
	pending := s.takePending(requestID)
	if pending == nil {
		return fmt.Errorf("plan request %q not found", requestID)
	}
	pending.RespCh <- PendingResponse{Feedback: strings.TrimSpace(feedback)}
	return nil
}

// CancelPending cancels a pending interactive request.
func (s *Service) CancelPending(requestID, message string) error {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return fmt.Errorf("missing request id")
	}
	pending := s.takePending(requestID)
	if pending == nil {
		return fmt.Errorf("pending request %q not found", requestID)
	}
	message = apputil.FirstNonEmpty(strings.TrimSpace(message), "cancelled by user")
	pending.RespCh <- PendingResponse{Err: errors.New(message)}
	return nil
}

// SessionStopped reports whether the named session has stopped.
func (s *Service) SessionStopped(sessionKey string) bool {
	if s == nil {
		return true
	}
	s.mu.Lock()
	state := s.sessions[strings.TrimSpace(sessionKey)]
	s.mu.Unlock()
	if state == nil || state.Session == nil {
		return true
	}
	return state.Session.Stopped()
}

// Close stops all active sessions.
func (s *Service) Close() error {
	s.mu.Lock()
	keys := make([]string, 0, len(s.sessions))
	for key := range s.sessions {
		keys = append(keys, key)
	}
	s.mu.Unlock()
	var firstErr error
	for _, key := range keys {
		if err := s.ResetSession(key); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// ---------------------------------------------------------------------------
// Exported accessors — for test and wrapper compatibility
// ---------------------------------------------------------------------------

// SessionState returns the session state for the given key.
func (s *Service) SessionState(sessionKey string) (*SessionState, error) {
	return s.sessionState(sessionKey)
}

// HandleTurnComplete handles a turn-complete event. Exported for test access.
func (s *Service) HandleTurnComplete(state *SessionState, event claudecli.TurnCompleteEvent) {
	s.handleTurnComplete(state, event)
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

func (s *Service) sessionState(sessionKey string) (*SessionState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.sessions[strings.TrimSpace(sessionKey)]
	if state == nil {
		return nil, fmt.Errorf("claude session %q not initialized", sessionKey)
	}
	return state, nil
}

func (s *Service) takePending(requestID string) *PendingInteraction {
	s.mu.Lock()
	defer s.mu.Unlock()
	pending := s.pending[requestID]
	delete(s.pending, requestID)
	return pending
}

func (s *Service) storePending(requestID string, pending *PendingInteraction) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pending[requestID] = pending
}

// ---------------------------------------------------------------------------
// Session lifecycle
// ---------------------------------------------------------------------------

func (s *Service) startSession(ctx context.Context, sessionKey string, ws *config.Workspace, runtimeCfg config.ClaudeConfig, model, resumeID string, fork bool) (string, error) {
	sessionCtx, cancel := context.WithCancel(context.Background())
	initialSessionID := resumeID
	if fork {
		initialSessionID = ""
	}
	state := &SessionState{
		SessionKey:  sessionKey,
		WorkspaceID: ws.ID,
		Ctx:         sessionCtx,
		Cancel:      cancel,
		StartedAt:   time.Now(),
		SessionID:   initialSessionID,
		ReadyCh:     make(chan struct{}),
		Turns:       map[int]*TurnState{},
	}
	permissionMode := s.permissionModeForSession(ctx, sessionKey, ws, runtimeCfg)
	opts := []claudecli.SessionOption{
		claudecli.WithCLIPath(apputil.FirstNonEmpty(strings.TrimSpace(runtimeCfg.Command), "claude")),
		claudecli.WithWorkDir(ws.Cwd),
		claudecli.WithModel(model),
		claudecli.WithPermissionMode(permissionMode),
		claudecli.WithStderrHandler(func(buf []byte) {
			text := strings.TrimSpace(string(buf))
			if text == "" {
				return
			}
			slog.Debug("Claude CLI stderr",
				"session_key", sessionKey,
				"workspace_id", ws.ID,
				"output", text,
			)
		}),
		claudecli.WithPermissionHandler(claudecli.PermissionHandlerFunc(func(reqCtx context.Context, req *claudecli.PermissionRequest) (*claudecli.PermissionResponse, error) {
			return s.handlePermission(reqCtx, state, req)
		})),
		claudecli.WithInteractiveToolHandler(&interactiveHandler{
			askQuestion: func(reqCtx context.Context, questions []claudecli.Question) (map[string]string, error) {
				return s.handleAskUserQuestion(reqCtx, state, questions)
			},
			exitPlan: func(reqCtx context.Context, plan claudecli.PlanInfo) (string, error) {
				return s.HandleExitPlanMode(reqCtx, state, plan)
			},
		}),
	}
	if runtimeCfg.DangerouslySkipPermissions {
		opts = append(opts, claudecli.WithDangerouslySkipPermissions())
	}
	if effort := strings.TrimSpace(runtimeCfg.Effort); effort != "" {
		opts = append(opts, claudecli.WithEffort(effort))
	}
	if runtimeCfg.PermissionPromptToolStdio {
		opts = append(opts, claudecli.WithPermissionPromptToolStdio())
	}
	if runtimeCfg.DisablePlugins {
		opts = append(opts, claudecli.WithDisablePlugins())
	}
	if strings.TrimSpace(runtimeCfg.SystemPrompt) != "" {
		opts = append(opts, claudecli.WithSystemPrompt(strings.TrimSpace(runtimeCfg.SystemPrompt)))
	}
	if resumeID != "" {
		opts = append(opts, claudecli.WithResume(resumeID))
	}
	if fork {
		opts = append(opts, claudecli.WithForkSession())
	}
	session := claudecli.NewSession(opts...)
	state.Session = session

	if err := session.Start(sessionCtx); err != nil {
		cancel()
		return "", err
	}

	s.mu.Lock()
	s.sessions[sessionKey] = state
	s.mu.Unlock()

	go s.runSession(state)

	if err := session.Initialize(ctx); err != nil {
		_ = s.ResetSession(sessionKey)
		return "", err
	}
	state.Mu.Lock()
	sessionID := strings.TrimSpace(state.SessionID)
	state.Mu.Unlock()

	if fork {
		return sessionID, nil
	}

	if sessionID != "" {
		return sessionID, nil
	}
	if resumeID != "" {
		return resumeID, nil
	}
	return "", nil
}

func (s *Service) permissionModeForSession(ctx context.Context, sessionKey string, ws *config.Workspace, cfg config.ClaudeConfig) claudecli.PermissionMode {
	_ = ctx
	_ = sessionKey
	var sess *state.Session
	if s != nil {
		sess = s.GetSession(sessionKey)
	}
	return PermissionModeValue(s.effectivePermissionMode(sess, ws, cfg))
}

func (s *Service) effectivePermissionMode(sess *state.Session, ws *config.Workspace, cfg config.ClaudeConfig) string {
	return s.EffectivePermissionMode(sess, ws, cfg)
}

// ---------------------------------------------------------------------------
// Turn readiness
// ---------------------------------------------------------------------------

func (s *Service) waitForTurnReady(ctx context.Context, state *SessionState, threadID string) error {
	if state == nil {
		return fmt.Errorf("claude session state missing")
	}
	state.Mu.Lock()
	readyCh := state.ReadyCh
	state.Mu.Unlock()
	if readyCh == nil {
		return nil
	}
	select {
	case <-readyCh:
	case <-ctx.Done():
		if state.Session != nil && state.Session.Stopped() {
			return s.claudeSessionStoppedError(state, threadID)
		}
		return ctx.Err()
	}
	if state.Session != nil && state.Session.Stopped() {
		return s.claudeSessionStoppedError(state, threadID)
	}
	return nil
}

func (s *Service) claudeSessionStoppedError(state *SessionState, threadID string) error {
	if state == nil || state.Session == nil {
		return fmt.Errorf("claude session stopped")
	}
	state.Mu.Lock()
	sessionID := strings.TrimSpace(state.SessionID)
	state.Mu.Unlock()
	exitErr := state.Session.ExitError()
	message := "Claude session stopped before initialization"
	if strings.TrimSpace(threadID) != "" && strings.TrimSpace(sessionID) == strings.TrimSpace(threadID) {
		message = "Claude resume session became unavailable"
	}
	if exitErr != nil {
		return &claudecli.ProcessError{Message: message, Cause: exitErr}
	}
	return &claudecli.ProcessError{Message: message}
}

// ---------------------------------------------------------------------------
// Event loop
// ---------------------------------------------------------------------------

func (s *Service) runSession(state *SessionState) {
	for event := range state.Session.Events() {
		switch e := event.(type) {
		case claudecli.ReadyEvent:
			state.Mu.Lock()
			state.SessionID = strings.TrimSpace(e.Info.SessionID)
			sessionKey := strings.TrimSpace(state.SessionKey)
			threadID := strings.TrimSpace(state.SessionID)
			state.Mu.Unlock()
			s.BindClaudeSessionThread(sessionKey, "", threadID)
			state.ReadyOnce.Do(func() { close(state.ReadyCh) })
		case claudecli.TurnStartedEvent:
			state.Mu.Lock()
			state.CurrentTurnNumber = e.TurnNumber
			turn := state.Turns[e.TurnNumber]
			threadID := strings.TrimSpace(state.SessionID)
			sessionKey := strings.TrimSpace(state.SessionKey)
			turnID := ""
			if turn != nil {
				turnID = strings.TrimSpace(turn.TurnID)
			}
			state.Mu.Unlock()
			if threadID != "" && turnID != "" {
				s.BindClaudeSessionThread(sessionKey, turnID, threadID)
			}
		case claudecli.TextEvent:
			s.HandleTextEvent(state, e)
		case claudecli.ThinkingEvent:
			s.HandleThinkingEvent(state, e)
		case claudecli.ToolCompleteEvent:
			s.HandleToolComplete(state, e)
		case claudecli.TurnCompleteEvent:
			s.handleTurnComplete(state, e)
		case claudecli.ErrorEvent:
			s.HandleSessionError(state, e)
		}
	}
	state.ReadyOnce.Do(func() { close(state.ReadyCh) })
	s.cleanupStaleSessionOps(state)
}

// ---------------------------------------------------------------------------
// Event handlers
// ---------------------------------------------------------------------------

func (s *Service) HandleThinkingEvent(state *SessionState, event claudecli.ThinkingEvent) {
	var (
		threadID string
		turnID   string
	)

	state.Mu.Lock()
	turn := state.Turns[event.TurnNumber]
	if turn == nil {
		turn = &TurnState{TurnNumber: event.TurnNumber}
		state.Turns[event.TurnNumber] = turn
	}
	turn.Thinking = strings.TrimSpace(event.FullThinking)
	threadID = strings.TrimSpace(state.SessionID)
	turnID = strings.TrimSpace(turn.TurnID)
	state.Mu.Unlock()

	if s == nil || turnID == "" {
		return
	}
	quietEnabled := s.QuietWorkingCardEnabled()
	if !quietEnabled {
		return
	}
	sessionKey, sub := s.FindSubmissionByTurn(threadID, turnID)
	if sub == nil {
		return
	}
	workspaceCwd := s.WorkspaceCwd(sub.WorkspaceID)
	op := s.PrepareTurnStreamQuietUpdate(sessionKey, sub, threadID, "claude-thinking-"+turnID, map[string]any{
		"type": "reasoning",
	}, workspaceCwd)
	s.ExecuteQuietWorkingCardOp(context.Background(), sub, op)
}

func (s *Service) HandleTextEvent(state *SessionState, event claudecli.TextEvent) {
	body := strings.TrimSpace(event.Text)
	if body == "" {
		return
	}

	var (
		threadID string
		turnID   string
	)

	state.Mu.Lock()
	turn := state.Turns[event.TurnNumber]
	if turn == nil {
		turn = &TurnState{TurnNumber: event.TurnNumber}
		state.Turns[event.TurnNumber] = turn
	}
	turn.LastAssistantText = body
	threadID = strings.TrimSpace(state.SessionID)
	turnID = strings.TrimSpace(turn.TurnID)
	state.Mu.Unlock()
	if turnID == "" {
		return
	}
	sub, reuseMessageID := s.prepareQuietWorkingBoundary(threadID, turnID)
	if sub != nil {
		s.ExecuteQuietWorkingCardOp(context.Background(), sub, appturn.QuietWorkingCardOp{})
	}
	chunks, ok := s.UpdateOutputSegment(context.Background(), threadID, turnID, body, reuseMessageID)
	if !ok {
		return
	}
	state.Mu.Lock()
	if turn := state.Turns[event.TurnNumber]; turn != nil {
		if len(chunks) > 0 {
			turn.DeliveredAnyText = true
			turn.LastTextChunks = append([]appdelivery.SentReplyChunk(nil), chunks...)
		}
	}
	state.Mu.Unlock()
}

func (s *Service) HandleToolComplete(state *SessionState, event claudecli.ToolCompleteEvent) {
	state.Mu.Lock()
	if planFilePath := PlanFilePathFromTool(event.Name, event.Input); planFilePath != "" {
		state.LastPlanFilePath = planFilePath
	}
	threadID := strings.TrimSpace(state.SessionID)
	turn := state.Turns[event.TurnNumber]
	state.Mu.Unlock()
	if IsInternalTool(event.Name) {
		return
	}
	if turn == nil || strings.TrimSpace(turn.TurnID) == "" {
		return
	}
	item := map[string]any{
		"type":   "dynamic_tool_call",
		"id":     strings.TrimSpace(event.ID),
		"tool":   strings.TrimSpace(event.Name),
		"status": "completed",
		"input":  event.Input,
	}
	s.CompleteTurnItem(context.Background(), threadID, turn.TurnID, strings.TrimSpace(event.ID), item)
}

func (s *Service) handleTurnComplete(state *SessionState, event claudecli.TurnCompleteEvent) {
	completedAt := time.Now()
	state.Mu.Lock()
	turn := state.Turns[event.TurnNumber]
	threadID := strings.TrimSpace(state.SessionID)
	turnID := ""
	suppressFailedCompletion := false
	deliveredAnyText := false
	lastAssistantText := ""
	lastTextChunks := []appdelivery.SentReplyChunk(nil)
	if turn != nil {
		turnID = strings.TrimSpace(turn.TurnID)
		suppressFailedCompletion = turn.SuppressFailedCompletion && !event.Success
		deliveredAnyText = turn.DeliveredAnyText
		lastAssistantText = strings.TrimSpace(turn.LastAssistantText)
		lastTextChunks = append([]appdelivery.SentReplyChunk(nil), turn.LastTextChunks...)
	}
	if suppressFailedCompletion {
		delete(state.Turns, event.TurnNumber)
		if state.CurrentTurnNumber == event.TurnNumber {
			state.CurrentTurnNumber = 0
		}
		state.InterruptPending = false
		state.Mu.Unlock()
		slog.Warn("suppressing Claude failed completion during turn start",
			"session_key", state.SessionKey,
			"thread_id", threadID,
			"turn_id", turnID,
			"turn_number", event.TurnNumber,
			"error", event.Error,
		)
		return
	}
	interruptPending := state.InterruptPending
	delete(state.Turns, event.TurnNumber)
	if state.CurrentTurnNumber == event.TurnNumber {
		state.CurrentTurnNumber = 0
	}
	state.InterruptPending = false
	state.Mu.Unlock()
	slog.Debug("handleTurnComplete lookup",
		"session_key", state.SessionKey,
		"thread_id", threadID,
		"turn_number", event.TurnNumber,
		"turn_id", turnID,
		"turn_found", turn != nil,
		"success", event.Success,
	)
	if turn == nil || strings.TrimSpace(turn.TurnID) == "" {
		return
	}

	// Record usage
	s.RecordClaudeThreadUsage(threadID, event.Usage)
	s.RecordTurnTokenUsage(threadID, turn.TurnID, TurnUsageAsThreadUsage(event.Usage))
	if percentage, ok := TurnContextUsagePercent(event.Usage); ok {
		s.RecordTurnContextUsagePercent(turn.TurnID, percentage)
	}
	if event.Error != nil {
		s.RecordTurnError(threadID, turn.TurnID, event.Error.Error())
	}

	// Handle delivered text completion (both success and error with prior streamed text)
	if deliveredAnyText {
		resultText := strings.TrimSpace(event.Result)
		var finalText string
		if event.Success {
			finalText = strings.TrimSpace(apputil.FirstNonEmpty(lastAssistantText, resultText))
		} else {
			finalText = strings.TrimSpace(apputil.FirstNonEmpty(resultText, lastAssistantText))
		}
		if finalText != "" {
			sub, reuseMessageID := s.prepareQuietWorkingBoundary(threadID, turn.TurnID)
			if sub != nil {
				s.ExecuteQuietWorkingCardOp(context.Background(), sub, appturn.QuietWorkingCardOp{})
				reuseMessageIDs := []string(nil)
				if id := strings.TrimSpace(reuseMessageID); id != "" {
					reuseMessageIDs = append(reuseMessageIDs, id)
				} else {
					reuseMessageIDs = make([]string, 0, len(lastTextChunks))
					for _, chunk := range lastTextChunks {
						if id := strings.TrimSpace(chunk.MessageID); id != "" {
							reuseMessageIDs = append(reuseMessageIDs, id)
						}
					}
				}
				footerLines := s.turnFinalFooterLines(turn.TurnID, completedAt)
				inThread := s.ReplyInThread(sub)
				results := s.SendFinalMessages(context.Background(), sub, finalText, footerLines, inThread, reuseMessageIDs)
				if len(results) > 0 {
					s.MarkTurnStreamFinal(turn.TurnID)
				} else if !s.FinalizeOutputSegment(context.Background(), threadID, turn.TurnID, finalText) {
					s.CompleteTurnItem(context.Background(), threadID, turn.TurnID, "claude-agent-"+turn.TurnID, map[string]any{
						"type":  "agent_message",
						"id":    "claude-agent-" + turn.TurnID,
						"text":  finalText,
						"phase": "final_answer",
					})
				}
			}
		}
	}

	// Handle non-delivered text completion
	if !deliveredAnyText {
		finalText := strings.TrimSpace(apputil.FirstNonEmpty(strings.TrimSpace(event.Result), lastAssistantText))
		if finalText != "" {
			sub, reuseMessageID := s.prepareQuietWorkingBoundary(threadID, turn.TurnID)
			if sub != nil {
				s.ExecuteQuietWorkingCardOp(context.Background(), sub, appturn.QuietWorkingCardOp{})
				reuseMessageIDs := []string(nil)
				if id := strings.TrimSpace(reuseMessageID); id != "" {
					reuseMessageIDs = append(reuseMessageIDs, id)
				}
				footerLines := s.turnFinalFooterLines(turn.TurnID, completedAt)
				inThread := s.ReplyInThread(sub)
				results := s.SendFinalMessages(context.Background(), sub, finalText, footerLines, inThread, reuseMessageIDs)
				if len(results) > 0 {
					s.MarkTurnStreamFinal(turn.TurnID)
				} else if !s.FinalizeOutputSegment(context.Background(), threadID, turn.TurnID, finalText) {
					s.CompleteTurnItem(context.Background(), threadID, turn.TurnID, "claude-agent-"+turn.TurnID, map[string]any{
						"type":  "agent_message",
						"id":    "claude-agent-" + turn.TurnID,
						"text":  finalText,
						"phase": "final_answer",
					})
				}
			} else if !s.FinalizeOutputSegment(context.Background(), threadID, turn.TurnID, finalText) {
				s.CompleteTurnItem(context.Background(), threadID, turn.TurnID, "claude-agent-"+turn.TurnID, map[string]any{
					"type":  "agent_message",
					"id":    "claude-agent-" + turn.TurnID,
					"text":  finalText,
					"phase": "final_answer",
				})
			}
		}
	}

	// Determine turn status
	status := "completed"
	if !event.Success {
		if interruptPending {
			status = "interrupted"
		} else {
			status = "failed"
		}
	}
	s.FinishTurn(threadID, turn.TurnID, status)

	// Finalize the steer submission if one was attached to this turn.
	// The steer message was processed as part of this conversation round,
	// so its submission completes together with the turn.
	if steerID := strings.TrimSpace(turn.SteerSubmissionID); steerID != "" {
		s.FinishSteerSubmission(steerID, status)
	}
}

func (s *Service) HandleSessionError(state *SessionState, event claudecli.ErrorEvent) {
	state.Mu.Lock()
	sessionKey := strings.TrimSpace(state.SessionKey)
	threadID := strings.TrimSpace(state.SessionID)
	turn := state.Turns[event.TurnNumber]
	turnID := ""
	if turn != nil {
		turnID = strings.TrimSpace(turn.TurnID)
	}
	state.Mu.Unlock()
	slog.Warn("Claude session event error",
		"session_key", sessionKey,
		"thread_id", threadID,
		"turn_id", turnID,
		"turn_number", event.TurnNumber,
		"context", event.Context,
		"error", event.Error,
	)
	if turn == nil || strings.TrimSpace(turn.TurnID) == "" {
		if IsFatalSessionErrorFromState(state, event) {
			s.FailClaudeSessionWork(sessionKey, threadID, event.Error)
		}
		return
	}
	if event.Error != nil {
		s.RecordTurnError(threadID, turn.TurnID, event.Error.Error())
	}
	if IsFatalSessionErrorFromState(state, event) {
		s.FailClaudeSessionWork(sessionKey, threadID, event.Error)
	}
}

func (s *Service) cleanupStaleSessionOps(state *SessionState) {
	if s == nil || state == nil {
		return
	}
	state.Mu.Lock()
	sessionKey := strings.TrimSpace(state.SessionKey)
	threadID := strings.TrimSpace(state.SessionID)
	state.Mu.Unlock()
	if sessionKey == "" {
		return
	}
	sess := s.GetSession(sessionKey)
	if sess == nil || !s.SessionHasActiveOps(sess) {
		return
	}
	slog.Warn("Claude event loop exited with stale active operations, forcing cleanup",
		"session_key", sessionKey,
		"thread_id", threadID,
	)
	for _, op := range sess.ActiveOperations {
		tID := strings.TrimSpace(op.TurnID)
		if tID != "" {
			s.FinishTurn(threadID, tID, "failed")
		}
	}
	sess = s.GetSession(sessionKey)
	if sess != nil && s.SessionHasActiveOps(sess) {
		s.FailBackendActiveWork("claude", sessionKey, threadID, "Claude 会话事件循环异常退出")
	}
}

// ---------------------------------------------------------------------------
// Permission and interactive tool handlers
// ---------------------------------------------------------------------------

func (s *Service) handlePermission(ctx context.Context, state *SessionState, req *claudecli.PermissionRequest) (*claudecli.PermissionResponse, error) {
	if state == nil || req == nil {
		return &claudecli.PermissionResponse{Behavior: claudecli.PermissionDeny, Message: "invalid permission request"}, nil
	}
	state.Mu.Lock()
	threadID := strings.TrimSpace(state.SessionID)
	turnID := ""
	if turn := state.Turns[state.CurrentTurnNumber]; turn != nil {
		turnID = strings.TrimSpace(turn.TurnID)
	}
	state.Mu.Unlock()

	sessionKey, sub := s.FindSubmissionByTurn(threadID, turnID)
	if sub == nil {
		return &claudecli.PermissionResponse{Behavior: claudecli.PermissionDeny, Message: "no active submission for approval"}, nil
	}

	requestID := strings.TrimSpace(req.RequestID)
	sessionUpdates := SafeClaudeSessionPermissionUpdates(req.PermissionSuggestions)
	sessionLabel := DescribeClaudeSessionPermissionUpdates(sessionUpdates)
	kind, body, payload := s.approvalPresentation(sub.WorkspaceID, req)

	if err := s.SendClaudeApprovalCard(kind, requestID, sessionKey, sub, threadID, turnID, requestID, body, payload, sessionLabel); err != nil {
		return &claudecli.PermissionResponse{Behavior: claudecli.PermissionDeny, Message: err.Error()}, nil
	}

	pending := &PendingInteraction{
		Kind:                     kind,
		Session:                  state,
		Tool:                     req.ToolName,
		SessionPermissionUpdates: CopyPermissionUpdates(sessionUpdates),
		RespCh:                   make(chan PendingResponse, 1),
	}
	s.storePending(requestID, pending)
	select {
	case <-ctx.Done():
		_ = s.CancelPending(requestID, ctx.Err().Error())
		return &claudecli.PermissionResponse{Behavior: claudecli.PermissionDeny, Message: ctx.Err().Error()}, nil
	case resp := <-pending.RespCh:
		if resp.Err != nil {
			return &claudecli.PermissionResponse{Behavior: claudecli.PermissionDeny, Message: resp.Err.Error()}, nil
		}
		if resp.Approval == nil {
			return &claudecli.PermissionResponse{Behavior: claudecli.PermissionDeny, Message: "approval response missing"}, nil
		}
		return resp.Approval, nil
	}
}

func (s *Service) handleAskUserQuestion(ctx context.Context, state *SessionState, questions []claudecli.Question) (map[string]string, error) {
	state.Mu.Lock()
	threadID := strings.TrimSpace(state.SessionID)
	turnID := ""
	if turn := state.Turns[state.CurrentTurnNumber]; turn != nil {
		turnID = strings.TrimSpace(turn.TurnID)
	}
	state.Mu.Unlock()

	sessionKey, sub := s.FindSubmissionByTurn(threadID, turnID)
	if sub == nil {
		return nil, fmt.Errorf("no active submission for question")
	}

	requestID := "claude-question-" + strings.TrimSpace(turnID)
	if nextID, err := s.NextLocalID("claude-question"); err == nil && strings.TrimSpace(nextID) != "" {
		requestID = nextID
	}
	payload := apppendingforms.ToolUserInputPayload{
		ThreadID:  threadID,
		TurnID:    turnID,
		ItemID:    requestID,
		Questions: QuestionsAsToolUserInput(questions),
	}
	if len(payload.Questions) == 0 {
		return nil, fmt.Errorf("Claude question payload was empty")
	}

	if len(payload.Questions) == 1 && len(payload.Questions[0].Options) > 0 && len(payload.Questions[0].Options) <= 3 && !payload.Questions[0].MultiSelect && !payload.Questions[0].IsOther {
		if err := s.SendClaudeUserInputCard(requestID, sessionKey, sub, payload); err != nil {
			return nil, err
		}
	} else {
		if err := s.SendClaudeUserInputFormCard(requestID, sessionKey, sub, payload); err != nil {
			return nil, err
		}
	}

	pending := &PendingInteraction{
		Kind:   "question",
		RespCh: make(chan PendingResponse, 1),
	}
	s.storePending(requestID, pending)
	select {
	case <-ctx.Done():
		_ = s.CancelPending(requestID, ctx.Err().Error())
		return nil, ctx.Err()
	case resp := <-pending.RespCh:
		if resp.Err != nil {
			return nil, resp.Err
		}
		if resp.Answers == nil {
			return nil, fmt.Errorf("question response missing answers")
		}
		return resp.Answers, nil
	}
}

func (s *Service) HandleExitPlanMode(ctx context.Context, state *SessionState, plan claudecli.PlanInfo) (string, error) {
	state.Mu.Lock()
	threadID := strings.TrimSpace(state.SessionID)
	turnID := ""
	if turn := state.Turns[state.CurrentTurnNumber]; turn != nil {
		turnID = strings.TrimSpace(turn.TurnID)
	}
	workspaceID := strings.TrimSpace(state.WorkspaceID)
	planFilePath := strings.TrimSpace(state.LastPlanFilePath)
	startedAt := state.StartedAt
	state.Mu.Unlock()

	sessionKey, sub := s.FindSubmissionByTurn(threadID, turnID)
	if sub == nil {
		return "", fmt.Errorf("no active submission for plan confirmation")
	}

	workspaceID = apputil.FirstNonEmpty(strings.TrimSpace(sub.WorkspaceID), workspaceID)
	workspaceCwdVal := s.WorkspaceCwd(workspaceID)
	plan = EnrichPlanForDisplay(plan, planFilePath, workspaceCwdVal, startedAt)

	requestID := "claude-plan-" + strings.TrimSpace(turnID)
	if nextID, err := s.NextLocalID("claude-plan"); err == nil && strings.TrimSpace(nextID) != "" {
		requestID = nextID
	}
	if err := s.SendClaudePlanModeCard(requestID, sessionKey, sub, threadID, turnID, PlanModeBody(plan)); err != nil {
		return "", err
	}

	pending := &PendingInteraction{
		Kind:   "plan",
		RespCh: make(chan PendingResponse, 1),
	}
	s.storePending(requestID, pending)
	select {
	case <-ctx.Done():
		_ = s.CancelPending(requestID, ctx.Err().Error())
		return "", ctx.Err()
	case resp := <-pending.RespCh:
		if resp.Err != nil {
			return "", resp.Err
		}
		if strings.TrimSpace(resp.Feedback) == "" {
			return "", fmt.Errorf("plan feedback is required")
		}
		return strings.TrimSpace(resp.Feedback), nil
	}
}

// ---------------------------------------------------------------------------
// Approval presentation
// ---------------------------------------------------------------------------

func (s *Service) approvalPresentation(workspaceID string, req *claudecli.PermissionRequest) (kind, body string, payload map[string]any) {
	workspaceCwdFunc := func(wsID string) string {
		return s.WorkspaceCwd(wsID)
	}
	return RenderApprovalPresentation(workspaceID, req, workspaceCwdFunc)
}

// ---------------------------------------------------------------------------
// Quiet working card helpers
// ---------------------------------------------------------------------------

func (s *Service) prepareQuietWorkingBoundary(threadID, turnID string) (*state.Submission, string) {
	if s == nil || strings.TrimSpace(turnID) == "" {
		return nil, ""
	}
	_, sub := s.FindSubmissionByTurn(threadID, turnID)
	if sub == nil {
		return nil, ""
	}
	return sub, s.PrepareTurnStreamQuietBoundary(turnID)
}

func (s *Service) turnFinalFooterLines(turnID string, completedAt time.Time) []string {
	return s.TurnFinalFooterLines(turnID, completedAt)
}

// ---------------------------------------------------------------------------
// Interactive handler — bridges claudecli callbacks to service methods
// ---------------------------------------------------------------------------

type interactiveHandler struct {
	askQuestion func(context.Context, []claudecli.Question) (map[string]string, error)
	exitPlan    func(context.Context, claudecli.PlanInfo) (string, error)
}

func (h *interactiveHandler) HandleAskUserQuestion(ctx context.Context, questions []claudecli.Question) (map[string]string, error) {
	if h == nil || h.askQuestion == nil {
		return nil, errors.New("interactive handler not configured")
	}
	return h.askQuestion(ctx, questions)
}

func (h *interactiveHandler) HandleExitPlanMode(ctx context.Context, plan claudecli.PlanInfo) (string, error) {
	if h == nil || h.exitPlan == nil {
		return "", errors.New("interactive handler not configured")
	}
	return h.exitPlan(ctx, plan)
}

// ---------------------------------------------------------------------------
// Fatal session error check
// ---------------------------------------------------------------------------

// IsFatalSessionErrorFromState checks if the event is a fatal session error.
func IsFatalSessionErrorFromState(state *SessionState, event claudecli.ErrorEvent) bool {
	stopped := false
	exitErr := false
	if state != nil && state.Session != nil {
		stopped = state.Session.Stopped()
		exitErr = state.Session.ExitError() != nil
	}
	return IsFatalSessionError(stopped, exitErr, event.Error)
}
