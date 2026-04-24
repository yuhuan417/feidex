package app

import (
	"context"
	"strings"
	"sync"
	"time"

	"feidex/internal/state"
)

const finalCardPatchTimeout = 15 * time.Second
const finalCardPatchIdleRetention = 30 * time.Second

type finalCardPatchTracker struct {
	mu      sync.Mutex
	patches map[string]*finalCardPatchState
}

func newFinalCardPatchTracker() *finalCardPatchTracker {
	return &finalCardPatchTracker{patches: map[string]*finalCardPatchState{}}
}

func (s finalCardPatchService) finalCardPatchTracker() *finalCardPatchTracker {
	if s.app == nil {
		return nil
	}
	if s.app.finalCardPatches == nil {
		s.app.finalCardPatches = newFinalCardPatchTracker()
	}
	return s.app.finalCardPatches
}

type finalCardPatchState struct {
	Submission     *state.Submission
	Title          string
	Color          string
	ShowHeader     bool
	Body           string
	FooterLines    []string
	Patching       bool
	Dirty          bool
	PreviewPending bool
	PruneScheduled bool
}

type finalCardPatchSnapshot struct {
	Submission  *state.Submission
	Title       string
	Color       string
	ShowHeader  bool
	Body        string
	FooterLines []string
}

func (s *finalCardPatchState) snapshot() finalCardPatchSnapshot {
	if s == nil {
		return finalCardPatchSnapshot{}
	}
	return finalCardPatchSnapshot{
		Submission:  s.Submission,
		Title:       strings.TrimSpace(s.Title),
		Color:       strings.TrimSpace(s.Color),
		ShowHeader:  s.ShowHeader,
		Body:        strings.TrimSpace(s.Body),
		FooterLines: append([]string(nil), s.FooterLines...),
	}
}

func (s finalCardPatchService) registerFinalCardPatchState(messageID string, sub *state.Submission, title, color string, showHeader bool, body string, footerLines []string) {
	if s.app == nil || sub == nil {
		return
	}
	messageID = strings.TrimSpace(messageID)
	body = strings.TrimSpace(body)
	if messageID == "" || body == "" {
		return
	}

	snapshotSub := appState(s.app).submission(sub.ID)
	if snapshotSub == nil {
		snapshotSub = sub
	}

	tracker := s.finalCardPatchTracker()
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	tracker.patches[messageID] = &finalCardPatchState{
		Submission:  snapshotSub,
		Title:       strings.TrimSpace(title),
		Color:       strings.TrimSpace(color),
		ShowHeader:  showHeader,
		Body:        body,
		FooterLines: append([]string(nil), footerLines...),
	}
}

func (s finalCardPatchService) markFinalCardPreviewPending(messageID string) bool {
	return s.setFinalCardPatchPending(messageID, "preview", true)
}

func (s finalCardPatchService) markFinalCardPreviewDone(messageID string) {
	_ = s.setFinalCardPatchPending(messageID, "preview", false)
}

func (s finalCardPatchService) setFinalCardPatchPending(messageID, kind string, pending bool) bool {
	if s.app == nil {
		return false
	}
	messageID = strings.TrimSpace(messageID)
	if messageID == "" {
		return false
	}
	tracker := s.finalCardPatchTracker()
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	current := tracker.patches[messageID]
	if current == nil {
		return false
	}
	switch strings.TrimSpace(kind) {
	case "preview":
		current.PreviewPending = pending
	default:
		return false
	}
	s.pruneFinalCardPatchLocked(tracker, messageID, current)
	return true
}

func (s finalCardPatchService) updateFinalCardPatchBody(messageID, body string) bool {
	body = strings.TrimSpace(body)
	if body == "" {
		return false
	}
	return s.updateFinalCardPatchState(messageID, func(current *finalCardPatchState) bool {
		if current == nil || strings.TrimSpace(current.Body) == body {
			return false
		}
		current.Body = body
		return true
	})
}

func (s finalCardPatchService) updateFinalCardPatchFooterLines(messageID string, footerLines []string) bool {
	normalized := normalizeFooterLines(footerLines)
	return s.updateFinalCardPatchState(messageID, func(current *finalCardPatchState) bool {
		if current == nil || stringSlicesEqualTrimmed(current.FooterLines, normalized) {
			return false
		}
		current.FooterLines = append([]string(nil), normalized...)
		return true
	})
}

