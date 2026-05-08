package claudecli

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
)

type Session struct {
	mu sync.Mutex

	cfg    SessionConfig
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	stderr io.ReadCloser
	reader *ndjsonReader
	writer *ndjsonWriter

	started bool
	stopped bool
	exitErr error

	done       chan struct{}
	doneOnce   sync.Once
	eventsOnce sync.Once
	events     chan Event
	waitDone   chan struct{}
	turnNumber int
	current    *turnState
	pending    []int
	turns      map[int]*turnState
	info       *SessionInfo
	pendingCtl map[string]chan wireControlResponsePayload
}

type turnState struct {
	Number        int
	StartTime     time.Time
	Tools         map[string]*toolState
	SeenAssistant map[string]bool
}

type toolState struct {
	ID        string
	Name      string
	Input     map[string]any
	Completed bool
}

func NewSession(opts ...SessionOption) *Session {
	cfg := defaultConfig()
	for _, opt := range opts {
		opt(&cfg)
	}
	return &Session{
		cfg:        cfg,
		done:       make(chan struct{}),
		events:     make(chan Event, cfg.EventBufferSize),
		waitDone:   make(chan struct{}),
		turns:      map[int]*turnState{},
		pending:    []int{},
		pendingCtl: map[string]chan wireControlResponsePayload{},
	}
}

func (s *Session) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.started {
		return ErrAlreadyStarted
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	args := s.cliArgs()
	command := strings.TrimSpace(s.cfg.CLIPath)
	if command == "" {
		command = "claude"
	}

	cmd := exec.Command(command, args...)
	if s.cfg.WorkDir != "" {
		cmd.Dir = s.cfg.WorkDir
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return &ProcessError{Message: "failed to create stdin pipe", Cause: err}
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return &ProcessError{Message: "failed to create stdout pipe", Cause: err}
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return &ProcessError{Message: "failed to create stderr pipe", Cause: err}
	}
	if err := cmd.Start(); err != nil {
		return &ProcessError{Message: "failed to start Claude CLI", Cause: err}
	}

	s.cmd = cmd
	s.stdin = stdin
	s.stdout = stdout
	s.stderr = stderr
	s.reader = newNDJSONReader(stdout)
	s.writer = newNDJSONWriter(stdin)
	s.started = true

	go s.waitLoop(cmd)
	go s.readLoop()
	if s.cfg.StderrHandler != nil {
		go s.stderrLoop()
	}
	go s.stopOnContextDone(ctx)
	return nil
}

func (s *Session) Stop() error {
	s.mu.Lock()
	if !s.started || s.stopped {
		s.mu.Unlock()
		return nil
	}
	s.stopped = true
	stdin := s.stdin
	cmd := s.cmd
	s.mu.Unlock()

	s.closeDone()
	if stdin != nil {
		_ = stdin.Close()
	}
	if cmd == nil || cmd.Process == nil {
		s.closeEvents()
		return nil
	}

	if s.waitForExit(500 * time.Millisecond) {
		s.closeEvents()
		return nil
	}

	_ = cmd.Process.Signal(syscall.SIGTERM)

	if s.waitForExit(500 * time.Millisecond) {
		s.closeEvents()
		return nil
	}

	_ = cmd.Process.Kill()

	_ = s.waitForExit(100 * time.Millisecond)
	s.closeEvents()
	return nil
}

func (s *Session) Events() <-chan Event {
	return s.events
}

func (s *Session) Stopped() bool {
	return s.isStopped()
}

func (s *Session) ExitError() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.exitErr
}

func (s *Session) SendMessage(ctx context.Context, content string) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.started {
		return 0, ErrNotStarted
	}
	if s.stopped {
		return 0, ErrStopping
	}

	s.turnNumber++
	turn := &turnState{
		Number:        s.turnNumber,
		StartTime:     time.Now(),
		Tools:         map[string]*toolState{},
		SeenAssistant: map[string]bool{},
	}
	s.turns[turn.Number] = turn
	if s.current == nil {
		s.current = turn
	} else {
		s.pending = append(s.pending, turn.Number)
	}

	msg := wireUserMessageToSend{
		Type: "user",
		Message: wireUserMessageInner{
			Role:    "user",
			Content: content,
		},
	}
	if err := s.writer.Write(msg); err != nil {
		return 0, err
	}
	return turn.Number, nil
}

