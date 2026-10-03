package feishuapp

import (
	storagejson "feidex/internal/adapter/storage/json"
	applicationinteraction "feidex/internal/application/interaction"
	appsubmission "feidex/internal/application/submission"
	"feidex/internal/state"
)

func isPendingRequestOpen(req *state.PendingRequest) bool {
	return storagejson.IsPendingRequestOpen(req)
}

func InteractionPorts(a *App) applicationinteraction.Dependencies {
	store := a.State()
	return applicationinteraction.Dependencies{Repository: storagejson.InteractionRepository{
		Store: store.StateStore(), FrontendID: store.FrontendID(),
	}, Submissions: interactionSubmissionPort{
		SubmissionLookupService: a.bindings.SubmissionLookup,
		Store:                   store,
	}}
}

type interactionSubmissionPort struct {
	appsubmission.SubmissionLookupService
	Store interface{ SetSubmissionStatus(string, string) error }
}

func (p interactionSubmissionPort) SetSubmissionStatus(id, status string) error {
	return p.Store.SetSubmissionStatus(id, status)
}
