package modelconfig

import "feidex/internal/app/cards"

const TurnApplyNotice = "以上为已保存配置。本轮不变；尚未启动的排队消息在下一轮启动前应用最新模型/推理强度。追加到当前轮的输入不触发切换。"
const CodexAuxiliaryApplyNotice = "Plan 模型/强度在下一次本地启动 Plan 轮次时应用；review/subagent 配置待下次新建或恢复 thread 生效。后台 goal 自动续跑不保证采用新配置。"
const ClaudeAuxiliaryApplyNotice = "辅助模型配置待当前轮、审批和后台任务结束后，在下一轮启动前应用；必要时恢复原会话重新初始化。"

func (s ModelConfigService) appendApplyStatus(card map[string]any, sessionKey string) {
	if s.ModelConfigStatus != nil {
		cards.AppendMarkdownBodyCardElement(card, map[string]any{"tag": "markdown", "content": s.ModelConfigStatus(sessionKey)})
	}
}
