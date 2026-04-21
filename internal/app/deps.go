package app

import (
	"context"
	"encoding/json"
	"runtime"
	"time"

	"feidex/internal/buildinfo"
	"feidex/internal/claudeinstall"
	"feidex/internal/codexinstall"
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

type claudeCore interface {
	EnsureSession(context.Context, string, *config.Workspace, string, string) (string, error)
	ResetSession(string) error
	StartTurn(context.Context, string, string, string, string) error
	Interrupt(context.Context, string) error
	ResolveApproval(string, claudeApprovalResolution) error
	ResolveUserInput(string, map[string]string) error
	ResolvePlanFeedback(string, string) error
	CancelPending(string, string) error
	Close() error
}

type feishuClient interface {
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
	DownloadMessageResource(context.Context, string, feishu.Attachment, string) (string, string, error)
	ResolveMergeForward(context.Context, string, []string) (string, []feishu.Attachment, error)
	ShareLocalFile(context.Context, feishu.SharedFileRequest) (feishu.SharedFileResult, error)
	SimpleStatusCard(string, string, string, []feishu.Button) map[string]any
	UrgentApp(context.Context, string, string) error
	LookupMessageSenderOpenID(context.Context, string) (string, error)
}

type releaseClient interface {
	LatestLinuxBinary(context.Context, string) (*release.ReleaseInfo, error)
	LatestDevLinuxBinary(context.Context, string) (*release.ReleaseInfo, error)
	LinuxBinaryByVersion(context.Context, string, string) (*release.ReleaseInfo, error)
}

type codexInstallManager interface {
	Probe(context.Context) (codexinstall.Probe, error)
	LatestVersion(context.Context) (string, error)
	InstallVersion(context.Context, string) error
}

type claudeInstallManager interface {
	Probe(context.Context) (claudeinstall.Probe, error)
	LatestVersion(context.Context) (string, error)
	InstallVersion(context.Context, string) error
}

var (
	newCodexClient   = func(cfg config.CodexConfig) codexClient { return codexrpc.New(cfg) }
	newClaudeCore    = func(app *App, cfg config.ClaudeConfig) claudeCore { return newClaudeRuntime(app, cfg) }
	newFeishuClient  = func(cfg config.FeishuConfig) feishuClient { return feishu.New(cfg) }
	newDaemonManager = daemon.NewManager
	newReleaseClient = func() releaseClient {
		return release.NewGitHubClient(release.DefaultRepoOwner, release.DefaultRepoName, nil)
	}
	newCodexInstallManager  = func(command string) codexInstallManager { return codexinstall.New(command) }
	newClaudeInstallManager = func(command string) claudeInstallManager { return claudeinstall.New(command) }
	runClaudeSmokeTest      = func(a *App, ctx context.Context) error { return a.claudeSmokeTest(ctx) }
	startDaemonUpgrade      = daemon.StartBackgroundUpgrade
	currentVersion          = buildinfo.CurrentVersion
	currentGOARCH           = func() string { return runtime.GOARCH }
)
