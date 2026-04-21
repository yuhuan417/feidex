package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"feidex/internal/claudecli"
	"feidex/internal/codexrpc"
	"feidex/internal/config"
	"feidex/internal/state"
)

const claudePlanModePendingKind = "claude_exit_plan_mode"

type claudeRuntime struct {
	app *App
	cfg config.ClaudeConfig

	mu       sync.Mutex
	sessions map[string]*claudeSessionState
	pending  map[string]*claudePendingInteraction
}

type claudeSessionState struct {
	sessionKey  string
	workspaceID string
	session     *claudecli.Session
	ctx         context.Context
	cancel      context.CancelFunc
	startedAt   time.Time

	mu                sync.Mutex
	sessionID         string
	readyCh           chan struct{}
	readyOnce         sync.Once
	currentTurnNumber int
	interruptPending  bool
	allowTools        map[string]bool
	turns             map[int]*claudeTurnState
	lastPlanFilePath  string
}

type claudeTurnState struct {
	TurnNumber int
	TurnID     string
	Thinking   string

	LastAssistantText        string
	LastTextChunks           []sentReplyChunk
	DeliveredAnyText         bool
	SuppressFailedCompletion bool
}

type claudePendingInteraction struct {
	kind    string
	session *claudeSessionState
	tool    string
	respCh  chan claudePendingResponse
}

type claudePendingResponse struct {
	approval *claudecli.PermissionResponse
	answers  map[string]string
	feedback string
	err      error
}

type claudeInteractiveHandler struct {
	askQuestion func(context.Context, []claudecli.Question) (map[string]string, error)
	exitPlan    func(context.Context, claudecli.PlanInfo) (string, error)
}

func (h *claudeInteractiveHandler) HandleAskUserQuestion(ctx context.Context, questions []claudecli.Question) (map[string]string, error) {
	if h == nil || h.askQuestion == nil {
		return nil, errors.New("interactive handler not configured")
	}
	return h.askQuestion(ctx, questions)
}

func (h *claudeInteractiveHandler) HandleExitPlanMode(ctx context.Context, plan claudecli.PlanInfo) (string, error) {
	if h == nil || h.exitPlan == nil {
		return "", errors.New("interactive handler not configured")
	}
	return h.exitPlan(ctx, plan)
}

func newClaudeRuntime(app *App, cfg config.ClaudeConfig) claudeCore {
	return &claudeRuntime{
		app:      app,
		cfg:      cfg,
		sessions: map[string]*claudeSessionState{},
		pending:  map[string]*claudePendingInteraction{},
	}
}

func (r *claudeRuntime) EnsureSession(ctx context.Context, sessionKey string, ws *config.Workspace, resumeID, model string) (string, error) {
	if r == nil {
		return "", fmt.Errorf("claude runtime not initialized")
	}
	_ = ctx
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return "", fmt.Errorf("missing session key")
	}
	if ws == nil {
		return "", fmt.Errorf("workspace not found")
	}
	resumeID = strings.TrimSpace(resumeID)
	model = strings.TrimSpace(firstNonEmpty(model, strings.TrimSpace(ws.Model), strings.TrimSpace(r.cfg.Model)))

	r.mu.Lock()
	current := r.sessions[sessionKey]
	if current != nil && current.workspaceID == ws.ID {
		current.mu.Lock()
		currentID := strings.TrimSpace(current.sessionID)
		current.mu.Unlock()
		currentStopped := current.session != nil && current.session.Stopped()
		currentExitErr := error(nil)
		if current.session != nil {
			currentExitErr = current.session.ExitError()
		}
		r.mu.Unlock()
		if currentStopped {
			slog.Warn("discarding stopped Claude session before ensure",
				"session_key", sessionKey,
				"workspace_id", ws.ID,
				"resume_id", resumeID,
				"session_id", currentID,
				"exit_error", currentExitErr,
			)
		} else {
			if resumeID == "" || currentID == "" || resumeID == currentID {
				return firstNonEmpty(currentID, resumeID), nil
			}
		}
	} else {
		r.mu.Unlock()
	}

	_ = r.ResetSession(sessionKey)

	sessionCtx, cancel := context.WithCancel(context.Background())
	state := &claudeSessionState{
		sessionKey:  sessionKey,
		workspaceID: ws.ID,
		ctx:         sessionCtx,
		cancel:      cancel,
		startedAt:   time.Now(),
		sessionID:   resumeID,
		readyCh:     make(chan struct{}),
		allowTools:  map[string]bool{},
		turns:       map[int]*claudeTurnState{},
	}

	permissionMode := claudePermissionModeForWorkspace(r.cfg, ws)
	opts := []claudecli.SessionOption{
		claudecli.WithCLIPath(firstNonEmpty(strings.TrimSpace(r.cfg.Command), "claude")),
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
			return r.handlePermission(reqCtx, state, req)
		})),
		claudecli.WithInteractiveToolHandler(&claudeInteractiveHandler{
			askQuestion: func(reqCtx context.Context, questions []claudecli.Question) (map[string]string, error) {
				return r.handleAskUserQuestion(reqCtx, state, questions)
			},
			exitPlan: func(reqCtx context.Context, plan claudecli.PlanInfo) (string, error) {
				return r.handleExitPlanMode(reqCtx, state, plan)
			},
		}),
	}
	if r.cfg.PermissionPromptToolStdio {
		opts = append(opts, claudecli.WithPermissionPromptToolStdio())
	}
	if r.cfg.DisablePlugins {
		opts = append(opts, claudecli.WithDisablePlugins())
	}
	if strings.TrimSpace(r.cfg.SystemPrompt) != "" {
		opts = append(opts, claudecli.WithSystemPrompt(strings.TrimSpace(r.cfg.SystemPrompt)))
	}
	if resumeID != "" {
		opts = append(opts, claudecli.WithResume(resumeID))
	}
	session := claudecli.NewSession(opts...)
	state.session = session

	if err := session.Start(sessionCtx); err != nil {
		cancel()
		return "", err
	}

	r.mu.Lock()
	r.sessions[sessionKey] = state
	r.mu.Unlock()

	go r.runSession(state)

	return strings.TrimSpace(state.sessionID), nil
}

