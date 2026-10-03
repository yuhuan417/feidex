package feishuapp

import (
	filesystem "feidex/internal/adapter/filesystem/upgrade"
	releaseadapter "feidex/internal/adapter/release"
	"feidex/internal/application/upgrade"
	"feidex/internal/daemon"
	upgraderuntime "feidex/internal/runtime/upgrade"
	"strings"
)

func UpgradeWorkflowPorts(a *App) (upgrade.Platform, upgrade.Releases, upgrade.Artifacts, upgrade.Launcher) {
	name := func() string {
		a.configMutex().RLock()
		defer a.configMutex().RUnlock()
		return strings.TrimSpace(a.cfg.Daemon.ServiceName)
	}
	return upgraderuntime.Platform{Version: func() string { return currentVersion() }, GOOS: func() string { return currentGOOS() }, GOARCH: func() string { return currentGOARCH() }, ServiceName: name, Manager: func(name string) (daemon.Manager, error) { return newDaemonManager(name) }},
		releaseadapter.Gateway{Client: func() releaseadapter.Client { return newReleaseClient() }},
		filesystem.Artifacts{DataDir: func() string { return a.cfg.DataDir }},
		upgraderuntime.Launcher{Start: func(spec daemon.UpgradeSpec) (string, error) { return startDaemonUpgrade(spec) }, Deduper: a.runtimeOwner.EffectRunner.Deduper}
}
