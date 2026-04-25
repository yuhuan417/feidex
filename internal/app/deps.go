package app

import (
	"context"
	"runtime"

	"feidex/internal/app/appcore"
	"feidex/internal/buildinfo"
	"feidex/internal/claudeinstall"
	"feidex/internal/codexinstall"
	"feidex/internal/codexrpc"
	"feidex/internal/config"
	"feidex/internal/daemon"
	"feidex/internal/feishu"
	"feidex/internal/release"
)

// CodexClient, ClaudeCore, and FeishuClient are defined in appcore/.
type CodexClient = appcore.CodexClient
type ClaudeCore = appcore.ClaudeCore
type FeishuClient = appcore.FeishuClient

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
	newCodexClient   = func(cfg config.CodexConfig) CodexClient { return codexrpc.New(cfg) }
	newClaudeCore    = func(app *App, cfg config.ClaudeConfig) ClaudeCore { return newClaudeRuntime(app, cfg) }
	newFeishuClient  = func(cfg config.FeishuConfig) FeishuClient { return feishu.New(cfg) }
	newDaemonManager = daemon.NewManager
	newReleaseClient = func() releaseClient {
		return release.NewGitHubClient(release.DefaultRepoOwner, release.DefaultRepoName, nil)
	}
	newCodexInstallManager  = func(command string) codexInstallManager { return codexinstall.New(command) }
	newClaudeInstallManager = func(command string) claudeInstallManager { return claudeinstall.New(command) }
	runClaudeSmokeTest      = func(a *App, ctx context.Context) error { return newBackendUpgradeService(a).claudeSmokeTest(ctx) }
	startDaemonUpgrade      = daemon.StartBackgroundUpgrade
	currentVersion          = buildinfo.CurrentVersion
	currentGOARCH           = func() string { return runtime.GOARCH }
)
