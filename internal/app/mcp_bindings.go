package app

import (
	"context"
	"net/http"
	"strings"

	"feidex/internal/app/mcpbridge"
	"feidex/internal/app/turnitem"
	"feidex/internal/state"
)

type feidexMCPPublication = mcpbridge.Publication

const (
	feidexMCPServerID       = mcpbridge.ServerID
	feidexMCPBearerEnvName  = mcpbridge.BearerEnvName
	feidexMCPProtocol       = mcpbridge.Protocol
	feidexMCPPath           = mcpbridge.Path
	feidexMCPSessionKeyName = mcpbridge.SessionKeyName

	feidexSendIMFileToolName  = mcpbridge.SendIMFileToolName
	feidexSendIMImageToolName = mcpbridge.SendIMImageToolName
	feidexSendIMVideoToolName = mcpbridge.SendIMVideoToolName
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
	if a.mcp != nil {
		return nil
	}
	svc, err := newFeidexMCPService(a)
	if err != nil {
		return err
	}
	if err := svc.Start(ctx); err != nil {
		return err
	}
	a.mcp = svc
	publishMCPToCodexClient(a, currentCodexClient(a))
	return nil
}

func stopMCPService(a *App, ctx context.Context) error {
	if a == nil || a.mcp == nil {
		return nil
	}
	svc := a.mcp
	a.mcp = nil
	if ctx == nil {
		ctx = context.Background()
	}
	return svc.Stop(ctx)
}

func currentMCPPublication(a *App) feidexMCPPublication {
	if a == nil || a.mcp == nil {
		return feidexMCPPublication{}
	}
	return a.mcp.Publication()
}

func newFeidexMCPService(a *App) (*feidexMCPService, error) {
	svc, err := mcpbridge.NewService(mcpBridgeAppAdapter{app: a})
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

type mcpBridgeAppAdapter struct {
	app *App
}

func (a mcpBridgeAppAdapter) Feishu() mcpbridge.FeishuClient {
	if a.app == nil {
		return nil
	}
	return a.app.feishu
}

func (a mcpBridgeAppAdapter) State() mcpbridge.StateProvider {
	if a.app == nil {
		return nil
	}
	return a.app.store
}

func (a mcpBridgeAppAdapter) StartedTurnItems() []mcpbridge.StartedTurnItem {
	if a.app == nil {
		return nil
	}
	tracker := newRuntimeStateService(a.app).turnItemTracker()
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
			ThreadID: strings.TrimSpace(itemState.ThreadID),
			TurnID:   strings.TrimSpace(itemState.TurnID),
			ItemID:   strings.TrimSpace(itemState.ItemID),
			Type:     strings.TrimSpace(itemState.Started.Type),
			ToolName: strings.TrimSpace(itemState.Started.ToolName),
			Raw:      turnitem.CloneJSONMap(raw),
		})
	}
	return items
}

func (a mcpBridgeAppAdapter) FindSubmissionByTurn(threadID, turnID string) (string, *state.Submission) {
	if a.app == nil {
		return "", nil
	}
	return newSubmissionQueueServiceFromApp(a.app).FindSubmissionByTurn(threadID, turnID)
}

func (a mcpBridgeAppAdapter) ReplyInThreadForSubmission(sub *state.Submission) bool {
	return replyInThreadForSubmission(a.app, sub)
}
