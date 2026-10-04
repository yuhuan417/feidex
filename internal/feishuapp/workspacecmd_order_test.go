package feishuapp

import (
	"testing"

	appbackend "feidex/internal/adapter/feishu/backend"
)

// Production composition builds WorkspaceConfiguration before
// BackendConfiguration (internal/composition/app.go). The workspace services
// used to read the backend configuration service at construction, which bound
// its zero value and panicked on the first workspace switch notice. They now
// read the notices from the active driver, so both construction orders must
// yield a working service.
func TestWorkspaceServiceDoesNotDependOnBackendConfigurationOrder(t *testing.T) {
	a, _, _ := newTestApp(t)

	saved := a.bindings.BackendConfiguration
	a.bindings.BackendConfiguration = appbackend.ConfigurationService{}
	early := BuildWorkspaceConfiguration(a)
	a.bindings.BackendConfiguration = saved

	late := BuildWorkspaceConfiguration(a)

	for name, svc := range map[string]interface {
		BackendWorkspaceSwitchInFlightNotice() string
		BackendWorkspaceCommandUsage() string
	}{"before BackendConfiguration": early, "after BackendConfiguration": late} {
		if got := svc.BackendWorkspaceSwitchInFlightNotice(); got == "" {
			t.Fatalf("%s: in-flight notice is empty", name)
		}
		if got := svc.BackendWorkspaceCommandUsage(); got == "" {
			t.Fatalf("%s: workspace command usage is empty", name)
		}
	}
}
