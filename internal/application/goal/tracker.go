package goal

import (
	"feidex/internal/domain/conversation"
	"feidex/internal/textutil"
	"strings"
	"sync"
)

type Tracker struct {
	mu                 sync.Mutex
	goals              map[string]conversation.ThreadGoal
	anchors            map[string]Anchor
	continuationCounts map[string]int
}

type Anchor struct {
	SessionKey string
	ThreadID   string
	MessageID  string
	ChatID     string
	ChatType   string
	UserID     string
}

func NewTracker() *Tracker {
	return &Tracker{
		goals:              map[string]conversation.ThreadGoal{},
		anchors:            map[string]Anchor{},
		continuationCounts: map[string]int{},
	}
}

func (t *Tracker) NoteGoal(goal conversation.ThreadGoal) {
	if t == nil {
		return
	}
	threadID := strings.TrimSpace(goal.ThreadID)
	if threadID == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.goals == nil {
		t.goals = map[string]conversation.ThreadGoal{}
	}
	t.goals[threadID] = goal
}

func (t *Tracker) ClearGoal(threadID string) {
	if t == nil {
		return
	}
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.goals, threadID)
	delete(t.continuationCounts, threadID)
}

func (t *Tracker) ActiveGoal(threadID string) (conversation.ThreadGoal, bool) {
	if t == nil {
		return conversation.ThreadGoal{}, false
	}
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return conversation.ThreadGoal{}, false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	goal, ok := t.goals[threadID]
	return goal, ok && goal.Status == conversation.ThreadGoalStatusActive
}

func (t *Tracker) RecordAnchor(anchor Anchor) {
	if t == nil {
		return
	}
	anchor.ThreadID = strings.TrimSpace(anchor.ThreadID)
	anchor.SessionKey = strings.TrimSpace(anchor.SessionKey)
	anchor.MessageID = strings.TrimSpace(anchor.MessageID)
	if anchor.ThreadID == "" || anchor.SessionKey == "" || anchor.MessageID == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.anchors == nil {
		t.anchors = map[string]Anchor{}
	}
	t.anchors[anchor.ThreadID] = anchor
}

func (t *Tracker) RecordContext(anchor Anchor) {
	if t == nil {
		return
	}
	anchor.ThreadID = strings.TrimSpace(anchor.ThreadID)
	anchor.SessionKey = strings.TrimSpace(anchor.SessionKey)
	if anchor.ThreadID == "" || anchor.SessionKey == "" {
		return
	}
	anchor.MessageID = ""
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.anchors == nil {
		t.anchors = map[string]Anchor{}
	}
	if existing, ok := t.anchors[anchor.ThreadID]; ok {
		anchor.ChatID = textutil.FirstNonEmpty(strings.TrimSpace(anchor.ChatID), strings.TrimSpace(existing.ChatID))
		anchor.ChatType = textutil.FirstNonEmpty(strings.TrimSpace(anchor.ChatType), strings.TrimSpace(existing.ChatType))
		anchor.UserID = textutil.FirstNonEmpty(strings.TrimSpace(anchor.UserID), strings.TrimSpace(existing.UserID))
	}
	t.anchors[anchor.ThreadID] = anchor
}

func (t *Tracker) Anchor(threadID string) (Anchor, bool) {
	if t == nil {
		return Anchor{}, false
	}
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return Anchor{}, false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	anchor, ok := t.anchors[threadID]
	return anchor, ok
}

func (t *Tracker) NextContinuationOrdinal(threadID string) int {
	if t == nil {
		return 0
	}
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.continuationCounts == nil {
		t.continuationCounts = map[string]int{}
	}
	t.continuationCounts[threadID]++
	return t.continuationCounts[threadID]
}
