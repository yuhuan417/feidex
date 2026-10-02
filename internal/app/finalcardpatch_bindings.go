package app

import (
	"context"
	"feidex/internal/adapter/feishu/finalcardpatch"
	domainsubmission "feidex/internal/domain/submission"
)

func newFinalCardPatchService(a *App) finalcardpatch.Service {
	if a == nil {
		return finalcardpatch.Service{}
	}
	trackers := a.Trackers()
	if trackers.finalCardPatches == nil {
		trackers.finalCardPatches = finalcardpatch.NewTracker()
	}
	return finalcardpatch.NewService(finalcardpatch.Dependencies{
		Context: a.Context, Tracker: trackers.finalCardPatches, Finder: a.State(), Patcher: a.feishu,
		RunAsync: func(fn func()) { runAsync(a, fn) },
		Renderer: func(ctx context.Context, sub *domainsubmission.Submission, title, color string, showHeader bool, body string, footerLines []string) map[string]any {
			card := cardRendererForApp(a).renderReplyMarkdownCardWithHeaderOptions(ctx, sub, contentCardTitleForSubmission(a, sub, title), color, showHeader, body, nil, true)
			appendReplyCardFooter(card, footerLines)
			return card
		},
	})
}
