package app

import (
	"context"

	appfinalcardpatch "feidex/internal/app/finalcardpatch"
	"feidex/internal/state"
)

// ---------------------------------------------------------------------------
// Provider adapters — satisfy finalcardpatch narrow interfaces
// ---------------------------------------------------------------------------

type finalCardPatchSubmissionFinderAdapter struct{ app *App }

func (a finalCardPatchSubmissionFinderAdapter) Submission(id string) *state.Submission {
	return a.app.State().Submission(id)
}

type finalCardPatchFeishuAdapter struct{ app *App }

func (a finalCardPatchFeishuAdapter) PatchCard(ctx context.Context, messageID string, card map[string]any) error {
	if a.app == nil || a.app.feishu == nil {
		return nil
	}
	return a.app.feishu.PatchCard(ctx, messageID, card)
}

// ---------------------------------------------------------------------------
// *App methods satisfying finalcardpatch.App
// ---------------------------------------------------------------------------

// FinalCardPatchTracker returns the final-card-patch tracker, lazily
// initializing it.
func (a *App) FinalCardPatchTracker() *appfinalcardpatch.Tracker {
	if a == nil {
		return nil
	}
	if a.trackers.finalCardPatches == nil {
		a.trackers.finalCardPatches = appfinalcardpatch.NewTracker()
	}
	return a.trackers.finalCardPatches
}

// FinalCardPatchSubmissionFinder returns the narrowed submission finder for the
// final-card-patch service.
func (a *App) FinalCardPatchSubmissionFinder() appfinalcardpatch.SubmissionFinderProvider {
	if a == nil {
		return nil
	}
	return finalCardPatchSubmissionFinderAdapter{app: a}
}

// FinalCardPatchCardRenderer returns the card renderer callback for the
// final-card-patch service.
func (a *App) FinalCardPatchCardRenderer() appfinalcardpatch.CardRendererFunc {
	return func(ctx context.Context, sub *state.Submission, title, color string, showHeader bool, body string, footerLines []string) map[string]any {
		card := cardRendererForApp(a).renderReplyMarkdownCardWithHeaderOptions(ctx, sub, title, color, showHeader, body, nil, true)
		appendReplyCardFooter(card, footerLines)
		return card
	}
}

// FinalCardPatchFeishu returns the narrowed Feishu client for the
// final-card-patch service.
func (a *App) FinalCardPatchFeishu() appfinalcardpatch.FeishuPatcher {
	if a == nil {
		return nil
	}
	return finalCardPatchFeishuAdapter{app: a}
}
