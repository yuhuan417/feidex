package codex

import (
	"context"
	"feidex/internal/application/backendops"
	submission "feidex/internal/application/submission"
	"feidex/internal/codexrpc"
	"feidex/internal/domain/modelconfig"
	"strings"
)

type ConversationClient interface {
	Call(context.Context, string, any, any) error
}

// StartConversation converts initialization snapshots to Codex config and maps the response back to semantic values.
func StartConversation(ctx context.Context, client ConversationClient, config backendops.ThreadStartConfig, snapshot modelconfig.Snapshot) (submission.ConversationStarted, error) {
	params := ThreadStartParams(config)
	if snapshot.Valid {
		if params.Config == nil {
			params.Config = map[string]any{}
		}
		for key, value := range map[string]string{
			"review_model":                             snapshot.ReviewModel,
			"agents.default_subagent_model":            snapshot.SubagentModel,
			"agents.default_subagent_reasoning_effort": snapshot.SubagentEffort,
		} {
			delete(params.Config, key)
			if strings.TrimSpace(value) != "" {
				params.Config[key] = value
			}
		}
	}
	var result codexrpc.ThreadStartResult
	if err := client.Call(ctx, "thread/start", params.Map(), &result); err != nil {
		return submission.ConversationStarted{}, err
	}
	return submission.ConversationStarted{ID: result.Thread.ID, Name: result.Thread.Name, Preview: result.Thread.Preview}, nil
}

// ThreadStartParams is the only Codex thread/start wire encoder.
func ThreadStartParams(config backendops.ThreadStartConfig) codexrpc.ThreadStartParams {
	return codexrpc.ThreadStartParams{
		Cwd:                    config.Cwd,
		ApprovalPolicy:         config.ApprovalPolicy,
		Sandbox:                config.SandboxMode,
		ServiceName:            config.ServiceName,
		ExperimentalRawEvents:  config.ExperimentalRawEvents,
		PersistExtendedHistory: config.PersistExtendedHistory,
		ServiceTier:            config.ServiceTier,
		Model:                  config.Model,
		Config:                 AuxiliaryConfig(config.Initialization),
	}
}

func AuxiliaryConfig(snapshot modelconfig.Snapshot) map[string]any {
	result := map[string]any{}
	for key, value := range map[string]string{"review_model": snapshot.ReviewModel, "agents.default_subagent_model": snapshot.SubagentModel, "agents.default_subagent_reasoning_effort": snapshot.SubagentEffort} {
		if value = strings.TrimSpace(value); value != "" {
			result[key] = value
		}
	}
	return result
}

// ThreadForkParams is the only Codex thread/fork wire encoder.
func ThreadForkParams(request backendops.ThreadForkRequest) map[string]any {
	params := map[string]any{
		"threadId":       strings.TrimSpace(request.ThreadID),
		"cwd":            strings.TrimSpace(request.Cwd),
		"approvalPolicy": strings.TrimSpace(request.ApprovalPolicy),
		"sandbox":        strings.TrimSpace(request.SandboxMode),
	}
	for key, value := range map[string]string{
		"serviceTier":    request.ServiceTier,
		"model":          request.Model,
		"multiAgentMode": request.MultiAgentMode,
	} {
		if strings.TrimSpace(value) != "" {
			params[key] = strings.TrimSpace(value)
		}
	}
	return params
}
