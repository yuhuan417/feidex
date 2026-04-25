package app

import (
	appfeishuwrap "feidex/internal/app/feishuwrap"
)

type feishuNotifyTarget = appfeishuwrap.NotifyTarget
type notifyingFeishuClient = appfeishuwrap.NotifyingFeishuClient
type commandCaptureClient = appfeishuwrap.CommandCaptureClient
type commandCaptureFeishuClient = appfeishuwrap.CommandCaptureFeishuClient

var wrapFeishuClient = appfeishuwrap.WrapFeishuClient
