package app

import (
	"strings"
	"testing"
)

func toolUserInputFormForTest(_ *testing.T, card map[string]any) map[string]any {
	for _, elem := range cardElements(card) {
		if tag, _ := elem["tag"].(string); tag == "form" {
			name, _ := elem["name"].(string)
			if name == "tool_user_input_form" {
				return elem
			}
		}
	}
	return nil
}

func toolUserInputFormInputsForTest(_ *testing.T, form map[string]any) map[string]map[string]any {
	elements, _ := form["elements"].([]map[string]any)
	inputs := make(map[string]map[string]any)
	for _, elem := range elements {
		if tag, _ := elem["tag"].(string); tag != "input" {
			continue
		}
		name, _ := elem["name"].(string)
		inputs[name] = elem
	}
	return inputs
}

func toolUserInputFormSelectsForTest(_ *testing.T, form map[string]any) map[string]map[string]any {
	elements, _ := form["elements"].([]map[string]any)
	selects := make(map[string]map[string]any)
	for _, elem := range elements {
		if tag, _ := elem["tag"].(string); tag != "select_static" {
			continue
		}
		name, _ := elem["name"].(string)
		selects[name] = elem
	}
	return selects
}

func toolUserInputFormButtonsForTest(_ *testing.T, form map[string]any) map[string]map[string]any {
	elements, _ := form["elements"].([]map[string]any)
	buttons := make(map[string]map[string]any)
	for _, elem := range elements {
		if tag, _ := elem["tag"].(string); tag != "column_set" {
			continue
		}
		columns, _ := elem["columns"].([]map[string]any)
		for _, column := range columns {
			columnElems, _ := column["elements"].([]map[string]any)
			for _, child := range columnElems {
				if tag, _ := child["tag"].(string); tag != "button" {
					continue
				}
				name, _ := child["name"].(string)
				buttons[name] = child
			}
		}
	}
	return buttons
}

func toolUserInputToggleButtonsForTest(_ *testing.T, form map[string]any) []map[string]any {
	all := toolUserInputFormButtonsForTest(nil, form)
	out := make([]map[string]any, 0, len(all))
	for name, button := range all {
		if strings.HasPrefix(name, "toggle_") {
			out = append(out, button)
		}
	}
	return out
}
