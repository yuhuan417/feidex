package app

import (
	"context"
	appdelivery "feidex/internal/app/delivery"
	"strings"
	"time"

	"feidex/internal/state"
)

func replyInThreadForSubmission(a *App, sub *state.Submission) bool {
	if sub == nil {
		return false
	}
	sess := appState(a).session(sub.SessionKey)
	return sess != nil && sess.ChatType == "group" && replyInThreadEnabled(a, sess.ChatType)
}

func sendSubmissionQueuedNotice(a *App, ctx context.Context, sub *state.Submission) {
	if sub == nil {
		return
	}
	sendTurnEventMessages(a, ctx, sub, "已加入队列，等待当前任务结束后开始处理。", replyInThreadForSubmission(a, sub), "turn_queued")
}

func sendSubmissionStartedNotice(a *App, ctx context.Context, sub *state.Submission) {
	if sub == nil {
		return
	}
	sendTurnEventMessages(a, ctx, sub, "已轮到这条消息，开始处理。", replyInThreadForSubmission(a, sub), "turn_started")
}

func (s outboundCardService) sendPlanCard(ctx context.Context, sub *state.Submission, planText string) string {
	return newOutboundCardService(s.app).sendPlanCardWithReuse(ctx, sub, planText, "")
}

func (s outboundCardService) sendPlanCardWithReuse(ctx context.Context, sub *state.Submission, planText, reuseMessageID string) string {
	return newOutboundCardService(s.app).sendTurnEventCardWithReuse(ctx, sub, "计划更新", "blue", "计划:\n"+strings.TrimSpace(planText), "turn_plan", "", reuseMessageID)
}

type turnItemCardPayload struct {
	ItemID           string `json:"item_id"`
	ItemType         string `json:"item_type"`
	ProtocolItemType string `json:"protocol_item_type"`
	ToolName         string `json:"tool_name"`
	Title            string `json:"title"`
	Color            string `json:"color"`
	SummaryText      string `json:"summary_text"`
	DetailText       string `json:"detail_text"`
	IsFinalAnswer    bool   `json:"is_final_answer"`
}

func (s outboundCardService) sendTurnItemCard(ctx context.Context, sub *state.Submission, payload turnItemCardPayload) string {
	return newOutboundCardService(s.app).sendTurnItemCardWithReuse(ctx, sub, payload, "")
}

func (s outboundCardService) sendTurnItemCardWithReuse(ctx context.Context, sub *state.Submission, payload turnItemCardPayload, reuseMessageID string) string {
	if s.app == nil || s.app.feishu == nil || sub == nil || strings.TrimSpace(sub.TriggerMessageID) == "" {
		return ""
	}
	if strings.TrimSpace(payload.SummaryText) == "" && strings.TrimSpace(payload.DetailText) == "" {
		return ""
	}
	if payload.Title == "" || payload.Color == "" {
		payload.Title, payload.Color = turnItemCardMeta(payload.ItemType, payload.IsFinalAnswer)
	}
	if quietModeEnabled(feishuConfig(s.app)) && !shouldDeliverTurnItemPayloadInQuiet(quietMode(feishuConfig(s.app)), payload) {
		return ""
	}
	kind := turnItemEventKind(payload.ItemType)
	footerLines := []string(nil)
	if payload.IsFinalAnswer {
		footerLines = newRuntimeStateService(s.app).turnFinalFooterLines(sub.TurnID, time.Now())
	}
	if isReplyTurnItem(payload.ItemType) {
		body := replyTurnItemCardBody(payload)
		if body == "" {
			body = payload.DetailText
		}
		results := sendReplyCardChunksWithReuse(s.app,
			ctx,
			sub,
			replyTurnItemCardTitle(payload),
			payload.Color,
			appdelivery.BuildReplyCardChunks(body, payload.IsFinalAnswer, footerLines),
			replyInThreadForSubmission(s.app, sub),
			payload.IsFinalAnswer,
			reuseMessageID,
		)
		if len(results) == 0 {
			fallback := payload.SummaryText
			if fallback == "" {
				fallback = payload.DetailText
			}
			if payload.IsFinalAnswer {
				sendFinalMessagesWithFooter(s.app, ctx, sub, fallback, footerLines, replyInThreadForSubmission(s.app, sub))
			} else {
				sendTurnEventMessages(s.app, ctx, sub, fallback, replyInThreadForSubmission(s.app, sub), kind)
			}
			return ""
		}
		for _, result := range results {
			recordMessageLink(s.app, result.MessageID, kind, sub, payload.ItemID)
			if payload.IsFinalAnswer && result.CardID != "" {
				scheduleLocalFileLinkPatch(s.app, sub, result.CardID, result.Title, payload.Color, result.ShowHeader, result.Body, result.FooterLines)
			}
		}
		return results[0].MessageID
	}
	card := newOutboundCardService(s.app).renderTurnItemCard(ctx, sub, payload, payload.IsFinalAnswer)
	if strings.TrimSpace(reuseMessageID) != "" {
		if err := s.app.feishu.PatchCard(ctx, reuseMessageID, card); err == nil {
			recordMessageLink(s.app, reuseMessageID, kind, sub, payload.ItemID)
			return reuseMessageID
		}
	}
	id, err := s.app.feishu.ReplyCard(ctx, sub.TriggerMessageID, card, replyInThreadForSubmission(s.app, sub))
	if err != nil || strings.TrimSpace(id) == "" {
		fallback := payload.SummaryText
		if fallback == "" {
			fallback = payload.DetailText
		}
		if payload.IsFinalAnswer {
			sendFinalMessagesWithFooter(s.app, ctx, sub, fallback, footerLines, replyInThreadForSubmission(s.app, sub))
		} else {
			sendTurnEventMessages(s.app, ctx, sub, fallback, replyInThreadForSubmission(s.app, sub), kind)
		}
		return ""
	}
	recordMessageLink(s.app, id, kind, sub, payload.ItemID)
	return id
}

