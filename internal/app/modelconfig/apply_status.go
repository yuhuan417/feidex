package modelconfig

import "feidex/internal/adapter/feishu/cards"

func (s ModelConfigService) appendApplyStatus(card map[string]any, sessionKey string) {
	if s.ModelConfigStatus != nil {
		cards.AppendMarkdownBodyCardElement(card, map[string]any{"tag": "markdown", "content": s.ModelConfigStatus(sessionKey)})
	}
}
