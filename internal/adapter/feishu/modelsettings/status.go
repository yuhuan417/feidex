package modelsettings

import (
	"feidex/internal/application/modelconfig"
	domain "feidex/internal/domain/modelconfig"
	"feidex/internal/textutil"
)

const turnApplyNotice = "以上为已保存配置。本轮不变；尚未启动的排队消息在下一轮启动前应用最新模型/推理强度。追加到当前轮的输入不触发切换。"
const codexAuxiliaryApplyNotice = "Plan 模型/强度在下一次本地启动 Plan 轮次时应用；review/subagent 配置待下次新建或恢复 thread 生效。后台 goal 自动续跑不保证采用新配置。"
const claudeAuxiliaryApplyNotice = "辅助模型配置待当前轮、审批和后台任务结束后，在下一轮启动前应用；必要时恢复原会话重新初始化。"

func RenderStatus(view modelconfig.StatusView) string {
	notice := turnApplyNotice
	switch view.Backend {
	case domain.BackendCodex:
		notice += "\n" + codexAuxiliaryApplyNotice
	case domain.BackendClaude:
		notice += "\n" + claudeAuxiliaryApplyNotice
	}
	if !view.HasSession {
		return notice
	}
	status := view.Status
	notice += "\n下一轮本地启动模型：`" + textutil.FirstNonEmpty(status.NextModel, "默认") + "`；推理强度：`" + textutil.FirstNonEmpty(status.NextEffort, "默认") + "`。"
	if status.Error != "" {
		notice += "\n配置应用失败/待生效：" + status.Error
	}
	if status.HasApplied {
		notice += "\n最近已应用模型：`" + textutil.FirstNonEmpty(status.AppliedModel, "默认") + "`；推理强度：`" + textutil.FirstNonEmpty(status.AppliedEffort, "默认") + "`。"
		if status.Pending {
			notice += "\n已保存配置与当前应用值不同，待对应边界生效。"
		}
	} else {
		notice += "\n当前会话尚无已确认的配置应用记录。"
	}
	return notice
}
