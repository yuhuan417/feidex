package approval

import (
	"feidex/internal/adapter/feishu/turnitem"
	"strings"
)

type ItemContext struct {
	Items   *turnitem.Tracker
	Started func(string, string)
}

func (c ItemContext) Start(threadID, turnID string, item turnitem.ProtocolItem) {
	if item.Raw == nil && item.ID == "" && item.Type == "" {
		return
	}
	c.Started(threadID, turnID)
	c.Items.Start(threadID, turnID, item)
}
func (c ItemContext) StartRaw(threadID, turnID string, item map[string]any) {
	c.Start(threadID, turnID, turnitem.NewProtocolItem(item))
}
func (c ItemContext) SnapshotRaw(threadID, turnID, itemID string) map[string]any {
	return c.Items.Snapshot(threadID, turnID, itemID).MergedRaw()
}
func (c ItemContext) CompleteRaw(threadID, turnID, itemID string, item map[string]any) map[string]any {
	return c.Items.Complete(threadID, turnID, itemID, turnitem.NewProtocolItem(item)).MergedRaw()
}
func (c ItemContext) MergeRequest(threadID, turnID, itemID string, payload map[string]any) map[string]any {
	snapshot := c.Items.Snapshot(threadID, turnID, itemID)
	return turnitem.MergeJSONMaps(snapshot.MergedRaw(), payload)
}
func (c ItemContext) MergePresentation(p Presentation) Presentation {
	p.Payload.Request = c.MergeRequest(p.ThreadID, p.TurnID, p.ItemID, p.Payload.Request)
	if NormalizeKind(p.Kind.String()) == KindPermissions && len(p.Payload.Permissions) == 0 {
		if permissions, ok := p.Payload.Request["permissions"].(map[string]any); ok {
			p.Payload.Permissions = CloneJSONMap(permissions)
		}
	}
	if strings.TrimSpace(p.Body) == "" {
		p.Body = strings.TrimSpace(p.Payload.Body)
	}
	return p
}
