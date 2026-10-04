package feishuapp

import (
	"context"
	"feidex/internal/adapter/feishu/attachments"
	"feidex/internal/adapter/feishu/quietmode"
	domainsubmission "feidex/internal/domain/submission"
	apputil "feidex/internal/formatutil"

	appdelivery "feidex/internal/adapter/feishu/delivery"
	"feidex/internal/config"
	"feidex/internal/state"
	"fmt"
	"log/slog"
	"strings"

	"github.com/larksuite/oapi-sdk-go/v3/channel/outbound"
)

type replyChunkRenderSpec struct {
	Title         string
	Color         string
	Body          string
	FooterLines   []string
	ShowHeader    bool
	EnablePreview bool
}

type replyChunkDelivery struct {
	renderer cardRenderer
	state    interface {
		turnStopStateProvider
		SaveMessageLink(*state.MessageLink) error
	}
	outbound effectOutbound
	ready    bool
	config   frontendConfigView
	links    messageLinkRecorder
	local    localFileLinkPatcher
}

func newReplyChunkDelivery(renderer cardRenderer, state interface {
	turnStopStateProvider
	SaveMessageLink(*state.MessageLink) error
}, outbound effectOutbound, ready bool, cfg frontendConfigView, links messageLinkRecorder, local localFileLinkPatcher) replyChunkDelivery {
	return replyChunkDelivery{renderer: renderer, state: state, outbound: outbound, ready: ready, config: cfg, links: links, local: local}
}

func (d replyChunkDelivery) SendWithReuse(ctx context.Context, sub *domainsubmission.Submission, title, color string, chunks []appdelivery.ReplyCardChunk, inThread, enablePreview bool, reuseMessageID string) []appdelivery.SentReplyChunk {
	reuseMessageIDs := []string(nil)
	if strings.TrimSpace(reuseMessageID) != "" {
		reuseMessageIDs = []string{strings.TrimSpace(reuseMessageID)}
	}
	return d.SendWithReuseIDs(ctx, sub, title, color, chunks, inThread, enablePreview, reuseMessageIDs)
}

func (d replyChunkDelivery) SendWithReuseIDs(ctx context.Context, sub *domainsubmission.Submission, title, color string, chunks []appdelivery.ReplyCardChunk, inThread, enablePreview bool, reuseMessageIDs []string) []appdelivery.SentReplyChunk {
	if !d.ready || sub == nil || strings.TrimSpace(sub.TriggerMessageID) == "" {
		return nil
	}
	specs := d.prepareSpecs(ctx, sub, title, color, chunks, enablePreview)
	results := make([]appdelivery.SentReplyChunk, 0, len(specs))
	for i, spec := range specs {
		currentReuse := ""
		if i < len(reuseMessageIDs) {
			currentReuse = strings.TrimSpace(reuseMessageIDs[i])
		}
		result, ok := d.sendChunk(ctx, sub, spec, inThread, currentReuse)
		if !ok {
			break
		}
		results = append(results, result)
	}
	return results
}

