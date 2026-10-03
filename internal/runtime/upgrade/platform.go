package upgrade

import (
	"context"
	upgradeapp "feidex/internal/application/upgrade"
	"feidex/internal/daemon"
	"feidex/internal/release"
	"fmt"
	"os"
	"path/filepath"
)

type Platform struct {
	Version, GOOS, GOARCH, ServiceName func() string
	Manager                            func(string) (daemon.Manager, error)
}

func (p Platform) Inspect(ctx context.Context, daemonStatus bool) (upgradeapp.Environment, error) {
	if err := ctx.Err(); err != nil {
		return upgradeapp.Environment{}, err
	}
	exe, err := os.Executable()
	if err != nil {
		return upgradeapp.Environment{}, fmt.Errorf("获取当前二进制路径失败: %w", err)
	}
	if path, err := filepath.EvalSymlinks(exe); err == nil {
		exe = path
	}
	env := upgradeapp.Environment{Version: p.Version(), GOOS: p.GOOS(), GOARCH: p.GOARCH(), Executable: exe, ServiceName: p.ServiceName(), ProcessPID: os.Getpid()}
	env.AssetName, err = release.CurrentAssetName(env.GOOS, env.GOARCH)
	if err != nil {
		return env, err
	}
	if !daemonStatus {
		return env, nil
	}
	manager, err := p.Manager(env.ServiceName)
	if err != nil {
		return env, fmt.Errorf("当前环境不支持 daemon 升级: %w", err)
	}
	status, err := manager.Status()
	if err != nil {
		return env, fmt.Errorf("查询 daemon 状态失败: %w", err)
	}
	if status != nil {
		env.Installed, env.Running, env.PID = status.Installed, status.Running, status.PID
	}
	return env, nil
}

type LaunchDeduper interface {
	Do(context.Context, string, func() (any, error)) (any, error)
}
type Launcher struct {
	Start   func(daemon.UpgradeSpec) (string, error)
	Deduper LaunchDeduper
}

func (l Launcher) Launch(ctx context.Context, spec upgradeapp.LaunchSpec) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	call := func() (any, error) {
		return l.Start(daemon.UpgradeSpec{UnitName: spec.UnitName, ServiceName: spec.ServiceName, Version: spec.Version, BinaryPath: spec.BinaryPath, DownloadURL: spec.DownloadURL, SourcePath: spec.SourcePath, ExpectedSHA256: spec.ExpectedSHA256})
	}
	if l.Deduper == nil {
		value, err := call()
		result, _ := value.(string)
		return result, err
	}
	value, err := l.Deduper.Do(ctx, "daemon-upgrade:"+spec.OperationID, call)
	result, _ := value.(string)
	return result, err
}
