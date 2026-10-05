package feishuapp

import (
	"testing"

	history "feidex/internal/adapter/feishu/history"
	"feidex/internal/adapter/feishu/planmode"
	appreviewcmd "feidex/internal/adapter/feishu/reviewcmd"
	"feidex/internal/adapter/feishu/workspacecmd"
)

func TestCardActionHandlerSetsHaveUniqueKeys(t *testing.T) {
	appSets := []struct {
		name     string
		handlers map[string]cardActionHandler
	}{
		{name: "menu", handlers: menuCardActionHandlers()},
	}
	portSets := []struct {
		name     string
		handlers map[string]cardActionPortHandler
	}{
		{name: "workspace", handlers: workspaceCardActionHandlers(WorkspaceCardActionInputs{})},
		{name: "maintenance", handlers: maintenanceCardActionHandlers(MaintenanceCardActionInputs{})},
		{name: "system", handlers: systemCardActionHandlers(SystemCardActionInputs{})},
		{name: "menu-core", handlers: menuCoreCardActionHandlers(MenuCoreCardActionInputs{})},
		{name: "binding", handlers: bindingCardActionHandlers(BindingCardActionInputs{})},
		{name: "tools", handlers: toolsCardActionHandlers(ToolsCardActionInputs{})},
		{name: "thread-fork", handlers: threadForkCardActionHandlers(ThreadForkCardActionInputs{})},
		{name: "pending-ports", handlers: pendingPortCardActionHandlers(nil, nil, appreviewcmd.ReviewFormService{})},
		{name: "pending-plan-exit-ports", handlers: pendingPlanModeExitPortCardActionHandlers(planmode.Dependencies{})},
		{name: "workspace-delete-ports", handlers: workspaceDeletePortCardActionHandlers(workspacecmd.WorkspaceDeleteActions{})},
		{name: "history-ports", handlers: historyCardActionHandlers(history.Service{})},
		{name: "server-request-ports", handlers: serverRequestCardActionHandlers(nil)},
		{name: "path-picker-ports", handlers: pathPickerActionHandlers(PathPickerActionInputs{})},
		{name: "thread-menu-ports", handlers: threadMenuPortCardActionHandlers(nil)},
		{name: "async-user-input-ports", handlers: asyncUserInputPortCardActionHandlers(AsyncUserInputActionInputs{})},
		{name: "pending-form-cancel-ports", handlers: pendingFormCancelPortCardActionHandlers(PendingFormCancelActionInputs{})},
	}
	appMaps := make([]map[string]cardActionHandler, 0, len(appSets))
	for _, set := range appSets {
		appMaps = append(appMaps, set.handlers)
	}
	allAppHandlers := mergeCardActionHandlerSets(appMaps...)
	portMaps := make([]map[string]cardActionPortHandler, 0, len(portSets))
	for _, set := range portSets {
		portMaps = append(portMaps, set.handlers)
	}
	allPortHandlers := mergeCardActionPortHandlerSets(portMaps...)

	seen := map[string]string{}
	total := 0
	register := func(setName string, actionNames []string) {
		for _, actionName := range actionNames {
			if previous, exists := seen[actionName]; exists {
				t.Fatalf("duplicate card action %q in %s and %s", actionName, previous, setName)
			}
			seen[actionName] = setName
		}
	}
	for _, set := range appSets {
		total += len(set.handlers)
		for actionName := range set.handlers {
			register(set.name, []string{actionName})
			if _, ok := allAppHandlers[actionName]; !ok {
				t.Fatalf("merged app handlers missing %q from %s", actionName, set.name)
			}
		}
	}
	for _, set := range portSets {
		total += len(set.handlers)
		for actionName := range set.handlers {
			register(set.name, []string{actionName})
			if _, ok := allPortHandlers[actionName]; !ok {
				t.Fatalf("merged port handlers missing %q from %s", actionName, set.name)
			}
		}
	}
	if len(allAppHandlers)+len(allPortHandlers) != total {
		t.Fatalf("merged card action handlers size = %d, want %d unique handlers", len(allAppHandlers)+len(allPortHandlers), total)
	}
}
