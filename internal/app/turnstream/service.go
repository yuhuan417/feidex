// Package turnstream provides the turn-stream service extracted from the app
// god package. It manages per-turn stream state, quiet working cards, and
// turn-item card dispatch.
package turnstream

import (
	"context"
	"strings"
	"sync"

	appcore "feidex/internal/app/appcore"
	apputil "feidex/internal/app/apputil"
	"feidex/internal/app/quietmode"
	"feidex/internal/app/turn"
	"feidex/internal/app/turnitem"
	"feidex/internal/config"
	"feidex/internal/state"
)

// ---------------------------------------------------------------------------
// App interface — what the service needs from the host application
// ---------------------------------------------------------------------------

// App defines the interface the turn-stream service requires from the host
// application. It embeds appcore.AppConfig so that appcore helpers like
// FeishuConfig, ConfiguredBackend, etc. can be called directly.
type App interface {
	appcore.AppConfig

	// Feishu returns the Feishu bot client.
	Feishu() appcore.FeishuClient

	// TurnStreamState returns the narrowed state provider for the turn stream.
	TurnStreamState() StateProvider

	// TurnStreamSubmissionFinder returns the narrowed submission finder.
	TurnStreamSubmissionFinder() SubmissionFinderProvider

	// TurnStreamTurnLifecycle returns the narrowed turn lifecycle provider.
	TurnStreamTurnLifecycle() TurnLifecycleProvider

	// TurnStreamRuntimeState returns the narrowed runtime state provider.
	TurnStreamRuntimeState() RuntimeStateProvider

	// TurnStreamOutboundCards returns the narrowed outbound card provider.
	TurnStreamOutboundCards() OutboundCardProvider

	// TurnStreamQuietCardExecutor returns the quiet card executor.
	TurnStreamQuietCardExecutor() QuietCardExecutorProvider

	// SendSubmissionStartedNotice sends the "turn started" notice for a
	// submission, respecting the start-notice-sent guard.
	SendSubmissionStartedNotice(ctx context.Context, sub *state.Submission)

	// TurnStreamTracker returns the turn stream tracker, lazily initializing it.
	TurnStreamTracker() *Tracker
}

// ---------------------------------------------------------------------------
// Narrow provider interfaces
// ---------------------------------------------------------------------------

// StateProvider narrows app state access to the methods used by the service.
type StateProvider interface {
	Submission(id string) *state.Submission
	UpdateSubmission(id string, mutate func(*state.Submission)) error
}

// SubmissionFinderProvider narrows the submission-by-turn lookup to the
// methods used by the service.
type SubmissionFinderProvider interface {
	FindSubmissionByTurn(threadID, turnID string) (string, *state.Submission)
}

// TurnLifecycleProvider narrows turn lifecycle access to the methods used by
// the service.
type TurnLifecycleProvider interface {
	BindPendingSubmissionTurn(threadID, turnID string, allowReview bool) bool
}

// RuntimeStateProvider narrows runtime state access to the methods used by
// the service.
type RuntimeStateProvider interface {
	CompleteTurnItemState(threadID, turnID, itemID string, item map[string]any) map[string]any
	ClearTurnItemStates(turnID string)
}

// OutboundCardProvider narrows outbound card access to the methods used by
// the service.
type OutboundCardProvider interface {
	SendPlanCardWithReuse(ctx context.Context, sub *state.Submission, planText, reuseMessageID string) string
	SendTurnItemCardWithReuse(ctx context.Context, sub *state.Submission, payload turnitem.CardPayload, reuseMessageID string) string
	CompleteStandaloneCompactItem(threadID, turnID string, item map[string]any) bool
}

// QuietCardExecutorProvider provides the executeQuietWorkingCardOp callback
// used by the service to send/patch quiet working cards.
type QuietCardExecutorProvider interface {
	ExecuteQuietWorkingCardOp(ctx context.Context, sub *state.Submission, op turn.QuietWorkingCardOp)
}

// ---------------------------------------------------------------------------
// Local helpers (used by exported methods below)
// ---------------------------------------------------------------------------