func (r *claudeRuntime) StartTurn(ctx context.Context, sessionKey, threadID, turnID, prompt string) error {
	state, err := r.sessionState(sessionKey)
	if err != nil {
		return err
	}
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return fmt.Errorf("empty Claude prompt")
	}
	turnNumber, err := state.session.SendMessage(ctx, prompt)
	if err != nil {
		return err
	}
	state.mu.Lock()
	turn := state.turns[turnNumber]
	if turn == nil {
		turn = &claudeTurnState{TurnNumber: turnNumber}
		state.turns[turnNumber] = turn
	}
	turn.TurnID = strings.TrimSpace(turnID)
	turn.SuppressFailedCompletion = true
	state.mu.Unlock()
	if err := r.waitForTurnReady(ctx, state, threadID); err != nil {
		return err
	}
	state.mu.Lock()
	if turn := state.turns[turnNumber]; turn != nil {
		turn.SuppressFailedCompletion = false
	}
	state.mu.Unlock()
	return nil
}

func (r *claudeRuntime) Interrupt(ctx context.Context, sessionKey string) error {
	state, err := r.sessionState(sessionKey)
	if err != nil {
		return err
	}
	state.mu.Lock()
	state.interruptPending = true
	state.mu.Unlock()
	return state.session.Interrupt(ctx)
}

func (r *claudeRuntime) ResetSession(sessionKey string) error {
	sessionKey = strings.TrimSpace(sessionKey)
	r.mu.Lock()
	state := r.sessions[sessionKey]
	delete(r.sessions, sessionKey)
	for requestID, pending := range r.pending {
		if pending == nil || pending.session == nil {
			continue
		}
		if strings.TrimSpace(pending.session.sessionKey) == sessionKey {
			delete(r.pending, requestID)
			select {
			case pending.respCh <- claudePendingResponse{err: errors.New("session reset")}:
			default:
			}
		}
	}
	r.mu.Unlock()
	if state == nil {
		return nil
	}
	state.cancel()
	return state.session.Stop()
}

func (r *claudeRuntime) ResolveApproval(requestID string, resolution claudeApprovalResolution) error {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return fmt.Errorf("missing request id")
	}
	pending := r.takePending(requestID)
	if pending == nil {
		return fmt.Errorf("approval request %q not found", requestID)
	}
	resp := &claudecli.PermissionResponse{}
	switch strings.TrimSpace(resolution.Behavior) {
	case "allow":
		resp.Behavior = claudecli.PermissionAllow
		if strings.TrimSpace(resolution.Scope) == "session" && pending.session != nil {
			pending.session.mu.Lock()
			pending.session.allowTools[claudeSessionApprovalKey(pending.tool)] = true
			pending.session.mu.Unlock()
		}
	default:
		resp.Behavior = claudecli.PermissionDeny
		resp.Message = firstNonEmpty(strings.TrimSpace(resolution.Message), "Declined by user")
		resp.Interrupt = resolution.Interrupt
	}
	pending.respCh <- claudePendingResponse{approval: resp}
	return nil
}

func (r *claudeRuntime) ResolveUserInput(requestID string, answers map[string]string) error {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return fmt.Errorf("missing request id")
	}
	pending := r.takePending(requestID)
	if pending == nil {
		return fmt.Errorf("user input request %q not found", requestID)
	}
	pending.respCh <- claudePendingResponse{answers: answers}
	return nil
}

func (r *claudeRuntime) ResolvePlanFeedback(requestID, feedback string) error {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return fmt.Errorf("missing request id")
	}
	pending := r.takePending(requestID)
	if pending == nil {
		return fmt.Errorf("plan request %q not found", requestID)
	}
	pending.respCh <- claudePendingResponse{feedback: strings.TrimSpace(feedback)}
	return nil
}

func (r *claudeRuntime) CancelPending(requestID, message string) error {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return fmt.Errorf("missing request id")
	}
	pending := r.takePending(requestID)
	if pending == nil {
		return fmt.Errorf("pending request %q not found", requestID)
	}
	message = firstNonEmpty(strings.TrimSpace(message), "cancelled by user")
	pending.respCh <- claudePendingResponse{err: errors.New(message)}
	return nil
}

