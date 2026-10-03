// Package frontend owns frontend-wide admission policy.
package frontend

import (
	"strings"

	"feidex/internal/domain/conversation"
	"feidex/internal/domain/interaction"
)

type SessionActivity struct {
	Session  *conversation.Session
	Retrying bool
}

type Activity struct {
	SwitchBlockedReason      string
	MaintenanceBlockedReason string
	MessageTraffic           int
	Sessions                 []SessionActivity
	Requests                 []interaction.Request
}

// Model writes save desired configuration. Active turns and open requests
// only block applying it; replacement and maintenance also block saving.
func (a Activity) ModelWriteBlockedReason() string {
	if a.SwitchBlockedReason != "" {
		return a.SwitchBlockedReason
	}
	return a.MaintenanceBlockedReason
}

func (a Activity) IdleBlockedReason(allowedMessageTraffic int) string {
	if reason := a.ModelWriteBlockedReason(); reason != "" {
		return reason
	}
	if a.MessageTraffic > allowedMessageTraffic {
		return "当前仍有消息处理中"
	}
	for _, entry := range a.Sessions {
		sess := entry.Session
		if sess == nil {
			continue
		}
		if conversation.HasActiveWork(sess) {
			return "当前仍有运行中的任务"
		}
		if entry.Retrying {
			return "当前仍有自动重试中的任务"
		}
		if len(sess.Queue) > 0 {
			return "当前仍有排队中的消息"
		}
		if len(sess.StagedImages) > 0 {
			return "当前仍有暂存图片待提交"
		}
		status := strings.TrimSpace(sess.Status)
		if status == "" {
			status = conversation.SessionStatusIdle.String()
		}
		if conversation.NormalizeSessionStatus(status) != conversation.SessionStatusIdle {
			return "当前会话还没有完全回到空闲态"
		}
	}
	for _, req := range a.Requests {
		if interaction.IsPendingRequestOpen(req.Status) {
			return "当前仍有待处理审批或表单"
		}
	}
	return ""
}
