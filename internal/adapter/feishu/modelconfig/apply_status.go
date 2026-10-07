package modelconfig

import "feidex/internal/adapter/feishu/cards"

func (s ModelConfigService) appendApplyStatus(card map[string]any, sessionKey string) {
	for _, element := range s.applyStatusTail(sessionKey) {
		cards.AppendMarkdownBodyCardElement(card, element)
	}
}

// applyStatusTail renders the non-interactive apply-status note that pages
// append after the back control.
func (s ModelConfigService) applyStatusTail(sessionKey string) []map[string]any {
	if s.ModelConfigStatus == nil {
		return nil
	}
	return []map[string]any{{"tag": "markdown", "content": s.ModelConfigStatus(sessionKey)}}
}
