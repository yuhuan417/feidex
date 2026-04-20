package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"feidex/internal/state"
)

type claudeStreamingSegment struct {
	ItemID string
	Chunks []sentReplyChunk
}

func (a *App) updateClaudeOutputSegment(ctx context.Context, threadID, turnID, body string) bool {
	return a.syncClaudeOutputSegment(ctx, threadID, turnID, body, false)
}

func (a *App) finalizeClaudeOutputSegment(ctx context.Context, threadID, turnID, body string) bool {
	return a.syncClaudeOutputSegment(ctx, threadID, turnID, body, true)
}

func (a *App) closeClaudeOutputSegment(threadID, turnID string) {
	if a == nil {
		return
	}
	sessionKey, sub := a.findSubmissionByTurn(threadID, turnID)
	if sub == nil {
		return
	}
	a.turnStreamsMu.Lock()
	defer a.turnStreamsMu.Unlock()
	stream := a.ensureTurnStreamLocked(sessionKey, sub)
	stream.ClaudeSegment = nil
}

func (a *App) syncClaudeOutputSegment(ctx context.Context, threadID, turnID, body string, final bool) bool {
	if a == nil {
		return false
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return false
	}
	sessionKey, sub := a.findSubmissionByTurn(threadID, turnID)
	if sub == nil {
		return false
	}
	kind := "turn_output"
	if final {
		kind = "final_message"
	}
	if a.quietModeEnabled() && !shouldDeliverTurnKindInQuiet(a.quietMode(), kind) {
		return true
	}

	title := ""
	color := "green"
	showHeader := false
	enablePreview := false
	footerLines := []string(nil)
	if final {
		title = "最终答复"
		showHeader = true
		enablePreview = true
		footerLines = a.turnFinalFooterLines(turnID, time.Now())
	}

	specs := a.prepareReplyChunkRenderSpecs(ctx, sub, title, color, buildReplyCardChunks(body, showHeader, footerLines), enablePreview)
	if len(specs) == 0 {
		return false
	}

	a.turnStreamsMu.Lock()
	stream := a.ensureTurnStreamLocked(sessionKey, sub)
	if strings.TrimSpace(threadID) != "" {
		stream.ThreadID = threadID
	}
	if stream.ClaudeSegment == nil {
		stream.ClaudeSegmentCount++
		stream.ClaudeSegment = &claudeStreamingSegment{
			ItemID: fmt.Sprintf("claude-segment-%s-%d", strings.TrimSpace(turnID), stream.ClaudeSegmentCount),
		}
	}
	itemID := stream.ClaudeSegment.ItemID
	existing := append([]sentReplyChunk(nil), stream.ClaudeSegment.Chunks...)
	a.turnStreamsMu.Unlock()

	results := a.syncReplyChunkSpecs(ctx, sub, existing, specs, a.replyInThreadForSubmission(sub))
	if len(results) == 0 {
		return false
	}
	for _, result := range results {
		a.recordMessageLink(result.MessageID, kind, sub, itemID)
		if final && result.CardID != "" {
			a.scheduleLocalFileLinkPatch(sub, result.CardID, result.Title, color, result.ShowHeader, result.Body, result.FooterLines)
		}
	}

	a.turnStreamsMu.Lock()
	defer a.turnStreamsMu.Unlock()
	stream = a.turnStreams[turnID]
	if stream == nil {
		return true
	}
	if final {
		stream.SentFinal = true
	}
	if stream.ClaudeSegment == nil || stream.ClaudeSegment.ItemID != itemID {
		return true
	}
	if final {
		stream.ClaudeSegment = nil
		return true
	}
	stream.ClaudeSegment.Chunks = results
	return true
}

func (a *App) syncReplyChunkSpecs(ctx context.Context, sub *state.Submission, existing []sentReplyChunk, specs []replyChunkRenderSpec, inThread bool) []sentReplyChunk {
	if a == nil || len(specs) == 0 {
		return nil
	}
	results := make([]sentReplyChunk, 0, len(specs))
	for i, spec := range specs {
		if i < len(existing) && sentReplyChunkMatchesSpec(existing[i], spec) {
			results = append(results, existing[i])
			continue
		}
		reuseMessageID := ""
		if i < len(existing) {
			reuseMessageID = existing[i].MessageID
		}
		result, ok := a.sendReplyChunk(ctx, sub, spec, inThread, reuseMessageID)
		if !ok {
			break
		}
		results = append(results, result)
	}
	return results
}
