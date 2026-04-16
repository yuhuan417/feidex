package app

import (
	"context"
	"strings"
	"time"

	"feidex/internal/state"
)

func (a *App) replyInThreadForSubmission(sub *state.Submission) bool {
	if sub == nil {
		return false
	}
	sess := a.appState().session(sub.SessionKey)
	return sess != nil && sess.ChatType == "group" && a.cfg.Feishu.ReplyInThread
}

func (a *App) sendSubmissionQueuedNotice(ctx context.Context, sub *state.Submission) {
	if sub == nil {
		return
	}
	a.sendTurnEventMessages(ctx, sub, "已加入队列，等待当前任务结束后开始处理。", a.replyInThreadForSubmission(sub), "turn_queued")
}

func (a *App) sendPlanCard(ctx context.Context, sub *state.Submission, planText string) string {
	return a.sendPlanCardWithReuse(ctx, sub, planText, "")
}

func (a *App) sendPlanCardWithReuse(ctx context.Context, sub *state.Submission, planText, reuseMessageID string) string {
	return a.sendTurnEventCardWithReuse(ctx, sub, "计划更新", "blue", "计划:\n"+strings.TrimSpace(planText), "turn_plan", "", reuseMessageID)
}

type turnItemCardPayload struct {
	ItemID        string `json:"item_id"`
	ItemType      string `json:"item_type"`
	Title         string `json:"title"`
	Color         string `json:"color"`
	SummaryText   string `json:"summary_text"`
	DetailText    string `json:"detail_text"`
	IsFinalAnswer bool   `json:"is_final_answer"`
}

func (a *App) sendTurnItemCard(ctx context.Context, sub *state.Submission, payload turnItemCardPayload) string {
	return a.sendTurnItemCardWithReuse(ctx, sub, payload, "")
}

func (a *App) sendTurnItemCardWithReuse(ctx context.Context, sub *state.Submission, payload turnItemCardPayload, reuseMessageID string) string {
	if a == nil || a.feishu == nil || sub == nil || strings.TrimSpace(sub.TriggerMessageID) == "" {
		return ""
	}
	if strings.TrimSpace(payload.SummaryText) == "" && strings.TrimSpace(payload.DetailText) == "" {
		return ""
	}
	if payload.Title == "" || payload.Color == "" {
		payload.Title, payload.Color = turnItemCardMeta(payload.ItemType, payload.IsFinalAnswer)
	}
	if a.quietModeEnabled() && !shouldDeliverTurnItemInQuiet(a.quietMode(), payload.ItemType, payload.IsFinalAnswer) {
		return ""
	}
	kind := turnItemEventKind(payload.ItemType)
	footerLines := []string(nil)
	if payload.IsFinalAnswer {
		footerLines = a.turnFinalFooterLines(sub.TurnID, time.Now())
	}
	if isReplyTurnItem(payload.ItemType) {
		body := replyTurnItemCardBody(payload)
		if body == "" {
			body = payload.DetailText
		}
		results := a.sendReplyCardChunksWithReuse(
			ctx,
			sub,
			replyTurnItemCardTitle(payload),
			payload.Color,
			buildReplyCardChunks(body, payload.IsFinalAnswer, footerLines),
			a.replyInThreadForSubmission(sub),
			payload.IsFinalAnswer,
			reuseMessageID,
		)
		if len(results) == 0 {
			fallback := payload.SummaryText
			if fallback == "" {
				fallback = payload.DetailText
			}
			if payload.IsFinalAnswer {
				a.sendFinalMessagesWithFooter(ctx, sub, fallback, footerLines, a.replyInThreadForSubmission(sub))
			} else {
				a.sendTurnEventMessages(ctx, sub, fallback, a.replyInThreadForSubmission(sub), kind)
			}
			return ""
		}
		for _, result := range results {
			a.recordMessageLink(result.MessageID, kind, sub, payload.ItemID)
		}
		return results[0].MessageID
	}
	card := a.renderTurnItemCard(ctx, sub, payload, payload.IsFinalAnswer)
	if strings.TrimSpace(reuseMessageID) != "" {
		if err := a.feishu.PatchCard(ctx, reuseMessageID, card); err == nil {
			a.recordMessageLink(reuseMessageID, kind, sub, payload.ItemID)
			return reuseMessageID
		}
	}
	id, err := a.feishu.ReplyCard(ctx, sub.TriggerMessageID, card, a.replyInThreadForSubmission(sub))
	if err != nil || strings.TrimSpace(id) == "" {
		fallback := payload.SummaryText
		if fallback == "" {
			fallback = payload.DetailText
		}
		if payload.IsFinalAnswer {
			a.sendFinalMessagesWithFooter(ctx, sub, fallback, footerLines, a.replyInThreadForSubmission(sub))
		} else {
			a.sendTurnEventMessages(ctx, sub, fallback, a.replyInThreadForSubmission(sub), kind)
		}
		return ""
	}
	a.recordMessageLink(id, kind, sub, payload.ItemID)
	return id
}

func (a *App) sendTurnEventCard(ctx context.Context, sub *state.Submission, title, color, body, kind, itemID string) string {
	return a.sendTurnEventCardWithReuse(ctx, sub, title, color, body, kind, itemID, "")
}

func (a *App) replaceTurnEventCardWithReuse(ctx context.Context, sub *state.Submission, title, color, body, kind, itemID, reuseMessageID string) string {
	if a == nil || a.feishu == nil || sub == nil || strings.TrimSpace(sub.TriggerMessageID) == "" {
		return ""
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return ""
	}
	if strings.TrimSpace(reuseMessageID) != "" {
		card := a.renderCompactMarkdownCard(sub, title, color, "", body, nil)
		if err := a.feishu.PatchCard(ctx, reuseMessageID, card); err == nil {
			a.recordMessageLink(reuseMessageID, kind, sub, itemID)
			return reuseMessageID
		}
	}
	return a.sendTurnEventCardWithReuse(ctx, sub, title, color, body, kind, itemID, "")
}