func (d replyChunkDelivery) SendMessagesWithReuse(ctx context.Context, sub *domainsubmission.Submission, text string, inThread bool, kind, reuseMessageID string) []string {
	if !d.ready || sub == nil || strings.TrimSpace(sub.TriggerMessageID) == "" {
		return nil
	}
	feishuConfig := d.config.feishuConfig()
	if quietmode.Enabled(feishuConfig) && !quietmode.ShouldDeliverTurnKind(quietmode.Mode(feishuConfig), kind) {
		return nil
	}
	enablePreview := strings.TrimSpace(kind) == "final_message"
	if !enablePreview {
		if ws := config.FindWorkspace(d.renderer.cfg, sub.WorkspaceID); ws != nil {
			text = attachments.NeutralizeLocalMarkdownLinks(text, ws.Cwd)
		}
	}
	text = strings.TrimSpace(text)
	if text == "" {
		text = "任务已结束。"
	}
	title, color, replyClass, showHeader := outboundMessageCardMeta(kind, sub.WorkspaceID)
	if replyClass {
		results := d.SendWithReuse(ctx, sub, title, color, appdelivery.BuildReplyCardChunks(text, showHeader, nil), inThread, enablePreview, reuseMessageID)
		if len(results) == 0 {
			return nil
		}
		ids := make([]string, 0, len(results))
		for _, result := range results {
			ids = append(ids, result.MessageID)
			_ = d.state.SaveMessageLink(&state.MessageLink{MessageID: result.MessageID, SessionKey: sub.SessionKey, SubmissionID: sub.ID, ThreadID: sub.ThreadID, TurnID: sub.TurnID})
			if strings.TrimSpace(kind) == "final_message" && result.CardID != "" {
				d.local.Schedule(sub, result.CardID, result.Title, color, result.ShowHeader, result.Body, result.FooterLines)
			}
		}
		return ids
	}

	card := d.renderer.renderCompactMarkdownCard(sub, contentCardTitleForSubmission(d.state, sub, title), color, "", text, nil)
	if strings.TrimSpace(reuseMessageID) != "" {
		if err := d.outbound.PatchCard(ctx, reuseMessageID, card); err == nil {
			_ = d.state.SaveMessageLink(&state.MessageLink{MessageID: reuseMessageID, SessionKey: sub.SessionKey, SubmissionID: sub.ID, ThreadID: sub.ThreadID, TurnID: sub.TurnID})
			return []string{reuseMessageID}
		}
	}
	cardID := ""
	id, err := d.outbound.ReplyCard(ctx, sub.TriggerMessageID, card, inThread)
	if err == nil {
		cardID = strings.TrimSpace(id)
	}
	if err != nil {
		id, err = d.replyTextChunked(ctx, sub.TriggerMessageID, text, inThread)
	}
	if err != nil || strings.TrimSpace(id) == "" {
		return nil
	}
	_ = d.state.SaveMessageLink(&state.MessageLink{MessageID: id, SessionKey: sub.SessionKey, SubmissionID: sub.ID, ThreadID: sub.ThreadID, TurnID: sub.TurnID})
	if strings.TrimSpace(kind) == "final_message" && cardID != "" {
		d.local.Schedule(sub, cardID, title, color, showHeader, text, nil)
	}
	return []string{id}
}

func (d replyChunkDelivery) SendFinalMessagesWithFooter(ctx context.Context, sub *domainsubmission.Submission, text string, footerLines []string, inThread bool, reuseMessageID string) []string {
	if !d.ready || sub == nil || strings.TrimSpace(sub.TriggerMessageID) == "" {
		return nil
	}
	feishuConfig := d.config.feishuConfig()
	if quietmode.Enabled(feishuConfig) && !quietmode.ShouldDeliverTurnKind(quietmode.Mode(feishuConfig), "final_message") {
		return nil
	}
	title, color, _, _ := outboundMessageCardMeta("final_message", sub.WorkspaceID)
	chunks := appdelivery.BuildReplyCardChunks(strings.TrimSpace(text), true, footerLines)
	results := d.SendWithReuse(ctx, sub, title, color, chunks, inThread, true, reuseMessageID)
	if len(results) == 0 {
		return nil
	}
	ids := make([]string, 0, len(results))
	for _, result := range results {
		ids = append(ids, result.MessageID)
		d.links.Record(result.MessageID, "final_message", anchorForSubmission(sub), "")
		if result.CardID != "" {
			d.local.finalCards.RegisterFinalCardPatchState(result.CardID, sub, result.Title, "green", result.ShowHeader, result.Body, result.FooterLines)
			d.local.Schedule(sub, result.CardID, result.Title, "green", result.ShowHeader, result.Body, result.FooterLines)
		}
	}
	return ids
}