// SendSteerInput writes a user message to the CLI's stdin without creating
// a separate turn state. The message becomes part of the current conversation
// round. Use this for steer messages that should be processed together with
// the active turn rather than as independent turns.
func (s *Session) SendSteerInput(ctx context.Context, content string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.started {
		return ErrNotStarted
	}
	if s.stopped {
		return ErrStopping
	}
	if s.current == nil {
		return fmt.Errorf("no active turn to steer into")
	}

	msg := wireUserMessageToSend{
		Type: "user",
		Message: wireUserMessageInner{
			Role:    "user",
			Content: content,
		},
	}
	return s.writer.Write(msg)
}

func (s *Session) Interrupt(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.started {
		return ErrNotStarted
	}
	if s.stopped {
		return ErrStopping
	}

	return s.writer.Write(wireControlRequestToSend{
		Type:      "control_request",
		RequestID: generateRequestID(),
		Request: wireInterruptRequest{
			Subtype: "interrupt",
		},
	})
}

func (s *Session) Initialize(ctx context.Context) error {
	_, err := s.sendControlRequestAndWait(ctx, wireInitializeRequest{
		Subtype: "initialize",
		Hooks:   map[string]any{},
	})
	return err
}

func (s *Session) SetPermissionMode(ctx context.Context, mode PermissionMode) error {
	_, err := s.sendControlRequestAndWait(ctx, wireSetPermissionModeRequest{
		Subtype: "set_permission_mode",
		Mode:    string(mode),
	})
	if err != nil {
		return err
	}
	s.mu.Lock()
	if s.info != nil {
		s.info.PermissionMode = mode
	}
	s.cfg.PermissionMode = mode
	s.mu.Unlock()
	return nil
}

func (s *Session) SetModel(ctx context.Context, model string) error {
	model = strings.TrimSpace(model)
	_, err := s.sendControlRequestAndWait(ctx, wireSetModelRequest{
		Subtype: "set_model",
		Model:   model,
	})
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.cfg.Model = model
	s.mu.Unlock()
	return nil
}

func (s *Session) SetEffort(ctx context.Context, effort string) error {
	effort = strings.TrimSpace(effort)
	if effort == "" {
		return ErrEffortDefaultHotApplyUnsupported
	}
	_, err := s.sendControlRequestAndWait(ctx, wireApplyFlagSettingsRequest{
		Subtype: "apply_flag_settings",
		Settings: map[string]any{
			"effortLevel": effort,
		},
	})
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.cfg.Effort = effort
	s.mu.Unlock()
	return nil
}

func (s *Session) sendControlRequestAndWait(ctx context.Context, request any) (wireControlResponsePayload, error) {
	if err := ctx.Err(); err != nil {
		return wireControlResponsePayload{}, err
	}

	requestID := generateRequestID()
	respCh := make(chan wireControlResponsePayload, 1)

	s.mu.Lock()
	if !s.started {
		s.mu.Unlock()
		return wireControlResponsePayload{}, ErrNotStarted
	}
	if s.stopped {
		s.mu.Unlock()
		return wireControlResponsePayload{}, ErrStopping
	}
	if s.pendingCtl == nil {
		s.pendingCtl = map[string]chan wireControlResponsePayload{}
	}
	s.pendingCtl[requestID] = respCh
	writer := s.writer
	s.mu.Unlock()

	cleanup := func() {
		s.mu.Lock()
		delete(s.pendingCtl, requestID)
		s.mu.Unlock()
	}

	if writer == nil {
		cleanup()
		return wireControlResponsePayload{}, ErrNotStarted
	}
	if err := writer.Write(wireControlRequestToSend{
		Type:      "control_request",
		RequestID: requestID,
		Request:   request,
	}); err != nil {
		cleanup()
		return wireControlResponsePayload{}, err
	}

	select {
	case <-ctx.Done():
		cleanup()
		return wireControlResponsePayload{}, ctx.Err()
	case <-s.done:
		cleanup()
		return wireControlResponsePayload{}, s.processExitError()
	case <-s.waitDone:
		cleanup()
		return wireControlResponsePayload{}, s.processExitError()
	case resp := <-respCh:
		cleanup()
		switch strings.TrimSpace(resp.Subtype) {
		case "success":
			return resp, nil
		case "error":
			return wireControlResponsePayload{}, fmt.Errorf("control request failed: %s", strings.TrimSpace(resp.Error))
		default:
			return wireControlResponsePayload{}, fmt.Errorf("unexpected control response subtype: %s", strings.TrimSpace(resp.Subtype))
		}
	}
}