func (a *App) sendTurnEventCardWithReuse(ctx context.Context, sub *state.Submission, title, color, body, kind, itemID, reuseMessageID string) string {
	if a == nil || a.feishu == nil || sub == nil || strings.TrimSpace(sub.TriggerMessageID) == "" {
		return ""
	}
	if a.quietModeEnabled() && !shouldDeliverTurnKindInQuiet(a.quietMode(), kind) {
		return ""
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return ""
	}
	card := a.renderCompactMarkdownCard(sub, title, color, "", body, nil)
	if strings.TrimSpace(reuseMessageID) != "" {
		if err := a.feishu.PatchCard(ctx, reuseMessageID, card); err == nil {
			a.recordMessageLink(reuseMessageID, kind, sub, itemID)
			return reuseMessageID
		}
	}
	id, err := a.feishu.ReplyCard(ctx, sub.TriggerMessageID, card, a.replyInThreadForSubmission(sub))
	if err != nil || strings.TrimSpace(id) == "" {
		a.sendTurnEventMessages(ctx, sub, body, a.replyInThreadForSubmission(sub), kind)
		return ""
	}
	a.recordMessageLink(id, kind, sub, itemID)
	return id
}

func (a *App) renderTurnItemCard(ctx context.Context, sub *state.Submission, payload turnItemCardPayload, enablePreview bool) map[string]any {
	if isReplyTurnItem(payload.ItemType) {
		return a.renderReplyMarkdownCardWithHeaderOptions(ctx, sub, replyTurnItemCardTitle(payload), payload.Color, payload.IsFinalAnswer, replyTurnItemCardBody(payload), nil, enablePreview)
	}
	meta, body := compactTurnItemCardContent(payload)
	return a.renderCompactMarkdownCard(sub, payload.Title, payload.Color, meta, body, nil)
}

func isReplyTurnItem(itemType string) bool {
	return normalizeTurnItemType(itemType) == "agent_message"
}

func replyTurnItemCardBody(payload turnItemCardPayload) string {
	body := stripTurnItemCardHeading(payload.SummaryText, payload.Title, payload.ItemType)
	if body == "" {
		body = stripTurnItemCardHeading(payload.DetailText, payload.Title, payload.ItemType)
	}
	return body
}

func replyTurnItemCardTitle(payload turnItemCardPayload) string {
	if payload.IsFinalAnswer {
		return payload.Title
	}
	return ""
}

func compactTurnItemCardContent(payload turnItemCardPayload) (string, string) {
	summary := stripTurnItemCardHeading(payload.SummaryText, payload.Title, payload.ItemType)
	detail := stripTurnItemCardHeading(payload.DetailText, payload.Title, payload.ItemType)

	switch normalizeTurnItemType(payload.ItemType) {
	case "command_execution":
		body, meta := splitCompactMetaLine(summary)
		return meta, joinMarkdownSections(body, detail)
	case "mcp_tool_call", "dynamic_tool_call", "collab_agent_tool_call":
		body, meta := splitCompactMetaLine(summary)
		if body == "" {
			body = detail
		}
		return meta, body
	default:
		if summary != "" {
			return "", summary
		}
		return "", detail
	}
}

func stripTurnItemCardHeading(text, title, itemType string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	parts := strings.SplitN(text, "\n", 2)
	if len(parts) != 2 {
		return text
	}
	first := strings.TrimSpace(parts[0])
	if !strings.HasSuffix(first, ":") {
		return text
	}
	base := strings.TrimSuffix(first, ":")
	labels := []string{strings.TrimSpace(title), turnItemLabel(itemType)}
	for _, label := range labels {
		label = strings.TrimSpace(label)
		if label == "" {
			continue
		}
		if base == label || strings.HasPrefix(base, label+"（") {
			return strings.TrimSpace(parts[1])
		}
	}
	return text
}

func splitCompactMetaLine(text string) (string, string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", ""
	}
	lines := strings.Split(text, "\n")
	last := strings.TrimSpace(lines[len(lines)-1])
	if last == "" || strings.HasPrefix(last, "```") {
		return text, ""
	}
	if !strings.Contains(last, "status=") && !strings.Contains(last, "exit_code=") {
		return text, ""
	}
	meta := strings.Join(strings.Fields(last), " · ")
	if len(lines) == 1 {
		return "", meta
	}
	return strings.TrimSpace(strings.Join(lines[:len(lines)-1], "\n")), meta
}

func joinMarkdownSections(parts ...string) string {
	sections := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		sections = append(sections, part)
	}
	return strings.Join(sections, "\n\n")
}

func turnItemCardMeta(itemType string, isFinalAnswer bool) (string, string) {
	if isFinalAnswer {
		return "最终答复", "green"
	}
	switch normalizeTurnItemType(itemType) {
	case "reasoning":
		return "思考", "grey"
	case "command_execution":
		return "命令执行", "blue"
	case "file_change":
		return "文件改动", "orange"
	case "agent_message":
		return "回复", "green"
	case "context_compaction":
		return "上下文压缩", "blue"
	default:
		return turnItemLabel(itemType), "blue"
	}
}

func turnItemEventKind(itemType string) string {
	switch normalizeTurnItemType(itemType) {
	case "plan":
		return "turn_plan"
	case "reasoning":
		return "turn_reasoning"
	case "agent_message":
		return "turn_output"
	case "command_execution":
		return "turn_command_execution"
	case "file_change":
		return "turn_file_change"
	case "context_compaction":
		return "turn_item"
	default:
		return "turn_item"
	}
}
