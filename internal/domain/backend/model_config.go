package backend

import "errors"

// ErrModelConfigApply retains queued work and conversation lineage until desired settings can be applied.
var ErrModelConfigApply = errors.New("Claude 模型配置尚未应用")
