package runtime

import (
	"context"
	"sync"

	"feidex/internal/application"
	appautoretry "feidex/internal/application/autoretry"
	appcodexruntime "feidex/internal/runtime/codex"
	"feidex/internal/runtime/skill"
	"feidex/internal/runtime/turnbinding"
	"feidex/internal/runtime/workspace"
)

// FrontendOwner contains the mutable runtime state that belongs to one
// frontend. It is deliberately limited to lifecycle, recovery, dispatch and
// backend client state; menu and product policy services remain composition
// concerns.
type FrontendOwner struct {
	BackendTransition   BackendTransition
	TurnBindings        *turnbinding.Tracker
	WorkspaceCloneOps   *workspace.CloneTracker
	PendingSkills       *skill.Tracker
	MaintenanceTrackers TrackerMap
	Lifecycle           FrontendRuntime
	RecoveryMu          sync.Mutex
	InboundDeduper      *InboundDeduper
	SessionActors       *SessionActors
	LiveThreads         *LiveThreads
	AutoRetries         *appautoretry.Tracker
	CodexRecovery       *appcodexruntime.RecoveryState
	SubmissionStarts    *SubmissionStarts
	EffectDeduper       EffectDeduper
	EffectRunner        *EffectRunner
	Dispatcher          *application.Dispatcher
	Announcements       *CoalescedRefresh
	MCP                 *Resource

	clientsMu      sync.RWMutex
	claude         ClaudeCore
	backend        string
	trafficMu      sync.Mutex
	messageTraffic int
}

func (o *FrontendOwner) Shutdown(ctx context.Context, stopTransport func()) error {
	o.Lifecycle.Cancel()
	if o.Announcements != nil {
		o.Announcements.Stop()
	}
	stopTransport()
	backendErr := (&BackendHandle{Backend: o.Backend(), Codex: o.CodexClient(), Claude: o.ClaudeCore()}).Close()
	var mcpErr error
	if o.MCP != nil {
		mcpErr = o.MCP.Stop(ctx)
	}
	if err := o.Lifecycle.Wait(ctx); err != nil {
		return err
	}
	if backendErr != nil {
		return backendErr
	}
	return mcpErr
}

// NewFrontendOwner creates all frontend-scoped mutable runtime state through
// one construction path shared by production and tests.
func NewFrontendOwner() *FrontendOwner {
	return &FrontendOwner{
		WorkspaceCloneOps:   workspace.NewCloneTracker(),
		PendingSkills:       skill.NewTracker(),
		MaintenanceTrackers: TrackerMap{BackendKeyCodex: NewMaintenanceTracker(), BackendKeyClaude: NewMaintenanceTracker()},
		SessionActors:       NewSessionActors(),
		LiveThreads:         NewLiveThreads(),
		AutoRetries:         appautoretry.NewTracker(ScheduleDelayedTask),
		CodexRecovery:       appcodexruntime.NewRecoveryState(),
		SubmissionStarts:    &SubmissionStarts{},
		EffectDeduper:       NewMemoryEffectDeduper(),
		InboundDeduper:      NewInboundDeduper(),
	}
}

func (o *FrontendOwner) BeginMessageTraffic() {
	o.trafficMu.Lock()
	o.messageTraffic++
	o.trafficMu.Unlock()
}

func (o *FrontendOwner) EndMessageTraffic() {
	o.trafficMu.Lock()
	if o.messageTraffic > 0 {
		o.messageTraffic--
	}
	o.trafficMu.Unlock()
}

func (o *FrontendOwner) MessageTraffic() int {
	o.trafficMu.Lock()
	defer o.trafficMu.Unlock()
	return o.messageTraffic
}

func (o *FrontendOwner) CodexClient() CodexClient {
	if o == nil {
		return nil
	}
	client, _ := o.CodexRecovery.CurrentClient().(CodexClient)
	return client
}

func (o *FrontendOwner) SetCodexClient(client CodexClient) {
	if o == nil {
		return
	}
	o.CodexRecovery.ReplaceClient(client)
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

func (o *FrontendOwner) Backend() string {
	o.clientsMu.RLock()
	defer o.clientsMu.RUnlock()
	return o.backend
}
func (o *FrontendOwner) SetBackend(kind string) {
	o.clientsMu.Lock()
	defer o.clientsMu.Unlock()
	o.backend = kind
}
