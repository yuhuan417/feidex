package turnitem

import (
	"encoding/json"
	"strings"
	"sync"
)

type Tracker struct {
	mu    sync.Mutex
	items map[string]*State
}

func NewTracker() *Tracker {
	return &Tracker{items: map[string]*State{}}
}

func (t *Tracker) StartedItems() []State {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	items := make([]State, 0, len(t.items))
	for _, entry := range t.items {
		if entry == nil || entry.Status != "started" {
			continue
		}
		value := *entry
		value.Started = NewProtocolItemWithID(value.ItemID, entry.Started.MergedRaw())
		value.Completed = NewProtocolItemWithID(value.ItemID, entry.Completed.MergedRaw())
		items = append(items, value)
	}
	return items
}

// Start records detached protocol context so progress and approval overlays
// can merge with it until the item completes.
func (t *Tracker) Start(threadID, turnID string, item ProtocolItem) {
	if t == nil || (item.Raw == nil && item.ID == "" && item.Type == "") {
		return
	}
	itemID := strings.TrimSpace(item.EffectiveID(""))
	key := StateKey(turnID, itemID)
	if key == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.items == nil {
		t.items = map[string]*State{}
	}
	state := t.items[key]
	if state == nil {
		state = &State{ThreadID: strings.TrimSpace(threadID), TurnID: strings.TrimSpace(turnID), ItemID: itemID}
		t.items[key] = state
	}
	if strings.TrimSpace(threadID) != "" {
		state.ThreadID = strings.TrimSpace(threadID)
	}
	state.Status = "started"
	state.Started = NewProtocolItemWithID(itemID, item.MergedRaw())
}

func (t *Tracker) Snapshot(threadID, turnID, itemID string) ProtocolItem {
	if t == nil {
		return ProtocolItem{}
	}
	key := StateKey(turnID, itemID)
	if key == "" {
		return ProtocolItem{}
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	state := t.items[key]
	if state == nil || (strings.TrimSpace(threadID) != "" && strings.TrimSpace(state.ThreadID) != "" && strings.TrimSpace(state.ThreadID) != strings.TrimSpace(threadID)) {
		return ProtocolItem{}
	}
	return NewProtocolItemWithID(itemID, MergeJSONMaps(state.Started.MergedRaw(), state.Completed.MergedRaw()))
}

// Complete atomically consumes the started context. Returned payloads never
// retain references to mutable tracker state.
func (t *Tracker) Complete(threadID, turnID, itemID string, item ProtocolItem) ProtocolItem {
	if item.Raw == nil && item.ID == "" && item.Type == "" {
		return ProtocolItem{}
	}
	itemID = strings.TrimSpace(item.EffectiveID(itemID))
	completed := NewProtocolItemWithID(itemID, item.MergedRaw())
	key := StateKey(turnID, itemID)
	if t == nil || key == "" {
		return completed
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	state := t.items[key]
	delete(t.items, key)
	if state == nil {
		return completed
	}
	return NewProtocolItemWithID(itemID, MergeJSONMaps(state.Started.MergedRaw(), completed.MergedRaw()))
}

func (t *Tracker) Progress(threadID, turnID, itemID string, overlay map[string]any) ProtocolItem {
	if t == nil {
		return NewProtocolItemWithID(itemID, overlay)
	}
	key := StateKey(turnID, itemID)
	if key == "" {
		return NewProtocolItemWithID(itemID, overlay)
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	state := t.items[key]
	if state == nil {
		return NewProtocolItemWithID(itemID, overlay)
	}
	if strings.TrimSpace(threadID) != "" {
		state.ThreadID = strings.TrimSpace(threadID)
	}
	state.Started = NewProtocolItemWithID(itemID, MergeJSONMaps(state.Started.MergedRaw(), overlay))
	return NewProtocolItemWithID(itemID, MergeJSONMaps(state.Started.MergedRaw(), state.Completed.MergedRaw()))
}

func (t *Tracker) ClearTurn(turnID string) {
	turnID = strings.TrimSpace(turnID)
	if t == nil || turnID == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	for key := range t.items {
		if strings.HasPrefix(key, turnID+"\x00") {
			delete(t.items, key)
		}
	}
}

type State struct {
	ThreadID  string
	TurnID    string
	ItemID    string
	Status    string
	Started   ProtocolItem
	Completed ProtocolItem
}

func StateKey(turnID, itemID string) string {
	turnID = strings.TrimSpace(turnID)
	itemID = strings.TrimSpace(itemID)
	if turnID == "" || itemID == "" {
		return ""
	}
	return turnID + "\x00" + itemID
}

func CloneJSONMap(src map[string]any) map[string]any {
	if src == nil {
		return nil
	}
	cloned, _ := CloneJSONValue(src).(map[string]any)
	return cloned
}

func CloneJSONValue(src any) any {
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

func MergeJSONMaps(base, overlay map[string]any) map[string]any {
	switch {
	case base == nil && overlay == nil:
		return nil
	case base == nil:
		return CloneJSONMap(overlay)
	case overlay == nil:
		return CloneJSONMap(base)
	}
	out := CloneJSONMap(base)
	if out == nil {
		out = map[string]any{}
	}
	for key, value := range overlay {
		if existing, ok := out[key].(map[string]any); ok {
			if next, ok := value.(map[string]any); ok {
				out[key] = MergeJSONMaps(existing, next)
				continue
			}
		}
		out[key] = CloneJSONValue(value)
	}
	return out
}
