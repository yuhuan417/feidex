package claudecli

import "errors"

// ErrModelConfigApply prevents a configuration failure from falling back to a
// fresh conversation or consuming a queued prompt with stale settings.
var ErrModelConfigApply = errors.New("Claude 模型配置尚未应用")
