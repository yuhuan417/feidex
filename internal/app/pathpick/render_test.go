package pathpick

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderCardShowsDropdownAndShortButtons(t *testing.T) {
	root := t.TempDir()
	current := filepath.Join(root, "work")
	if err := os.MkdirAll(filepath.Join(current, "dir-a"), 0o755); err != nil {
		t.Fatalf("Mkdir(dir-a) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(current, "file-a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatalf("WriteFile(file-a.txt) error = %v", err)
	}

	card, err := RenderCard("path-1", Payload{
		Mode:        ModeDirectory,
		Style:       StyleDropdown,
		RootPath:    root,
		CurrentPath: current,
	})
	if err != nil {
		t.Fatalf("RenderCard() error = %v", err)
	}
	if !testCardHasTag(card, "select_static") {
		t.Fatalf("path picker card missing select_static: %#v", card)
	}

	body := testCardMarkdownContent(t, card)
	for _, want := range []string{"浏览根目录", "当前目录", "已隐藏文件: `1`"} {
		if !strings.Contains(body, want) {
			t.Fatalf("path picker body = %q, want %q", body, want)
		}
	}
	for _, want := range []string{"上一级", "确认", "取消"} {
		if !testCardHasButtonText(card, want) {
			t.Fatalf("path picker missing button %q", want)
		}
	}
}

func testCardHasTag(card map[string]any, wantTag string) bool {
	for _, elem := range testCardElements(card) {
		if tag, _ := elem["tag"].(string); tag == wantTag {
			return true
		}
		if actions, ok := elem["actions"].([]map[string]any); ok {
			for _, action := range actions {
				if tag, _ := action["tag"].(string); tag == wantTag {
					return true
				}
			}
		}
		if columns, ok := elem["columns"].([]map[string]any); ok {
			for _, column := range columns {
				columnElems, _ := column["elements"].([]map[string]any)
				for _, child := range columnElems {
					if tag, _ := child["tag"].(string); tag == wantTag {
						return true
					}
				}
			}
		}
	}
	return false
}

func testCardHasButtonText(card map[string]any, want string) bool {
	for _, elem := range testCardElements(card) {
		if actions, ok := elem["actions"].([]map[string]any); ok {
			for _, action := range actions {
				text, _ := action["text"].(map[string]any)
				if content, _ := text["content"].(string); content == want {
					return true
				}
			}
		}
		if columns, ok := elem["columns"].([]map[string]any); ok {
			for _, column := range columns {
				columnElems, _ := column["elements"].([]map[string]any)
				for _, child := range columnElems {
					text, _ := child["text"].(map[string]any)
					if content, _ := text["content"].(string); content == want {
						return true
					}
				}
			}
		}
	}
	return false
}

func testCardMarkdownContent(t *testing.T, card map[string]any) string {
	t.Helper()
	elements := testCardElements(card)
	if len(elements) == 0 {
		t.Fatalf("unexpected card elements: %#v", card)
	}
	var parts []string
	for _, elem := range elements {
		if content, ok := elem["content"].(string); ok {
			parts = append(parts, content)
			continue
		}
		if text, ok := elem["text"].(map[string]any); ok {
			if content, ok := text["content"].(string); ok {
				parts = append(parts, content)
			}
		}
	}
	return strings.Join(parts, "\n")
}

func testCardElements(card map[string]any) []map[string]any {
	if elements, ok := card["elements"].([]map[string]any); ok {
		return elements
	}
	body, _ := card["body"].(map[string]any)
	elements, _ := body["elements"].([]map[string]any)
	return elements
}
