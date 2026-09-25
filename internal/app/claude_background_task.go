package app

import (
	"context"
	"strings"

	appclauderuntime "feidex/internal/app/clauderuntime"
	"feidex/internal/claudecli"
)

// sendClaudeBackgroundTaskNotification delivers the task_notification event
// on the original conversation, even when the parent submission has already
// been finalized and removed from runtime state.
func sendClaudeBackgroundTaskNotification(a *App, ctx context.Context, target appclauderuntime.BackgroundTaskTarget, event claudecli.BackgroundTaskEvent) {
	if a == nil || a.feishu == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}

	status := strings.TrimSpace(event.Status)
	if status == "" {
		status = "completed"
	}
	color := "green"
	title := "后台 Agent 已完成"
	switch strings.ToLower(status) {
	case "failed", "error", "killed", "stopped":
		color = "red"
		title = "后台 Agent 未完成"
	case "cancelled", "canceled":
		color = "grey"
		title = "后台 Agent 已取消"
	}
	title = contentCardTitleForSession(a, target.SessionKey, target.WorkspaceID, title)

	lines := []string{"Claude 后台 Agent 任务已返回。"}
	if description := strings.TrimSpace(event.Description); description != "" {
		lines = append(lines, "", "任务：", description)
	}
	lines = append(lines, "", "状态：`"+status+"`")
	if summary := strings.TrimSpace(event.Summary); summary != "" {
		lines = append(lines, "", "结果：", summary)
	}
	card := a.feishu.SimpleStatusCard(title, color, strings.Join(lines, "\n"), nil)
	if triggerID := strings.TrimSpace(target.TriggerMessageID); triggerID != "" {
		if _, err := a.feishu.ReplyCard(ctx, triggerID, card, false); err != nil {
			// The original message can be unavailable after retention or recall;
			// fall back to a standalone card when a chat ID is known.
			if chatID := strings.TrimSpace(target.ChatID); chatID != "" {
				_, _ = a.feishu.SendCard(ctx, chatID, card)
			}
		}
		return
	}
	if chatID := strings.TrimSpace(target.ChatID); chatID != "" {
		_, _ = a.feishu.SendCard(ctx, chatID, card)
	}
}
