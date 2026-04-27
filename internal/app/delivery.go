package app

import (
	"context"
	appdelivery "feidex/internal/app/delivery"
	"strings"

	"feidex/internal/config"
	"feidex/internal/state"
)

func sendTurnEventMessages(a *App, ctx context.Context, sub *state.Submission, text string, inThread bool, kind string) []string {
	return sendReplyMessages(a, ctx, sub, text, inThread, kind)
}

func sendReplyMessages(a *App, ctx context.Context, sub *state.Submission, text string, inThread bool, kind string) []string {
	return sendReplyMessagesWithReuse(a, ctx, sub, text, inThread, kind, "")
}

func sendReplyMessagesWithReuse(a *App, ctx context.Context, sub *state.Submission, text string, inThread bool, kind, reuseMessageID string) []string {
	if a == nil || a.feishu == nil || sub == nil || strings.TrimSpace(sub.TriggerMessageID) == "" {
		return nil
	}
	if quietModeEnabled(feishuConfig(a)) && !shouldDeliverTurnKindInQuiet(quietMode(feishuConfig(a)), kind) {
		return nil
	}
	appState := appState(a)
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
	title, color, replyClass, showHeader := outboundMessageCardMeta(kind, sub.WorkspaceID)
	if replyClass {
		results := sendReplyCardChunksWithReuse(a, ctx, sub, title, color, appdelivery.BuildReplyCardChunks(text, showHeader, nil), inThread, enablePreview, reuseMessageID)
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
				scheduleLocalFileLinkPatch(a, sub, result.CardID, result.Title, color, result.ShowHeader, result.Body, result.FooterLines)
			}
		}
		return ids
	}

	card := cardRendererForApp(a).renderCompactMarkdownCard(sub, title, color, "", text, nil)
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
			scheduleLocalFileLinkPatch(a, sub, cardID, title, color, showHeader, text, nil)
		}
	}
	return []string{id}
}

func sendReplyCardChunksWithReuse(a *App, ctx context.Context, sub *state.Submission, title, color string, chunks []appdelivery.ReplyCardChunk, inThread bool, enablePreview bool, reuseMessageID string) []appdelivery.SentReplyChunk {
	reuseMessageIDs := []string(nil)
	if strings.TrimSpace(reuseMessageID) != "" {
		reuseMessageIDs = []string{strings.TrimSpace(reuseMessageID)}
	}
	return sendReplyCardChunksWithReuseIDs(a, ctx, sub, title, color, chunks, inThread, enablePreview, reuseMessageIDs)
}

func sendReplyCardChunksWithReuseIDs(a *App, ctx context.Context, sub *state.Submission, title, color string, chunks []appdelivery.ReplyCardChunk, inThread bool, enablePreview bool, reuseMessageIDs []string) []appdelivery.SentReplyChunk {
	if a == nil || a.feishu == nil || sub == nil || strings.TrimSpace(sub.TriggerMessageID) == "" {
		return nil
	}
	specs := prepareReplyChunkRenderSpecs(a, ctx, sub, title, color, chunks, enablePreview)
	results := make([]appdelivery.SentReplyChunk, 0, len(specs))
	for i, spec := range specs {
		currentReuse := ""
		if i < len(reuseMessageIDs) {
			currentReuse = strings.TrimSpace(reuseMessageIDs[i])
		}
		result, ok := sendReplyChunk(a, ctx, sub, spec, inThread, currentReuse)
		if !ok {
			break
		}
		results = append(results, result)
	}
	return results
}

func outboundMessageCardMeta(kind string, workspaceID ...string) (title, color string, replyClass bool, showHeader bool) {
	var base string
	switch strings.TrimSpace(kind) {
	case "final_message":
		base, color, replyClass, showHeader = "最终答复", "green", true, true
	case "turn_output":
		base, color, replyClass, showHeader = "反馈中", "green", true, true
	case "turn_reasoning":
		base, color, replyClass, showHeader = "思考", "grey", false, true
	case "turn_command_execution":
		base, color, replyClass, showHeader = "命令执行", "blue", false, true
	case "turn_file_change":
		base, color, replyClass, showHeader = "文件改动", "orange", false, true
	case "turn_plan":
		base, color, replyClass, showHeader = "计划更新", "blue", false, true
	case "turn_queued":
		base, color, replyClass, showHeader = "排队中", "grey", false, true
	case "turn_started":
		base, color, replyClass, showHeader = "开始处理", "blue", false, true
	case "turn_terminal":
		base, color, replyClass, showHeader = "任务状态", "grey", false, true
	default:
		base, color, replyClass, showHeader = "状态更新", "blue", false, true
	}
	ws := ""
	if len(workspaceID) > 0 {
		ws = strings.TrimSpace(workspaceID[0])
	}
	if ws != "" {
		title = "[" + ws + "] " + base
	} else {
		title = base
	}
	return
}
