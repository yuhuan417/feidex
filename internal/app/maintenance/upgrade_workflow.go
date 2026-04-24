package maintenance

import (
	"context"
	"strings"
	"time"
)

type UpgradeProbe struct {
	Supported      bool
	Reason         string
	CurrentVersion string
}

type UpgradeWorkflow struct {
	PackageName       string
	BackendName       string
	CurrentVersion    string
	TargetVersion     string
	Probe             func(context.Context) (UpgradeProbe, error)
	InstallVersion    func(context.Context, string) error
	SmokeTest         func(context.Context) error
	RefreshRuntime    func(context.Context) (bool, error)
	RuntimeBusyReason func() string
	RecordVersions    func(previousVersion, targetVersion string)
	Update            func(phase, message string)
	Finalize          func(result, message string)
}

func RunUpgradeWorkflow(w UpgradeWorkflow) {
	rollback := func(previousVersion string, cause error) {
		w.Update("rolling_back", "升级失败，正在回滚到 "+firstNonEmpty(previousVersion, "-"))
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		if strings.TrimSpace(previousVersion) == "" {
			w.Finalize("rollback_failed", "升级失败，且缺少可回滚的旧版本。原因: "+cause.Error())
			return
		}
		if err := w.InstallVersion(ctx, previousVersion); err != nil {
			w.Finalize("rollback_failed", "升级失败，自动回滚也失败。原始错误: "+cause.Error()+"；回滚错误: "+err.Error())
			return
		}
		if err := w.SmokeTest(ctx); err != nil {
			w.Finalize("rollback_failed", "升级失败，回滚后的 smoke test 也失败。原始错误: "+cause.Error()+"；回滚验证错误: "+err.Error())
			return
		}
		w.Finalize("rolled_back", "升级失败，已自动回滚到 `"+previousVersion+"`。原因: "+cause.Error())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	probe, err := w.Probe(ctx)
	cancel()
	if err != nil {
		w.Finalize("failed", "升级前检查失败: "+err.Error())
		return
	}
	if !probe.Supported {
		w.Finalize("failed", "当前环境不支持自动升级: "+firstNonEmpty(probe.Reason, "unknown"))
		return
	}
	previousVersion := firstNonEmpty(probe.CurrentVersion, w.CurrentVersion)
	if w.RecordVersions != nil {
		w.RecordVersions(previousVersion, w.TargetVersion)
	}
	if w.RuntimeBusyReason != nil {
		if reason := w.RuntimeBusyReason(); strings.TrimSpace(reason) != "" {
			w.Finalize("failed", "升级前检查失败: "+reason)
			return
		}
	}
	if strings.TrimSpace(previousVersion) == strings.TrimSpace(w.TargetVersion) {
		w.Finalize("success", "当前已经是最新稳定版 `"+w.TargetVersion+"`")
		return
	}
	w.Update("installing", "正在安装 "+w.PackageName+"@"+w.TargetVersion)
	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Minute)
	err = w.InstallVersion(ctx, w.TargetVersion)
	cancel()
	if err != nil {
		rollback(previousVersion, err)
		return
	}
	w.Update("smoke_testing", "正在验证新版本")
	ctx, cancel = context.WithTimeout(context.Background(), 45*time.Second)
	switched, err := w.RefreshRuntime(ctx)
	cancel()
	if err != nil {
		rollback(previousVersion, err)
		return
	}
	if switched {
		w.Finalize("success", "升级成功，已切换到 `"+w.TargetVersion+"`")
		return
	}
	w.Finalize("success", "升级成功，已验证 `"+w.TargetVersion+"` 可用；当前 frontend 未启用 "+w.BackendName+" backend")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
