package feishu

import "strings"

const menuBackButtonText = "返回上一级"

// BackButtonsLast keeps menu back buttons at the end while preserving the
// relative order of all other buttons.
func BackButtonsLast(buttons []Button) []Button {
	if len(buttons) < 2 {
		return buttons
	}

	needReorder := false
	sawBack := false
	for _, btn := range buttons {
		if strings.TrimSpace(btn.Text) == menuBackButtonText {
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
		if strings.TrimSpace(btn.Text) != menuBackButtonText {
			reordered = append(reordered, btn)
		}
	}
	for _, btn := range buttons {
		if strings.TrimSpace(btn.Text) == menuBackButtonText {
			reordered = append(reordered, btn)
		}
	}
	return reordered
}
