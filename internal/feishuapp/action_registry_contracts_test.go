package feishuapp

import (
	"testing"

	appreviewcmd "feidex/internal/adapter/feishu/reviewcmd"
	appupgradecmd "feidex/internal/adapter/feishu/upgradecmd"
)

func TestCardActionHandlerSetsHaveUniqueKeys(t *testing.T) {
	sets := []struct {
		name     string
		handlers map[string]cardActionHandler
	}{
		{name: "menu", handlers: menuCardActionHandlers()},
		{name: "workspace", handlers: workspaceCardActionHandlers()},
		{name: "maintenance", handlers: maintenanceCardActionHandlers(appupgradecmd.UpgradeService{}, backendUpgradeService{})},
		{name: "pending", handlers: pendingCardActionHandlers(nil, appreviewcmd.ReviewFormService{})},
		{name: "server-request", handlers: serverRequestCardActionHandlers(nil)},
	}
	merged := make([]map[string]cardActionHandler, 0, len(sets))
	for _, set := range sets {
		merged = append(merged, set.handlers)
	}
	allHandlers := mergeCardActionHandlerSets(merged...)

	seen := map[string]string{}
	total := 0
	for _, set := range sets {
		total += len(set.handlers)
		for actionName := range set.handlers {
			if previous, exists := seen[actionName]; exists {
				t.Fatalf("duplicate card action %q in %s and %s", actionName, previous, set.name)
			}
			seen[actionName] = set.name
			if _, ok := allHandlers[actionName]; !ok {
				t.Fatalf("merged cardActionHandlers missing %q from %s", actionName, set.name)
			}
		}
	}
	if len(allHandlers) != total {
		t.Fatalf("merged card action handlers size = %d, want %d unique handlers", len(allHandlers), total)
	}
}
