package feishuapp

import (
	"context"
	"feidex/internal/adapter/feishu/mcpbridge"
	"feidex/internal/adapter/feishu/turnitem"
	appsubmission "feidex/internal/application/submission"
	"feidex/internal/config"
	domainsubmission "feidex/internal/domain/submission"
	frontendruntime "feidex/internal/runtime"
	"net/http"
	"strings"
)

type feidexMCPService struct {
	*mcpbridge.Service

	handler http.Handler
	token   string
}

func startMCPService(owner *frontendruntime.FrontendOwner, deps BackendRuntimeDeps, ctx context.Context) error {
	if owner == nil || owner.MCP == nil {
		return nil
	}
	if err := owner.MCP.Start(ctx); err != nil {
		return err
	}
	publishMCPToCodexClient(deps, deps.runtime.currentCodexClient())
	return nil
}

// currentMCPPublicationFor needs only the runtime owner and the MCP service.
func currentMCPPublicationFor(owner *frontendruntime.FrontendOwner, mcp *feidexMCPService) mcpbridge.Publication {
	if owner == nil || owner.MCP == nil || !owner.MCP.Started() || mcp == nil {
		return mcpbridge.Publication{}
	}
	return mcp.Publication()
}

type MCPPortInputs struct {
	AttachmentSender mcpbridge.AttachmentSender
	StateProvider    mcpbridge.StateProvider
	TurnItems        *turnitem.Tracker
	SubmissionLookup appsubmission.SubmissionLookupService
}

func MCPPorts(inputs MCPPortInputs) mcpbridge.Dependencies {
	return mcpbridge.Dependencies{
		AttachmentSender: inputs.AttachmentSender,
		StateProvider:    inputs.StateProvider,
		StartedTurnItemsFn: func() []mcpbridge.StartedTurnItem {
			if inputs.TurnItems == nil {
				return nil
			}
			started := inputs.TurnItems.StartedItems()
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
			return findSubmissionByTurn(inputs.SubmissionLookup, threadID, turnID)
		},
		ReplyInThreadForSubmissionFn: replyInThreadForSubmission,
	}
}

func BuildMCP(dependencies mcpbridge.Dependencies) (*feidexMCPService, error) {
	svc, err := mcpbridge.NewService(dependencies)
	if err != nil {
		return nil, err
	}
	return &feidexMCPService{
		Service: svc,
		handler: svc.Handler(),
		token:   svc.Token(),
	}, nil
}

func prepareClaudeMCPConfig(cfg *config.Config, owner *frontendruntime.FrontendOwner, mcp *feidexMCPService, sessionKey string) (string, []string, func(), error) {
	if cfg == nil {
		return "", nil, nil, nil
	}
	return mcpbridge.PrepareClaudeConfig(cfg.DataDir, currentMCPPublicationFor(owner, mcp), sessionKey)
}
