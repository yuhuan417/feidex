package app

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
	if a.composition != nil && a.composition.mcp != nil {
		return nil
	}
	svc, err := newFeidexMCPService(a)
	if err != nil {
		return err
	}
	if err := svc.Start(ctx); err != nil {
		return err
	}
	ensureCompositionState(a)
	a.composition.mcp = svc
	publishMCPToCodexClient(a, currentCodexClient(a))
	return nil
}

func stopMCPService(a *App, ctx context.Context) error {
	if a == nil || a.composition == nil || a.composition.mcp == nil {
		return nil
	}
	svc := a.composition.mcp
	a.composition.mcp = nil
	if ctx == nil {
		ctx = context.Background()
	}
	return svc.Stop(ctx)
}

func currentMCPPublication(a *App) mcpbridge.Publication {
	if a == nil || a.composition == nil || a.composition.mcp == nil {
		return mcpbridge.Publication{}
	}
	return a.composition.mcp.Publication()
}

func newFeidexMCPService(a *App) (*feidexMCPService, error) {
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
			tracker := newRuntimeStateService(a).turnItemTracker()
			if tracker == nil {
				return nil
			}
			tracker.Mu.Lock()
			defer tracker.Mu.Unlock()
			items := make([]mcpbridge.StartedTurnItem, 0, len(tracker.Items))
			for _, itemState := range tracker.Items {
				if itemState == nil || strings.TrimSpace(itemState.Status) != "started" {
					continue
				}
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