func (s *Session) processExitError() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.exitErr != nil {
		return &ProcessError{Message: "Claude CLI process exited", Cause: s.exitErr}
	}
	if s.stopped {
		return &ProcessError{Message: "Claude CLI process exited"}
	}
	return ErrStopping
}

func (s *Session) cliArgs() []string {
	args := []string{
		"--print",
		"--input-format", "stream-json",
		"--output-format", "stream-json",
		"--verbose",
	}
	if strings.TrimSpace(s.cfg.Model) != "" {
		args = append(args, "--model", strings.TrimSpace(s.cfg.Model))
	}
	if strings.TrimSpace(s.cfg.Effort) != "" {
		args = append(args, "--effort", strings.TrimSpace(s.cfg.Effort))
	}
	if strings.TrimSpace(string(s.cfg.PermissionMode)) != "" {
		args = append(args, "--permission-mode", string(s.cfg.PermissionMode))
	}
	if s.cfg.DangerouslySkipPermissions {
		args = append(args, "--dangerously-skip-permissions")
	}
	if s.cfg.DisablePlugins {
		args = append(args, "--plugin-dir", "/dev/null")
	}
	if s.cfg.PermissionPromptToolStdio {
		args = append(args, "--permission-prompt-tool", "stdio")
	}
	if strings.TrimSpace(s.cfg.SystemPrompt) != "" {
		args = append(args, "--system-prompt", strings.TrimSpace(s.cfg.SystemPrompt))
	}
	if strings.TrimSpace(s.cfg.Resume) != "" {
		args = append(args, "--resume", strings.TrimSpace(s.cfg.Resume))
	}
	if s.cfg.ForkSession {
		args = append(args, "--fork-session")
	}
	args = append(args, "--include-partial-messages")
	return args
}

func (s *Session) readLoop() {
	defer s.closeEvents()
	for {
		select {
		case <-s.done:
			return
		default:
		}

		line, err := s.reader.ReadLine()
		if err != nil {
			if !s.isStopped() {
				if err == io.EOF {
					s.emitProcessExit("stdout_eof")
				} else {
					s.emitError(err, "read_line")
				}
			}
			return
		}
		s.handleLine(line)
	}
}

func (s *Session) waitLoop(cmd *exec.Cmd) {
	err := cmd.Wait()
	s.mu.Lock()
	s.exitErr = err
	waitDone := s.waitDone
	s.mu.Unlock()
	if waitDone != nil {
		close(waitDone)
	}
}

func (s *Session) stopOnContextDone(ctx context.Context) {
	if ctx == nil {
		return
	}
	select {
	case <-s.done:
		return
	case <-ctx.Done():
		_ = s.Stop()
	}
}

func (s *Session) stderrLoop() {
	buf := make([]byte, 4096)
	for {
		select {
		case <-s.done:
			return
		default:
		}
		n, err := s.stderr.Read(buf)
		if err != nil {
			return
		}
		if n > 0 {
			s.cfg.StderrHandler(buf[:n])
		}
	}
}

func (s *Session) handleLine(line []byte) {
	msg, err := parseWireMessage(line)
	if err != nil {
		s.emitError(&ProtocolError{Message: "failed to parse message", Line: string(line), Cause: err}, "parse_message")
		return
	}

	switch value := msg.(type) {
	case wireSystemMessage:
		s.handleSystemMessage(value)
	case wireStreamMessage:
		s.handleStreamMessage(value)
	case wireAssistantMessage:
		s.handleAssistantMessage(value)
	case wireResultMessage:
		s.handleResultMessage(value)
	case wireControlRequest:
		s.handleControlRequest(value)
	case wireControlResponse:
		s.handleControlResponse(value)
	}
}

func (s *Session) handleSystemMessage(msg wireSystemMessage) {
	if msg.Subtype != "init" {
		return
	}
	info := &SessionInfo{
		SessionID:      strings.TrimSpace(msg.SessionID),
		Model:          strings.TrimSpace(msg.Model),
		WorkDir:        strings.TrimSpace(msg.CWD),
		Tools:          append([]string(nil), msg.Tools...),
		PermissionMode: PermissionMode(strings.TrimSpace(msg.PermissionMode)),
	}
	s.mu.Lock()
	s.info = info
	s.mu.Unlock()
	s.emit(ReadyEvent{Info: *info})
}