var (
	firstNonEmpty               = apputil.FirstNonEmpty
	stringValue                 = turnitem.StringValue
	normalizeTurnItemType       = turnitem.NormalizeTurnItemType
	buildTurnItemCardPayload    = turnitem.BuildTurnItemCardPayload
	isQuietBoundaryTurnItem     = turn.IsQuietBoundaryTurnItem
	prepareUpdateLocked         = turn.PrepareUpdateLocked
	prepareBoundaryLocked       = turn.PrepareBoundaryLocked
	quietMode                   = quietmode.Mode
	quietModeEnabled            = quietmode.Enabled
	quietWorkingCardEnabled     = quietmode.WorkingCardEnabled
	shouldDeliverTurnItemPayload = quietmode.ShouldDeliverTurnItemPayload
	isClaudeTodoToolPayload     = quietmode.IsClaudeTodoToolPayload
)

// workspaceCwd returns the Cwd for the given workspace ID.
func workspaceCwd(cfg *config.Config, workspaceID string) string {
	if cfg == nil {
		return ""
	}
	if ws := config.FindWorkspace(cfg, workspaceID); ws != nil {
		return strings.TrimSpace(ws.Cwd)
	}
	return ""
}

// IsQuietBoundaryTurnPayload reports whether the payload marks a quiet-card boundary.
func IsQuietBoundaryTurnPayload(payload turnitem.CardPayload) bool {
	return isQuietBoundaryTurnItem(payload.ItemType) || isClaudeTodoToolPayload(payload.ProtocolItemType, payload.ToolName)
}

// ---------------------------------------------------------------------------
// Tracker and Stream — per-turn stream state
// ---------------------------------------------------------------------------

// Tracker tracks all active turn streams.
type Tracker struct {
	Mu      sync.Mutex
	Streams map[string]*Stream
}

// NewTracker creates a new empty Tracker.
func NewTracker() *Tracker {
	return &Tracker{Streams: map[string]*Stream{}}
}

// Stream tracks the state of a single turn stream.
type Stream struct {
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
	QuietWorking *turn.QuietWorkingCard
}

// FlushResult captures the result of flushing a turn stream.
type FlushResult struct {
	SawFinal         bool
	LastError        string
	WorkingMessageID string
}

// ---------------------------------------------------------------------------
// Service — manages turn streams
// ---------------------------------------------------------------------------

// Service manages turn streams for a single app instance.
type Service struct {
	app App
}

// NewService creates a new turn-stream service bound to the given app.
func NewService(app App) Service {
	return Service{app: app}
}

// Tracker returns the turn stream tracker via the App interface.
func (svc Service) Tracker() *Tracker {
	if svc.app == nil {
		return nil
	}
	return svc.app.TurnStreamTracker()
}

// NoteTurnStarted records that a turn has started for the given submission.
func (svc Service) NoteTurnStarted(ctx context.Context, sessionKey string, sub *state.Submission) {
	if sub == nil || strings.TrimSpace(sub.TurnID) == "" {
		return
	}
	svc.app.SendSubmissionStartedNotice(ctx, sub)
	tracker := svc.Tracker()
	tracker.Mu.Lock()
	defer tracker.Mu.Unlock()
	svc.ensureStreamLocked(tracker, sessionKey, sub)
}

// UpdatePendingPlan records a pending plan update for the given turn.
func (svc Service) UpdatePendingPlan(turnID, plan string) {
	sessionKey, sub := svc.app.TurnStreamSubmissionFinder().FindSubmissionByTurn("", turnID)
	if sub == nil {
		return
	}
	tracker := svc.Tracker()
	tracker.Mu.Lock()
	defer tracker.Mu.Unlock()
	stream := svc.ensureStreamLocked(tracker, sessionKey, sub)
	stream.PendingPlan = strings.TrimSpace(plan)
}

