package turnitem

import "strings"

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
	item.ID = strings.TrimSpace(StringValue(item.Raw["id"]))
	item.Type = strings.TrimSpace(StringValue(item.Raw["type"]))
	item.Status = strings.TrimSpace(StringValue(item.Raw["status"]))
	item.ToolName = strings.TrimSpace(FirstNonEmpty(
		StringValue(item.Raw["tool"]),
		StringValue(item.Raw["toolName"]),
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
	return strings.TrimSpace(FirstNonEmpty(p.ID, fallback))
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