func (s *Session) handleStreamMessage(msg wireStreamMessage) {
	event, err := parseWireStreamEvent(msg.Event)
	if err != nil {
		s.emitError(err, "parse_stream_event")
		return
	}
	switch event.(type) {
	case wireMessageStartEvent:
		s.mu.Lock()
		if s.current == nil && len(s.pending) > 0 {
			nextNumber := s.pending[0]
			s.pending = append([]int(nil), s.pending[1:]...)
			s.current = s.turns[nextNumber]
		}
		turnNumber := 0
		if s.current != nil {
			turnNumber = s.current.Number
		}
		s.mu.Unlock()
		if turnNumber != 0 {
			s.emit(TurnStartedEvent{TurnNumber: turnNumber})
		}
	}
}

func (s *Session) handleAssistantMessage(msg wireAssistantMessage) {
	role := strings.TrimSpace(msg.Message.Role)
	if role != "" && role != "assistant" {
		return
	}

	blocks, ok, err := msg.Message.Content.AsBlocks()
	if err != nil {
		s.emitError(err, "parse_assistant_message")
		return
	}
	if !ok || len(blocks) == 0 {
		return
	}

	s.mu.Lock()
	turn := s.current
	turnNumber := 0
	if turn != nil {
		turnNumber = turn.Number
	}
	s.mu.Unlock()
	if turnNumber == 0 {
		return
	}

	messageID := strings.TrimSpace(msg.Message.ID)
	for i := 0; i < len(blocks); {
		switch value := blocks[i].(type) {
		case wireTextBlock:
			text := value.Text
			for j := i + 1; j < len(blocks); j++ {
				next, ok := blocks[j].(wireTextBlock)
				if !ok {
					break
				}
				text += next.Text
				i = j
			}
			if text != "" && s.markAssistantBlockSeen(turnNumber, assistantBlockSeenKey(messageID, "text", i, text)) {
				s.emit(TextEvent{TurnNumber: turnNumber, Text: text, FullText: text})
			}
		case wireThinkingBlock:
			thinking := value.Thinking
			for j := i + 1; j < len(blocks); j++ {
				next, ok := blocks[j].(wireThinkingBlock)
				if !ok {
					break
				}
				thinking += next.Thinking
				i = j
			}
			if thinking != "" && s.markAssistantBlockSeen(turnNumber, assistantBlockSeenKey(messageID, "thinking", i, thinking)) {
				s.emit(ThinkingEvent{TurnNumber: turnNumber, Thinking: thinking, FullThinking: thinking})
			}
		case wireToolUseBlock:
			s.completeToolFromAssistant(turnNumber, value)
		}
		i++
	}
}

