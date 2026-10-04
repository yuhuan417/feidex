package feishuapp

import (
	"context"
	"strings"

	"feidex/internal/adapter/feishu/outbound"
	"feidex/internal/adapter/feishu/planmode"
	"feidex/internal/application"
	"feidex/internal/claudecli"
	"feidex/internal/domain/identity"
	frontendruntime "feidex/internal/runtime"
	appclauderuntime "feidex/internal/runtime/claude"
)

type claudeBackgroundTaskNotifier struct {
	client   simpleStatusCardClient
	state    planmode.SessionStateProvider
	frontend identity.FrontendID
	runner   frontendruntime.EffectRunner
	ready    bool
}

func (n claudeBackgroundTaskNotifier) Send(ctx context.Context, target appclauderuntime.BackgroundTaskTarget, event claudecli.BackgroundTaskEvent) {
	if !n.ready {
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
	title = planmode.ContentCardTitleForSessionFromState(n.state, true, target.SessionKey, target.WorkspaceID, title)

	lines := []string{"Claude 后台 Agent 任务已返回。"}
	if description := strings.TrimSpace(event.Description); description != "" {
		lines = append(lines, "", "任务：", description)
	}
	lines = append(lines, "", "状态：`"+status+"`")
	if summary := strings.TrimSpace(event.Summary); summary != "" {
		lines = append(lines, "", "结果：", summary)
	}
	card := n.client.SimpleStatusCard(title, color, strings.Join(lines, "\n"), nil)
	if triggerID := strings.TrimSpace(target.TriggerMessageID); triggerID != "" {
		err := n.runner.Run(ctx, []application.Effect{application.SendCard{
			Frontend:       n.frontend,
			Chat:           identity.ChatRef{ID: target.ChatID},
			ReplyMessageID: triggerID,
			View:           outbound.Card(card),
		}})
		if err == nil {
			return
		}
	}
	if chatID := strings.TrimSpace(target.ChatID); chatID != "" {
		_ = n.runner.Run(ctx, []application.Effect{application.SendCard{
			Frontend: n.frontend,
			Chat:     identity.ChatRef{ID: chatID},
			View:     outbound.Card(card),
		}})
	}
}