// RecordTurnError records an error for the given turn.
func (svc Service) RecordTurnError(threadID, turnID, message string) {
	sessionKey, sub := svc.app.TurnStreamSubmissionFinder().FindSubmissionByTurn(threadID, turnID)
	if sub == nil {
		return
	}
	tracker := svc.Tracker()
	tracker.Mu.Lock()
	defer tracker.Mu.Unlock()
	stream := svc.ensureStreamLocked(tracker, sessionKey, sub)
	stream.LastError = strings.TrimSpace(message)
}

// CompleteTurnItem processes a completed turn item, building and sending the
// appropriate card, managing quiet working cards, and updating stream state.
func (svc Service) CompleteTurnItem(ctx context.Context, threadID, turnID, itemID string, item map[string]any) {
	if svc.app == nil {
		return
	}
	svc.app.TurnStreamTurnLifecycle().BindPendingSubmissionTurn(threadID, turnID, true)
	item = svc.app.TurnStreamRuntimeState().CompleteTurnItemState(threadID, turnID, itemID, item)
	itemID = strings.TrimSpace(firstNonEmpty(strings.TrimSpace(itemID), stringValue(item["id"])))
	if svc.app.TurnStreamOutboundCards().CompleteStandaloneCompactItem(threadID, turnID, item) {
		return
	}
	sessionKey, sub := svc.app.TurnStreamSubmissionFinder().FindSubmissionByTurn(threadID, turnID)
	if sub == nil {
		return
	}
	workspaceCwd := workspaceCwd(svc.app.Config(), sub.WorkspaceID)
	payload, hasPayload := buildTurnItemCardPayload(itemID, item, workspaceCwd)

	var (
		planText         string
		planBoundary     turn.QuietWorkingBoundary
		itemBoundary     turn.QuietWorkingBoundary
		workingUpdate    turn.QuietWorkingCardOp
		planReuseMessage string
		itemReuseMessage string
		skipPayload      bool
	)

	tracker := svc.Tracker()
	tracker.Mu.Lock()
	stream := svc.ensureStreamLocked(tracker, sessionKey, sub)
	if strings.TrimSpace(threadID) != "" {
		stream.ThreadID = threadID
	}
	if text := strings.TrimSpace(stream.PendingPlan); text != "" && text != stream.LastSentPlan {
		planText = text
		stream.LastSentPlan = text
		stream.PendingPlan = ""
		if stream.QuietWorking != nil {
			planBoundary = prepareStreamBoundaryLocked(stream)
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
	if hasPayload && IsQuietBoundaryTurnPayload(payload) {
		if stream.QuietWorking != nil {
			itemBoundary = prepareStreamBoundaryLocked(stream)
			itemReuseMessage = itemBoundary.ReuseMessageID
		}
	} else if quietWorkingCardEnabled(svc.feishuConfig()) {
		workingUpdate = prepareStreamUpdateLocked(stream, itemID, item, workspaceCwd)
	}
	tracker.Mu.Unlock()

	svc.app.TurnStreamQuietCardExecutor().ExecuteQuietWorkingCardOp(ctx, sub, planBoundary.Op)
	if planText != "" {
		svc.app.TurnStreamOutboundCards().SendPlanCardWithReuse(ctx, sub, planText, planReuseMessage)
	}
	svc.app.TurnStreamQuietCardExecutor().ExecuteQuietWorkingCardOp(ctx, sub, itemBoundary.Op)
	if quietWorkingCardEnabled(svc.feishuConfig()) {
		svc.app.TurnStreamQuietCardExecutor().ExecuteQuietWorkingCardOp(ctx, sub, workingUpdate)
	}
	if hasPayload && !skipPayload && (!quietModeEnabled(svc.feishuConfig()) || shouldDeliverTurnItemPayload(quietMode(svc.feishuConfig()), payload.ItemType, payload.ProtocolItemType, payload.ToolName, payload.IsFinalAnswer)) {
		svc.app.TurnStreamOutboundCards().SendTurnItemCardWithReuse(ctx, sub, payload, itemReuseMessage)
	}
}

// FlushTurnStream flushes the turn stream for the given turn, sending any
// pending plan and cleaning up the stream.
func (svc Service) FlushTurnStream(ctx context.Context, threadID, turnID string) FlushResult {
	sessionKey, sub := svc.app.TurnStreamSubmissionFinder().FindSubmissionByTurn(threadID, turnID)
	if sub == nil {
		svc.DeleteStream(turnID)
		svc.app.TurnStreamRuntimeState().ClearTurnItemStates(turnID)
		return FlushResult{}
	}

	var (
		planText         string
		planBoundary     turn.QuietWorkingBoundary
		planReuseMessage string
		result           FlushResult
	)

	tracker := svc.Tracker()
	tracker.Mu.Lock()
	stream := svc.ensureStreamLocked(tracker, sessionKey, sub)
	result.SawFinal = stream.SentFinal
	result.LastError = stream.LastError
	pendingPlan := strings.TrimSpace(stream.PendingPlan)
	if stream.QuietWorking != nil && (pendingPlan == "" || pendingPlan == stream.LastSentPlan) {
		result.WorkingMessageID = strings.TrimSpace(stream.QuietWorking.MessageID)
	}
	if pendingPlan != "" && pendingPlan != stream.LastSentPlan {
		planText = pendingPlan
		if stream.QuietWorking != nil {
			planBoundary = prepareStreamBoundaryLocked(stream)
			planReuseMessage = planBoundary.ReuseMessageID
		}
	}
	delete(tracker.Streams, turnID)
	tracker.Mu.Unlock()

	svc.app.TurnStreamQuietCardExecutor().ExecuteQuietWorkingCardOp(ctx, sub, planBoundary.Op)
	if planText != "" {
		svc.app.TurnStreamOutboundCards().SendPlanCardWithReuse(ctx, sub, planText, planReuseMessage)
	}
	return result
}

// StreamSawFinal reports whether the turn stream saw a final item.
func (svc Service) StreamSawFinal(turnID string) bool {
	if svc.app == nil || strings.TrimSpace(turnID) == "" {
		return false
	}
	tracker := svc.Tracker()
	tracker.Mu.Lock()
	defer tracker.Mu.Unlock()
	stream := tracker.Streams[strings.TrimSpace(turnID)]
	return stream != nil && stream.SentFinal
}

// EnsureStreamLocked ensures a stream entry exists for the given submission,
// updating it if it already exists. The caller must hold the tracker mutex.
func (svc Service) EnsureStreamLocked(tracker *Tracker, sessionKey string, sub *state.Submission) *Stream {
	if tracker == nil {
		return nil
	}
	if tracker.Streams == nil {
		tracker.Streams = map[string]*Stream{}
	}
	stream := tracker.Streams[sub.TurnID]
	if stream != nil {
		stream.SessionKey = sessionKey
		stream.SubmissionID = sub.ID
		stream.WorkspaceID = sub.WorkspaceID
		if strings.TrimSpace(sub.ThreadID) != "" {
			stream.ThreadID = sub.ThreadID
		}
		return stream
	}
	stream = &Stream{
		TurnID:       sub.TurnID,
		ThreadID:     sub.ThreadID,
		SubmissionID: sub.ID,
		SessionKey:   sessionKey,
		WorkspaceID:  sub.WorkspaceID,
	}
	tracker.Streams[sub.TurnID] = stream
	return stream
}

// DeleteStream removes the stream entry for the given turn.
func (svc Service) DeleteStream(turnID string) {
	tracker := svc.Tracker()
	if tracker == nil {
		return
	}
	turnID = strings.TrimSpace(turnID)
	if turnID == "" {
		return
	}
	tracker.Mu.Lock()
	delete(tracker.Streams, turnID)
	tracker.Mu.Unlock()
}

// MarkStreamFinal marks the stream for the given turn as having sent a final item.
func (svc Service) MarkStreamFinal(turnID string) {
	tracker := svc.Tracker()
	if tracker == nil {
		return
	}
	turnID = strings.TrimSpace(turnID)
	if turnID == "" {
		return
	}
	tracker.Mu.Lock()
	if stream := tracker.Streams[turnID]; stream != nil {
		stream.SentFinal = true
	}
	tracker.Mu.Unlock()
}

// PrepareStreamQuietBoundary prepares the quiet working card boundary for the
// given turn stream.
func (svc Service) PrepareStreamQuietBoundary(turnID string) turn.QuietWorkingBoundary {
	tracker := svc.Tracker()
	if tracker == nil {
		return turn.QuietWorkingBoundary{}
	}
	turnID = strings.TrimSpace(turnID)
	if turnID == "" {
		return turn.QuietWorkingBoundary{}
	}
	tracker.Mu.Lock()
	stream := tracker.Streams[turnID]
	ss := toStreamState(stream)
	boundary := prepareBoundaryLocked(ss)
	if stream != nil {
		stream.QuietWorking = ss.QuietWorking
	}
	tracker.Mu.Unlock()
	return boundary
}

// PrepareStreamQuietUpdate prepares a quiet working card update for the given
// turn stream.
func (svc Service) PrepareStreamQuietUpdate(sessionKey string, sub *state.Submission, threadID, itemID string, item map[string]any, workspaceCwd string) turn.QuietWorkingCardOp {
	tracker := svc.Tracker()
	if tracker == nil || sub == nil {
		return turn.QuietWorkingCardOp{}
	}
	tracker.Mu.Lock()
	stream := svc.ensureStreamLocked(tracker, sessionKey, sub)
	if strings.TrimSpace(threadID) != "" {
		stream.ThreadID = strings.TrimSpace(threadID)
	}
	op := prepareStreamUpdateLocked(stream, itemID, item, workspaceCwd)
	tracker.Mu.Unlock()
	return op
}

// CommitStreamQuietRender records the rendered body and message ID for a quiet
// working card.
func (svc Service) CommitStreamQuietRender(turnID, messageID, body string) {
	tracker := svc.Tracker()
	if tracker == nil {
		return
	}
	tracker.Mu.Lock()
	defer tracker.Mu.Unlock()
	stream := tracker.Streams[strings.TrimSpace(turnID)]
	if stream == nil || stream.QuietWorking == nil {
		return
	}
	if strings.TrimSpace(messageID) != "" {
		stream.QuietWorking.MessageID = messageID
	}
	stream.QuietWorking.RenderedBody = body
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

// feishuConfig returns the active Feishu config for the app.
func (svc Service) feishuConfig() *config.FeishuConfig {
	return appcore.FeishuConfig(svc.app)
}

// ensureStreamLocked ensures a stream exists for the submission. Alias for
// EnsureStreamLocked for internal use.
func (svc Service) ensureStreamLocked(tracker *Tracker, sessionKey string, sub *state.Submission) *Stream {
	return svc.EnsureStreamLocked(tracker, sessionKey, sub)
}

// toStreamState converts a Stream to a turn.StreamState for use with turn
// package functions.
func toStreamState(s *Stream) *turn.StreamState {
	if s == nil {
		return nil
	}
	return &turn.StreamState{
		TurnID:       s.TurnID,
		QuietWorking: s.QuietWorking,
	}
}

// prepareStreamUpdateLocked wraps turn.PrepareUpdateLocked for use within the
// service. It converts between Stream and turn.StreamState.
func prepareStreamUpdateLocked(stream *Stream, itemID string, item map[string]any, workspaceCwd string) turn.QuietWorkingCardOp {
	if stream == nil {
		return prepareUpdateLocked(nil, itemID, item, workspaceCwd)
	}
	ss := toStreamState(stream)
	op := prepareUpdateLocked(ss, itemID, item, workspaceCwd)
	stream.QuietWorking = ss.QuietWorking
	return op
}

// prepareStreamBoundaryLocked wraps turn.PrepareBoundaryLocked for use within
// the service. It converts between Stream and turn.StreamState.
func prepareStreamBoundaryLocked(stream *Stream) turn.QuietWorkingBoundary {
	if stream == nil {
		return prepareBoundaryLocked(nil)
	}
	ss := toStreamState(stream)
	boundary := prepareBoundaryLocked(ss)
	stream.QuietWorking = ss.QuietWorking
	return boundary
}
