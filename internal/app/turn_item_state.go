package app

import (
	appapproval "feidex/internal/app/approval"
	"feidex/internal/app/turnitem"
	"strings"
)

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
	s.noteTurnItemStartedPayload(threadID, turnID, turnitem.NewProtocolItem(item))
}

func (s runtimeStateService) noteTurnItemStartedPayload(threadID, turnID string, item turnitem.ProtocolItem) {
	if s.app == nil || (item.Raw == nil && item.ID == "" && item.Type == "") {
		return
	}
	newTurnLifecycleService(s.app).bindPendingSubmissionTurn(threadID, turnID, true)
	itemID := strings.TrimSpace(item.EffectiveID(""))
	key := turnItemStateKey(turnID, itemID)
	if key == "" {
		return
	}
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
	state.Started = item
}

func (s runtimeStateService) turnItemSnapshot(threadID, turnID, itemID string) map[string]any {
	return s.turnItemSnapshotPayload(threadID, turnID, itemID).MergedRaw()
}

func (s runtimeStateService) turnItemSnapshotPayload(threadID, turnID, itemID string) turnitem.ProtocolItem {
	if s.app == nil {
		return turnitem.ProtocolItem{}
	}
	key := turnItemStateKey(turnID, itemID)
	if key == "" {
		return turnitem.ProtocolItem{}
	}
	tracker := s.turnItemTracker()
	tracker.Mu.Lock()
	state := tracker.Items[key]
	tracker.Mu.Unlock()
	if state == nil {
		return turnitem.ProtocolItem{}
	}
	if strings.TrimSpace(threadID) != "" && strings.TrimSpace(state.ThreadID) != "" && strings.TrimSpace(state.ThreadID) != strings.TrimSpace(threadID) {
		return turnitem.ProtocolItem{}
	}
	return turnitem.NewProtocolItemWithID(itemID, mergeJSONMaps(state.Started.MergedRaw(), state.Completed.MergedRaw()))
}

func (s runtimeStateService) completeTurnItemState(threadID, turnID, itemID string, item map[string]any) map[string]any {
	return s.completeTurnItemStatePayload(threadID, turnID, itemID, turnitem.NewProtocolItem(item)).MergedRaw()
}

func (s runtimeStateService) completeTurnItemStatePayload(threadID, turnID, itemID string, item turnitem.ProtocolItem) turnitem.ProtocolItem {
	if item.Raw == nil && item.ID == "" && item.Type == "" {
		return turnitem.ProtocolItem{}
	}
	itemID = strings.TrimSpace(item.EffectiveID(itemID))
	key := turnItemStateKey(turnID, itemID)
	if key == "" {
		return turnitem.NewProtocolItemWithID(itemID, item.MergedRaw())
	}
	completed := turnitem.NewProtocolItemWithID(itemID, item.MergedRaw())
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
	return turnitem.NewProtocolItemWithID(itemID, mergeJSONMaps(state.Started.MergedRaw(), completed.MergedRaw()))
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

func (s runtimeStateService) updateInFlightTurnItemPayload(threadID, turnID, itemID string, overlay map[string]any) turnitem.ProtocolItem {
	if s.app == nil {
		return turnitem.NewProtocolItemWithID(itemID, overlay)
	}
	key := turnItemStateKey(turnID, itemID)
	if key == "" {
		return turnitem.NewProtocolItemWithID(itemID, overlay)
	}
	tracker := s.turnItemTracker()
	tracker.Mu.Lock()
	defer tracker.Mu.Unlock()
	state := tracker.Items[key]
	if state == nil {
		return turnitem.NewProtocolItemWithID(itemID, overlay)
	}
	if strings.TrimSpace(threadID) != "" {
		state.ThreadID = strings.TrimSpace(threadID)
	}
	state.Started = turnitem.NewProtocolItemWithID(itemID, mergeJSONMaps(state.Started.MergedRaw(), overlay))
	return turnitem.NewProtocolItemWithID(itemID, mergeJSONMaps(state.Started.MergedRaw(), state.Completed.MergedRaw()))
}

func (s runtimeStateService) mergeRequestPayloadWithTurnItem(threadID, turnID, itemID string, payload map[string]any) map[string]any {
	snapshot := s.turnItemSnapshotPayload(threadID, turnID, itemID)
	if snapshot.Raw == nil && snapshot.ID == "" && snapshot.Type == "" {
		return cloneJSONMap(payload)
	}
	return mergeJSONMaps(snapshot.MergedRaw(), payload)
}

func (s runtimeStateService) mergeApprovalPresentationWithTurnItem(presentation appapproval.Presentation) appapproval.Presentation {
	presentation.Payload.Request = s.mergeRequestPayloadWithTurnItem(
		presentation.ThreadID,
		presentation.TurnID,
		presentation.ItemID,
		presentation.Payload.Request,
	)
	if appapproval.NormalizeKind(presentation.Kind.String()) == appapproval.KindPermissions && len(presentation.Payload.Permissions) == 0 {
		if permissions, ok := presentation.Payload.Request["permissions"].(map[string]any); ok {
			presentation.Payload.Permissions = appapproval.CloneJSONMap(permissions)
		}
	}
	if strings.TrimSpace(presentation.Body) == "" {
		presentation.Body = strings.TrimSpace(presentation.Payload.Body)
	}
	return presentation
}

// Exported wrappers for sub-package interface satisfaction.

func (s runtimeStateService) CompleteTurnItemState(threadID, turnID, itemID string, item turnitem.ProtocolItem) turnitem.ProtocolItem {
	return s.completeTurnItemStatePayload(threadID, turnID, itemID, item)
}
func (s runtimeStateService) ClearTurnItemStates(turnID string) { s.clearTurnItemStates(turnID) }
