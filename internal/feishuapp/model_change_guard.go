package feishuapp

import (
	"fmt"

	"feidex/internal/application/frontend"
)

// Model writes change desired settings only. Active work, queues, images and
// open forms do not block saving; backend replacement still does.
func modelConfigBlockedReason(query frontend.Query) string {
	if query.Facts == nil {
		return ""
	}
	return frontendActivity(query, false).ModelWriteBlockedReason()
}

func ensureSessionModelConfigWritable(a *App, _ string) error {
	if a == nil {
		return nil
	}
	if reason := modelConfigBlockedReason(a.bindings.FrontendQuery); reason != "" {
		return fmt.Errorf("模型配置暂不可保存: %s", reason)
	}
	return nil
}
