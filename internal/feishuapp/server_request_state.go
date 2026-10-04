package feishuapp

import (
	storagejson "feidex/internal/adapter/storage/json"
	appstate "feidex/internal/adapter/storage/json/scoped"
	applicationinteraction "feidex/internal/application/interaction"
	appsubmission "feidex/internal/application/submission"
	"feidex/internal/state"
)

func isPendingRequestOpen(req *state.PendingRequest) bool {
	return storagejson.IsPendingRequestOpen(req)
}

// InteractionPorts builds the interaction use case's dependencies from the two
// values it actually needs, instead of reaching into the frontend aggregate.
func InteractionPorts(store *appstate.Store, submissionLookup appsubmission.SubmissionLookupService) applicationinteraction.Dependencies {
	return applicationinteraction.Dependencies{Repository: storagejson.InteractionRepository{
		Store: store.StateStore(), FrontendID: store.FrontendID(),
	}, Submissions: interactionSubmissionPort{
		SubmissionLookupService: submissionLookup,
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
