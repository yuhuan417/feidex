package app

import (
	"context"
	"strings"

	"feidex/internal/config"
	"feidex/internal/state"
)

func (a *App) sendTurnEventMessages(ctx context.Context, sub *state.Submission, text string, inThread bool, kind string) []string {
	return a.sendReplyMessages(ctx, sub, text, inThread, kind)
}

func (a *App) sendReplyMessages(ctx context.Context, sub *state.Submission, text string, inThread bool, kind string) []string {
	return a.sendReplyMessagesWithReuse(ctx, sub, text, inThread, kind, "")
}

func (a *App) sendReplyMessagesWithReuse(ctx context.Context, sub *state.Submission, text string, inThread bool, kind, reuseMessageID string) []string {
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
		results := a.sendReplyCardChunksWithReuse(ctx, sub, title, color, buildReplyCardChunks(text, showHeader, nil), inThread, enablePreview, reuseMessageID)
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
	if strings.TrimSpace(reuseMessageID) != "" {
		if err := a.feishu.PatchCard(ctx, reuseMessageID, card); err == nil {
			_ = appState.saveMessageLink(&state.MessageLink{
				MessageID:    reuseMessageID,
				SessionKey:   sub.SessionKey,
				SubmissionID: sub.ID,
				ThreadID:     sub.ThreadID,
				TurnID:       sub.TurnID,
			})
			return []string{reuseMessageID}
		}
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
	reuseMessageIDs := []string(nil)
	if strings.TrimSpace(reuseMessageID) != "" {
		reuseMessageIDs = []string{strings.TrimSpace(reuseMessageID)}
	}
	return a.sendReplyCardChunksWithReuseIDs(ctx, sub, title, color, chunks, inThread, enablePreview, reuseMessageIDs)
}

func (a *App) sendReplyCardChunksWithReuseIDs(ctx context.Context, sub *state.Submission, title, color string, chunks []replyCardChunk, inThread bool, enablePreview bool, reuseMessageIDs []string) []sentReplyChunk {
	if a == nil || a.feishu == nil || sub == nil || strings.TrimSpace(sub.TriggerMessageID) == "" {
		return nil
	}
	specs := a.prepareReplyChunkRenderSpecs(ctx, sub, title, color, chunks, enablePreview)
	results := make([]sentReplyChunk, 0, len(specs))
	for i, spec := range specs {
		currentReuse := ""
		if i < len(reuseMessageIDs) {
			currentReuse = strings.TrimSpace(reuseMessageIDs[i])
		}
		result, ok := a.sendReplyChunk(ctx, sub, spec, inThread, currentReuse)
		if !ok {
			break
		}
		results = append(results, result)
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
	case "turn_started":
		return "开始处理", "blue", false, true
	case "turn_terminal":
		return "任务状态", "grey", false, true
	default:
		return "状态更新", "blue", false, true
	}
}
