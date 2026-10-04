package feishuapp

import (
	"fmt"

	frontendruntime "feidex/internal/runtime"
)

// runtimeView narrows the frontend aggregate to the runtime owner that owns
// the backend clients. Every helper in this cluster used to take *App and
// reach for a.runtimeOwner, so they now hang off this view instead and the
// port factories can capture the owner rather than the aggregate.
type runtimeView struct {
	owner *frontendruntime.FrontendOwner
}

func (a *App) runtimeView() runtimeView {
	if a == nil {
		return runtimeView{}
	}
	return runtimeView{owner: a.runtimeOwner}
}

// ensureRuntimeOwner returns the owner itself for the call sites that use it
// directly (lifecycle, recovery mutex, backend transition).
func (v runtimeView) ensureRuntimeOwner() *frontendruntime.FrontendOwner { return v.owner }

func (v runtimeView) getCodex() CodexClient {
	if v.owner == nil {
		return nil
	}
	return v.owner.CodexClient()
}

func (v runtimeView) setCodex(c CodexClient) {
	if v.owner == nil {
		return
	}
	v.owner.SetCodexClient(c)
}

func (v runtimeView) currentCodexClient() CodexClient { return v.getCodex() }

func (v runtimeView) requireCodexClient() (CodexClient, error) {
	client := v.getCodex()
	if client == nil {
		return nil, fmt.Errorf("codex client not initialized")
	}
	return client, nil
}

func (v runtimeView) currentClaudeCore() ClaudeCore {
	if v.owner == nil {
		return nil
	}
	return v.owner.ClaudeCore()
}

func (v runtimeView) setClaudeCore(core ClaudeCore) {
	if v.owner == nil {
		return
	}
	v.owner.SetClaudeCore(core)
}
