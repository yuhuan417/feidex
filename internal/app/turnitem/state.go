package turnitem

import (
	"encoding/json"
	"strings"
	"sync"
)

type Tracker struct {
	Mu    sync.Mutex
	Items map[string]*State
}

func NewTracker() *Tracker {
	return &Tracker{Items: map[string]*State{}}
}

type State struct {
	ThreadID  string
	TurnID    string
	ItemID    string
	Status    string
	Started   map[string]any
	Completed map[string]any
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
