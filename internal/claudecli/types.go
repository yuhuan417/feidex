package claudecli

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

type PermissionMode string

const (
	PermissionModeDefault     PermissionMode = "default"
	PermissionModeAcceptEdits PermissionMode = "acceptEdits"
	PermissionModePlan        PermissionMode = "plan"
	PermissionModeBypass      PermissionMode = "bypassPermissions"
)

type PermissionBehavior string

const (
	PermissionAllow PermissionBehavior = "allow"
	PermissionDeny  PermissionBehavior = "deny"
)

var (
	ErrAlreadyStarted = errors.New("session already started")
	ErrNotStarted     = errors.New("session not started")
	ErrStopping       = errors.New("session is stopping")
)

type SessionConfig struct {
	Model                     string
	Effort                    string
	WorkDir                   string
	PermissionMode            PermissionMode
	CLIPath                   string
	DisablePlugins            bool
	SystemPrompt              string
	PermissionPromptToolStdio bool
	Resume                    string
	ForkSession               bool
	EventBufferSize           int
	StderrHandler             func([]byte)
	PermissionHandler         PermissionHandler
	InteractiveToolHandler    InteractiveToolHandler
}

type SessionOption func(*SessionConfig)

func defaultConfig() SessionConfig {
	return SessionConfig{
		PermissionMode:  PermissionModeDefault,
		EventBufferSize: 128,
	}
}

func WithModel(model string) SessionOption {
	return func(c *SessionConfig) { c.Model = strings.TrimSpace(model) }
}

func WithEffort(effort string) SessionOption {
	return func(c *SessionConfig) { c.Effort = strings.TrimSpace(effort) }
}

func WithWorkDir(dir string) SessionOption {
	return func(c *SessionConfig) { c.WorkDir = strings.TrimSpace(dir) }
}

func WithPermissionMode(mode PermissionMode) SessionOption {
	return func(c *SessionConfig) { c.PermissionMode = mode }
}

func WithCLIPath(path string) SessionOption {
	return func(c *SessionConfig) { c.CLIPath = strings.TrimSpace(path) }
}

func WithDisablePlugins() SessionOption {
	return func(c *SessionConfig) { c.DisablePlugins = true }
}

func WithSystemPrompt(prompt string) SessionOption {
	return func(c *SessionConfig) { c.SystemPrompt = strings.TrimSpace(prompt) }
}

func WithPermissionPromptToolStdio() SessionOption {
	return func(c *SessionConfig) { c.PermissionPromptToolStdio = true }
}

func WithResume(sessionID string) SessionOption {
	return func(c *SessionConfig) { c.Resume = strings.TrimSpace(sessionID) }
}

func WithForkSession() SessionOption {
	return func(c *SessionConfig) { c.ForkSession = true }
}

func WithEventBufferSize(size int) SessionOption {
	return func(c *SessionConfig) {
		if size > 0 {
			c.EventBufferSize = size
		}
	}
}

func WithStderrHandler(handler func([]byte)) SessionOption {
	return func(c *SessionConfig) { c.StderrHandler = handler }
}

func WithPermissionHandler(handler PermissionHandler) SessionOption {
	return func(c *SessionConfig) { c.PermissionHandler = handler }
}

func WithInteractiveToolHandler(handler InteractiveToolHandler) SessionOption {
	return func(c *SessionConfig) { c.InteractiveToolHandler = handler }
}

type SessionInfo struct {
	SessionID      string
	Model          string
	WorkDir        string
	Tools          []string
	PermissionMode PermissionMode
}

type TurnUsage struct {
	InputTokens                   int
	OutputTokens                  int
	CacheReadTokens               int
	CacheCreationTokens           int
	CumulativeInputTokens         int
	CumulativeOutputTokens        int
	CumulativeCacheReadTokens     int
	CumulativeCacheCreationTokens int
	HasCumulativeUsage            bool
	ContextWindow                 int
	CostUSD                       float64
}

type EventType int

const (
	EventTypeReady EventType = iota
	EventTypeTurnStarted
	EventTypeText
	EventTypeThinking
	EventTypeToolComplete
	EventTypeTurnComplete
	EventTypeError
)

type Event interface {
	Type() EventType
}

type ReadyEvent struct {
	Info SessionInfo
}

func (e ReadyEvent) Type() EventType { return EventTypeReady }

type TurnStartedEvent struct {
	TurnNumber int
}

func (e TurnStartedEvent) Type() EventType { return EventTypeTurnStarted }

type TextEvent struct {
	TurnNumber int
	Text       string
	FullText   string
}

func (e TextEvent) Type() EventType { return EventTypeText }

type ThinkingEvent struct {
	TurnNumber   int
	Thinking     string
	FullThinking string
}

func (e ThinkingEvent) Type() EventType { return EventTypeThinking }

type ToolCompleteEvent struct {
	TurnNumber int
	ID         string
	Name       string
	Input      map[string]any
	Timestamp  time.Time
}

func (e ToolCompleteEvent) Type() EventType { return EventTypeToolComplete }

type TurnCompleteEvent struct {
	TurnNumber int
	Success    bool
	DurationMs int64
	Usage      TurnUsage
	Error      error
	Result     string
}

func (e TurnCompleteEvent) Type() EventType { return EventTypeTurnComplete }

type ErrorEvent struct {
	TurnNumber int
	Error      error
	Context    string
}

func (e ErrorEvent) Type() EventType { return EventTypeError }

type PermissionRequest struct {
	RequestID   string
	ToolName    string
	Input       map[string]any
	BlockedPath *string
}

type PermissionResponse struct {
	Behavior           PermissionBehavior
	Message            string
	Interrupt          bool
	UpdatedInput       map[string]any
	UpdatedPermissions []map[string]any
}

type PermissionHandler interface {
	HandlePermission(context.Context, *PermissionRequest) (*PermissionResponse, error)
}

type PermissionHandlerFunc func(context.Context, *PermissionRequest) (*PermissionResponse, error)

func (f PermissionHandlerFunc) HandlePermission(ctx context.Context, req *PermissionRequest) (*PermissionResponse, error) {
	return f(ctx, req)
}

type InteractiveToolHandler interface {
	HandleAskUserQuestion(context.Context, []Question) (map[string]string, error)
	HandleExitPlanMode(context.Context, PlanInfo) (string, error)
}

type Question struct {
	Text        string
	Options     []QuestionOption
	MultiSelect bool
}

type QuestionOption struct {
	Label string
}

type PlanInfo struct {
	Plan           string
	AllowedPrompts []AllowedPrompt
}

type AllowedPrompt struct {
	Tool   string
	Prompt string
}

type ProtocolError struct {
	Message string
	Line    string
	Cause   error
}

func (e *ProtocolError) Error() string {
	if e == nil {
		return ""
	}
	if e.Cause != nil {
		return fmt.Sprintf("protocol error: %s: %v", e.Message, e.Cause)
	}
	return "protocol error: " + e.Message
}

func (e *ProtocolError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

type ProcessError struct {
	Message string
	Cause   error
}

func (e *ProcessError) Error() string {
	if e == nil {
		return ""
	}
	if e.Cause != nil {
		return fmt.Sprintf("process error: %s: %v", e.Message, e.Cause)
	}
	return "process error: " + e.Message
}

func (e *ProcessError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func generateRequestID() string {
	buf := make([]byte, 8)
	_, _ = rand.Read(buf)
	return fmt.Sprintf("req_%d_%s", time.Now().UnixNano(), hex.EncodeToString(buf))
}
