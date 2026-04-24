package app

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"feidex/internal/claudecli"
	"feidex/internal/codexrpc"
	"feidex/internal/feishu"
	"feidex/internal/state"
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

func formatUsageCost(value float64) string {
	return fmt.Sprintf("$%.6f", value)
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

func renderClaudeThreadUsageCardBody(threadLabel, threadID string, usage claudeThreadUsageSnapshot) string {
	totalTokens := usage.TotalInputTokens + usage.TotalCacheReadTokens + usage.TotalCacheCreationTokens + usage.TotalOutputTokens
	lines := []string{
		"当前会话: " + firstNonEmpty(strings.TrimSpace(threadLabel), "-"),
		"session: `" + firstNonEmpty(strings.TrimSpace(threadID), "-") + "`",
		"",
		"累计 token usage (`modelUsage`):",
		"- total: `" + formatUsageInt(totalTokens) + "`",
		"- input: `" + formatUsageInt(usage.TotalInputTokens) + "`",
		"- cache read: `" + formatUsageInt(usage.TotalCacheReadTokens) + "`",
		"- cache write: `" + formatUsageInt(usage.TotalCacheCreationTokens) + "`",
		"- output: `" + formatUsageInt(usage.TotalOutputTokens) + "`",
		"- cost: `" + formatUsageCost(usage.TotalCostUSD) + "`",
	}
	if usage.HasContextUsagePercent {
		lines = append(lines, "", formatContextUsedLine(usage.ContextUsagePercent))
	}
	return strings.Join(lines, "\n")
}

func (s usageService) recordClaudeThreadUsage(threadID string, usage claudecli.TurnUsage) {
	if s.app == nil {
		return
	}
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return
	}
	snapshot := claudeThreadUsageSnapshot{
		TotalCostUSD:  usage.CostUSD,
		ContextWindow: int64(usage.ContextWindow),
	}
	if usage.HasCumulativeUsage {
		snapshot.TotalInputTokens = int64(usage.CumulativeInputTokens)
		snapshot.TotalOutputTokens = int64(usage.CumulativeOutputTokens)
		snapshot.TotalCacheReadTokens = int64(usage.CumulativeCacheReadTokens)
		snapshot.TotalCacheCreationTokens = int64(usage.CumulativeCacheCreationTokens)
	} else {
		snapshot.TotalInputTokens = int64(usage.InputTokens)
		snapshot.TotalOutputTokens = int64(usage.OutputTokens)
		snapshot.TotalCacheReadTokens = int64(usage.CacheReadTokens)
		snapshot.TotalCacheCreationTokens = int64(usage.CacheCreationTokens)
	}
	if percentage, ok := claudeTurnContextUsagePercent(usage); ok {
		snapshot.ContextUsagePercent = percentage
		snapshot.HasContextUsagePercent = true
	}

	tracker := newRuntimeStateService(s.app).turnBindingTracker()
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	if tracker.claudeUsage == nil {
		tracker.claudeUsage = map[string]claudeThreadUsageSnapshot{}
	}
	tracker.claudeUsage[threadID] = snapshot
}

func (s usageService) currentClaudeThreadUsage(threadID string) (claudeThreadUsageSnapshot, bool) {
	if s.app == nil {
		return claudeThreadUsageSnapshot{}, false
	}
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return claudeThreadUsageSnapshot{}, false
	}
	tracker := newRuntimeStateService(s.app).turnBindingTracker()
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	usage, ok := tracker.claudeUsage[threadID]
	return usage, ok
}

func (s usageService) commandUsage(msg *feishu.InboundMessage, args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("usage: /usage")
	}
	card := newUsageService(s.app).renderUsageCard(makeSessionKey(s.app, msg))
	_, err := s.app.feishu.ReplyCard(context.Background(), msg.MessageID, card, replyInThreadEnabled(s.app, msg.ChatType))
	return err
}

func (s usageService) renderUsageCard(sessionKey string) map[string]any {
	sess := appState(s.app).session(sessionKey)
	body := primaryConversationMissingLabel(configuredBackend(s.app)) + "。"
	if sess != nil && strings.TrimSpace(sess.ActiveThreadID) != "" {
		body = conversationBackend(s.app).renderUsageBody(sess)
	}
	return s.app.feishu.SimpleStatusCard("Token Usage", "blue", menuCardBody("menu.usage", body), []feishu.Button{
		{Text: "返回上一级", Type: "default", Value: map[string]any{"action": "menu.tools", "session_key": sessionKey}},
	})
}

func (s usageService) renderClaudeUsageBody(sess *state.Session) string {
	if sess == nil || strings.TrimSpace(sess.ActiveThreadID) == "" {
		return primaryConversationMissingLabel(backendClaude) + "。"
	}
	body := "当前会话暂无 Claude usage 数据。"
	if usage, ok := newUsageService(s.app).currentClaudeThreadUsage(sess.ActiveThreadID); ok {
		body = renderClaudeThreadUsageCardBody(currentThreadLabel(sess), sess.ActiveThreadID, usage)
	}
	return body
}

func (s usageService) renderCodexUsageBody(sess *state.Session) string {
	if sess == nil || strings.TrimSpace(sess.ActiveThreadID) == "" {
		return primaryConversationMissingLabel(backendCodex) + "。"
	}
	body := "当前线程暂无 token usage 数据。"
	if usage, ok := newRuntimeStateService(s.app).currentThreadUsage(sess.ActiveThreadID); ok {
		contextLine := ""
		if usage.ModelContextWindow != nil {
			contextLine = formatContextLeftLine(usage.Last.InputTokens, *usage.ModelContextWindow)
		}
		body = renderThreadUsageCardBody(currentThreadLabel(sess), sess.ActiveThreadID, usage, contextLine)
	}
	return body
}
