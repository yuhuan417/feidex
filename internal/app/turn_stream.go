package app

import (
	"context"
	"strings"
	"sync"

	"feidex/internal/state"
)

type turnStreamTracker struct {
	mu      sync.Mutex
	streams map[string]*turnStream
}

func newTurnStreamTracker() *turnStreamTracker {
	return &turnStreamTracker{streams: map[string]*turnStream{}}
}

func (a *App) turnStreamTracker() *turnStreamTracker {
	if a == nil {
		return nil
	}
	if a.turnStreams == nil {
		a.turnStreams = newTurnStreamTracker()
	}
	return a.turnStreams
}

type turnStream struct {
	TurnID       string
	ThreadID     string
	SubmissionID string
	SessionKey   string
	WorkspaceID  string

	PendingPlan  string
	LastSentPlan string
	LastError    string
	SentFinal    bool
	ReviewFinal  bool
	QuietWorking *quietWorkingCard
}

type turnStreamFlushResult struct {
	SawFinal         bool
	LastError        string
	WorkingMessageID string
}

func (a *App) noteTurnStarted(sessionKey string, sub *state.Submission) {
	if sub == nil || strings.TrimSpace(sub.TurnID) == "" {
		return
	}
	a.maybeSendSubmissionStartedNotice(context.Background(), sub)
	tracker := a.turnStreamTracker()
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	a.ensureTurnStreamLocked(tracker, sessionKey, sub)
}

func (a *App) maybeSendSubmissionStartedNotice(ctx context.Context, sub *state.Submission) {
	if a == nil || sub == nil || strings.TrimSpace(sub.ID) == "" {
		return
	}
	appState := a.appState()
	shouldSend := false
	if err := appState.updateSubmission(sub.ID, func(current *state.Submission) {
		if current == nil || !current.WaitedInQueue || current.StartNoticeSent {
			return
		}
		current.StartNoticeSent = true
		shouldSend = true
	}); err != nil || !shouldSend {
		return
	}
	updated := appState.submission(sub.ID)
	if updated != nil {
		sub = updated
	}
	a.sendSubmissionStartedNotice(ctx, sub)
}

func (a *App) updatePendingPlan(turnID, plan string) {
	sessionKey, sub := a.findSubmissionByTurn("", turnID)
	if sub == nil {
		return
	}
	tracker := a.turnStreamTracker()
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	stream := a.ensureTurnStreamLocked(tracker, sessionKey, sub)
	stream.PendingPlan = strings.TrimSpace(plan)
}

func (a *App) recordTurnError(threadID, turnID, message string) {
	sessionKey, sub := a.findSubmissionByTurn(threadID, turnID)
	if sub == nil {
		return
	}
	tracker := a.turnStreamTracker()
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	stream := a.ensureTurnStreamLocked(tracker, sessionKey, sub)
	stream.LastError = strings.TrimSpace(message)
}

