// Package finalcardpatch provides the final-card-patch service extracted from
// the app god package. It manages incremental card patches during streaming,
// tracking dirty state, patching cards asynchronously, and pruning idle states.
package finalcardpatch

import (
	"context"
	"strings"
	"sync"
	"time"

	"feidex/internal/state"
)

// ---------------------------------------------------------------------------
// App interface — what the service needs from the host application
// ---------------------------------------------------------------------------

// App defines the interface the final-card-patch service requires from the host
// application.
type App interface {
	// FinalCardPatchTracker returns the tracker, lazily initializing it.
	FinalCardPatchTracker() *Tracker

	// FinalCardPatchSubmissionFinder returns the narrowed submission finder.
	FinalCardPatchSubmissionFinder() SubmissionFinderProvider

	// FinalCardPatchCardRenderer returns the card renderer callback.
	FinalCardPatchCardRenderer() CardRendererFunc

	// FinalCardPatchFeishu returns the narrowed Feishu client.
	FinalCardPatchFeishu() FeishuPatcher
}

// ---------------------------------------------------------------------------
// Narrow provider interfaces
// ---------------------------------------------------------------------------

// SubmissionFinderProvider narrows app state access to the submission lookup
// used by the service.
type SubmissionFinderProvider interface {
	Submission(id string) *state.Submission
}

// FeishuPatcher narrows the Feishu client to the PatchCard method.
type FeishuPatcher interface {
	PatchCard(ctx context.Context, messageID string, card map[string]any) error
}

// CardRendererFunc renders a reply markdown card with header options and appends
// footer lines. It combines card rendering and footer appending so that
// internal card-building logic stays in the host app.
type CardRendererFunc func(ctx context.Context, sub *state.Submission, title, color string, showHeader bool, body string, footerLines []string) map[string]any

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	Timeout        = 15 * time.Second
	IdleRetention  = 30 * time.Second
)

// ---------------------------------------------------------------------------
// Tracker and state types
// ---------------------------------------------------------------------------

// Tracker tracks all active final-card-patch states.
type Tracker struct {
	Mu      sync.Mutex
	Patches map[string]*PatchState
}

// NewTracker creates a new empty Tracker.
func NewTracker() *Tracker {
	return &Tracker{Patches: map[string]*PatchState{}}
}

