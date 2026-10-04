package feishuapp

import (
	"context"
	"feidex/internal/adapter/feishu/attachments"
	domainsubmission "feidex/internal/domain/submission"

	appdelivery "feidex/internal/adapter/feishu/delivery"
	"feidex/internal/adapter/feishu/quietmode"
	"feidex/internal/config"
	"feidex/internal/state"
	"log/slog"
	"strings"

	"github.com/larksuite/oapi-sdk-go/v3/channel/outbound"
)

// replyTextChunked replies in chunks when the body is too long for one message.
//
// The text fallback carries the same body the card was rendered from, so it can
// be arbitrarily long; sending it whole meant a long reply plus a failed card
// send yielded a message that failed on its own, leaving the user with nothing.
// Splitting keeps code fences intact, which matters for agent output.
//
// Returns the first delivered message id. If the first chunk fails the error is
// propagated; if a later chunk fails, what already reached the user is reported
// as delivered so the caller can still record the link.
func replyTextChunked(ctx context.Context, client feishuTextReplier, messageID, text string, inThread bool) (string, error) {
	chunks := outbound.SplitWithCodeFences(text, appdelivery.ReplyTextMaxBytes)
	firstID := ""
	for i, chunk := range chunks {
		id, err := client.ReplyTextWithID(ctx, messageID, chunk, inThread)
		if err != nil {
			if firstID == "" {
				return "", err
			}
			slog.Warn("feishu reply text fallback partially delivered",
				"message_id", messageID,
				"chunk", i+1,
				"chunks", len(chunks),
				"error", err,
			)
			return firstID, nil
		}
		if firstID == "" {
			firstID = strings.TrimSpace(id)
		}
	}
	return firstID, nil
}

func replyTextChunkedEffect(ctx context.Context, a *App, messageID, text string, inThread bool) (string, error) {
	chunks := outbound.SplitWithCodeFences(text, appdelivery.ReplyTextMaxBytes)
	firstID := ""
	for i, chunk := range chunks {
		id, err := replyTextWithIDEffect(ctx, a, messageID, chunk, inThread)
		if err != nil {
			if firstID == "" {
				return "", err
			}
			slog.Warn("feishu reply text fallback partially delivered", "message_id", messageID, "chunk", i+1, "chunks", len(chunks), "error", err)
			return firstID, nil
		}
		if firstID == "" {
			firstID = strings.TrimSpace(id)
		}
	}
	return firstID, nil
}

// feishuTextReplier is the slice of the Feishu client the chunked fallback
// needs, so the helper stays testable without a full client.
type feishuTextReplier interface {
	ReplyTextWithID(context.Context, string, string, bool) (string, error)
}

func sendReplyMessagesWithReuse(a *App, ctx context.Context, sub *domainsubmission.Submission, text string, inThread bool, kind, reuseMessageID string) []string {
	if a == nil || a.feishu == nil || sub == nil || strings.TrimSpace(sub.TriggerMessageID) == "" {
		return nil
	}
	if quietmode.Enabled(a.configView().feishuConfig()) && !quietmode.ShouldDeliverTurnKind(quietmode.Mode(a.configView().feishuConfig()), kind) {
		return nil
	}
	appState := a.State()
	enablePreview := strings.TrimSpace(kind) == "final_message"
	if !enablePreview {
		if ws := config.FindWorkspace(a.cfg, sub.WorkspaceID); ws != nil {
			text = attachments.NeutralizeLocalMarkdownLinks(text, ws.Cwd)
		}
	}
	text = strings.TrimSpace(text)
	if text == "" {
		text = "任务已结束。"
	}
	title, color, replyClass, showHeader := outboundMessageCardMeta(kind, sub.WorkspaceID)
	if replyClass {
		results := newReplyChunkDelivery(newCardRenderer(a.Config()), a.State(), newEffectOutbound(a.FrontendID(), newEffectRunner(a.runtimeOwner)), a.feishu != nil,
			a.configView(), newMessageLinkRecorder(a.configView(), a.runtimeOwner, a.bindings.Continuation),
			newLocalFileLinkPatcher(a.Config(), a.State(), a.feishu, &a.runtimeOwner.Lifecycle, a.asyncRunner, a.bindings.FinalCardPatch, newEffectOutbound(a.FrontendID(), newEffectRunner(a.runtimeOwner)), a.feishu != nil),
		).SendWithReuse(ctx, sub, title, color, appdelivery.BuildReplyCardChunks(text, showHeader, nil), inThread, enablePreview, reuseMessageID)
		ids := make([]string, 0, len(results))
		for _, result := range results {
			ids = append(ids, result.MessageID)
		}
		return ids
	}

	card := newCardRenderer(a.Config()).renderCompactMarkdownCard(sub, contentCardTitleForSubmission(a.State(), sub, title), color, "", text, nil)
	if strings.TrimSpace(reuseMessageID) != "" {
		if err := patchCardEffect(ctx, a, reuseMessageID, card); err == nil {
			_ = appState.SaveMessageLink(&state.MessageLink{
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
	id, err := replyCardWithIDEffect(ctx, a, sub.TriggerMessageID, card, inThread)
	if err == nil {
		cardID = strings.TrimSpace(id)
	}
	if err != nil {
		id, err = replyTextChunkedEffect(ctx, a, sub.TriggerMessageID, text, inThread)
	}
	if err != nil || strings.TrimSpace(id) == "" {
		return nil
	}
	_ = appState.SaveMessageLink(&state.MessageLink{
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

func outboundMessageCardMeta(kind string, workspaceID ...string) (title, color string, replyClass bool, showHeader bool) {
	var base string
	switch strings.TrimSpace(kind) {
	case "final_message":
		base, color, replyClass, showHeader = "最终答复", "green", true, true
	case "turn_output":
		base, color, replyClass, showHeader = "反馈中", "blue", true, true
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
