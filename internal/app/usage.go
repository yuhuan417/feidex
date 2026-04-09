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
		"token: input %s | cache %s (%s) | output %s | reasoning %s",
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

func formatContextRemainingLine(totalTokens, autoCompactLimit int64) string {
	if autoCompactLimit <= 0 {
		return ""
	}
	if totalTokens <= 0 {
		return "context remaining: 100.0%"
	}
	remaining := 100 * (1 - float64(totalTokens)/float64(autoCompactLimit))
	if remaining < 0 {
		remaining = 0
	}
	if remaining > 100 {
		remaining = 100
	}
	return fmt.Sprintf("context remaining: %.1f%%", remaining)
}

func renderThreadUsageCardBody(threadLabel, threadID string, usage codexrpc.ThreadTokenUsage, contextLine string) string {
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
	}
	if contextLine = strings.TrimSpace(contextLine); contextLine != "" {
		lines = append(lines, "", contextLine)
	}
	return strings.Join(lines, "\n")
}

func (a *App) fetchAutoCompactTokenLimit(ctx context.Context, cwd string) *int64 {
	if a == nil || a.codex == nil {
		return nil
	}
	cwd = strings.TrimSpace(cwd)
	a.configReadMu.Lock()
	if a.autoCompact == nil {
		a.autoCompact = map[string]*int64{}
	}
	if value, ok := a.autoCompact[cwd]; ok {
		a.configReadMu.Unlock()
		return value
	}
	a.configReadMu.Unlock()

	params := codexrpc.ConfigReadParams{IncludeLayers: true}
	if cwd != "" {
		params.CWD = &cwd
	}
	var resp codexrpc.ConfigReadResponse
	if err := a.codex.Call(ctx, "config/read", params, &resp); err != nil {
		return nil
	}

	a.configReadMu.Lock()
	defer a.configReadMu.Unlock()
	a.autoCompact[cwd] = resp.Config.ModelAutoCompactTokenLimit
	return resp.Config.ModelAutoCompactTokenLimit
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
			contextLine := ""
			if limit := a.fetchAutoCompactTokenLimit(context.Background(), ""); limit != nil {
				contextLine = formatContextRemainingLine(usage.Total.TotalTokens, *limit)
			}
			body = renderThreadUsageCardBody(currentThreadLabel(sess), sess.ActiveThreadID, usage, contextLine)
		}
	}
	return a.feishu.SimpleStatusCard("Token Usage", "blue", menuCardBody("menu.usage", body), []feishu.Button{
		{Text: "返回上一级", Type: "default", Value: map[string]any{"action": "menu.group.session", "session_key": sessionKey}},
	})
}
