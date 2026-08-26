package app

import (
	appfinalcardpatch "feidex/internal/app/finalcardpatch"
	"feidex/internal/state"
)

// Type aliases preserve the original names within the app package.
type (
	finalCardPatchTracker  = appfinalcardpatch.Tracker
	finalCardPatchState    = appfinalcardpatch.PatchState
	finalCardPatchSnapshot = appfinalcardpatch.PatchSnapshot
)

func newFinalCardPatchTracker() *finalCardPatchTracker {
	return appfinalcardpatch.NewTracker()
}

// finalCardPatchService wraps the extracted service with thin delegation.
type finalCardPatchService struct {
	svc appfinalcardpatch.Service
}

func newFinalCardPatchService(app *App) finalCardPatchService {
	return finalCardPatchService{svc: appfinalcardpatch.NewService(app)}
}

func (s finalCardPatchService) registerFinalCardPatchState(messageID string, sub *state.Submission, title, color string, showHeader bool, body string, footerLines []string) {
	s.svc.RegisterFinalCardPatchState(messageID, sub, title, color, showHeader, body, footerLines)
}

func (s finalCardPatchService) markFinalCardPreviewPending(messageID string) bool {
	return s.svc.MarkFinalCardPreviewPending(messageID)
}

func (s finalCardPatchService) markFinalCardPreviewDone(messageID string) {
	s.svc.MarkFinalCardPreviewDone(messageID)
}

func (s finalCardPatchService) updateFinalCardPatchBody(messageID, body string) bool {
	return s.svc.UpdateFinalCardPatchBody(messageID, body)
}

func (s finalCardPatchService) updateFinalCardPatchFooterLines(messageID string, footerLines []string) bool {
	return s.svc.UpdateFinalCardPatchFooterLines(messageID, footerLines)
}
