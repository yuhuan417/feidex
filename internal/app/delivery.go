package app

import (
	"context"
	"fmt"
	"strings"

	"feidex/internal/config"
	"feidex/internal/state"
)

func (a *App) sendTurnEventMessages(ctx context.Context, sub *state.Submission, text string, inThread bool, kind string) []string {
	return a.sendReplyMessages(ctx, sub, text, inThread, kind)
}

func (a *App) sendReplyMessages(ctx context.Context, sub *state.Submission, text string, inThread bool, kind string) []string {
	if a == nil || a.feishu == nil || sub == nil || strings.TrimSpace(sub.TriggerMessageID) == "" {
		return nil
	}
	if a.quietModeEnabled() && !shouldDeliverTurnKindInQuiet(a.quietMode(), kind) {
		return nil
	}
	appState := a.appState()
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
	if replyClass {
		results := a.sendReplyCardChunks(ctx, sub, title, color, buildReplyCardChunks(text, showHeader, nil), inThread, enablePreview)
		if len(results) == 0 {
			return nil
		}
		ids := make([]string, 0, len(results))
		for _, result := range results {
			ids = append(ids, result.MessageID)
			_ = appState.saveMessageLink(&state.MessageLink{
				MessageID:    result.MessageID,
				SessionKey:   sub.SessionKey,
				SubmissionID: sub.ID,
				ThreadID:     sub.ThreadID,
				TurnID:       sub.TurnID,
			})
			if strings.TrimSpace(kind) == "final_message" && result.CardID != "" {
				a.scheduleLocalFileLinkPatch(sub, result.CardID, result.Title, color, result.ShowHeader, result.Body, result.FooterLines)
			}
		}
		return ids
	}

	card := a.renderCompactMarkdownCard(sub, title, color, "", text, nil)
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
	_ = appState.saveMessageLink(&state.MessageLink{
		MessageID:    id,
		SessionKey:   sub.SessionKey,
		SubmissionID: sub.ID,
		ThreadID:     sub.ThreadID,
		TurnID:       sub.TurnID,
	})
	if strings.TrimSpace(kind) == "final_message" {
		if cardID != "" {
			a.scheduleLocalFileLinkPatch(sub, cardID, title, color, showHeader, text, nil)
		}
	}
	return []string{id}
}

func (a *App) sendReplyCardChunks(ctx context.Context, sub *state.Submission, title, color string, chunks []replyCardChunk, inThread bool, enablePreview bool) []sentReplyChunk {
	return a.sendReplyCardChunksWithReuse(ctx, sub, title, color, chunks, inThread, enablePreview, "")
}

func (a *App) sendReplyCardChunksWithReuse(ctx context.Context, sub *state.Submission, title, color string, chunks []replyCardChunk, inThread bool, enablePreview bool, reuseMessageID string) []sentReplyChunk {
	if a == nil || a.feishu == nil || sub == nil || strings.TrimSpace(sub.TriggerMessageID) == "" {
		return nil
	}
	chunks = a.fitReplyCardChunks(ctx, sub, title, color, chunks, enablePreview)
	results := make([]sentReplyChunk, 0, len(chunks))
	for i, chunk := range chunks {
		effectiveTitle := title
		showHeader := chunk.ShowHeader
		if strings.TrimSpace(title) == "最终答复" && len(chunks) > 1 {
			effectiveTitle = fmt.Sprintf("%s %d/%d", strings.TrimSpace(title), i+1, len(chunks))
			showHeader = true
		}
		card := a.renderReplyMarkdownCardWithHeaderOptions(ctx, sub, effectiveTitle, color, showHeader, chunk.Body, nil, enablePreview)
		appendReplyCardFooter(card, chunk.FooterLines)

		cardID := ""
		id := ""
		var err error
		if i == 0 && strings.TrimSpace(reuseMessageID) != "" {
			id = strings.TrimSpace(reuseMessageID)
			err = a.feishu.PatchCard(ctx, id, card)
			if err == nil {
				cardID = id
			} else {
				id, err = a.feishu.ReplyCard(ctx, sub.TriggerMessageID, card, inThread)
				if err == nil && strings.TrimSpace(id) != "" {
					cardID = strings.TrimSpace(id)
				}
			}
		} else {
			id, err = a.feishu.ReplyCard(ctx, sub.TriggerMessageID, card, inThread)
			if err == nil && strings.TrimSpace(id) != "" {
				cardID = strings.TrimSpace(id)
			}
		}
		if err != nil || strings.TrimSpace(id) == "" {
			fallback := appendFooterText(strings.TrimSpace(chunk.Body), chunk.FooterLines)
			id, err = a.feishu.ReplyTextWithID(ctx, sub.TriggerMessageID, fallback, inThread)
		}
		if err != nil || strings.TrimSpace(id) == "" {
			break
		}
		results = append(results, sentReplyChunk{
			MessageID:   strings.TrimSpace(id),
			CardID:      cardID,
			Title:       effectiveTitle,
			Body:        chunk.Body,
			FooterLines: append([]string(nil), chunk.FooterLines...),
			ShowHeader:  showHeader,
		})
	}
	return results
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
