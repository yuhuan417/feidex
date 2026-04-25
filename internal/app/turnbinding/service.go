// Package turnbinding manages the mapping between turn IDs, thread IDs, and
// submissions. It tracks pending bindings, records token usage, and produces
// turn-final metadata lines for display.
//
// Extracted from internal/app/turn_binding.go so that the app god package
// delegates to a focused sub-package.
package turnbinding

import (
	"strings"
	"sync"
	"time"

	appusageview "feidex/internal/app/usageview"
	"feidex/internal/codexrpc"
	"feidex/internal/state"
)

// Binding records the association between a turn, thread, and submission.
type Binding struct {
	SessionKey             string
	SubmissionID           string
	ThreadID               string
	StartedAt              time.Time
	LastUsage              codexrpc.TokenUsageBreakdown
	HasLastUsage           bool
	ContextUsagePercent    float64
	HasContextUsagePercent bool
}

// ClaudeThreadUsageSnapshot captures cumulative Claude token usage for a thread.
type ClaudeThreadUsageSnapshot struct {
	TotalInputTokens         int64
	TotalOutputTokens        int64
	TotalCacheReadTokens     int64
	TotalCacheCreationTokens int64
	TotalCostUSD             float64
	ContextWindow            int64
	ContextUsagePercent      float64
	HasContextUsagePercent   bool
}

// Tracker holds the mutable state for turn-to-submission bindings.
type Tracker struct {
	mu          sync.Mutex
	Bindings    map[string]Binding
	Pending     map[string][]Binding
	ThreadUsage map[string]codexrpc.ThreadTokenUsage
	ClaudeUsage map[string]ClaudeThreadUsageSnapshot

	// store is used to look up submissions by ID. It is set at construction
	// time and must not be nil.
	store *state.Store
}

// NewTracker creates a new Tracker. The store is used for submission lookups.
func NewTracker(store *state.Store) *Tracker {
	return &Tracker{
		Bindings:    map[string]Binding{},
		Pending:     map[string][]Binding{},
		ThreadUsage: map[string]codexrpc.ThreadTokenUsage{},
		ClaudeUsage: map[string]ClaudeThreadUsageSnapshot{},
		store:       store,
	}
}

// Store returns the underlying state store.
func (t *Tracker) Store() *state.Store {
	return t.store
}

// submission looks up a submission by ID from the store.
func (t *Tracker) submission(id string) *state.Submission {
	if t == nil || t.store == nil {
		return nil
	}
	return t.store.GetSubmission(strings.TrimSpace(id))
}

// NotePendingTurnBinding records that a submission is pending for a thread.
func (t *Tracker) NotePendingTurnBinding(threadID, sessionKey, submissionID string) {
	if t == nil {
		return
	}
	threadID = strings.TrimSpace(threadID)
	submissionID = strings.TrimSpace(submissionID)
	if threadID == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.Pending == nil {
		t.Pending = map[string][]Binding{}
	}
	for _, binding := range t.Pending[threadID] {
		if strings.TrimSpace(binding.SubmissionID) == submissionID {
			return
		}
	}
	t.Pending[threadID] = append(t.Pending[threadID], Binding{
		ThreadID:     threadID,
		SessionKey:   strings.TrimSpace(sessionKey),
		SubmissionID: submissionID,
	})
}

// PendingSubmissionForThread returns the session key and submission for the
// first non-finalized pending binding for the given thread.
func (t *Tracker) PendingSubmissionForThread(threadID string) (string, *state.Submission) {
	if t == nil {
		return "", nil
	}
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return "", nil
	}
	t.mu.Lock()
	bindings := append([]Binding(nil), t.Pending[threadID]...)
	next := make([]Binding, 0, len(bindings))
	var matched Binding
	found := false
	for _, binding := range bindings {
		sub := t.submission(binding.SubmissionID)
		if sub == nil || sub.Finalized {
			continue
		}
		next = append(next, binding)
		if !found {
			matched = binding
			found = true
		}
	}
	if len(next) == 0 {
		delete(t.Pending, threadID)
	} else {
		t.Pending[threadID] = next
	}
	t.mu.Unlock()
	if !found {
		return "", nil
	}
	sub := t.submission(matched.SubmissionID)
	if sub == nil {
		return "", nil
	}
	return matched.SessionKey, sub
}