func (s finalCardPatchService) updateFinalCardPatchState(messageID string, mutate func(*finalCardPatchState) bool) bool {
	if s.app == nil {
		return false
	}
	messageID = strings.TrimSpace(messageID)
	if messageID == "" {
		return false
	}

	shouldStart := false
	tracker := s.finalCardPatchTracker()
	tracker.mu.Lock()
	current := tracker.patches[messageID]
	if current == nil {
		tracker.mu.Unlock()
		return false
	}
	if mutate != nil && !mutate(current) {
		tracker.mu.Unlock()
		return true
	}
	current.Dirty = true
	if !current.Patching {
		current.Patching = true
		shouldStart = true
	}
	tracker.mu.Unlock()

	if shouldStart {
		go s.runFinalCardPatchLoop(messageID)
	}
	return true
}

func (s finalCardPatchService) runFinalCardPatchLoop(messageID string) {
	messageID = strings.TrimSpace(messageID)
	if s.app == nil || s.app.feishu == nil || messageID == "" {
		return
	}

	for {
		snapshot, ok := s.nextFinalCardPatchSnapshot(messageID)
		if !ok {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), finalCardPatchTimeout)
		err := s.patchFinalCardSnapshot(ctx, messageID, snapshot)
		cancel()
		_ = err
		if !s.finishFinalCardPatchIteration(messageID) {
			return
		}
	}
}

func (s finalCardPatchService) nextFinalCardPatchSnapshot(messageID string) (finalCardPatchSnapshot, bool) {
	tracker := s.finalCardPatchTracker()
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	current := tracker.patches[messageID]
	if current == nil {
		return finalCardPatchSnapshot{}, false
	}
	current.Dirty = false
	return current.snapshot(), true
}

func (s finalCardPatchService) finishFinalCardPatchIteration(messageID string) bool {
	tracker := s.finalCardPatchTracker()
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	current := tracker.patches[messageID]
	if current == nil {
		return false
	}
	if current.Dirty {
		return true
	}
	current.Patching = false
	s.pruneFinalCardPatchLocked(tracker, messageID, current)
	return false
}

func (s finalCardPatchService) pruneFinalCardPatchLocked(tracker *finalCardPatchTracker, messageID string, current *finalCardPatchState) {
	if tracker == nil {
		return
	}
	if current == nil {
		delete(tracker.patches, messageID)
		return
	}
	if current.Patching || current.Dirty || current.PreviewPending {
		return
	}
	if current.PruneScheduled {
		return
	}
	current.PruneScheduled = true
	time.AfterFunc(finalCardPatchIdleRetention, func() {
		s.tryPruneFinalCardPatch(messageID)
	})
}

func (s finalCardPatchService) tryPruneFinalCardPatch(messageID string) {
	if s.app == nil {
		return
	}
	messageID = strings.TrimSpace(messageID)
	if messageID == "" {
		return
	}
	tracker := s.finalCardPatchTracker()
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	current := tracker.patches[messageID]
	if current == nil {
		return
	}
	current.PruneScheduled = false
	if current.Patching || current.Dirty || current.PreviewPending {
		return
	}
	delete(s.finalCardPatchTracker().patches, messageID)
}

func (s finalCardPatchService) patchFinalCardSnapshot(ctx context.Context, messageID string, snapshot finalCardPatchSnapshot) error {
	if s.app == nil || s.app.feishu == nil || snapshot.Submission == nil || strings.TrimSpace(messageID) == "" {
		return nil
	}
	card := cardRendererForApp(s.app).renderReplyMarkdownCardWithHeaderOptions(ctx, snapshot.Submission, snapshot.Title, snapshot.Color, snapshot.ShowHeader, snapshot.Body, nil, true)
	appendReplyCardFooter(card, snapshot.FooterLines)
	return s.app.feishu.PatchCard(ctx, messageID, card)
}

func normalizeFooterLines(lines []string) []string {
	if len(lines) == 0 {
		return nil
	}
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

func stringSlicesEqualTrimmed(left, right []string) bool {
	left = normalizeFooterLines(left)
	right = normalizeFooterLines(right)
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
