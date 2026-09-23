package menuutil

import (
	"testing"

	"feidex/internal/app/menutypes"
)

func TestRenderGroupMenuButtonsPutsBackActionsLast(t *testing.T) {
	buttons := RenderGroupMenuButtons("menu.example", "session-1", func(string) []menutypes.MenuItemSpec {
		return []menutypes.MenuItemSpec{
			{GroupAction: "menu.example", Action: "menu.root", Label: "返回上一级", Kind: menutypes.MenuItemBack},
			{GroupAction: "menu.example", Action: "menu.first", Label: "First", Kind: menutypes.MenuItemDirect, Slash: "/first"},
			{GroupAction: "menu.example", Action: "menu.second", Label: "Second", Kind: menutypes.MenuItemSubmenu},
		}
	})

	if len(buttons) != 3 {
		t.Fatalf("rendered buttons = %#v, want 3 buttons", buttons)
	}
	if buttons[len(buttons)-1].Text != "返回上一级" {
		t.Fatalf("last button = %#v, want 返回上一级", buttons[len(buttons)-1])
	}
	if buttons[0].Text == "返回上一级" || buttons[1].Text == "返回上一级" {
		t.Fatalf("back button was rendered before another menu action: %#v", buttons)
	}
}