func (d replyChunkDelivery) prepareSpecs(ctx context.Context, sub *domainsubmission.Submission, title, color string, chunks []appdelivery.ReplyCardChunk, enablePreview bool) []replyChunkRenderSpec {
	if strings.Contains(strings.TrimSpace(title), "最终答复") && len(chunks) > 0 {
		copied := append([]appdelivery.ReplyCardChunk(nil), chunks...)
		copied[0].Body = apputil.PrependAttentionMentionMarkdown(copied[0].Body, turnStopAttentionUserID(d.state, sub, sub.TurnID))
		chunks = copied
	}
	chunks = fitReplyCardChunks(d.renderer, ctx, sub, title, color, chunks, enablePreview)
	if len(chunks) == 0 {
		return nil
	}
	specs := make([]replyChunkRenderSpec, 0, len(chunks))
	for i, chunk := range chunks {
		effectiveTitle := title
		showHeader := chunk.ShowHeader
		if strings.Contains(strings.TrimSpace(title), "最终答复") && len(chunks) > 1 {
			effectiveTitle = fmt.Sprintf("%s %d/%d", strings.TrimSpace(title), i+1, len(chunks))
			showHeader = true
		}
		specs = append(specs, replyChunkRenderSpec{
			Title:         effectiveTitle,
			Color:         color,
			Body:          chunk.Body,
			FooterLines:   append([]string(nil), chunk.FooterLines...),
			ShowHeader:    showHeader,
			EnablePreview: enablePreview,
		})
	}
	return specs
}

func (d replyChunkDelivery) sendChunk(ctx context.Context, sub *domainsubmission.Submission, spec replyChunkRenderSpec, inThread bool, reuseMessageID string) (appdelivery.SentReplyChunk, bool) {
	card := d.renderer.renderReplyMarkdownCardWithHeaderOptions(ctx, sub, contentCardTitleForSubmission(d.state, sub, spec.Title), spec.Color, spec.ShowHeader, spec.Body, nil, spec.EnablePreview)
	appendReplyCardFooter(card, spec.FooterLines)

	cardID := ""
	id := ""
	var err error
	if strings.TrimSpace(reuseMessageID) != "" {
		id = strings.TrimSpace(reuseMessageID)
		err = d.outbound.PatchCard(ctx, id, card)
		if err == nil {
			cardID = id
		} else {
			id, err = d.outbound.ReplyCard(ctx, sub.TriggerMessageID, card, inThread)
			if err == nil && strings.TrimSpace(id) != "" {
				cardID = strings.TrimSpace(id)
			}
		}
	} else {
		id, err = d.outbound.ReplyCard(ctx, sub.TriggerMessageID, card, inThread)
		if err == nil && strings.TrimSpace(id) != "" {
			cardID = strings.TrimSpace(id)
		}
	}
	if err != nil || strings.TrimSpace(id) == "" {
		fallback := appendFooterText(strings.TrimSpace(spec.Body), spec.FooterLines)
		id, err = d.replyTextChunked(ctx, sub.TriggerMessageID, fallback, inThread)
	}
	if err != nil || strings.TrimSpace(id) == "" {
		return appdelivery.SentReplyChunk{}, false
	}
	return appdelivery.SentReplyChunk{
		MessageID:   strings.TrimSpace(id),
		CardID:      cardID,
		Title:       spec.Title,
		Body:        spec.Body,
		FooterLines: append([]string(nil), spec.FooterLines...),
		ShowHeader:  spec.ShowHeader,
	}, true
}

func (d replyChunkDelivery) replyTextChunked(ctx context.Context, messageID, text string, inThread bool) (string, error) {
	chunks := outbound.SplitWithCodeFences(text, appdelivery.ReplyTextMaxBytes)
	firstID := ""
	for i, chunk := range chunks {
		id, err := d.outbound.ReplyTextWithID(ctx, messageID, chunk, inThread)
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
