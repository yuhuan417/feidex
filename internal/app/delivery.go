package app

import (
	"context"
	"strings"

	"feidex/internal/config"
	"feidex/internal/state"
)

func (a *App) sendFinalMessages(ctx context.Context, sub *state.Submission, text string, inThread bool) []string {
	return a.sendFinalMessagesWithFooter(ctx, sub, text, nil, inThread)
}

func (a *App) sendEmptyFinalCard(ctx context.Context, sub *state.Submission, footerLines []string) string {
	if a == nil || a.feishu == nil || sub == nil || strings.TrimSpace(sub.TriggerMessageID) == "" {
		return ""
	}
	if a.quietModeEnabled() && !shouldDeliverTurnKindInQuiet("final_message") {
		return ""
	}
	card := a.renderReplyMarkdownCardWithHeaderOptions(ctx, sub, "最终答复", "green", true, "", nil, true)
	appendReplyCardFooter(card, footerLines)
	id, err := a.feishu.ReplyCard(ctx, sub.TriggerMessageID, card, a.replyInThreadForSubmission(sub))
	if err != nil || strings.TrimSpace(id) == "" {
		return ""
	}
	a.recordMessageLink(id, "final_message", sub, "")
	_ = a.store.UpdateSubmission(sub.ID, func(s *state.Submission) {
		s.FinalMessageIDs = append([]string(nil), id)
	})
	return id
}

func (a *App) sendFinalMessagesWithFooter(ctx context.Context, sub *state.Submission, text string, footerLines []string, inThread bool) []string {
	if a == nil || a.feishu == nil || sub == nil || strings.TrimSpace(sub.TriggerMessageID) == "" {
		return nil
	}
	if a.quietModeEnabled() && !shouldDeliverTurnKindInQuiet("final_message") {
		return nil
	}
	card := a.renderReplyMarkdownCardWithHeaderOptions(ctx, sub, "最终答复", "green", true, strings.TrimSpace(text), nil, true)
	appendReplyCardFooter(card, footerLines)
	cardID := ""
	id, err := a.feishu.ReplyCard(ctx, sub.TriggerMessageID, card, inThread)
	if err == nil {
		cardID = strings.TrimSpace(id)
	}
	if err != nil {
		fallback := appendFooterText(strings.TrimSpace(text), footerLines)
		id, err = a.feishu.ReplyTextWithID(ctx, sub.TriggerMessageID, fallback, inThread)
	}
	if err != nil || strings.TrimSpace(id) == "" {
		return nil
	}
	_ = a.store.UpsertMessageLink(&state.MessageLink{
		MessageID:    id,
		Kind:         "final_message",
		SessionKey:   sub.SessionKey,
		SubmissionID: sub.ID,
		ThreadID:     sub.ThreadID,
		TurnID:       sub.TurnID,
	})
	_ = a.store.UpdateSubmission(sub.ID, func(s *state.Submission) {
		s.FinalMessageIDs = []string{id}
	})
	if cardID != "" {
		a.scheduleMarkdownPreviewPatch(sub, cardID, "最终答复", "green", true, strings.TrimSpace(text), footerLines)
	}
	return []string{id}
}

func appendReplyCardFooter(card map[string]any, footerLines []string) {
	lines := make([]string, 0, len(footerLines))
	for _, line := range footerLines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		return
	}
	appendMarkdownBodyCardElement(card, map[string]any{
		"tag": "div",
		"text": map[string]any{
			"tag":        "plain_text",
			"content":    strings.Join(lines, "\n"),
			"text_size":  "notation",
			"text_color": "grey",
		},
	})
}

func appendFooterText(body string, footerLines []string) string {
	parts := []string{}
	if strings.TrimSpace(body) != "" {
		parts = append(parts, strings.TrimSpace(body))
	}
	for _, line := range footerLines {
		line = strings.TrimSpace(line)
		if line != "" {
			parts = append(parts, line)
		}
	}
	return strings.Join(parts, "\n")
}

func (a *App) sendTurnEventMessages(ctx context.Context, sub *state.Submission, text string, inThread bool, kind string) []string {
	return a.sendReplyMessages(ctx, sub, text, inThread, kind)
}

func (a *App) sendReplyMessages(ctx context.Context, sub *state.Submission, text string, inThread bool, kind string) []string {
	if a == nil || a.feishu == nil || sub == nil || strings.TrimSpace(sub.TriggerMessageID) == "" {
		return nil
	}
	if a.quietModeEnabled() && !shouldDeliverTurnKindInQuiet(kind) {
		return nil
	}
	enablePreview := strings.TrimSpace(kind) == "final_message"
	if !enablePreview {
		if ws := config.FindWorkspace(a.cfg, sub.WorkspaceID); ws != nil {
			text = sanitizeLocalMarkdownLinks(text, ws.Cwd)
		}
	}
	text = strings.TrimSpace(text)
	if text == "" {
		text = "任务已结束。"
	}
	title, color, replyClass, showHeader := outboundMessageCardMeta(kind)
	var card map[string]any
	if replyClass {
		card = a.renderReplyMarkdownCardWithHeaderOptions(ctx, sub, title, color, showHeader, text, nil, enablePreview)
	} else {
		card = a.renderCompactMarkdownCard(sub, title, color, "", text, nil)
	}
	cardID := ""
	id, err := a.feishu.ReplyCard(ctx, sub.TriggerMessageID, card, inThread)
	if err == nil {
		cardID = strings.TrimSpace(id)
	}
	if err != nil {
		id, err = a.feishu.ReplyTextWithID(ctx, sub.TriggerMessageID, text, inThread)
	}
	if err != nil || strings.TrimSpace(id) == "" {
		return nil
	}
	_ = a.store.UpsertMessageLink(&state.MessageLink{
		MessageID:    id,
		Kind:         kind,
		SessionKey:   sub.SessionKey,
		SubmissionID: sub.ID,
		ThreadID:     sub.ThreadID,
		TurnID:       sub.TurnID,
	})
	if strings.TrimSpace(kind) == "final_message" {
		_ = a.store.UpdateSubmission(sub.ID, func(s *state.Submission) {
			s.FinalMessageIDs = []string{id}
		})
		if cardID != "" {
			a.scheduleMarkdownPreviewPatch(sub, cardID, title, color, showHeader, text, nil)
		}
	}
	return []string{id}
}

func outboundMessageCardMeta(kind string) (title, color string, replyClass bool, showHeader bool) {
	switch strings.TrimSpace(kind) {
	case "final_message":
		return "最终答复", "green", true, true
	case "turn_output":
		return "", "green", true, false
	case "turn_reasoning":
		return "思考", "grey", false, true
	case "turn_command_execution":
		return "命令执行", "blue", false, true
	case "turn_file_change":
		return "文件改动", "orange", false, true
	case "turn_plan":
		return "计划更新", "blue", false, true
	case "turn_queued":
		return "排队中", "grey", false, true
	case "turn_terminal":
		return "任务状态", "grey", false, true
	default:
		return "状态更新", "blue", false, true
	}
}

func (a *App) recordMessageLink(messageID, kind string, sub *state.Submission, requestID string) {
	if strings.TrimSpace(messageID) == "" {
		return
	}
	link := &state.MessageLink{
		MessageID: messageID,
		Kind:      kind,
		RequestID: requestID,
	}
	if sub != nil {
		link.SessionKey = sub.SessionKey
		link.SubmissionID = sub.ID
		link.ThreadID = sub.ThreadID
		link.TurnID = sub.TurnID
	}
	_ = a.store.UpsertMessageLink(link)
}
