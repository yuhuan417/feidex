package turn

import (
	"encoding/json"
	"feidex/internal/textutil"
	"fmt"
	"strings"
)

type ProtocolItem struct {
	ID       string
	Type     string
	Status   string
	ToolName string
	Raw      map[string]any
}

func NewProtocolItem(raw map[string]any) ProtocolItem {
	item := ProtocolItem{Raw: CloneJSONMap(raw)}
	if len(item.Raw) == 0 {
		item.Raw = nil
		return item
	}
	item.ID = strings.TrimSpace(itemString(item.Raw["id"]))
	item.Type = strings.TrimSpace(itemString(item.Raw["type"]))
	item.Status = strings.TrimSpace(itemString(item.Raw["status"]))
	item.ToolName = strings.TrimSpace(textutil.FirstNonEmpty(
		itemString(item.Raw["tool"]),
		itemString(item.Raw["toolName"]),
	))
	return item
}

func NewProtocolItemWithID(itemID string, raw map[string]any) ProtocolItem {
	item := NewProtocolItem(raw)
	if strings.TrimSpace(item.ID) != "" {
		return item
	}
	item.ID = strings.TrimSpace(itemID)
	if item.ID == "" {
		return item
	}
	if item.Raw == nil {
		item.Raw = map[string]any{}
	}
	item.Raw["id"] = item.ID
	return item
}

func (p ProtocolItem) EffectiveID(fallback string) string {
	return strings.TrimSpace(textutil.FirstNonEmpty(p.ID, fallback))
}

func (p ProtocolItem) MergedRaw() map[string]any {
	if p.Raw == nil && strings.TrimSpace(p.ID) == "" && strings.TrimSpace(p.Type) == "" && strings.TrimSpace(p.Status) == "" && strings.TrimSpace(p.ToolName) == "" {
		return nil
	}
	out := CloneJSONMap(p.Raw)
	if out == nil {
		out = map[string]any{}
	}
	if strings.TrimSpace(p.ID) != "" {
		out["id"] = p.ID
	}
	if strings.TrimSpace(p.Type) != "" {
		out["type"] = p.Type
	}
	if strings.TrimSpace(p.Status) != "" {
		out["status"] = p.Status
	}
	if strings.TrimSpace(p.ToolName) != "" {
		out["tool"] = p.ToolName
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func itemString(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}
func CloneJSONMap(src map[string]any) map[string]any {
	if len(src) == 0 {
		return nil
	}
	b, err := json.Marshal(src)
	if err != nil {
		out := map[string]any{}
		for k, v := range src {
			out[k] = v
		}
		return out
	}
	var out map[string]any
	if json.Unmarshal(b, &out) != nil {
		return nil
	}
	return out
}