// PatchState tracks the patch state for a single card.
type PatchState struct {
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

// PatchSnapshot is an immutable snapshot of a PatchState for rendering.
type PatchSnapshot struct {
	Submission  *state.Submission
	Title       string
	Color       string
	ShowHeader  bool
	Body        string
	FooterLines []string
}

func (s *PatchState) snapshot() PatchSnapshot {
	if s == nil {
		return PatchSnapshot{}
	}
	return PatchSnapshot{
		Submission:  s.Submission,
		Title:       strings.TrimSpace(s.Title),
		Color:       strings.TrimSpace(s.Color),
		ShowHeader:  s.ShowHeader,
		Body:        strings.TrimSpace(s.Body),
		FooterLines: append([]string(nil), s.FooterLines...),
	}
}

// ---------------------------------------------------------------------------
// Service — manages final-card patches
// ---------------------------------------------------------------------------

// Service manages incremental card patches for a single app instance.
type Service struct {
	app App
}

// NewService creates a new final-card-patch service bound to the given app.
func NewService(app App) Service {
	return Service{app: app}
}

// Tracker returns the tracker via the App interface.
func (svc Service) tracker() *Tracker {
	if svc.app == nil {
		return nil
	}
	t := svc.app.FinalCardPatchTracker()
	if t == nil {
		t = NewTracker()
	}
	return t
}

// ---------------------------------------------------------------------------
// Public methods
// ---------------------------------------------------------------------------

// RegisterFinalCardPatchState registers a new card patch state for the given
// message ID.
func (svc Service) RegisterFinalCardPatchState(messageID string, sub *state.Submission, title, color string, showHeader bool, body string, footerLines []string) {
	if svc.app == nil || sub == nil {
		return
	}
	messageID = strings.TrimSpace(messageID)
	body = strings.TrimSpace(body)
	if messageID == "" || body == "" {
		return
	}

	finder := svc.app.FinalCardPatchSubmissionFinder()
	if finder != nil {
		if snapshotSub := finder.Submission(sub.ID); snapshotSub != nil {
			sub = snapshotSub
		}
	}

	t := svc.tracker()
	t.Mu.Lock()
	defer t.Mu.Unlock()
	t.Patches[messageID] = &PatchState{
		Submission:  sub,
		Title:       strings.TrimSpace(title),
		Color:       strings.TrimSpace(color),
		ShowHeader:  showHeader,
		Body:        body,
		FooterLines: append([]string(nil), footerLines...),
	}
}

// MarkFinalCardPreviewPending marks the preview as pending for the given
// message ID. Returns true if the state existed.
func (svc Service) MarkFinalCardPreviewPending(messageID string) bool {
	return svc.setPatchPending(messageID, "preview", true)
}

// MarkFinalCardPreviewDone marks the preview as done for the given message ID.
func (svc Service) MarkFinalCardPreviewDone(messageID string) {
	_ = svc.setPatchPending(messageID, "preview", false)
}

// UpdateFinalCardPatchBody updates the body for the given message ID. Returns
// true if the state existed and the body changed.
func (svc Service) UpdateFinalCardPatchBody(messageID, body string) bool {
	body = strings.TrimSpace(body)
	if body == "" {
		return false
	}
	return svc.updatePatchState(messageID, func(current *PatchState) bool {
		if current == nil || strings.TrimSpace(current.Body) == body {
			return false
		}
		current.Body = body
		return true
	})
}

// UpdateFinalCardPatchFooterLines updates the footer lines for the given
// message ID. Returns true if the state existed and the footer changed.
func (svc Service) UpdateFinalCardPatchFooterLines(messageID string, footerLines []string) bool {
	normalized := NormalizeFooterLines(footerLines)
	return svc.updatePatchState(messageID, func(current *PatchState) bool {
		if current == nil || StringSlicesEqualTrimmed(current.FooterLines, normalized) {
			return false
		}
		current.FooterLines = append([]string(nil), normalized...)
		return true
	})
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

func (svc Service) setPatchPending(messageID, kind string, pending bool) bool {
	if svc.app == nil {
		return false
	}
	messageID = strings.TrimSpace(messageID)
	if messageID == "" {
		return false
	}
	t := svc.tracker()
	t.Mu.Lock()
	defer t.Mu.Unlock()
	current := t.Patches[messageID]
	if current == nil {
		return false
	}
	switch strings.TrimSpace(kind) {
	case "preview":
		current.PreviewPending = pending
	default:
		return false
	}
	svc.pruneLocked(t, messageID, current)
	return true
}

func (svc Service) updatePatchState(messageID string, mutate func(*PatchState) bool) bool {
	if svc.app == nil {
		return false
	}
	messageID = strings.TrimSpace(messageID)
	if messageID == "" {
		return false
	}

	shouldStart := false
	t := svc.tracker()
	t.Mu.Lock()
	current := t.Patches[messageID]
	if current == nil {
		t.Mu.Unlock()
		return false
	}
	if mutate != nil && !mutate(current) {
		t.Mu.Unlock()
		return true
	}
	current.Dirty = true
	if !current.Patching {
		current.Patching = true
		shouldStart = true
	}
	t.Mu.Unlock()

	if shouldStart {
		go svc.runPatchLoop(messageID)
	}
	return true
}

func (svc Service) runPatchLoop(messageID string) {
	messageID = strings.TrimSpace(messageID)
	feishu := svc.app.FinalCardPatchFeishu()
	if svc.app == nil || feishu == nil || messageID == "" {
		return
	}

	for {
		snapshot, ok := svc.nextSnapshot(messageID)
		if !ok {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), Timeout)
		err := svc.patchSnapshot(ctx, messageID, snapshot)
		cancel()
		_ = err
		if !svc.finishIteration(messageID) {
			return
		}
	}
}

func (svc Service) nextSnapshot(messageID string) (PatchSnapshot, bool) {
	t := svc.tracker()
	t.Mu.Lock()
	defer t.Mu.Unlock()
	current := t.Patches[messageID]
	if current == nil {
		return PatchSnapshot{}, false
	}
	current.Dirty = false
	return current.snapshot(), true
}

func (svc Service) finishIteration(messageID string) bool {
	t := svc.tracker()
	t.Mu.Lock()
	defer t.Mu.Unlock()
	current := t.Patches[messageID]
	if current == nil {
		return false
	}
	if current.Dirty {
		return true
	}
	current.Patching = false
	svc.pruneLocked(t, messageID, current)
	return false
}

func (svc Service) pruneLocked(t *Tracker, messageID string, current *PatchState) {
	if t == nil {
		return
	}
	if current == nil {
		delete(t.Patches, messageID)
		return
	}
	if current.Patching || current.Dirty || current.PreviewPending {
		return
	}
	if current.PruneScheduled {
		return
	}
	current.PruneScheduled = true
	time.AfterFunc(IdleRetention, func() {
		svc.tryPrune(messageID)
	})
}

func (svc Service) tryPrune(messageID string) {
	if svc.app == nil {
		return
	}
	messageID = strings.TrimSpace(messageID)
	if messageID == "" {
		return
	}
	t := svc.tracker()
	t.Mu.Lock()
	defer t.Mu.Unlock()
	current := t.Patches[messageID]
	if current == nil {
		return
	}
	current.PruneScheduled = false
	if current.Patching || current.Dirty || current.PreviewPending {
		return
	}
	delete(t.Patches, messageID)
}

func (svc Service) patchSnapshot(ctx context.Context, messageID string, snapshot PatchSnapshot) error {
	feishu := svc.app.FinalCardPatchFeishu()
	renderer := svc.app.FinalCardPatchCardRenderer()
	if svc.app == nil || feishu == nil || snapshot.Submission == nil || strings.TrimSpace(messageID) == "" || renderer == nil {
		return nil
	}
	card := renderer(ctx, snapshot.Submission, snapshot.Title, snapshot.Color, snapshot.ShowHeader, snapshot.Body, snapshot.FooterLines)
	return feishu.PatchCard(ctx, messageID, card)
}

// ---------------------------------------------------------------------------
// Exported helpers
// ---------------------------------------------------------------------------

// NormalizeFooterLines trims and filters empty lines from the input slice.
func NormalizeFooterLines(lines []string) []string {
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

// StringSlicesEqualTrimmed reports whether two string slices are equal after
// trimming and filtering empty entries.
func StringSlicesEqualTrimmed(left, right []string) bool {
	left = NormalizeFooterLines(left)
	right = NormalizeFooterLines(right)
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
