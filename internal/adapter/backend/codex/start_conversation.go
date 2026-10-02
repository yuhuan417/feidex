package codex

import (
	"context"
	submission "feidex/internal/application/submission"
	"feidex/internal/codexrpc"
	"feidex/internal/domain/modelconfig"
	"strings"
)

type ConversationClient interface {
	Call(context.Context, string, any, any) error
}

// StartConversation converts initialization snapshots to Codex config and maps the response back to semantic values.
func StartConversation(ctx context.Context, client ConversationClient, params codexrpc.ThreadStartParams, snapshot modelconfig.Snapshot) (submission.ConversationStarted, error) {
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
