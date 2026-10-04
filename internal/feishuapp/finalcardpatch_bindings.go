package feishuapp

import (
	"context"
	"feidex/internal/adapter/feishu/finalcardpatch"
	"feidex/internal/adapter/feishu/planmode"
	"feidex/internal/config"
	domainsubmission "feidex/internal/domain/submission"
)

type FinalCardPatchInputs struct {
	Context  func() context.Context
	Tracker  *finalcardpatch.Tracker
	Finder   finalcardpatch.SubmissionFinderProvider
	Patcher  finalcardpatch.FeishuPatcher
	RunAsync func(func())
	Config   *config.Config
	State    planmode.SessionStateProvider
}

func BuildFinalCardPatch(inputs FinalCardPatchInputs) finalcardpatch.Service {
	renderer := newCardRenderer(inputs.Config)
	return finalcardpatch.NewService(finalcardpatch.Dependencies{
		Context: inputs.Context, Tracker: inputs.Tracker, Finder: inputs.Finder, Patcher: inputs.Patcher,
		RunAsync: inputs.RunAsync,
		Renderer: func(ctx context.Context, sub *domainsubmission.Submission, title, color string, showHeader bool, body string, footerLines []string) map[string]any {
			card := renderer.renderReplyMarkdownCardWithHeaderOptions(ctx, sub, contentCardTitleForSubmission(inputs.State, sub, title), color, showHeader, body, nil, true)
			appendReplyCardFooter(card, footerLines)
			return card
		},
	})
}
