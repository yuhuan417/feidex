package turnmeta

import (
	"strings"
	"time"

	"feidex/internal/application/presentation/usageview"
	"feidex/internal/runtime/turnbinding"
)

type Service struct{ *turnbinding.Tracker }

func (s Service) Metadata(turnID string, completedAt time.Time) (usageLine, contextLine, elapsedLine string) {
	binding, ok := s.Tracker.TurnMetadata(turnID)
	if ok && binding.HasLastUsage {
		usageLine = usageview.FormatTurnUsageLine(binding.LastUsage)
	}
	if ok {
		if binding.HasContextUsagePercent {
			contextLine = usageview.FormatContextUsedLine(binding.ContextUsagePercent)
		} else if usage, found := s.CurrentThreadUsage(binding.ThreadID); found && usage.ModelContextWindow != nil {
			contextLine = usageview.FormatContextLeftLine(usage.Last.InputTokens, *usage.ModelContextWindow)
		}
	}
	if ok && !binding.StartedAt.IsZero() && !completedAt.IsZero() {
		elapsedLine = usageview.FormatTurnElapsedLine(completedAt.Sub(binding.StartedAt))
	}
	return
}

func (s Service) TurnFinalFooterLines(turnID string, completedAt time.Time) []string {
	_, contextLine, elapsedLine := s.Metadata(turnID, completedAt)
	var lines []string
	for _, line := range []string{contextLine, elapsedLine} {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
	}
	return lines
}
