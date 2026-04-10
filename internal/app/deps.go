package app

import (
	"context"
	"encoding/json"
	"runtime"
	"time"

	"feidex/internal/buildinfo"
	"feidex/internal/codexrpc"
	"feidex/internal/config"
	"feidex/internal/daemon"
	"feidex/internal/feishu"
	"feidex/internal/release"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

type codexClient interface {
	SetHandlers(func(string, json.RawMessage), func(codexrpc.RequestEnvelope))
	Start(context.Context, bool) error
	Close() error
	Call(context.Context, string, any, any) error
	Reply(json.RawMessage, any) error
	ReplyError(json.RawMessage, int, string) error
}

type feishuClient interface {
	SetHandlers(func(*feishu.InboundMessage), func(*feishu.CardAction) (*callback.CardActionTriggerResponse, error), func(*feishu.BotMenuClick), func(*feishu.MessageRecall), func(*feishu.MessageReaction))
	Start(context.Context) error
	Stop()
	ConfigureMarkdownPreview(string, string)
	RewriteMarkdownPreview(context.Context, feishu.MarkdownPreviewRequest) (string, error)
	CleanupMarkdownPreviewsBefore(context.Context, time.Time) (feishu.PreviewDriveCleanupResult, error)
	CleanupSharedFilesBefore(context.Context, time.Time) (feishu.PreviewDriveCleanupResult, error)
	AddReaction(context.Context, string, string) error
	RemoveReaction(context.Context, string, string) error
	ReplyText(context.Context, string, string, bool) error
	ReplyTextWithID(context.Context, string, string, bool) (string, error)
	SendText(context.Context, string, string) error
	ReplyCard(context.Context, string, map[string]any, bool) (string, error)
	SendCard(context.Context, string, map[string]any) (string, error)
	PatchCard(context.Context, string, map[string]any) error
	DownloadMessageResource(context.Context, string, feishu.Attachment, string) (string, string, error)
	ShareLocalFile(context.Context, feishu.SharedFileRequest) (feishu.SharedFileResult, error)
	SimpleStatusCard(string, string, string, []feishu.Button) map[string]any
}

type releaseClient interface {
	LatestLinuxBinary(context.Context, string) (*release.ReleaseInfo, error)
	LinuxBinaryByVersion(context.Context, string, string) (*release.ReleaseInfo, error)
}

var (
	newCodexClient   = func(cfg config.CodexConfig) codexClient { return codexrpc.New(cfg) }
	newFeishuClient  = func(cfg config.FeishuConfig) feishuClient { return feishu.New(cfg) }
	newDaemonManager = daemon.NewManager
	newReleaseClient = func() releaseClient {
		return release.NewGitHubClient(release.DefaultRepoOwner, release.DefaultRepoName, nil)
	}
	startDaemonUpgrade = daemon.StartBackgroundUpgrade
	currentVersion     = buildinfo.CurrentVersion
	currentGOARCH      = func() string { return runtime.GOARCH }
)
