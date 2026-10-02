package appcore

import (
	"context"
	"encoding/json"

	"feidex/internal/codexrpc"
	"feidex/internal/config"
	appruntime "feidex/internal/runtime"
)

// CodexClient is the interface for the Codex RPC client.
type CodexClient interface {
	SetHandlers(func(string, json.RawMessage), func(codexrpc.RequestEnvelope))
	Start(context.Context, bool) error
	Close() error
	Call(context.Context, string, any, any) error
	Reply(json.RawMessage, any) error
	ReplyError(json.RawMessage, int, string) error
}

// ClaudeCore is the interface for the Claude runtime.
type ClaudeCore interface {
	EnsureSession(context.Context, string, *config.Workspace, string, string) (string, error)
	ForkSession(context.Context, string, *config.Workspace, string, string) (string, error)
	UpdateConfig(config.ClaudeConfig)
	ResetSession(string) error
	StartTurn(context.Context, string, string, string, string) error
	StartSteerTurn(context.Context, string, string, string, string, string) error
	Interrupt(context.Context, string) error
	SetModel(context.Context, string, string) (bool, error)
	SetEffort(context.Context, string, string) (bool, error)
	SetPermissionMode(context.Context, string, string) error
	ResolveApproval(string, appruntime.ClaudeApprovalResolution) error
	ResolveUserInput(string, map[string]string) error
	ResolvePlanFeedback(string, string) error
	CancelPending(string, string) error
	SessionStopped(string) bool
	Close() error
}