func (a *App) completeTurnItem(ctx context.Context, threadID, turnID, itemID string, item map[string]any) {
	if a == nil {
		return
	}
	newSubmissionWorkflow(a).bindPendingSubmissionTurn(threadID, turnID, true)
	item = a.completeTurnItemState(threadID, turnID, itemID, item)
	itemID = strings.TrimSpace(firstNonEmpty(strings.TrimSpace(itemID), stringValue(item["id"])))
	if a.completeStandaloneCompactItem(threadID, turnID, item) {
		return
	}
	sessionKey, sub := a.findSubmissionByTurn(threadID, turnID)
	if sub == nil {
		return
	}
	workspaceCwd := a.workspaceCwd(sub.WorkspaceID)
	payload, hasPayload := buildTurnItemCardPayloadWithWorkspace(itemID, item, workspaceCwd)

	var (
		planText         string
		planBoundary     quietWorkingBoundary
		itemBoundary     quietWorkingBoundary
		workingUpdate    quietWorkingCardOp
		planReuseMessage string
		itemReuseMessage string
		skipPayload      bool
	)

	tracker := a.turnStreamTracker()
	tracker.mu.Lock()
	stream := a.ensureTurnStreamLocked(tracker, sessionKey, sub)
	if strings.TrimSpace(threadID) != "" {
		stream.ThreadID = threadID
	}
	if text := strings.TrimSpace(stream.PendingPlan); text != "" && text != stream.LastSentPlan {
		planText = text
		stream.LastSentPlan = text
		stream.PendingPlan = ""
		if stream.QuietWorking != nil {
			planBoundary = a.prepareQuietWorkingCardBoundaryLocked(stream)
			planReuseMessage = planBoundary.ReuseMessageID
		}
	}
	if hasPayload {
		switch normalizeTurnItemType(firstNonEmpty(payload.ProtocolItemType, payload.ItemType)) {
		case "exited_review_mode":
			if payload.IsFinalAnswer {
				stream.SentFinal = true
				stream.ReviewFinal = true
			}
		case "agent_message":
			if stream.ReviewFinal {
				skipPayload = true
			} else if payload.IsFinalAnswer {
				stream.SentFinal = true
			}
		default:
			if payload.IsFinalAnswer {
				stream.SentFinal = true
			}
		}
	}
	if hasPayload && isQuietBoundaryTurnPayload(payload) {
		if stream.QuietWorking != nil {
			itemBoundary = a.prepareQuietWorkingCardBoundaryLocked(stream)
			itemReuseMessage = itemBoundary.ReuseMessageID
		}
	} else if a.quietWorkingCardEnabled() {
		workingUpdate = a.prepareQuietWorkingCardUpdateLocked(stream, itemID, item, workspaceCwd)
	}
	tracker.mu.Unlock()

	a.executeQuietWorkingCardOp(ctx, sub, planBoundary.Op)
	if planText != "" {
		a.sendPlanCardWithReuse(ctx, sub, planText, planReuseMessage)
	}
	a.executeQuietWorkingCardOp(ctx, sub, itemBoundary.Op)
	if a.quietWorkingCardEnabled() {
		a.executeQuietWorkingCardOp(ctx, sub, workingUpdate)
	}
	if hasPayload && !skipPayload && (!a.quietModeEnabled() || shouldDeliverTurnItemPayloadInQuiet(a.quietMode(), payload)) {
		a.sendTurnItemCardWithReuse(ctx, sub, payload, itemReuseMessage)
	}
}

func (a *App) flushTurnStream(ctx context.Context, threadID, turnID string) turnStreamFlushResult {
	sessionKey, sub := a.findSubmissionByTurn(threadID, turnID)
	if sub == nil {
		a.deleteTurnStream(turnID)
		a.clearTurnItemStates(turnID)
		return turnStreamFlushResult{}
	}

	var (
		planText         string
		planBoundary     quietWorkingBoundary
		planReuseMessage string
		result           turnStreamFlushResult
	)

	tracker := a.turnStreamTracker()
	tracker.mu.Lock()
	stream := a.ensureTurnStreamLocked(tracker, sessionKey, sub)
	result.SawFinal = stream.SentFinal
	result.LastError = stream.LastError
	pendingPlan := strings.TrimSpace(stream.PendingPlan)
	if stream.QuietWorking != nil && (pendingPlan == "" || pendingPlan == stream.LastSentPlan) {
		result.WorkingMessageID = strings.TrimSpace(stream.QuietWorking.MessageID)
	}
	if pendingPlan != "" && pendingPlan != stream.LastSentPlan {
		planText = pendingPlan
		if stream.QuietWorking != nil {
			planBoundary = a.prepareQuietWorkingCardBoundaryLocked(stream)
			planReuseMessage = planBoundary.ReuseMessageID
		}
	}
	delete(tracker.streams, turnID)
	tracker.mu.Unlock()

	a.executeQuietWorkingCardOp(ctx, sub, planBoundary.Op)
	if planText != "" {
		a.sendPlanCardWithReuse(ctx, sub, planText, planReuseMessage)
	}
	return result
}

