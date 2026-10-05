package feishuapp

import (
	appfeishuwrap "feidex/internal/adapter/feishu/feishuwrap"
	"feidex/internal/application"
	"feidex/internal/domain/identity"
	"feidex/internal/runtime"
	"feidex/internal/state"
)

type BackendRuntimeHandle = runtime.BackendHandle

// NewEffectRunner creates the frontend effect executor after the shell and its
// transport have been composed.
func NewEffectRunner(inputs EffectRunnerInputs) runtime.EffectRunner {
	return buildEffectRunner(inputs)
}

// NewDispatcher creates the application input dispatcher for this frontend.
func NewDispatcher(inputs DispatcherInputs) application.Dispatcher { return newInputDispatcher(inputs) }

// CanonicalizeStoredSessionKeys performs the one-time state migration required
// when a frontend enters the runtime.
func CanonicalizeStoredSessionKeys(store *state.Store) error {
	return canonicalizeStoredSessionKeys(store)
}

func BuildBackendRuntimeHandle(deps BackendRuntimeDeps, target string) (*BackendRuntimeHandle, error) {
	return buildBackendRuntimeHandle(deps, target)
}

func InstallBackendRuntime(deps BackendRuntimeDeps, handle *BackendRuntimeHandle) {
	if handle != nil {
		installBackendRuntime(deps, handle)
	}
}

func AttachEffectRuntime(owner *runtime.FrontendOwner, client FeishuClient, frontendID string, runner runtime.EffectRunner) FeishuClient {
	if owner != nil {
		owner.EffectRunner = &runner
	}
	if notifying, ok := client.(*appfeishuwrap.NotifyingFeishuClient); ok {
		return &appfeishuwrap.EffectClient{
			NotifyingFeishuClient: notifying,
			Frontend:              identity.FrontendID(frontendID),
			Runner:                runner,
		}
	}
	return client
}

// InstallFeishuPolicies installs the Feishu-specific routing policies after
// composition has created the application/runtime graph. Event callback
// binding itself lives in internal/app's thin boundary.
func InstallFeishuPolicies(inputs FeishuPolicyInputs) {
	configureGroupMessagePolicy(inputs.Client, inputs.GroupMessages)
	configureGroupPrimaryEvents(inputs)
}
