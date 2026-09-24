package app

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	appcompact "feidex/internal/app/compact"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

func moduleRootForTest(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(".")
	if err != nil {
		t.Fatalf("Abs(.) error = %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("go.mod not found above %s", dir)
		}
		dir = parent
	}
}

// TestMenuBackButtonLabelsUseTheSharedLabel keeps a back control from being
// labelled after its destination again (返回模型配置, 返回工作区管理, ...). The
// label is a functional contract, not just copy: both orderings that keep the
// back control last match on it, so a renamed back button also silently stops
// being the final card action.
func TestMenuBackButtonLabelsUseTheSharedLabel(t *testing.T) {
	root := filepath.Join(moduleRootForTest(t), "internal", "app")
	fset := token.NewFileSet()
	scanned := 0
	var offenders []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, parseErr := parser.ParseFile(fset, path, nil, 0)
		if parseErr != nil {
			return parseErr
		}
		scanned++
		ast.Inspect(file, func(node ast.Node) bool {
			literal, ok := node.(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				return true
			}
			value, unquoteErr := strconv.Unquote(literal.Value)
			if unquoteErr != nil || !strings.HasPrefix(value, "返回") || value == feishu.MenuBackButtonText {
				return true
			}
			rel, _ := filepath.Rel(root, path)
			offenders = append(offenders, rel+":"+strconv.Itoa(fset.Position(literal.Pos()).Line)+" "+strconv.Quote(value))
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("scanning %s: %v", root, err)
	}
	if scanned == 0 {
		t.Fatalf("no Go files scanned under %s", root)
	}
	if len(offenders) > 0 {
		t.Fatalf("menu back controls must use feishu.MenuBackButtonText (%q) instead of naming the destination:\n  %s",
			feishu.MenuBackButtonText, strings.Join(offenders, "\n  "))
	}
}

func assertBackControlIsFinal(t *testing.T, name string, labels []string) {
	t.Helper()
	if len(labels) == 0 {
		t.Fatalf("%s: card has no buttons", name)
	}
	backCount := 0
	for i, label := range labels {
		if !feishu.IsMenuBackButtonText(label) {
			continue
		}
		backCount++
		if i != len(labels)-1 {
			t.Fatalf("%s: back control %q is at index %d of %d, want final", name, label, i, len(labels)-1)
		}
	}
	if backCount != 1 {
		t.Fatalf("%s: back control count = %d, want 1 (labels %q)", name, backCount, labels)
	}
}

func buttonLabelsForTest(card map[string]any) []string {
	labels := make([]string, 0, 4)
	for _, button := range cardButtonsForTest(card) {
		text, _ := button["text"].(map[string]any)
		label, _ := text["content"].(string)
		labels = append(labels, label)
	}
	return labels
}

// TestMenuBackControlIsTheFinalAction covers card families whose back control
// used to be labelled after its destination, so nothing was keeping it last.
func TestMenuBackControlIsTheFinalAction(t *testing.T) {
	a, _, _ := newTestApp(t)
	sessionKey := "sess-back-label"
	binding := &state.AgentBinding{
		ID:          defaultBindingID(a.frontendID, "group", "chat-1"),
		FrontendID:  a.frontendID,
		ChatID:      "chat-1",
		ChatType:    "group",
		WorkspaceID: "default",
		Status:      state.AgentBindingStatusActive.String(),
	}

	workspaceCard := newWorkspaceRenderServiceInner(a).RenderWorkspaceCloneSuccessCard(sessionKey, "ws-1", "/tmp/ws-1")
	assertBackControlIsFinal(t, "workspace clone success", buttonLabelsForTest(workspaceCard))

	quietCard := renderQuietModeMenuCard(a, sessionKey)
	assertBackControlIsFinal(t, "quiet mode", buttonLabelsForTest(quietCard))

	auxCard, err := newBindingService(a).renderBindingAuxiliaryModelConfigCard(sessionKey, binding)
	if err != nil {
		t.Fatalf("renderBindingAuxiliaryModelConfigCard() error = %v", err)
	}
	assertBackControlIsFinal(t, "auxiliary model config", buttonLabelsForTest(auxCard))

	compactButtons := appcompact.CompactMenuButtons(sessionKey, true)
	compactLabels := make([]string, 0, len(compactButtons))
	for _, button := range compactButtons {
		compactLabels = append(compactLabels, button.Text)
	}
	assertBackControlIsFinal(t, "compact", compactLabels)
}
