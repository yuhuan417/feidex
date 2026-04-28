package app

import "testing"

func TestCardActionHandlerSetsHaveUniqueKeys(t *testing.T) {
	sets := []struct {
		name     string
		handlers map[string]cardActionHandler
	}{
		{name: "menu", handlers: menuCardActionHandlers()},
		{name: "workspace", handlers: workspaceCardActionHandlers},
		{name: "maintenance", handlers: maintenanceCardActionHandlers},
		{name: "pending", handlers: pendingCardActionHandlers},
	}

	seen := map[string]string{}
	total := 0
	for _, set := range sets {
		total += len(set.handlers)
		for actionName := range set.handlers {
			if previous, exists := seen[actionName]; exists {
				t.Fatalf("duplicate card action %q in %s and %s", actionName, previous, set.name)
			}
			seen[actionName] = set.name
			if _, ok := cardActionHandlers()[actionName]; !ok {
				t.Fatalf("merged cardActionHandlers missing %q from %s", actionName, set.name)
			}
		}
	}
	if len(cardActionHandlers()) != total {
		t.Fatalf("merged cardActionHandlers size = %d, want %d unique handlers", len(cardActionHandlers()), total)
	}
}