func (r *claudeRuntime) Close() error {
	r.mu.Lock()
	keys := make([]string, 0, len(r.sessions))
	for key := range r.sessions {
		keys = append(keys, key)
	}
	r.mu.Unlock()
	var firstErr error
	for _, key := range keys {
		if err := r.ResetSession(key); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (r *claudeRuntime) sessionState(sessionKey string) (*claudeSessionState, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	state := r.sessions[strings.TrimSpace(sessionKey)]
	if state == nil {
		return nil, fmt.Errorf("claude session %q not initialized", sessionKey)
	}
	return state, nil
}

func (r *claudeRuntime) takePending(requestID string) *claudePendingInteraction {
	r.mu.Lock()
	defer r.mu.Unlock()
	pending := r.pending[requestID]
	delete(r.pending, requestID)
	return pending
}

func (r *claudeRuntime) storePending(requestID string, pending *claudePendingInteraction) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pending[requestID] = pending
}

func (r *claudeRuntime) runSession(state *claudeSessionState) {
	for event := range state.session.Events() {
		switch e := event.(type) {
		case claudecli.ReadyEvent:
			state.mu.Lock()
			state.sessionID = strings.TrimSpace(e.Info.SessionID)
			sessionKey := strings.TrimSpace(state.sessionKey)
			threadID := strings.TrimSpace(state.sessionID)
			state.mu.Unlock()
			r.app.bindClaudeSessionThread(sessionKey, "", threadID)
			state.readyOnce.Do(func() { close(state.readyCh) })
		case claudecli.TurnStartedEvent:
			state.mu.Lock()
			state.currentTurnNumber = e.TurnNumber
			turn := state.turns[e.TurnNumber]
			threadID := strings.TrimSpace(state.sessionID)
			sessionKey := strings.TrimSpace(state.sessionKey)
			turnID := ""
			if turn != nil {
				turnID = strings.TrimSpace(turn.TurnID)
			}
			state.mu.Unlock()
			if threadID != "" && turnID != "" {
				r.app.bindClaudeSessionThread(sessionKey, turnID, threadID)
			}
		case claudecli.TextEvent:
			r.handleTextEvent(state, e)
		case claudecli.ThinkingEvent:
			r.handleThinkingEvent(state, e)
		case claudecli.ToolCompleteEvent:
			r.handleToolComplete(state, e)
		case claudecli.TurnCompleteEvent:
			r.handleTurnComplete(state, e)
		case claudecli.ErrorEvent:
			r.handleSessionError(state, e)
		}
	}
	state.readyOnce.Do(func() { close(state.readyCh) })
}

func (r *claudeRuntime) waitForTurnReady(ctx context.Context, state *claudeSessionState, threadID string) error {
	if state == nil {
		return fmt.Errorf("claude session state missing")
	}
	state.mu.Lock()
	readyCh := state.readyCh
	state.mu.Unlock()
	if readyCh == nil {
		return nil
	}
	select {
	case <-readyCh:
	case <-ctx.Done():
		if state.session != nil && state.session.Stopped() {
			return r.claudeSessionStoppedError(state, threadID)
		}
		return ctx.Err()
	}
	if state.session != nil && state.session.Stopped() {
		return r.claudeSessionStoppedError(state, threadID)
	}
	return nil
}

func (r *claudeRuntime) claudeSessionStoppedError(state *claudeSessionState, threadID string) error {
	if state == nil || state.session == nil {
		return fmt.Errorf("claude session stopped")
	}
	state.mu.Lock()
	sessionID := strings.TrimSpace(state.sessionID)
	state.mu.Unlock()
	exitErr := state.session.ExitError()
	message := "Claude session stopped before initialization"
	if strings.TrimSpace(threadID) != "" && strings.TrimSpace(sessionID) == strings.TrimSpace(threadID) {
		message = "Claude resume session became unavailable"
	}
	if exitErr != nil {
		return &claudecli.ProcessError{Message: message, Cause: exitErr}
	}
	return &claudecli.ProcessError{Message: message}
}

func (r *claudeRuntime) prepareClaudeQuietWorkingBoundary(threadID, turnID string) (*state.Submission, quietWorkingBoundary) {
	if r == nil || r.app == nil || strings.TrimSpace(turnID) == "" {
		return nil, quietWorkingBoundary{}
	}
	_, sub := r.app.findSubmissionByTurn(threadID, turnID)
	if sub == nil {
		return nil, quietWorkingBoundary{}
	}
	r.app.turnStreamsMu.Lock()
	boundary := r.app.prepareQuietWorkingCardBoundaryLocked(r.app.turnStreams[turnID])
	r.app.turnStreamsMu.Unlock()
	return sub, boundary
}

func (r *claudeRuntime) handleThinkingEvent(state *claudeSessionState, event claudecli.ThinkingEvent) {
	var (
		threadID string
		turnID   string
	)

	state.mu.Lock()
	turn := state.turns[event.TurnNumber]
	if turn == nil {
		turn = &claudeTurnState{TurnNumber: event.TurnNumber}
		state.turns[event.TurnNumber] = turn
	}
	turn.Thinking = strings.TrimSpace(event.FullThinking)
	threadID = strings.TrimSpace(state.sessionID)
	turnID = strings.TrimSpace(turn.TurnID)
	state.mu.Unlock()

	if r == nil || r.app == nil || !r.app.quietWorkingCardEnabled() || turnID == "" {
		return
	}
	sessionKey, sub := r.app.findSubmissionByTurn(threadID, turnID)
	if sub == nil {
		return
	}
	workspaceCwd := r.app.workspaceCwd(sub.WorkspaceID)
	var op quietWorkingCardOp
	r.app.turnStreamsMu.Lock()
	stream := r.app.ensureTurnStreamLocked(sessionKey, sub)
	if threadID != "" {
		stream.ThreadID = threadID
	}
	op = r.app.prepareQuietWorkingCardUpdateLocked(stream, "claude-thinking-"+turnID, map[string]any{
		"type": "reasoning",
	}, workspaceCwd)
	r.app.turnStreamsMu.Unlock()
	r.app.executeQuietWorkingCardOp(context.Background(), sub, op)
}

func (r *claudeRuntime) handleTextEvent(state *claudeSessionState, event claudecli.TextEvent) {
	body := strings.TrimSpace(event.Text)
	if body == "" {
		return
	}

	var (
		threadID string
		turnID   string
	)

	state.mu.Lock()
	turn := state.turns[event.TurnNumber]
	if turn == nil {
		turn = &claudeTurnState{TurnNumber: event.TurnNumber}
		state.turns[event.TurnNumber] = turn
	}
	turn.LastAssistantText = body
	threadID = strings.TrimSpace(state.sessionID)
	turnID = strings.TrimSpace(turn.TurnID)
	state.mu.Unlock()
	if turnID == "" {
		return
	}
	sub, boundary := r.prepareClaudeQuietWorkingBoundary(threadID, turnID)
	if sub != nil {
		r.app.executeQuietWorkingCardOp(context.Background(), sub, boundary.Op)
	}
	chunks, ok := r.app.updateClaudeOutputSegmentWithReuse(context.Background(), threadID, turnID, body, boundary.ReuseMessageID)
	if !ok {
		return
	}
	state.mu.Lock()
	if turn := state.turns[event.TurnNumber]; turn != nil {
		if len(chunks) > 0 {
			turn.DeliveredAnyText = true
			turn.LastTextChunks = append([]sentReplyChunk(nil), chunks...)
		}
	}
	state.mu.Unlock()
}

func (r *claudeRuntime) handleToolComplete(state *claudeSessionState, event claudecli.ToolCompleteEvent) {
	state.mu.Lock()
	if planFilePath := claudePlanFilePathFromTool(event.Name, event.Input); planFilePath != "" {
		state.lastPlanFilePath = planFilePath
	}
	threadID := strings.TrimSpace(state.sessionID)
	turn := state.turns[event.TurnNumber]
	state.mu.Unlock()
	if isClaudeInternalTool(event.Name) {
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
	r.app.completeTurnItem(context.Background(), threadID, turn.TurnID, strings.TrimSpace(event.ID), item)
}

func (r *claudeRuntime) handleTurnComplete(state *claudeSessionState, event claudecli.TurnCompleteEvent) {
	completedAt := time.Now()
	state.mu.Lock()
	turn := state.turns[event.TurnNumber]
	threadID := strings.TrimSpace(state.sessionID)
	turnID := ""
	suppressFailedCompletion := false
	deliveredAnyText := false
	lastAssistantText := ""
	lastTextChunks := []sentReplyChunk(nil)
	if turn != nil {
		turnID = strings.TrimSpace(turn.TurnID)
		suppressFailedCompletion = turn.SuppressFailedCompletion && !event.Success
		deliveredAnyText = turn.DeliveredAnyText
		lastAssistantText = strings.TrimSpace(turn.LastAssistantText)
		lastTextChunks = append([]sentReplyChunk(nil), turn.LastTextChunks...)
	}
	if suppressFailedCompletion {
		delete(state.turns, event.TurnNumber)
		if state.currentTurnNumber == event.TurnNumber {
			state.currentTurnNumber = 0
		}
		state.interruptPending = false
		state.mu.Unlock()
		slog.Warn("suppressing Claude failed completion during turn start",
			"session_key", state.sessionKey,
			"thread_id", threadID,
			"turn_id", turnID,
			"turn_number", event.TurnNumber,
			"error", event.Error,
		)
		return
	}
	interruptPending := state.interruptPending
	delete(state.turns, event.TurnNumber)
	if state.currentTurnNumber == event.TurnNumber {
		state.currentTurnNumber = 0
	}
	state.interruptPending = false
	state.mu.Unlock()
	if turn == nil || strings.TrimSpace(turn.TurnID) == "" {
		return
	}
	r.app.recordTurnTokenUsage(threadID, turn.TurnID, claudeTurnUsageAsThreadUsage(event.Usage))
	if percentage, ok := claudeTurnContextUsagePercent(event.Usage); ok {
		r.app.recordTurnContextUsagePercent(turn.TurnID, percentage)
	}
	if event.Error != nil {
		r.app.recordTurnError(threadID, turn.TurnID, event.Error.Error())
	}
	if deliveredAnyText && event.Success {
		finalText := strings.TrimSpace(firstNonEmpty(lastAssistantText, strings.TrimSpace(event.Result)))
		if finalText != "" {
			sub, boundary := r.prepareClaudeQuietWorkingBoundary(threadID, turn.TurnID)
			if sub != nil {
				r.app.executeQuietWorkingCardOp(context.Background(), sub, boundary.Op)
				reuseMessageIDs := []string(nil)
				if id := strings.TrimSpace(boundary.ReuseMessageID); id != "" {
					reuseMessageIDs = append(reuseMessageIDs, id)
				} else {
					reuseMessageIDs = make([]string, 0, len(lastTextChunks))
					for _, chunk := range lastTextChunks {
						if id := strings.TrimSpace(chunk.MessageID); id != "" {
							reuseMessageIDs = append(reuseMessageIDs, id)
						}
					}
				}
				footerLines := r.app.turnFinalFooterLines(turn.TurnID, completedAt)
				results := r.app.sendFinalMessagesWithFooterAndReuse(context.Background(), sub, finalText, footerLines, r.app.replyInThreadForSubmission(sub), reuseMessageIDs)
				if len(results) > 0 {
					r.app.turnStreamsMu.Lock()
					if stream := r.app.turnStreams[turn.TurnID]; stream != nil {
						stream.SentFinal = true
					}
					r.app.turnStreamsMu.Unlock()
				} else if !r.app.finalizeClaudeOutputSegment(context.Background(), threadID, turn.TurnID, finalText) {
					r.app.completeTurnItem(context.Background(), threadID, turn.TurnID, "claude-agent-"+turn.TurnID, map[string]any{
						"type":  "agent_message",
						"id":    "claude-agent-" + turn.TurnID,
						"text":  finalText,
						"phase": "final_answer",
					})
				}
			}
		}
	}
	if !deliveredAnyText {
		finalText := strings.TrimSpace(firstNonEmpty(strings.TrimSpace(event.Result), lastAssistantText))
		if finalText != "" {
			sub, boundary := r.prepareClaudeQuietWorkingBoundary(threadID, turn.TurnID)
			if sub != nil {
				r.app.executeQuietWorkingCardOp(context.Background(), sub, boundary.Op)
				reuseMessageIDs := []string(nil)
				if id := strings.TrimSpace(boundary.ReuseMessageID); id != "" {
					reuseMessageIDs = append(reuseMessageIDs, id)
				}
				footerLines := r.app.turnFinalFooterLines(turn.TurnID, completedAt)
				results := r.app.sendFinalMessagesWithFooterAndReuse(context.Background(), sub, finalText, footerLines, r.app.replyInThreadForSubmission(sub), reuseMessageIDs)
				if len(results) > 0 {
					r.app.turnStreamsMu.Lock()
					if stream := r.app.turnStreams[turn.TurnID]; stream != nil {
						stream.SentFinal = true
					}
					r.app.turnStreamsMu.Unlock()
				} else if !r.app.finalizeClaudeOutputSegment(context.Background(), threadID, turn.TurnID, finalText) {
					r.app.completeTurnItem(context.Background(), threadID, turn.TurnID, "claude-agent-"+turn.TurnID, map[string]any{
						"type":  "agent_message",
						"id":    "claude-agent-" + turn.TurnID,
						"text":  finalText,
						"phase": "final_answer",
					})
				}
			} else if !r.app.finalizeClaudeOutputSegment(context.Background(), threadID, turn.TurnID, finalText) {
				r.app.completeTurnItem(context.Background(), threadID, turn.TurnID, "claude-agent-"+turn.TurnID, map[string]any{
					"type":  "agent_message",
					"id":    "claude-agent-" + turn.TurnID,
					"text":  finalText,
					"phase": "final_answer",
				})
			}
		}
	}
	status := "completed"
	if !event.Success {
		if interruptPending {
			status = "interrupted"
		} else {
			status = "failed"
		}
	}
	r.app.finishTurn(threadID, turn.TurnID, status)
}

func (r *claudeRuntime) handleSessionError(state *claudeSessionState, event claudecli.ErrorEvent) {
	state.mu.Lock()
	sessionKey := strings.TrimSpace(state.sessionKey)
	threadID := strings.TrimSpace(state.sessionID)
	turn := state.turns[event.TurnNumber]
	turnID := ""
	if turn != nil {
		turnID = strings.TrimSpace(turn.TurnID)
	}
	state.mu.Unlock()
	slog.Warn("Claude session event error",
		"session_key", sessionKey,
		"thread_id", threadID,
		"turn_id", turnID,
		"turn_number", event.TurnNumber,
		"context", event.Context,
		"error", event.Error,
	)
	if turn == nil || strings.TrimSpace(turn.TurnID) == "" {
		return
	}
	if event.Error != nil {
		r.app.recordTurnError(threadID, turn.TurnID, event.Error.Error())
	}
}

func (r *claudeRuntime) handlePermission(ctx context.Context, state *claudeSessionState, req *claudecli.PermissionRequest) (*claudecli.PermissionResponse, error) {
	if state == nil || req == nil {
		return &claudecli.PermissionResponse{Behavior: claudecli.PermissionDeny, Message: "invalid permission request"}, nil
	}
	state.mu.Lock()
	if state.allowTools[claudeSessionApprovalKey(req.ToolName)] {
		state.mu.Unlock()
		return &claudecli.PermissionResponse{Behavior: claudecli.PermissionAllow}, nil
	}
	threadID := strings.TrimSpace(state.sessionID)
	turnID := ""
	if turn := state.turns[state.currentTurnNumber]; turn != nil {
		turnID = strings.TrimSpace(turn.TurnID)
	}
	state.mu.Unlock()
	sessionKey, sub := r.app.findSubmissionByTurn(threadID, turnID)
	if sub == nil {
		return &claudecli.PermissionResponse{Behavior: claudecli.PermissionDeny, Message: "no active submission for approval"}, nil
	}
	requestID := strings.TrimSpace(req.RequestID)
	kind, body, payload := r.claudeApprovalPresentation(sub.WorkspaceID, req)
	if err := r.app.sendClaudeApprovalCardWithPayload(kind, requestID, sessionKey, sub, threadID, turnID, requestID, body, payload); err != nil {
		return &claudecli.PermissionResponse{Behavior: claudecli.PermissionDeny, Message: err.Error()}, nil
	}
	pending := &claudePendingInteraction{
		kind:    kind,
		session: state,
		tool:    req.ToolName,
		respCh:  make(chan claudePendingResponse, 1),
	}
	r.storePending(requestID, pending)
	select {
	case <-ctx.Done():
		_ = r.CancelPending(requestID, ctx.Err().Error())
		return &claudecli.PermissionResponse{Behavior: claudecli.PermissionDeny, Message: ctx.Err().Error()}, nil
	case resp := <-pending.respCh:
		if resp.err != nil {
			return &claudecli.PermissionResponse{Behavior: claudecli.PermissionDeny, Message: resp.err.Error()}, nil
		}
		if resp.approval == nil {
			return &claudecli.PermissionResponse{Behavior: claudecli.PermissionDeny, Message: "approval response missing"}, nil
		}
		return resp.approval, nil
	}
}

func (r *claudeRuntime) handleAskUserQuestion(ctx context.Context, state *claudeSessionState, questions []claudecli.Question) (map[string]string, error) {
	state.mu.Lock()
	threadID := strings.TrimSpace(state.sessionID)
	turnID := ""
	if turn := state.turns[state.currentTurnNumber]; turn != nil {
		turnID = strings.TrimSpace(turn.TurnID)
	}
	state.mu.Unlock()
	sessionKey, sub := r.app.findSubmissionByTurn(threadID, turnID)
	if sub == nil {
		return nil, fmt.Errorf("no active submission for question")
	}
	requestID := "claude-question-" + strings.TrimSpace(turnID)
	if nextID, err := r.app.appState().nextLocalID("claude-question"); err == nil && strings.TrimSpace(nextID) != "" {
		requestID = nextID
	}
	payload := toolUserInputPayload{
		ThreadID:  threadID,
		TurnID:    turnID,
		ItemID:    requestID,
		Questions: claudeQuestionsAsToolUserInput(questions),
	}
	if len(payload.Questions) == 0 {
		return nil, fmt.Errorf("Claude question payload was empty")
	}
	if len(payload.Questions) == 1 && len(payload.Questions[0].Options) > 0 && len(payload.Questions[0].Options) <= 3 && !payload.Questions[0].MultiSelect && !payload.Questions[0].IsOther {
		if err := r.app.sendClaudeUserInputCard(requestID, sessionKey, sub, payload); err != nil {
			return nil, err
		}
	} else {
		if err := r.app.sendClaudeUserInputFormCard(requestID, sessionKey, sub, payload); err != nil {
			return nil, err
		}
	}
	pending := &claudePendingInteraction{
		kind:   "question",
		respCh: make(chan claudePendingResponse, 1),
	}
	r.storePending(requestID, pending)
	select {
	case <-ctx.Done():
		_ = r.CancelPending(requestID, ctx.Err().Error())
		return nil, ctx.Err()
	case resp := <-pending.respCh:
		if resp.err != nil {
			return nil, resp.err
		}
		if resp.answers == nil {
			return nil, fmt.Errorf("question response missing answers")
		}
		return resp.answers, nil
	}
}

func (r *claudeRuntime) handleExitPlanMode(ctx context.Context, state *claudeSessionState, plan claudecli.PlanInfo) (string, error) {
	state.mu.Lock()
	threadID := strings.TrimSpace(state.sessionID)
	turnID := ""
	if turn := state.turns[state.currentTurnNumber]; turn != nil {
		turnID = strings.TrimSpace(turn.TurnID)
	}
	workspaceID := strings.TrimSpace(state.workspaceID)
	planFilePath := strings.TrimSpace(state.lastPlanFilePath)
	startedAt := state.startedAt
	state.mu.Unlock()
	sessionKey, sub := r.app.findSubmissionByTurn(threadID, turnID)
	if sub == nil {
		return "", fmt.Errorf("no active submission for plan confirmation")
	}
	workspaceID = firstNonEmpty(strings.TrimSpace(sub.WorkspaceID), workspaceID)
	plan = enrichClaudePlanForDisplay(plan, planFilePath, r.app.workspaceCwd(workspaceID), startedAt)
	requestID := "claude-plan-" + strings.TrimSpace(turnID)
	if nextID, err := r.app.appState().nextLocalID("claude-plan"); err == nil && strings.TrimSpace(nextID) != "" {
		requestID = nextID
	}
	if err := r.app.sendClaudePlanModeCard(requestID, sessionKey, sub, threadID, turnID, claudePlanModeBody(plan)); err != nil {
		return "", err
	}
	pending := &claudePendingInteraction{
		kind:   "plan",
		respCh: make(chan claudePendingResponse, 1),
	}
	r.storePending(requestID, pending)
	select {
	case <-ctx.Done():
		_ = r.CancelPending(requestID, ctx.Err().Error())
		return "", ctx.Err()
	case resp := <-pending.respCh:
		if resp.err != nil {
			return "", resp.err
		}
		if strings.TrimSpace(resp.feedback) == "" {
			return "", fmt.Errorf("plan feedback is required")
		}
		return strings.TrimSpace(resp.feedback), nil
	}
}

func (r *claudeRuntime) claudeApprovalPresentation(workspaceID string, req *claudecli.PermissionRequest) (kind, body string, payload map[string]any) {
	payload = map[string]any{
		"tool":       req.ToolName,
		"toolName":   req.ToolName,
		"tool_input": req.Input,
	}
	if req.BlockedPath != nil && strings.TrimSpace(*req.BlockedPath) != "" {
		payload["blockedPath"] = strings.TrimSpace(*req.BlockedPath)
	}
	switch strings.TrimSpace(req.ToolName) {
	case "Bash", "KillShell":
		kind = "command"
		payload["command"] = strings.TrimSpace(firstNonEmpty(stringValue(req.Input["command"]), stringValue(req.Input["cmd"])))
		body = renderCommandApprovalBody(payload)
	case "Write", "Edit", "NotebookEdit":
		kind = "file"
		path := strings.TrimSpace(firstNonEmpty(
			stringValue(req.Input["file_path"]),
			stringValue(req.Input["path"]),
			stringValue(req.Input["notebook_path"]),
		))
		if path == "" && req.BlockedPath != nil {
			path = strings.TrimSpace(*req.BlockedPath)
		}
		payload["changes"] = []map[string]any{{"path": path, "kind": strings.TrimSpace(req.ToolName)}}
		body = renderFileApprovalBodyWithWorkspace(payload, r.app.workspaceCwd(workspaceID))
	default:
		kind = "permissions"
		payload["permissions"] = map[string]any{
			"tool": req.ToolName,
			"blocked_path": firstNonEmpty(func() string {
				if req.BlockedPath == nil {
					return ""
				}
				return strings.TrimSpace(*req.BlockedPath)
			}(), ""),
		}
		body = renderPermissionsApprovalBody(payload)
	}
	return kind, body, payload
}

func claudeTurnUsageAsThreadUsage(usage claudecli.TurnUsage) codexrpc.ThreadTokenUsage {
	last := codexrpc.TokenUsageBreakdown{
		InputTokens:       int64(usage.InputTokens),
		CachedInputTokens: int64(usage.CacheReadTokens + usage.CacheCreationTokens),
		OutputTokens:      int64(usage.OutputTokens),
	}
	last.TotalTokens = last.InputTokens + last.CachedInputTokens + last.OutputTokens
	return codexrpc.ThreadTokenUsage{
		Total: last,
		Last:  last,
	}
}

func claudeTurnContextUsagePercent(usage claudecli.TurnUsage) (float64, bool) {
	if usage.ContextWindow <= 0 {
		return 0, false
	}
	usedTokens := usage.InputTokens + usage.CacheCreationTokens + usage.CacheReadTokens
	if usedTokens < 0 {
		usedTokens = 0
	}
	percentage := float64(usedTokens) * 100 / float64(usage.ContextWindow)
	if percentage < 0 {
		percentage = 0
	}
	if percentage > 100 {
		percentage = 100
	}
	return percentage, true
}

func claudeQuestionsAsToolUserInput(questions []claudecli.Question) []toolUserInputQuestion {
	out := make([]toolUserInputQuestion, 0, len(questions))
	for i, question := range questions {
		id := fmt.Sprintf("q%d", i+1)
		opts := make([]toolUserInputOption, 0, len(question.Options))
		for _, opt := range question.Options {
			opts = append(opts, toolUserInputOption{Label: strings.TrimSpace(opt.Label)})
		}
		out = append(out, toolUserInputQuestion{
			Header:      id,
			ID:          id,
			Question:    strings.TrimSpace(question.Text),
			Options:     opts,
			MultiSelect: question.MultiSelect,
		})
	}
	return out
}

func claudePlanModeBody(plan claudecli.PlanInfo) string {
	lines := []string{
		"Claude 已完成计划阶段，请直接回复下一条消息作为反馈。",
		"",
		"可回复示例：",
		"- `Proceed`",
		"- `继续`",
		"- `请先改成 ...`",
	}
	if strings.TrimSpace(plan.Plan) != "" {
		lines = append(lines, "", "计划：", strings.TrimSpace(plan.Plan))
	}
	if len(plan.AllowedPrompts) > 0 {
		lines = append(lines, "", "计划申请的能力：")
		for _, prompt := range plan.AllowedPrompts {
			lines = append(lines, fmt.Sprintf("- `%s`: %s", strings.TrimSpace(prompt.Tool), strings.TrimSpace(prompt.Prompt)))
		}
	}
	return strings.Join(lines, "\n")
}

func claudeSessionApprovalKey(tool string) string {
	return strings.ToLower(strings.TrimSpace(tool))
}

func claudePermissionModeForWorkspace(cfg config.ClaudeConfig, ws *config.Workspace) claudecli.PermissionMode {
	mode := strings.TrimSpace(cfg.PermissionMode)
	if strings.TrimSpace(ws.ApprovalPolicy) == "never" {
		mode = "bypassPermissions"
	}
	switch mode {
	case string(claudecli.PermissionModeAcceptEdits):
		return claudecli.PermissionModeAcceptEdits
	case string(claudecli.PermissionModePlan):
		return claudecli.PermissionModePlan
	case string(claudecli.PermissionModeBypass):
		return claudecli.PermissionModeBypass
	default:
		return claudecli.PermissionModeDefault
	}
}

func isClaudeInternalTool(name string) bool {
	switch strings.TrimSpace(name) {
	case "AskUserQuestion", "ExitPlanMode":
		return true
	default:
		return false
	}
}

func claudePlanFilePathFromTool(toolName string, input map[string]interface{}) string {
	if strings.TrimSpace(toolName) != "Write" {
		return ""
	}
	path := strings.TrimSpace(firstNonEmpty(
		stringValue(input["file_path"]),
		stringValue(input["path"]),
	))
	if !isClaudePlanFilePath(path) {
		return ""
	}
	return path
}

func isClaudePlanFilePath(path string) bool {
	path = filepath.ToSlash(strings.TrimSpace(path))
	return path != "" && strings.Contains(path, ".claude/plans/") && strings.HasSuffix(strings.ToLower(path), ".md")
}

func enrichClaudePlanForDisplay(plan claudecli.PlanInfo, trackedPlanPath, workspaceCwd string, startedAt time.Time) claudecli.PlanInfo {
	if strings.TrimSpace(plan.Plan) != "" {
		return plan
	}
	if text := readClaudePlanText(trackedPlanPath, workspaceCwd, startedAt); text != "" {
		plan.Plan = text
	}
	return plan
}

func readClaudePlanText(trackedPlanPath, workspaceCwd string, startedAt time.Time) string {
	for _, candidate := range claudePlanFileCandidates(trackedPlanPath, workspaceCwd, startedAt) {
		data, err := os.ReadFile(candidate)
		if err != nil {
			continue
		}
		if text := strings.TrimSpace(string(data)); text != "" {
			return text
		}
	}
	return ""
}

func claudePlanFileCandidates(trackedPlanPath, workspaceCwd string, startedAt time.Time) []string {
	seen := map[string]bool{}
	candidates := make([]string, 0, 4)
	add := func(path string) {
		path = strings.TrimSpace(path)
		if path == "" {
			return
		}
		if !filepath.IsAbs(path) && strings.TrimSpace(workspaceCwd) != "" {
			path = filepath.Join(workspaceCwd, path)
		}
		path = filepath.Clean(path)
		if seen[path] {
			return
		}
		seen[path] = true
		candidates = append(candidates, path)
	}

	add(trackedPlanPath)
	if latest := latestClaudePlanFile(filepath.Join(strings.TrimSpace(workspaceCwd), ".claude", "plans"), startedAt); latest != "" {
		add(latest)
	}
	if home, err := os.UserHomeDir(); err == nil {
		if latest := latestClaudePlanFile(filepath.Join(home, ".claude", "plans"), startedAt); latest != "" {
			add(latest)
		}
	}
	return candidates
}

func latestClaudePlanFile(dir string, startedAt time.Time) string {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return ""
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	var latestPath string
	var latestTime time.Time
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".md") {
			continue
		}
		fullPath := filepath.Join(dir, entry.Name())
		if !isClaudePlanFilePath(fullPath) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		modTime := info.ModTime()
		if !startedAt.IsZero() && modTime.Before(startedAt.Add(-1*time.Minute)) {
			continue
		}
		if latestPath == "" || modTime.After(latestTime) {
			latestPath = fullPath
			latestTime = modTime
		}
	}
	return latestPath
}

func buildClaudePrompt(sub *state.Submission) string {
	if sub == nil {
		return ""
	}
	parts := make([]string, 0, len(sub.Skills)+1+len(sub.Attachments))
	for _, skill := range sub.Skills {
		if strings.TrimSpace(skill.Name) == "" && strings.TrimSpace(skill.Path) == "" {
			continue
		}
		parts = append(parts, fmt.Sprintf("Use skill `%s` (`%s`) if it is available in this Claude session.", firstNonEmpty(strings.TrimSpace(skill.Name), "skill"), firstNonEmpty(strings.TrimSpace(skill.Path), "-")))
	}
	if text := strings.TrimSpace(sub.InputText); text != "" {
		parts = append(parts, text)
	}
	for _, attachment := range sub.Attachments {
		if prompt := attachmentPrompt(attachment); prompt != "" {
			parts = append(parts, prompt)
		}
	}
	return strings.TrimSpace(strings.Join(parts, "\n\n"))
}