func (s outboundCardService) sendTurnEventCard(ctx context.Context, sub *state.Submission, title, color, body, kind, itemID string) string {
	return newOutboundCardService(s.app).sendTurnEventCardWithReuse(ctx, sub, title, color, body, kind, itemID, "")
}

func (s outboundCardService) replaceTurnEventCardWithReuse(ctx context.Context, sub *state.Submission, title, color, body, kind, itemID, reuseMessageID string) string {
	if s.app == nil || s.app.feishu == nil || sub == nil || strings.TrimSpace(sub.TriggerMessageID) == "" {
		return ""
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return ""
	}
	if strings.TrimSpace(reuseMessageID) != "" {
		card := cardRendererForApp(s.app).renderCompactMarkdownCard(sub, title, color, "", body, nil)
		if err := s.app.feishu.PatchCard(ctx, reuseMessageID, card); err == nil {
			recordMessageLink(s.app, reuseMessageID, kind, sub, itemID)
			return reuseMessageID
		}
	}
	return newOutboundCardService(s.app).sendTurnEventCardWithReuse(ctx, sub, title, color, body, kind, itemID, "")
}

func (s outboundCardService) sendTurnEventCardWithReuse(ctx context.Context, sub *state.Submission, title, color, body, kind, itemID, reuseMessageID string) string {
	if s.app == nil || s.app.feishu == nil || sub == nil || strings.TrimSpace(sub.TriggerMessageID) == "" {
		return ""
	}
	if quietModeEnabled(feishuConfig(s.app)) && !shouldDeliverTurnKindInQuiet(quietMode(feishuConfig(s.app)), kind) {
		return ""
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return ""
	}
	card := cardRendererForApp(s.app).renderCompactMarkdownCard(sub, title, color, "", body, nil)
	if strings.TrimSpace(reuseMessageID) != "" {
		if err := s.app.feishu.PatchCard(ctx, reuseMessageID, card); err == nil {
			recordMessageLink(s.app, reuseMessageID, kind, sub, itemID)
			return reuseMessageID
		}
	}
	id, err := s.app.feishu.ReplyCard(ctx, sub.TriggerMessageID, card, replyInThreadForSubmission(s.app, sub))
	if err != nil || strings.TrimSpace(id) == "" {
		sendTurnEventMessages(s.app, ctx, sub, body, replyInThreadForSubmission(s.app, sub), kind)
		return ""
	}
	recordMessageLink(s.app, id, kind, sub, itemID)
	return id
}

func (s outboundCardService) renderTurnItemCard(ctx context.Context, sub *state.Submission, payload turnItemCardPayload, enablePreview bool) map[string]any {
	if isReplyTurnItem(payload.ItemType) {
		return cardRendererForApp(s.app).renderReplyMarkdownCardWithHeaderOptions(ctx, sub, replyTurnItemCardTitle(payload), payload.Color, payload.IsFinalAnswer, replyTurnItemCardBody(payload), nil, enablePreview)
	}
	meta, body := compactTurnItemCardContent(payload)
	return cardRendererForApp(s.app).renderCompactMarkdownCard(sub, payload.Title, payload.Color, meta, body, nil)
}

func isReplyTurnItem(itemType string) bool {
	switch normalizeTurnItemType(itemType) {
	case "agent_message", "exited_review_mode":
		return true
	default:
		return false
	}
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
	case "entered_review_mode":
		return "进入 Review", "blue"
	case "exited_review_mode":
		return "Review 结果", "green"
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
	case "exited_review_mode":
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
