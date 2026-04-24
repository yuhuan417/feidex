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

func (s turnStreamService) turnStreamTracker() *turnStreamTracker {
	if s.app == nil {
		return nil
	}
	if s.app.turnStreams == nil {
		s.app.turnStreams = newTurnStreamTracker()
	}
	return s.app.turnStreams
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

func (s turnStreamService) noteTurnStarted(sessionKey string, sub *state.Submission) {
	if sub == nil || strings.TrimSpace(sub.TurnID) == "" {
		return
	}
	s.maybeSendSubmissionStartedNotice(context.Background(), sub)
	tracker := s.turnStreamTracker()
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	s.ensureTurnStreamLocked(tracker, sessionKey, sub)
}

func (s turnStreamService) maybeSendSubmissionStartedNotice(ctx context.Context, sub *state.Submission) {
	if s.app == nil || sub == nil || strings.TrimSpace(sub.ID) == "" {
		return
	}
	appState := appState(s.app)
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
	sendSubmissionStartedNotice(s.app, ctx, sub)
}

func (s turnStreamService) updatePendingPlan(turnID, plan string) {
	sessionKey, sub := findSubmissionByTurn(s.app, "", turnID)
	if sub == nil {
		return
	}
	tracker := s.turnStreamTracker()
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	stream := s.ensureTurnStreamLocked(tracker, sessionKey, sub)
	stream.PendingPlan = strings.TrimSpace(plan)
}

func (s turnStreamService) recordTurnError(threadID, turnID, message string) {
	sessionKey, sub := findSubmissionByTurn(s.app, threadID, turnID)
	if sub == nil {
		return
	}
	tracker := s.turnStreamTracker()
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	stream := s.ensureTurnStreamLocked(tracker, sessionKey, sub)
	stream.LastError = strings.TrimSpace(message)
}

func (s turnStreamService) completeTurnItem(ctx context.Context, threadID, turnID, itemID string, item map[string]any) {
	if s.app == nil {
		return
	}
	newLifecycleCoordinator(s.app).bindPendingSubmissionTurn(threadID, turnID, true)
	item = newRuntimeStateService(s.app).completeTurnItemState(threadID, turnID, itemID, item)
	itemID = strings.TrimSpace(firstNonEmpty(strings.TrimSpace(itemID), stringValue(item["id"])))
	if completeStandaloneCompactItem(s.app, threadID, turnID, item) {
		return
	}
	sessionKey, sub := findSubmissionByTurn(s.app, threadID, turnID)
	if sub == nil {
		return
	}
	workspaceCwd := workspaceCwd(s.app.cfg, sub.WorkspaceID)
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

	tracker := s.turnStreamTracker()
	tracker.mu.Lock()
	stream := s.ensureTurnStreamLocked(tracker, sessionKey, sub)
	if strings.TrimSpace(threadID) != "" {
		stream.ThreadID = threadID
	}
	if text := strings.TrimSpace(stream.PendingPlan); text != "" && text != stream.LastSentPlan {
		planText = text
		stream.LastSentPlan = text
		stream.PendingPlan = ""
		if stream.QuietWorking != nil {
			planBoundary = prepareQuietWorkingCardBoundaryLocked(stream)
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
			itemBoundary = prepareQuietWorkingCardBoundaryLocked(stream)
			itemReuseMessage = itemBoundary.ReuseMessageID
		}
	} else if quietWorkingCardEnabled(feishuConfig(s.app)) {
		workingUpdate = prepareQuietWorkingCardUpdateLocked(stream, itemID, item, workspaceCwd)
	}
	tracker.mu.Unlock()

	executeQuietWorkingCardOp(s.app,ctx, sub, planBoundary.Op)
	if planText != "" {
		newOutboundCardService(s.app).sendPlanCardWithReuse(ctx, sub, planText, planReuseMessage)
	}
	executeQuietWorkingCardOp(s.app,ctx, sub, itemBoundary.Op)
	if quietWorkingCardEnabled(feishuConfig(s.app)) {
		executeQuietWorkingCardOp(s.app,ctx, sub, workingUpdate)
	}
	if hasPayload && !skipPayload && (!quietModeEnabled(feishuConfig(s.app)) || shouldDeliverTurnItemPayloadInQuiet(quietMode(feishuConfig(s.app)), payload)) {
		newOutboundCardService(s.app).sendTurnItemCardWithReuse(ctx, sub, payload, itemReuseMessage)
	}
}

func (s turnStreamService) flushTurnStream(ctx context.Context, threadID, turnID string) turnStreamFlushResult {
	sessionKey, sub := findSubmissionByTurn(s.app, threadID, turnID)
	if sub == nil {
		s.deleteTurnStream(turnID)
		newRuntimeStateService(s.app).clearTurnItemStates(turnID)
		return turnStreamFlushResult{}
	}

	var (
		planText         string
		planBoundary     quietWorkingBoundary
		planReuseMessage string
		result           turnStreamFlushResult
	)

	tracker := s.turnStreamTracker()
	tracker.mu.Lock()
	stream := s.ensureTurnStreamLocked(tracker, sessionKey, sub)
	result.SawFinal = stream.SentFinal
	result.LastError = stream.LastError
	pendingPlan := strings.TrimSpace(stream.PendingPlan)
	if stream.QuietWorking != nil && (pendingPlan == "" || pendingPlan == stream.LastSentPlan) {
		result.WorkingMessageID = strings.TrimSpace(stream.QuietWorking.MessageID)
	}
	if pendingPlan != "" && pendingPlan != stream.LastSentPlan {
		planText = pendingPlan
		if stream.QuietWorking != nil {
			planBoundary = prepareQuietWorkingCardBoundaryLocked(stream)
			planReuseMessage = planBoundary.ReuseMessageID
		}
	}
	delete(tracker.streams, turnID)
	tracker.mu.Unlock()

	executeQuietWorkingCardOp(s.app,ctx, sub, planBoundary.Op)
	if planText != "" {
		newOutboundCardService(s.app).sendPlanCardWithReuse(ctx, sub, planText, planReuseMessage)
	}
	return result
}

func (s turnStreamService) turnStreamSawFinal(turnID string) bool {
	if s.app == nil || strings.TrimSpace(turnID) == "" {
		return false
	}
	tracker := s.turnStreamTracker()
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	stream := tracker.streams[strings.TrimSpace(turnID)]
	return stream != nil && stream.SentFinal
}

func (s turnStreamService) ensureTurnStreamLocked(tracker *turnStreamTracker, sessionKey string, sub *state.Submission) *turnStream {
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

func (s turnStreamService) deleteTurnStream(turnID string) {
	tracker := s.turnStreamTracker()
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

func (s turnStreamService) markTurnStreamFinal(turnID string) {
	tracker := s.turnStreamTracker()
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

func (s turnStreamService) prepareTurnStreamQuietBoundary(turnID string) quietWorkingBoundary {
	tracker := s.turnStreamTracker()
	if tracker == nil {
		return quietWorkingBoundary{}
	}
	turnID = strings.TrimSpace(turnID)
	if turnID == "" {
		return quietWorkingBoundary{}
	}
	tracker.mu.Lock()
	boundary := prepareQuietWorkingCardBoundaryLocked(tracker.streams[turnID])
	tracker.mu.Unlock()
	return boundary
}

func (s turnStreamService) prepareTurnStreamQuietUpdate(sessionKey string, sub *state.Submission, threadID, itemID string, item map[string]any, workspaceCwd string) quietWorkingCardOp {
	tracker := s.turnStreamTracker()
	if tracker == nil || sub == nil {
		return quietWorkingCardOp{}
	}
	tracker.mu.Lock()
	stream := s.ensureTurnStreamLocked(tracker, sessionKey, sub)
	if strings.TrimSpace(threadID) != "" {
		stream.ThreadID = strings.TrimSpace(threadID)
	}
	op := prepareQuietWorkingCardUpdateLocked(stream, itemID, item, workspaceCwd)
	tracker.mu.Unlock()
	return op
}

func (s turnStreamService) commitTurnStreamQuietRender(turnID, messageID, body string) {
	tracker := s.turnStreamTracker()
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
