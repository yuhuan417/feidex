package app

import "strings"

func (s runtimeStateService) turnItemTracker() *turnItemTracker {
	if s.app == nil {
		return nil
	}
	if s.app.trackers.turnItems == nil {
		s.app.trackers.turnItems = newTurnItemTracker()
	}
	return s.app.trackers.turnItems
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
	tracker.Mu.Lock()
	defer tracker.Mu.Unlock()
	if tracker.Items == nil {
		tracker.Items = map[string]*turnItemState{}
	}
	state := tracker.Items[key]
	if state == nil {
		state = &turnItemState{
			ThreadID: strings.TrimSpace(threadID),
			TurnID:   strings.TrimSpace(turnID),
			ItemID:   itemID,
		}
		tracker.Items[key] = state
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
	tracker.Mu.Lock()
	state := tracker.Items[key]
	tracker.Mu.Unlock()
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
	tracker.Mu.Lock()
	state := tracker.Items[key]
	if state != nil {
		if strings.TrimSpace(threadID) != "" {
			state.ThreadID = strings.TrimSpace(threadID)
		}
		state.Status = "completed"
		state.Completed = completed
	}
	delete(tracker.Items, key)
	tracker.Mu.Unlock()
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
	tracker.Mu.Lock()
	defer tracker.Mu.Unlock()
	for key := range tracker.Items {
		if strings.HasPrefix(key, prefix) {
			delete(tracker.Items, key)
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