func (s *Session) markAssistantBlockSeen(turnNumber int, key string) bool {
	if strings.TrimSpace(key) == "" {
		return true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	turn := s.turns[turnNumber]
	if turn == nil {
		return false
	}
	if turn.SeenAssistant == nil {
		turn.SeenAssistant = map[string]bool{}
	}
	if turn.SeenAssistant[key] {
		return false
	}
	turn.SeenAssistant[key] = true
	return true
}

func assistantBlockSeenKey(messageID, blockType string, index int, content string) string {
	messageID = strings.TrimSpace(messageID)
	if messageID == "" {
		return ""
	}
	return fmt.Sprintf("%s:%s:%d:%s", messageID, strings.TrimSpace(blockType), index, content)
}

func (s *Session) completeToolFromAssistant(turnNumber int, block wireToolUseBlock) {
	input := copyMap(block.Input)

	s.mu.Lock()
	turn := s.turns[turnNumber]
	if turn == nil {
		s.mu.Unlock()
		return
	}
	if turn.Tools == nil {
		turn.Tools = map[string]*toolState{}
	}
	if tool := turn.Tools[block.ID]; tool != nil && tool.Completed {
		s.mu.Unlock()
		return
	}
	turn.Tools[block.ID] = &toolState{
		ID:        block.ID,
		Name:      block.Name,
		Input:     input,
		Completed: true,
	}
	s.mu.Unlock()

	s.emit(ToolCompleteEvent{
		TurnNumber: turnNumber,
		ID:         block.ID,
		Name:       block.Name,
		Input:      copyMap(input),
		Timestamp:  time.Now(),
	})
}

func (s *Session) handleResultMessage(msg wireResultMessage) {
	s.mu.Lock()
	turn := s.current
	if turn == nil {
		s.mu.Unlock()
		return
	}
	turnNumber := turn.Number
	delete(s.turns, turnNumber)
	s.current = nil

	if len(s.pending) > 0 {
		nextNumber := s.pending[0]
		s.pending = append([]int(nil), s.pending[1:]...)
		s.current = s.turns[nextNumber]
	}
	durationMs := msg.DurationMs
	if durationMs == 0 {
		durationMs = time.Since(turn.StartTime).Milliseconds()
	}
	s.mu.Unlock()

	var err error
	if msg.IsError {
		err = fmt.Errorf("%s", msg.Result)
	}
	cumulativeModelUsage, hasCumulativeModelUsage := aggregateWireModelUsage(msg.ModelUsage)
	s.emit(TurnCompleteEvent{
		TurnNumber: turnNumber,
		Success:    !msg.IsError,
		DurationMs: durationMs,
		Usage: TurnUsage{
			InputTokens:                   msg.Usage.InputTokens,
			OutputTokens:                  msg.Usage.OutputTokens,
			CacheReadTokens:               msg.Usage.CacheReadInputTokens,
			CacheCreationTokens:           msg.Usage.CacheCreationInputTokens,
			CumulativeInputTokens:         cumulativeModelUsage.InputTokens,
			CumulativeOutputTokens:        cumulativeModelUsage.OutputTokens,
			CumulativeCacheReadTokens:     cumulativeModelUsage.CacheReadInputTokens,
			CumulativeCacheCreationTokens: cumulativeModelUsage.CacheCreationInputTokens,
			HasCumulativeUsage:            hasCumulativeModelUsage,
			ContextWindow:                 msg.contextWindow(),
			CostUSD:                       msg.TotalCostUSD,
		},
		Error:  err,
		Result: msg.Result,
	})
}

func (s *Session) handleControlRequest(msg wireControlRequest) {
	ctx := context.Background()

	toolReq, err := parseToolUseRequest(msg.Request)
	if err != nil {
		s.emitError(err, "parse_control_request")
		return
	}
	if toolReq == nil {
		return
	}

	switch toolReq.ToolName {
	case "AskUserQuestion":
		if s.cfg.InteractiveToolHandler != nil {
			resp := s.buildAskUserQuestionResponse(ctx, msg.RequestID, toolReq)
			s.writeControlResponse(resp, "send_control_response")
			return
		}
	case "ExitPlanMode":
		if s.cfg.InteractiveToolHandler != nil {
			resp := s.buildExitPlanModeResponse(ctx, msg.RequestID, toolReq)
			s.writeControlResponse(resp, "send_control_response")
			return
		}
	}

	resp := s.buildPermissionResponse(ctx, msg.RequestID, toolReq)
	s.writeControlResponse(resp, "send_permission_response")
}

func (s *Session) handleControlResponse(msg wireControlResponse) {
	requestID := strings.TrimSpace(msg.Response.RequestID)
	if requestID == "" {
		return
	}

	s.mu.Lock()
	respCh := s.pendingCtl[requestID]
	s.mu.Unlock()
	if respCh == nil {
		return
	}

	select {
	case respCh <- msg.Response:
	default:
	}
}

func (s *Session) buildAskUserQuestionResponse(ctx context.Context, requestID string, toolReq *wireToolUseRequest) *wireControlResponse {
	questions, err := ParseQuestionsFromInput(toolReq.Input)
	if err != nil {
		return denyControlResponse(requestID, err.Error(), false)
	}
	answers, err := s.cfg.InteractiveToolHandler.HandleAskUserQuestion(ctx, questions)
	if err != nil {
		return denyControlResponse(requestID, err.Error(), false)
	}
	updatedInput := copyMap(toolReq.Input)
	updatedInput["answers"] = answers
	return allowControlResponse(requestID, updatedInput, nil)
}

func (s *Session) buildExitPlanModeResponse(ctx context.Context, requestID string, toolReq *wireToolUseRequest) *wireControlResponse {
	info, err := ParsePlanInfoFromInput(toolReq.Input)
	if err != nil {
		return denyControlResponse(requestID, err.Error(), false)
	}
	feedback, err := s.cfg.InteractiveToolHandler.HandleExitPlanMode(ctx, info)
	if err != nil {
		return denyControlResponse(requestID, err.Error(), true)
	}
	updatedInput := copyMap(toolReq.Input)
	updatedInput["feedback"] = feedback
	return allowControlResponse(requestID, updatedInput, nil)
}

func (s *Session) buildPermissionResponse(ctx context.Context, requestID string, toolReq *wireToolUseRequest) *wireControlResponse {
	if s.cfg.PermissionHandler == nil {
		return denyControlResponse(requestID, "No permission handler configured", false)
	}
	resp, err := s.cfg.PermissionHandler.HandlePermission(ctx, &PermissionRequest{
		RequestID:             requestID,
		ToolName:              toolReq.ToolName,
		Input:                 copyMap(toolReq.Input),
		BlockedPath:           toolReq.BlockedPath,
		PermissionSuggestions: copyPermissionSuggestions(toolReq.PermissionSuggestions),
	})
	if err != nil {
		s.emitError(err, "permission_handling")
		return denyControlResponse(requestID, "Permission handler error", false)
	}
	if resp == nil {
		return denyControlResponse(requestID, "Permission response missing", false)
	}
	if resp.Behavior == PermissionAllow {
		updatedInput := copyMap(resp.UpdatedInput)
		if updatedInput == nil {
			updatedInput = copyMap(toolReq.Input)
		}
		if updatedInput == nil {
			updatedInput = map[string]any{}
		}
		return allowControlResponse(requestID, updatedInput, resp.UpdatedPermissions)
	}
	return denyControlResponse(requestID, resp.Message, resp.Interrupt)
}

func (s *Session) writeControlResponse(resp *wireControlResponse, contextLabel string) {
	if resp == nil {
		return
	}
	s.mu.Lock()
	writer := s.writer
	s.mu.Unlock()
	if writer == nil {
		return
	}
	if err := writer.Write(resp); err != nil {
		s.emitError(err, contextLabel)
	}
}

func allowControlResponse(requestID string, updatedInput map[string]any, updates []map[string]any) *wireControlResponse {
	if updatedInput == nil {
		updatedInput = map[string]any{}
	}
	return &wireControlResponse{
		Type: "control_response",
		Response: wireControlResponsePayload{
			Subtype:   "success",
			RequestID: requestID,
			Response: wirePermissionAllow{
				Behavior:           "allow",
				UpdatedInput:       updatedInput,
				UpdatedPermissions: updates,
			},
		},
	}
}

func denyControlResponse(requestID, message string, interrupt bool) *wireControlResponse {
	return &wireControlResponse{
		Type: "control_response",
		Response: wireControlResponsePayload{
			Subtype:   "success",
			RequestID: requestID,
			Response: wirePermissionDeny{
				Behavior:  "deny",
				Message:   message,
				Interrupt: interrupt,
			},
		},
	}
}

func (s *Session) emit(event Event) {
	select {
	case <-s.done:
		return
	default:
	}

	select {
	case s.events <- event:
	case <-s.done:
	default:
		slog.Warn("Claude session event dropped (channel full)",
			"event_type", fmt.Sprintf("%T", event),
		)
	}
}

func (s *Session) emitError(err error, contextLabel string) {
	s.mu.Lock()
	turnNumber := 0
	if s.current != nil {
		turnNumber = s.current.Number
	}
	s.mu.Unlock()
	s.emit(ErrorEvent{
		TurnNumber: turnNumber,
		Error:      err,
		Context:    contextLabel,
	})
}

func (s *Session) emitProcessExit(contextLabel string) {
	waited := s.waitForExit(250 * time.Millisecond)
	s.mu.Lock()
	exitErr := s.exitErr
	s.mu.Unlock()
	if waited {
		if exitErr != nil {
			s.emitError(&ProcessError{Message: "Claude CLI process exited", Cause: exitErr}, contextLabel)
			return
		}
		s.emitError(&ProcessError{Message: "Claude CLI process exited"}, contextLabel)
		return
	}
	s.emitError(&ProcessError{Message: "Claude CLI process exited before wait completed"}, contextLabel)
}

func (s *Session) closeDone() {
	s.doneOnce.Do(func() {
		close(s.done)
	})
}

func (s *Session) closeEvents() {
	s.eventsOnce.Do(func() {
		s.closeDone()
		s.mu.Lock()
		s.stopped = true
		s.mu.Unlock()
		close(s.events)
	})
}

func (s *Session) isStopped() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stopped
}

func (s *Session) waitForExit(timeout time.Duration) bool {
	s.mu.Lock()
	waitDone := s.waitDone
	s.mu.Unlock()
	if waitDone == nil {
		return true
	}
	if timeout <= 0 {
		select {
		case <-waitDone:
			return true
		default:
			return false
		}
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-waitDone:
		return true
	case <-timer.C:
		return false
	}
}

func copyMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func copyPermissionSuggestions(in []map[string]any) []map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(in))
	for _, item := range in {
		if copied := copyMap(item); copied != nil {
			out = append(out, copied)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
