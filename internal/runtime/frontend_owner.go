package runtime

import (
	"sync"

	"feidex/internal/application"
	appautoretry "feidex/internal/runtime/autoretry"
	appcodexruntime "feidex/internal/runtime/codex"
)

// FrontendOwner contains the mutable runtime state that belongs to one
// frontend. It is deliberately limited to lifecycle, recovery, dispatch and
// backend client state; menu and product policy services remain composition
// concerns.
type FrontendOwner struct {
	SessionActors    *SessionActors
	LiveThreads      *LiveThreads
	AutoRetries      *appautoretry.Tracker
	CodexRecovery    *appcodexruntime.RecoveryState
	SubmissionStarts *SubmissionStarts
	EffectDeduper    EffectDeduper
	EffectRunner     *EffectRunner
	Dispatcher       *application.Dispatcher

	clientsMu sync.RWMutex
	codex     CodexClient
	claude    ClaudeCore
}

// NewFrontendOwner creates all frontend-scoped mutable runtime state through
// one construction path shared by production and tests.
func NewFrontendOwner() *FrontendOwner {
	return &FrontendOwner{
		SessionActors:    NewSessionActors(),
		LiveThreads:      NewLiveThreads(),
		AutoRetries:      appautoretry.NewTracker(),
		CodexRecovery:    appcodexruntime.NewRecoveryState(),
		SubmissionStarts: &SubmissionStarts{},
		EffectDeduper:    NewMemoryEffectDeduper(),
	}
}

func (o *FrontendOwner) CodexClient() CodexClient {
	if o == nil {
		return nil
	}
	o.clientsMu.RLock()
	defer o.clientsMu.RUnlock()
	return o.codex
}

func (o *FrontendOwner) SetCodexClient(client CodexClient) {
	if o == nil {
		return
	}
	o.clientsMu.Lock()
	o.codex = client
	o.clientsMu.Unlock()
}

func (o *FrontendOwner) ClaudeCore() ClaudeCore {
	if o == nil {
		return nil
	}
	o.clientsMu.RLock()
	defer o.clientsMu.RUnlock()
	return o.claude
}

func (o *FrontendOwner) SetClaudeCore(core ClaudeCore) {
	if o == nil {
		return
	}
	o.clientsMu.Lock()
	o.claude = core
	o.clientsMu.Unlock()
}
