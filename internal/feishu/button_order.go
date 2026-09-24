package feishu

import "strings"

// MenuBackButtonText is the only label a menu control may use when it returns to
// the previous menu level. Menu cards must not name the destination level
// instead of the shared label: the breadcrumb already shows where the control
// leads, and both back-button orderings below match on this exact text.
const MenuBackButtonText = "返回上一级"

// IsMenuBackButtonText reports whether text is the shared menu back label.
func IsMenuBackButtonText(text string) bool {
	return strings.TrimSpace(text) == MenuBackButtonText
}

// BackButtonsLast keeps menu back buttons at the end while preserving the
// relative order of all other buttons.
func BackButtonsLast(buttons []Button) []Button {
	if len(buttons) < 2 {
		return buttons
	}

	needReorder := false
	sawBack := false
	for _, btn := range buttons {
		if IsMenuBackButtonText(btn.Text) {
			sawBack = true
			continue
		}
		if sawBack {
			needReorder = true
			break
		}
	}
	if !needReorder {
		return buttons
	}

	reordered := make([]Button, 0, len(buttons))
	for _, btn := range buttons {
		if !IsMenuBackButtonText(btn.Text) {
			reordered = append(reordered, btn)
		}
	}
	for _, btn := range buttons {
		if IsMenuBackButtonText(btn.Text) {
			reordered = append(reordered, btn)
		}
	}
	return reordered
}
