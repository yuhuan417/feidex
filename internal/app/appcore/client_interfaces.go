package appcore

import (
	"context"
	"encoding/json"
	"time"

	appruntime "feidex/internal/app/runtime"
	"feidex/internal/codexrpc"
	"feidex/internal/config"
	"feidex/internal/feishu"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
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

// FeishuClient is the interface for the Feishu bot client.
type FeishuClient interface {
	SetHandlers(func(*feishu.InboundMessage), func(*feishu.CardAction) (*callback.CardActionTriggerResponse, error), func(*feishu.BotMenuClick), func(*feishu.MessageRecall), func(*feishu.MessageReaction))
	Start(context.Context) error
	Stop()
	ConfigureLocalFileLinks(string, string)
	RewriteLocalFileLinks(context.Context, feishu.LocalFileLinkRewriteRequest) (string, error)
	CleanupArtifactsBefore(context.Context, time.Time) (feishu.PreviewDriveCleanupResult, error)
	AddReaction(context.Context, string, string) error
	RemoveReaction(context.Context, string, string) error
	ReplyText(context.Context, string, string, bool) error
	ReplyTextWithID(context.Context, string, string, bool) (string, error)
	SendText(context.Context, string, string) error
	ReplyCard(context.Context, string, map[string]any, bool) (string, error)
	SendCard(context.Context, string, map[string]any) (string, error)
	PatchCard(context.Context, string, map[string]any) error
	ReplyLocalAttachment(context.Context, string, string, bool) error
	ReplyLocalImage(context.Context, string, string, bool) error
	ReplyLocalVideo(context.Context, string, string, bool) error
	DownloadMessageResource(context.Context, string, feishu.Attachment, string) (string, string, error)
	ResolveMergeForward(context.Context, string, []string) (string, []feishu.Attachment, error)
	ShareLocalFile(context.Context, feishu.SharedFileRequest) (feishu.SharedFileResult, error)
	SimpleStatusCard(string, string, string, []feishu.Button) map[string]any
	UrgentApp(context.Context, string, string) error
	LookupMessageSenderOpenID(context.Context, string) (string, error)
	GetGroupBotCount(context.Context, string) (int, error)
	ListAnnouncementBlocks(context.Context, string) ([]feishu.AnnouncementBlock, error)
	CreateAnnouncementTextBlock(context.Context, string, string, string, string) (feishu.AnnouncementBlock, error)
	UpdateAnnouncementTextBlock(context.Context, string, string, string, string) error
	BotOpenID() string
	BotName() string
}
