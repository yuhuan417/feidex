package feishuapp

import (
	"context"
	feishutransport "feidex/internal/adapter/feishu/transport"
	frontendclients "feidex/internal/runtime"
	"runtime"

	"feidex/internal/buildinfo"
	"feidex/internal/codexrpc"
	"feidex/internal/config"
	"feidex/internal/daemon"
	"feidex/internal/feishu"
	"feidex/internal/install"
	"feidex/internal/release"
)

// Backend clients come from the frontend composition; Feishu transport is adapter-owned.
type CodexClient = frontendclients.CodexClient
type ClaudeCore = frontendclients.ClaudeCore
type FeishuClient = feishutransport.Client

type releaseClient interface {
	LatestLinuxBinary(context.Context, string) (*release.ReleaseInfo, error)
	LatestDevLinuxBinary(context.Context, string) (*release.ReleaseInfo, error)
	LinuxBinaryByVersion(context.Context, string, string) (*release.ReleaseInfo, error)
}

type codexInstallManager interface {
	Probe(context.Context) (install.Probe, error)
	LatestVersion(context.Context) (string, error)
	InstallVersion(context.Context, string) error
}

type claudeInstallManager interface {
	Probe(context.Context) (install.Probe, error)
	LatestVersion(context.Context) (string, error)
	InstallVersion(context.Context, string) error
}

var (
	newCodexClient   = func(cfg config.CodexConfig) CodexClient { return codexrpc.New(cfg) }
	newClaudeCore    func(func(config.ClaudeConfig) ClaudeCore, config.ClaudeConfig) ClaudeCore
	newFeishuClient  = func(cfg config.FeishuConfig) FeishuClient { return feishu.New(cfg) }
	newDaemonManager = daemon.NewManager
	newReleaseClient = func() releaseClient {
		return release.NewGitHubClient(release.DefaultRepoOwner, release.DefaultRepoName, nil)
	}
	newCodexInstallManager  = func(command string) codexInstallManager { return install.New(install.Codex(), command) }
	newClaudeInstallManager = func(command string) claudeInstallManager { return install.New(install.Claude(), command) }
	startDaemonUpgrade      = daemon.StartBackgroundUpgrade
	currentVersion          = buildinfo.CurrentVersion
	currentGOOS             = func() string { return runtime.GOOS }
	currentGOARCH           = func() string { return runtime.GOARCH }
)

// Runtime factories are installed after package initialization. Input handlers
// now share one dispatch graph, so eagerly binding these mutable test seams
// would create a Go global-initialization cycle through command handlers.
func init() {
	newClaudeCore = func(factory func(config.ClaudeConfig) ClaudeCore, cfg config.ClaudeConfig) ClaudeCore {
		return factory(cfg)
	}
}
