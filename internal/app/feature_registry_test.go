package app

import (
	domainbackend "feidex/internal/domain/backend"
	"strings"
	"testing"

	appfeatures "feidex/internal/application/features"
)

func TestHiddenBackendFeaturesDoNotAppearInHelpOrMenus(t *testing.T) {
	a, _, _ := newTestApp(t)
	a.cfg.Feishu.Backend = domainbackend.BackendClaude
	setCodex(a, nil)
	setClaudeCore(a, &fakeClaudeCore{})
	sessionKey := "feishu:chat:chat"

	helpBody := renderHelpBodyFromRegistry(domainbackend.BackendClaude)
	menuButtonsByGroup := map[string]map[string]string{
		"menu.tools":       cardButtonLabelsByAction(renderToolsMenuCard(a, sessionKey)),
		"menu.group.model": cardButtonLabelsByAction(newBackendConfigurationService(a).renderModelMenuCard(sessionKey)),
		"menu.group.system": cardButtonLabelsByAction(
			renderSystemMenuCard(a, sessionKey),
		),
		"menu.group.backend": cardButtonLabelsByAction(
			renderBackendMenuCard(a, sessionKey),
		),
	}

	for _, spec := range appfeatures.All() {
		if spec.SupportsBackend(domainbackend.BackendClaude) {
			continue
		}
		for _, command := range spec.Commands {
			for _, entry := range command.HelpEntriesForBackend(domainbackend.BackendClaude) {
				if strings.Contains(helpBody, entry.Command) {
					t.Fatalf("Claude help should hide feature %q command %q", spec.ID, entry.Command)
				}
			}
			for _, entry := range command.HelpEntries {
				if strings.Contains(helpBody, entry.Command) {
					t.Fatalf("Claude help should hide unsupported feature %q command %q", spec.ID, entry.Command)
				}
			}
		}
		for _, item := range spec.MenuItems {
			if item.Kind == appfeatures.MenuItemBack {
				continue
			}
			labels := menuButtonsByGroup[item.GroupAction]
			if labels == nil {
				continue
			}
			if _, exists := labels[item.Action]; exists {
				t.Fatalf("Claude menu group %q should hide unsupported feature %q action %q", item.GroupAction, spec.ID, item.Action)
			}
		}
	}
}
