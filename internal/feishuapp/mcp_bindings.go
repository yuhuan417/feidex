package feishuapp

import (
	"context"
	"feidex/internal/adapter/feishu/mcpbridge"
	"feidex/internal/adapter/feishu/turnitem"
	domainsubmission "feidex/internal/domain/submission"
	"net/http"
	"strings"
)

type feidexMCPService struct {
	*mcpbridge.Service

	handler http.Handler
	token   string
}

func startMCPService(a *App, ctx context.Context) error {
	if a == nil {
		return nil
	}
	if err := a.runtimeOwner.MCP.Start(ctx); err != nil {
		return err
	}
	publishMCPToCodexClient(a, currentCodexClient(a))
	return nil
}

func currentMCPPublication(a *App) mcpbridge.Publication {
	if a == nil {
		return mcpbridge.Publication{}
	}
	if !a.runtimeOwner.MCP.Started() || a.bindings.MCP == nil {
		return mcpbridge.Publication{}
	}
	return a.bindings.MCP.Publication()
}

func BuildMCP(a *App) (*feidexMCPService, error) {
	svc, err := mcpbridge.NewService(mcpDependenciesForApp(a))
	if err != nil {
		return nil, err
	}
	return &feidexMCPService{
		Service: svc,
		handler: svc.Handler(),
		token:   svc.Token(),
	}, nil
}

func prepareClaudeMCPConfig(a *App, sessionKey string) (string, []string, func(), error) {
	if a == nil || a.cfg == nil {
		return "", nil, nil, nil
	}
	return mcpbridge.PrepareClaudeConfig(a.cfg.DataDir, currentMCPPublication(a), sessionKey)
}

func mcpDependenciesForApp(a *App) mcpbridge.Dependencies {
	if a == nil {
		return mcpbridge.Dependencies{}
	}
	return mcpbridge.Dependencies{
		AttachmentSender: a.feishu,
		StateProvider:    a.store,
		StartedTurnItemsFn: func() []mcpbridge.StartedTurnItem {
			tracker := a.bindings.TurnItems
			if tracker == nil {
				return nil
			}
			started := tracker.StartedItems()
			items := make([]mcpbridge.StartedTurnItem, 0, len(started))
			for _, itemState := range started {
				raw := itemState.Started.MergedRaw()
				items = append(items, mcpbridge.StartedTurnItem{
					ThreadID: strings.TrimSpace(itemState.ThreadID), TurnID: strings.TrimSpace(itemState.TurnID),
					ItemID: strings.TrimSpace(itemState.ItemID), Type: strings.TrimSpace(itemState.Started.Type),
					ToolName: strings.TrimSpace(itemState.Started.ToolName), Raw: turnitem.CloneJSONMap(raw),
				})
			}
			return items
		},
		FindSubmissionByTurnFn: func(threadID, turnID string) (string, *domainsubmission.Submission) {
			return findSubmissionByTurn(a, threadID, turnID)
		},
		ReplyInThreadForSubmissionFn: func(sub *domainsubmission.Submission) bool {
			return replyInThreadForSubmission(a, sub)
		},
	}
}
