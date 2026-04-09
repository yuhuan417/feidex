package app

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"feidex/internal/codexrpc"
	"feidex/internal/feishu"
)

func formatUsageInt(value int64) string {
	return strconv.FormatInt(value, 10)
}

func formatUsageRatio(part, whole int64) string {
	if whole <= 0 {
		return "-"
	}
	return fmt.Sprintf("%.1f%%", float64(part)*100/float64(whole))
}

func formatTurnUsageLine(usage codexrpc.TokenUsageBreakdown) string {
	return fmt.Sprintf(
		"usage: in %s | cache %s (%s) | out %s | reasoning %s",
		formatUsageInt(usage.InputTokens),
		formatUsageInt(usage.CachedInputTokens),
		formatUsageRatio(usage.CachedInputTokens, usage.InputTokens),
		formatUsageInt(usage.OutputTokens),
		formatUsageInt(usage.ReasoningOutputTokens),
	)
}

func formatTurnElapsedLine(d time.Duration) string {
	if d <= 0 {
		return "elapsed: -"
	}
	if d < time.Second {
		return fmt.Sprintf("elapsed: %dms", d.Milliseconds())
	}
	return fmt.Sprintf("elapsed: %.1fs", d.Seconds())
}

func renderThreadUsageCardBody(threadLabel, threadID string, usage codexrpc.ThreadTokenUsage) string {
	lines := []string{
		"当前线程: " + firstNonEmpty(strings.TrimSpace(threadLabel), "-"),
		"thread: `" + firstNonEmpty(strings.TrimSpace(threadID), "-") + "`",
		"",
		"累计 token usage (`total`):",
		"- total: `" + formatUsageInt(usage.Total.TotalTokens) + "`",
		"- input: `" + formatUsageInt(usage.Total.InputTokens) + "`",
		"- cached input: `" + formatUsageInt(usage.Total.CachedInputTokens) + "`",
		"- cache ratio: `" + formatUsageRatio(usage.Total.CachedInputTokens, usage.Total.InputTokens) + "`",
		"- output: `" + formatUsageInt(usage.Total.OutputTokens) + "`",
		"- reasoning output: `" + formatUsageInt(usage.Total.ReasoningOutputTokens) + "`",
		"",
		"最近一次 turn (`last`):",
		"- total: `" + formatUsageInt(usage.Last.TotalTokens) + "`",
		"- input: `" + formatUsageInt(usage.Last.InputTokens) + "`",
		"- cached input: `" + formatUsageInt(usage.Last.CachedInputTokens) + "`",
		"- cache ratio: `" + formatUsageRatio(usage.Last.CachedInputTokens, usage.Last.InputTokens) + "`",
		"- output: `" + formatUsageInt(usage.Last.OutputTokens) + "`",
		"- reasoning output: `" + formatUsageInt(usage.Last.ReasoningOutputTokens) + "`",
	}
	return strings.Join(lines, "\n")
}

func (a *App) commandUsage(msg *feishu.InboundMessage, args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("usage: /usage")
	}
	card := a.renderUsageCard(a.makeSessionKey(msg))
	_, err := a.feishu.ReplyCard(context.Background(), msg.MessageID, card, msg.ChatType == "group" && a.cfg.Feishu.ReplyInThread)
	return err
}

func (a *App) renderUsageCard(sessionKey string) map[string]any {
	sess := a.store.GetSession(sessionKey)
	body := "当前没有活动线程。"
	if sess != nil && strings.TrimSpace(sess.ActiveThreadID) != "" {
		body = "当前线程暂无 token usage 数据。"
		if usage, ok := a.currentThreadUsage(sess.ActiveThreadID); ok {
			body = renderThreadUsageCardBody(currentThreadLabel(sess), sess.ActiveThreadID, usage)
		}
	}
	return a.feishu.SimpleStatusCard("Token Usage", "blue", menuCardBody("menu.usage", body), []feishu.Button{
		{Text: "返回上一级", Type: "default", Value: map[string]any{"action": "menu.group.session", "session_key": sessionKey}},
	})
}
