package app

import (
	"encoding/json"
	"strings"
	"sync"
)

type turnItemTracker struct {
	mu    sync.Mutex
	items map[string]*turnItemState
}

func newTurnItemTracker() *turnItemTracker {
	return &turnItemTracker{items: map[string]*turnItemState{}}
}

func (s runtimeStateService) turnItemTracker() *turnItemTracker {
	if s.app == nil {
		return nil
	}
	if s.app.trackers.turnItems == nil {
		s.app.trackers.turnItems = newTurnItemTracker()
	}
	return s.app.trackers.turnItems
}

type turnItemState struct {
	ThreadID  string
	TurnID    string
	ItemID    string
	Status    string
	Started   map[string]any
	Completed map[string]any
}

func turnItemStateKey(turnID, itemID string) string {
	turnID = strings.TrimSpace(turnID)
	itemID = strings.TrimSpace(itemID)
	if turnID == "" || itemID == "" {
		return ""
	}
	return turnID + "\x00" + itemID
}

func (s runtimeStateService) noteTurnItemStarted(threadID, turnID string, item map[string]any) {
	if s.app == nil || item == nil {
		return
	}
	newTurnLifecycleService(s.app).bindPendingSubmissionTurn(threadID, turnID, true)
	itemID := strings.TrimSpace(stringValue(item["id"]))
	key := turnItemStateKey(turnID, itemID)
	if key == "" {
		return
	}
	started := cloneJSONMap(item)
	tracker := s.turnItemTracker()
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	if tracker.items == nil {
		tracker.items = map[string]*turnItemState{}
	}
	state := tracker.items[key]
	if state == nil {
		state = &turnItemState{
			ThreadID: strings.TrimSpace(threadID),
			TurnID:   strings.TrimSpace(turnID),
			ItemID:   itemID,
		}
		tracker.items[key] = state
	}
	if strings.TrimSpace(threadID) != "" {
		state.ThreadID = strings.TrimSpace(threadID)
	}
	state.Status = "started"
	state.Started = started
}

func (s runtimeStateService) turnItemSnapshot(threadID, turnID, itemID string) map[string]any {
	if s.app == nil {
		return nil
	}
	key := turnItemStateKey(turnID, itemID)
	if key == "" {
		return nil
	}
	tracker := s.turnItemTracker()
	tracker.mu.Lock()
	state := tracker.items[key]
	tracker.mu.Unlock()
	if state == nil {
		return nil
	}
	if strings.TrimSpace(threadID) != "" && strings.TrimSpace(state.ThreadID) != "" && strings.TrimSpace(state.ThreadID) != strings.TrimSpace(threadID) {
		return nil
	}
	return mergeJSONMaps(state.Started, state.Completed)
}

func (s runtimeStateService) completeTurnItemState(threadID, turnID, itemID string, item map[string]any) map[string]any {
	if item == nil {
		return nil
	}
	itemID = strings.TrimSpace(firstNonEmpty(itemID, stringValue(item["id"])))
	key := turnItemStateKey(turnID, itemID)
	if key == "" {
		return cloneJSONMap(item)
	}
	completed := cloneJSONMap(item)
	tracker := s.turnItemTracker()
	tracker.mu.Lock()
	state := tracker.items[key]
	if state != nil {
		if strings.TrimSpace(threadID) != "" {
			state.ThreadID = strings.TrimSpace(threadID)
		}
		state.Status = "completed"
		state.Completed = completed
	}
	delete(tracker.items, key)
	tracker.mu.Unlock()
	if state == nil {
		return completed
	}
	return mergeJSONMaps(state.Started, completed)
}

func (s runtimeStateService) clearTurnItemStates(turnID string) {
	if s.app == nil {
		return
	}
	turnID = strings.TrimSpace(turnID)
	if turnID == "" {
		return
	}
	prefix := turnID + "\x00"
	tracker := s.turnItemTracker()
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	for key := range tracker.items {
		if strings.HasPrefix(key, prefix) {
			delete(tracker.items, key)
		}
	}
}

func (s runtimeStateService) mergeRequestPayloadWithTurnItem(threadID, turnID, itemID string, payload map[string]any) map[string]any {
	snapshot := s.turnItemSnapshot(threadID, turnID, itemID)
	if snapshot == nil {
		return cloneJSONMap(payload)
	}
	return mergeJSONMaps(snapshot, payload)
}

func cloneJSONMap(src map[string]any) map[string]any {
	if src == nil {
		return nil
	}
	cloned, _ := cloneJSONValue(src).(map[string]any)
	return cloned
}

func cloneJSONValue(src any) any {
	if src == nil {
		return nil
	}
	b, err := json.Marshal(src)
	if err != nil {
		return src
	}
	var out any
	if err := json.Unmarshal(b, &out); err != nil {
		return src
	}
	return out
}

func mergeJSONMaps(base, overlay map[string]any) map[string]any {
	switch {
	case base == nil && overlay == nil:
		return nil
	case base == nil:
		return cloneJSONMap(overlay)
	case overlay == nil:
		return cloneJSONMap(base)
	}
	out := cloneJSONMap(base)
	if out == nil {
		out = map[string]any{}
	}
	for key, value := range overlay {
		if existing, ok := out[key].(map[string]any); ok {
			if next, ok := value.(map[string]any); ok {
				out[key] = mergeJSONMaps(existing, next)
				continue
			}
		}
		out[key] = cloneJSONValue(value)
	}
	return out
}
