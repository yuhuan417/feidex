package feishuapp

import (
	filesystem "feidex/internal/adapter/filesystem/upgrade"
	releaseadapter "feidex/internal/adapter/release"
	"feidex/internal/application/upgrade"
	"feidex/internal/config"
	"feidex/internal/daemon"
	frontendruntime "feidex/internal/runtime"
	upgraderuntime "feidex/internal/runtime/upgrade"
	"strings"
	"sync"
)

// UpgradeWorkflowPorts takes the config, its mutex and the runtime owner
// instead of the frontend aggregate.
func UpgradeWorkflowPorts(cfg *config.Config, mu *sync.RWMutex, owner *frontendruntime.FrontendOwner) (upgrade.Platform, upgrade.Releases, upgrade.Artifacts, upgrade.Launcher) {
	name := func() string {
		mu.RLock()
		defer mu.RUnlock()
		return strings.TrimSpace(cfg.Daemon.ServiceName)
	}
	return upgraderuntime.Platform{Version: func() string { return currentVersion() }, GOOS: func() string { return currentGOOS() }, GOARCH: func() string { return currentGOARCH() }, ServiceName: name, Manager: func(name string) (daemon.Manager, error) { return newDaemonManager(name) }},
		releaseadapter.Gateway{Client: func() releaseadapter.Client { return newReleaseClient() }},
		filesystem.Artifacts{DataDir: func() string { return cfg.DataDir }},
		upgraderuntime.Launcher{Start: func(spec daemon.UpgradeSpec) (string, error) { return startDaemonUpgrade(spec) }, Deduper: owner.EffectRunner.Deduper}
}
