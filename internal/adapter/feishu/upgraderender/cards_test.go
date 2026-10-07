package upgraderender

import (
	"strings"
	"testing"

	"feidex/internal/feishu"
)

func cardMarkdownBody(card map[string]any) string {
	body, _ := card["body"].(map[string]any)
	elements, _ := body["elements"].([]map[string]any)
	for _, element := range elements {
		if content, ok := element["content"].(string); ok {
			return content
		}
	}
	return ""
}

func cardButtonLabels(card map[string]any) []string {
	body, _ := card["body"].(map[string]any)
	elements, _ := body["elements"].([]map[string]any)
	labels := make([]string, 0, 4)
	for _, element := range elements {
		columns, _ := element["columns"].([]map[string]any)
		for _, column := range columns {
			children, _ := column["elements"].([]map[string]any)
			for _, child := range children {
				if tag, _ := child["tag"].(string); tag != "button" {
					continue
				}
				text, _ := child["text"].(map[string]any)
				label, _ := text["content"].(string)
				labels = append(labels, label)
			}
		}
	}
	return labels
}

// TestUpgradeStatusCardIsTheDeclaredPage pins the page side of the upgrade
// surface: the management page claims its declared breadcrumb and ends with the
// back control the menu tree derives.
func TestUpgradeStatusCardIsTheDeclaredPage(t *testing.T) {
	for name, spec := range map[string]Spec{"codex": CodexSpec, "claude": ClaudeSpec} {
		card := RenderUpgradeStatusCard(spec, "sess-1", UpgradeView{}, false)
		if body := cardMarkdownBody(card); !strings.Contains(body, "当前位置：") {
			t.Fatalf("%s status card body = %q, want a declared breadcrumb", name, body)
		}
		labels := cardButtonLabels(card)
		if len(labels) == 0 || !feishu.IsMenuBackButtonText(labels[len(labels)-1]) {
			t.Fatalf("%s status card buttons = %q, want a final back control", name, labels)
		}
	}
}

// TestUpgradeOperationCardsAreStatusDisplays pins the decision that the states
// produced by an upgrade request are status displays rather than menu pages:
// they carry no breadcrumb and no back control.
func TestUpgradeOperationCardsAreStatusDisplays(t *testing.T) {
	cards := map[string]map[string]any{
		"preparing":       RenderUpgradePreparingCard(CodexSpec, ""),
		"canceled":        RenderUpgradePreparingCard(CodexSpec, "已取消升级"),
		"upgrade running": RenderUpgradeOperationCard(CodexSpec, "sess-1", BackendUpgradeSnapshot{Running: true}),
		"upgrade done":    RenderUpgradeOperationCard(CodexSpec, "sess-1", BackendUpgradeSnapshot{Result: "success"}),
		"restart running": RenderRestartOperationCard(CodexSpec, "sess-1", BackendRestartSnapshot{Running: true}),
		"restart done":    RenderRestartOperationCard(CodexSpec, "sess-1", BackendRestartSnapshot{Result: "success"}),
	}
	for name, card := range cards {
		if body := cardMarkdownBody(card); strings.Contains(body, "当前位置：") {
			t.Fatalf("%s card body = %q, want no breadcrumb", name, body)
		}
		for _, label := range cardButtonLabels(card) {
			if feishu.IsMenuBackButtonText(label) {
				t.Fatalf("%s card carries a back control %q; status displays offer none", name, label)
			}
		}
	}
}