// ClearPendingTurnBindingForSubmission removes the pending binding for the
// given thread and submission.
func (t *Tracker) ClearPendingTurnBindingForSubmission(threadID, submissionID string) {
	if t == nil {
		return
	}
	threadID = strings.TrimSpace(threadID)
	submissionID = strings.TrimSpace(submissionID)
	if threadID == "" || submissionID == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	bindings := t.Pending[threadID]
	if len(bindings) == 0 {
		return
	}
	next := make([]Binding, 0, len(bindings))
	for _, binding := range bindings {
		if strings.TrimSpace(binding.SubmissionID) == submissionID {
			continue
		}
		next = append(next, binding)
	}
	if len(next) == 0 {
		delete(t.Pending, threadID)
		return
	}
	t.Pending[threadID] = next
}

// BindTurnSubmission creates a binding between a turn and a submission.
func (t *Tracker) BindTurnSubmission(threadID, turnID, sessionKey, submissionID string) {
	if t == nil {
		return
	}
	turnID = strings.TrimSpace(turnID)
	if turnID == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.Bindings == nil {
		t.Bindings = map[string]Binding{}
	}
	t.Bindings[turnID] = Binding{
		ThreadID:     strings.TrimSpace(threadID),
		SessionKey:   strings.TrimSpace(sessionKey),
		SubmissionID: strings.TrimSpace(submissionID),
	}
}

// RebindTurnThreadID updates the thread ID for an existing turn binding.
func (t *Tracker) RebindTurnThreadID(turnID, threadID string) {
	if t == nil {
		return
	}
	turnID = strings.TrimSpace(turnID)
	threadID = strings.TrimSpace(threadID)
	if turnID == "" || threadID == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	binding, ok := t.Bindings[turnID]
	if !ok {
		return
	}
	binding.ThreadID = threadID
	t.Bindings[turnID] = binding
}

// BoundSubmissionForTurn returns the session key and submission for a turn.
func (t *Tracker) BoundSubmissionForTurn(turnID string) (string, *state.Submission) {
	if t == nil {
		return "", nil
	}
	turnID = strings.TrimSpace(turnID)
	if turnID == "" {
		return "", nil
	}
	t.mu.Lock()
	binding, ok := t.Bindings[turnID]
	t.mu.Unlock()
	if !ok {
		return "", nil
	}
	sub := t.submission(binding.SubmissionID)
	if sub == nil {
		return "", nil
	}
	return binding.SessionKey, sub
}

// ClearTurnBinding removes the binding for a turn.
func (t *Tracker) ClearTurnBinding(turnID string) {
	if t == nil {
		return
	}
	turnID = strings.TrimSpace(turnID)
	if turnID == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.Bindings, turnID)
}

// MarkTurnStartedAt records the start time for a turn if not already set.
func (t *Tracker) MarkTurnStartedAt(turnID string, startedAt time.Time) {
	if t == nil {
		return
	}
	turnID = strings.TrimSpace(turnID)
	if turnID == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	binding, ok := t.Bindings[turnID]
	if !ok {
		return
	}
	if binding.StartedAt.IsZero() {
		binding.StartedAt = startedAt
		t.Bindings[turnID] = binding
	}
}

// RecordTurnTokenUsage records token usage for a thread and optionally
// associates it with a turn binding.
func (t *Tracker) RecordTurnTokenUsage(threadID, turnID string, usage codexrpc.ThreadTokenUsage) {
	if t == nil {
		return
	}
	threadID = strings.TrimSpace(threadID)
	turnID = strings.TrimSpace(turnID)
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.ThreadUsage == nil {
		t.ThreadUsage = map[string]codexrpc.ThreadTokenUsage{}
	}
	if threadID != "" {
		t.ThreadUsage[threadID] = usage
	}
	if turnID == "" {
		return
	}
	binding, ok := t.Bindings[turnID]
	if !ok {
		return
	}
	binding.LastUsage = usage.Last
	binding.HasLastUsage = true
	t.Bindings[turnID] = binding
}

