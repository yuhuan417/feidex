package modelconfig

// applyStatusTail renders the non-interactive apply-status note that pages
// append after the back control.
func (s ModelConfigService) applyStatusTail(sessionKey string) []map[string]any {
	if s.ModelConfigStatus == nil {
		return nil
	}
	return []map[string]any{{"tag": "markdown", "content": s.ModelConfigStatus(sessionKey)}}
}