func (a *App) turnStreamSawFinal(turnID string) bool {
	if a == nil || strings.TrimSpace(turnID) == "" {
		return false
	}
	tracker := a.turnStreamTracker()
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	stream := tracker.streams[strings.TrimSpace(turnID)]
	return stream != nil && stream.SentFinal
}

func (a *App) ensureTurnStreamLocked(tracker *turnStreamTracker, sessionKey string, sub *state.Submission) *turnStream {
	if tracker == nil {
		return nil
	}
	if tracker.streams == nil {
		tracker.streams = map[string]*turnStream{}
	}
	stream := tracker.streams[sub.TurnID]
	if stream != nil {
		stream.SessionKey = sessionKey
		stream.SubmissionID = sub.ID
		stream.WorkspaceID = sub.WorkspaceID
		if strings.TrimSpace(sub.ThreadID) != "" {
			stream.ThreadID = sub.ThreadID
		}
		return stream
	}
	stream = &turnStream{
		TurnID:       sub.TurnID,
		ThreadID:     sub.ThreadID,
		SubmissionID: sub.ID,
		SessionKey:   sessionKey,
		WorkspaceID:  sub.WorkspaceID,
	}
	tracker.streams[sub.TurnID] = stream
	return stream
}

func isQuietBoundaryTurnPayload(payload turnItemCardPayload) bool {
	return isQuietBoundaryTurnItem(payload.ItemType) || isClaudeTodoToolPayload(payload)
}

func (a *App) deleteTurnStream(turnID string) {
	tracker := a.turnStreamTracker()
	if tracker == nil {
		return
	}
	turnID = strings.TrimSpace(turnID)
	if turnID == "" {
		return
	}
	tracker.mu.Lock()
	delete(tracker.streams, turnID)
	tracker.mu.Unlock()
}

func (a *App) markTurnStreamFinal(turnID string) {
	tracker := a.turnStreamTracker()
	if tracker == nil {
		return
	}
	turnID = strings.TrimSpace(turnID)
	if turnID == "" {
		return
	}
	tracker.mu.Lock()
	if stream := tracker.streams[turnID]; stream != nil {
		stream.SentFinal = true
	}
	tracker.mu.Unlock()
}

func (a *App) prepareTurnStreamQuietBoundary(turnID string) quietWorkingBoundary {
	tracker := a.turnStreamTracker()
	if tracker == nil {
		return quietWorkingBoundary{}
	}
	turnID = strings.TrimSpace(turnID)
	if turnID == "" {
		return quietWorkingBoundary{}
	}
	tracker.mu.Lock()
	boundary := a.prepareQuietWorkingCardBoundaryLocked(tracker.streams[turnID])
	tracker.mu.Unlock()
	return boundary
}

func (a *App) prepareTurnStreamQuietUpdate(sessionKey string, sub *state.Submission, threadID, itemID string, item map[string]any, workspaceCwd string) quietWorkingCardOp {
	tracker := a.turnStreamTracker()
	if tracker == nil || sub == nil {
		return quietWorkingCardOp{}
	}
	tracker.mu.Lock()
	stream := a.ensureTurnStreamLocked(tracker, sessionKey, sub)
	if strings.TrimSpace(threadID) != "" {
		stream.ThreadID = strings.TrimSpace(threadID)
	}
	op := a.prepareQuietWorkingCardUpdateLocked(stream, itemID, item, workspaceCwd)
	tracker.mu.Unlock()
	return op
}

func (a *App) commitTurnStreamQuietRender(turnID, messageID, body string) {
	tracker := a.turnStreamTracker()
	if tracker == nil {
		return
	}
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	stream := tracker.streams[strings.TrimSpace(turnID)]
	if stream == nil || stream.QuietWorking == nil {
		return
	}
	if strings.TrimSpace(messageID) != "" {
		stream.QuietWorking.MessageID = messageID
	}
	stream.QuietWorking.RenderedBody = body
}
