package app

import (
	"context"
	"fmt"
	"math"
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
		return "elapsed: <1s"
	}
	totalSeconds := int64((d + 500*time.Millisecond) / time.Second)
	days := totalSeconds / (24 * 60 * 60)
	totalSeconds %= 24 * 60 * 60
	hours := totalSeconds / (60 * 60)
	totalSeconds %= 60 * 60
	minutes := totalSeconds / 60
	seconds := totalSeconds % 60

	parts := make([]string, 0, 4)
	if days > 0 {
		parts = append(parts, fmt.Sprintf("%dd", days))
	}
	if hours > 0 {
		parts = append(parts, fmt.Sprintf("%dh", hours))
	}
	if minutes > 0 {
		parts = append(parts, fmt.Sprintf("%dm", minutes))
	}
	if seconds > 0 {
		parts = append(parts, fmt.Sprintf("%ds", seconds))
	}
	if len(parts) == 0 {
		return "elapsed: <1s"
	}
	return "elapsed: " + strings.Join(parts, "")
}

func formatContextLeftLine(lastInputTokens, modelContextWindow int64) string {
	if modelContextWindow <= 0 {
		return ""
	}
	if lastInputTokens <= 0 {
		return "context left: 100.0%"
	}
	left := 100 * (1 - float64(lastInputTokens)/float64(modelContextWindow))
	if left < 0 {
		left = 0
	}
	if left > 100 {
		left = 100
	}
	return fmt.Sprintf("context left: %.1f%%", left)
}

func formatContextUsedLine(percentage float64) string {
	if math.IsNaN(percentage) || math.IsInf(percentage, 0) {
		return ""
	}
	if percentage < 0 {
		percentage = 0
	}
	if percentage > 100 {
		percentage = 100
	}
	return fmt.Sprintf("context used: %.1f%%", percentage)
}

func pendingContextUsedLine() string {
	return "context used: calculating..."
}

func withPendingContextUsedFooterLines(base []string) []string {
	lines := make([]string, 0, len(base)+1)
	lines = append(lines, pendingContextUsedLine())
	for _, line := range base {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "context left:") || strings.HasPrefix(line, "context used:") {
			continue
		}
		lines = append(lines, line)
	}
	return lines
}

func mergeContextUsedFooterLines(base []string, percentage float64) []string {
	contextLine := strings.TrimSpace(formatContextUsedLine(percentage))
	lines := make([]string, 0, len(base)+1)
	if contextLine != "" {
		lines = append(lines, contextLine)
	}
	for _, line := range base {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "context left:") || strings.HasPrefix(line, "context used:") {
			continue
		}
		lines = append(lines, line)
	}
	return lines
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

func (a *App) commandUsage(msg *feishu.InboundMessage, args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("usage: /usage")
	}
	card := a.renderUsageCard(a.makeSessionKey(msg))
	_, err := a.feishu.ReplyCard(context.Background(), msg.MessageID, card, a.replyInThreadEnabled(msg.ChatType))
	return err
}

func (a *App) renderUsageCard(sessionKey string) map[string]any {
	sess := a.appState().session(sessionKey)
	body := "当前没有活动线程。"
	if sess != nil && strings.TrimSpace(sess.ActiveThreadID) != "" {
		body = "当前线程暂无 token usage 数据。"
		if usage, ok := a.currentThreadUsage(sess.ActiveThreadID); ok {
			contextLine := ""
			if usage.ModelContextWindow != nil {
				contextLine = formatContextLeftLine(usage.Last.InputTokens, *usage.ModelContextWindow)
			}
			body = renderThreadUsageCardBody(currentThreadLabel(sess), sess.ActiveThreadID, usage, contextLine)
		}
	}
	return a.feishu.SimpleStatusCard("Token Usage", "blue", menuCardBody("menu.usage", body), []feishu.Button{
		{Text: "返回上一级", Type: "default", Value: map[string]any{"action": "menu.tools", "session_key": sessionKey}},
	})
}
