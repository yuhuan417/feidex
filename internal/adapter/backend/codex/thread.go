package codex

import (
	"context"
	"feidex/internal/application/backendops"
	"feidex/internal/codexrpc"
	"feidex/internal/domain/modelconfig"
	"strings"
)

// ThreadResult is the semantic result of thread/start or thread/resume.
type ThreadResult struct {
	ID, Name, Preview string
	Applied           modelconfig.Snapshot
}

// StartThread owns the Codex thread/start method and wire response mapping.
func StartThread(ctx context.Context, client ConversationClient, config backendops.ThreadStartConfig) (ThreadResult, error) {
	var out codexrpc.ThreadStartResult
	if err := client.Call(ctx, "thread/start", ThreadStartParams(config).Map(), &out); err != nil {
		return ThreadResult{}, err
	}
	return ThreadResult{ID: out.Thread.ID, Name: out.Thread.Name, Preview: out.Thread.Preview}, nil
}

// ResumeThread owns the Codex thread/resume method and wire response mapping.
func ResumeThread(ctx context.Context, client ConversationClient, threadID, model string, config map[string]any) (ThreadResult, error) {
	params := codexrpc.ThreadResumeParams{ThreadID: strings.TrimSpace(threadID), PersistExtendedHistory: true, Model: strings.TrimSpace(model), Config: config}
	var out codexrpc.ThreadStartResult
	if err := client.Call(ctx, "thread/resume", params.Map(), &out); err != nil {
		return ThreadResult{}, err
	}
	return ThreadResult{ID: strings.TrimSpace(out.Thread.ID), Name: out.Thread.Name, Preview: out.Thread.Preview, Applied: ResumedThreadConfig(params.Model, params.Config)}, nil
}
