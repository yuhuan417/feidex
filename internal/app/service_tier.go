package app

import (
	"context"
	"fmt"
	"strings"

	"feidex/internal/feishu"
)

const (
	serviceTierFast = "fast"
)

func normalizeServiceTier(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case serviceTierFast:
		return serviceTierFast
	default:
		return ""
	}
}

func toggleServiceTier(value string) string {
	if normalizeServiceTier(value) == serviceTierFast {
		return ""
	}
	return serviceTierFast
}

func renderServiceTierValue(value string) string {
	value = normalizeServiceTier(value)
	if value == "" {
		return "-"
	}
	return "`" + value + "`"
}

func renderServiceTierReplyValue(value string) string {
	value = normalizeServiceTier(value)
	if value == "" {
		return "未设置"
	}
	return "`" + value + "`"
}

func (a *App) commandFast(msg *feishu.InboundMessage, args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("usage: /fast")
	}
	if msg == nil {
		return nil
	}
	sessionKey := a.makeSessionKey(msg)
	sess := a.store.GetSession(sessionKey)
	if sess == nil || strings.TrimSpace(sess.ActiveThreadID) == "" {
		return fmt.Errorf("当前没有活动线程，无法切换 service tier")
	}
	next := toggleServiceTier(sess.ActiveThreadServiceTier)
	sess.ActiveThreadServiceTier = next
	if err := a.store.UpsertSession(sess); err != nil {
		return err
	}
	return a.feishu.ReplyText(context.Background(), msg.MessageID, "当前 thread ServiceTier 已切换为 "+renderServiceTierReplyValue(next)+"。", msg.ChatType == "group" && a.cfg.Feishu.ReplyInThread)
}
