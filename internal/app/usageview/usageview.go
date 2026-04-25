// Package usageview provides pure formatting and rendering functions for
// token usage display in Feishu cards.
package usageview

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"feidex/internal/app/apputil"
	"feidex/internal/codexrpc"
)

// FormatUsageInt formats an int64 value as a decimal string.
func FormatUsageInt(value int64) string {
	return strconv.FormatInt(value, 10)
}

// FormatUsageRatio formats a ratio of part/whole as a percentage string.
// Returns "-" if whole <= 0.
func FormatUsageRatio(part, whole int64) string {
	if whole <= 0 {
		return "-"
	}
	return fmt.Sprintf("%.1f%%", float64(part)*100/float64(whole))
}

// FormatUsageCost formats a dollar amount with 6 decimal places.
func FormatUsageCost(value float64) string {
	return fmt.Sprintf("$%.6f", value)
}

// FormatTurnUsageLine formats a single line of token usage for a turn.
func FormatTurnUsageLine(usage codexrpc.TokenUsageBreakdown) string {
	return fmt.Sprintf(
		"token: input %s | cache %s (%s) | output %s | reasoning %s",
		FormatUsageInt(usage.InputTokens),
		FormatUsageInt(usage.CachedInputTokens),
		FormatUsageRatio(usage.CachedInputTokens, usage.InputTokens),
		FormatUsageInt(usage.OutputTokens),
		FormatUsageInt(usage.ReasoningOutputTokens),
	)
}

// FormatTurnElapsedLine formats a duration as an elapsed time string
// like "elapsed: 2h3m4s".
func FormatTurnElapsedLine(d time.Duration) string {
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

// FormatContextLeftLine returns a "context left: X.X%" line based on
// the last input tokens and the model context window size.
// Returns "" if modelContextWindow <= 0.
func FormatContextLeftLine(lastInputTokens, modelContextWindow int64) string {
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

// FormatContextUsedLine returns a "context used: X.X%" line.
// Returns "" for NaN or Inf values.
func FormatContextUsedLine(percentage float64) string {
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

// RenderThreadUsageCardBody renders the markdown body for a Codex thread
// usage card. contextLine is optional and appended if non-empty.
func RenderThreadUsageCardBody(threadLabel, threadID string, usage codexrpc.ThreadTokenUsage, contextLine string) string {
	lines := []string{
		"当前线程: " + apputil.FirstNonEmpty(strings.TrimSpace(threadLabel), "-"),
		"thread: `" + apputil.FirstNonEmpty(strings.TrimSpace(threadID), "-") + "`",
		"",
		"累计 token usage (`total`):",
		"- total: `" + FormatUsageInt(usage.Total.TotalTokens) + "`",
		"- input: `" + FormatUsageInt(usage.Total.InputTokens) + "`",
		"- cached input: `" + FormatUsageInt(usage.Total.CachedInputTokens) + "`",
		"- cache ratio: `" + FormatUsageRatio(usage.Total.CachedInputTokens, usage.Total.InputTokens) + "`",
		"- output: `" + FormatUsageInt(usage.Total.OutputTokens) + "`",
		"- reasoning output: `" + FormatUsageInt(usage.Total.ReasoningOutputTokens) + "`",
	}
	if contextLine = strings.TrimSpace(contextLine); contextLine != "" {
		lines = append(lines, "", contextLine)
	}
	return strings.Join(lines, "\n")
}