// RecordTurnContextUsagePercent records the context usage percentage for a turn.
func (t *Tracker) RecordTurnContextUsagePercent(turnID string, percentage float64) {
	if t == nil {
		return
	}
	turnID = strings.TrimSpace(turnID)
	if turnID == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	binding, ok := t.Bindings[turnID]
	if !ok {
		return
	}
	binding.ContextUsagePercent = percentage
	binding.HasContextUsagePercent = true
	t.Bindings[turnID] = binding
}

// ThreadUsageFunc is a callback that returns the thread-level token usage for
// a given thread ID. It is used by TurnFinalMetadata to look up context window
// information when the binding does not carry its own context usage percentage.
type ThreadUsageFunc func(threadID string) (codexrpc.ThreadTokenUsage, bool)

// TurnFinalMetadata computes the usage, context, and elapsed time lines for a
// completed turn. The threadUsageFn callback is called with the binding's
// thread ID to look up thread-level token usage when needed.
func (t *Tracker) TurnFinalMetadata(turnID string, completedAt time.Time, threadUsageFn ThreadUsageFunc) (usageLine, contextLine, elapsedLine string) {
	if t == nil {
		return "", "", ""
	}
	turnID = strings.TrimSpace(turnID)
	if turnID == "" {
		return "", "", ""
	}
	t.mu.Lock()
	binding, ok := t.Bindings[turnID]
	t.mu.Unlock()
	if ok && binding.HasLastUsage {
		usageLine = appusageview.FormatTurnUsageLine(binding.LastUsage)
	}
	if ok {
		if binding.HasContextUsagePercent {
			contextLine = appusageview.FormatContextUsedLine(binding.ContextUsagePercent)
		} else if threadUsageFn != nil {
			if usage, found := threadUsageFn(binding.ThreadID); found && usage.ModelContextWindow != nil {
				contextLine = appusageview.FormatContextLeftLine(usage.Last.InputTokens, *usage.ModelContextWindow)
			}
		}
	}
	if ok && !binding.StartedAt.IsZero() && !completedAt.IsZero() {
		elapsedLine = appusageview.FormatTurnElapsedLine(completedAt.Sub(binding.StartedAt))
	}
	return usageLine, contextLine, elapsedLine
}

// TurnFinalFooterLines returns the non-empty context and elapsed lines for a
// completed turn, suitable for rendering in a card footer.
func (t *Tracker) TurnFinalFooterLines(turnID string, completedAt time.Time, threadUsageFn ThreadUsageFunc) []string {
	_, contextLine, elapsedLine := t.TurnFinalMetadata(turnID, completedAt, threadUsageFn)
	lines := make([]string, 0, 2)
	for _, line := range []string{contextLine, elapsedLine} {
		line = strings.TrimSpace(line)
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

// CurrentThreadUsage returns the recorded token usage for a thread.
func (t *Tracker) CurrentThreadUsage(threadID string) (codexrpc.ThreadTokenUsage, bool) {
	if t == nil {
		return codexrpc.ThreadTokenUsage{}, false
	}
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return codexrpc.ThreadTokenUsage{}, false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	usage, ok := t.ThreadUsage[threadID]
	return usage, ok
}

// SetClaudeThreadUsage records a Claude usage snapshot for a thread.
func (t *Tracker) SetClaudeThreadUsage(threadID string, snapshot ClaudeThreadUsageSnapshot) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.ClaudeUsage == nil {
		t.ClaudeUsage = map[string]ClaudeThreadUsageSnapshot{}
	}
	t.ClaudeUsage[threadID] = snapshot
}

// GetClaudeThreadUsage returns the Claude usage snapshot for a thread.
func (t *Tracker) GetClaudeThreadUsage(threadID string) (ClaudeThreadUsageSnapshot, bool) {
	if t == nil {
		return ClaudeThreadUsageSnapshot{}, false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	usage, ok := t.ClaudeUsage[threadID]
	return usage, ok
}
