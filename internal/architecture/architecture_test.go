package architecture

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

const modulePath = "feidex"

func TestTargetPackagesDoNotReintroduceGodInterfaces(t *testing.T) {
	root := repositoryRoot(t)
	forbidden := map[string][]string{"internal/application": {"appcore.AppConfig", "appcore.AppExtended", "type App struct"}, "internal/domain": {"type App struct", "internal/adapter", "internal/app"}, "internal/composition": {"internal/app/appstate"}}
	for relative, patterns := range forbidden {
		violations, err := textMatches(root, relative, patterns)
		if err != nil {
			t.Fatal(err)
		}
		if len(violations) > 0 {
			t.Fatalf("%s contains forbidden host capabilities: %v", relative, violations)
		}
	}
}

func TestTargetPackagesUseCapabilityFieldsInsteadOfAppPointers(t *testing.T) {
	root := repositoryRoot(t)
	for _, relative := range []string{"internal/application", "internal/domain", "internal/adapter", "internal/runtime"} {
		base := filepath.Join(root, relative)
		err := filepath.Walk(base, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() || !strings.HasSuffix(path, ".go") {
				return nil
			}
			file, parseErr := parser.ParseFile(token.NewFileSet(), path, nil, 0)
			if parseErr != nil {
				return parseErr
			}
			ast.Inspect(file, func(node ast.Node) bool {
				field, ok := node.(*ast.Field)
				if !ok || field.Type == nil {
					return true
				}
				if selectorContainsApp(field.Type) {
					t.Errorf("%s: capability field embeds a concrete App type", path)
				}
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

func selectorContainsApp(expr ast.Expr) bool {
	found := false
	ast.Inspect(expr, func(node ast.Node) bool {
		selector, ok := node.(*ast.SelectorExpr)
		if ok && selector.Sel != nil && (selector.Sel.Name == "App" || selector.Sel.Name == "AppConfig" || selector.Sel.Name == "AppExtended") {
			found = true
		}
		return !found
	})
	return found
}
func textMatches(root, relative string, patterns []string) ([]string, error) {
	var out []string
	err := filepath.Walk(filepath.Join(root, relative), func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		for _, pattern := range patterns {
			if strings.Contains(string(data), pattern) {
				out = append(out, path+" contains "+pattern)
			}
		}
		return nil
	})
	sort.Strings(out)
	return out, err
}

func TestLayerImportRules(t *testing.T) {
	root := repositoryRoot(t)
	rules := []struct {
		prefix string
		banned []string
	}{
		{prefix: filepath.Join("internal", "domain"), banned: []string{
			modulePath + "/internal/application",
			modulePath + "/internal/adapter",
			modulePath + "/internal/runtime",
			modulePath + "/internal/app",
			modulePath + "/internal/feishu",
			modulePath + "/internal/codexrpc",
			modulePath + "/internal/claudecli",
			modulePath + "/internal/state",
			modulePath + "/internal/config",
		}},
		{prefix: filepath.Join("internal", "application"), banned: []string{
			modulePath + "/internal/adapter",
			modulePath + "/internal/runtime",
			modulePath + "/internal/state",
			modulePath + "/internal/config",
			modulePath + "/internal/app",
			modulePath + "/internal/feishu",
			modulePath + "/internal/codexrpc",
			modulePath + "/internal/claudecli",
		}},
		{prefix: filepath.Join("internal", "adapter"), banned: []string{
			modulePath + "/internal/app",
		}},
		{prefix: filepath.Join("internal", "feishu"), banned: []string{
			modulePath + "/internal/app",
		}},
	}

	for _, rule := range rules {
		rule := rule
		t.Run(filepath.ToSlash(rule.prefix), func(t *testing.T) {
			violations, err := importsUnder(root, rule.prefix, rule.banned)
			if err != nil {
				t.Fatal(err)
			}
			if len(violations) > 0 {
				t.Fatalf("forbidden imports:\n%s", strings.Join(violations, "\n"))
			}
		})
	}
}

func TestMigratedCommandPackagesDoNotImportFeishuTransport(t *testing.T) {
	root := repositoryRoot(t)
	packages := []string{
		"internal/app/goalcmd",
		"internal/app/skillscmd",
		"internal/app/reviewcmd",
		"internal/app/planmode",
		"internal/app/upgradecmd",
		"internal/app/debugviewcmd",
		"internal/app/threadmenu",
		"internal/app/workspacecmd",
	}
	for _, relative := range packages {
		base := filepath.Join(root, relative)
		err := filepath.Walk(base, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			data, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			source := string(data)
			if strings.Contains(source, "internal/adapter/feishu/transport") {
				t.Errorf("%s imports the Feishu transport directly", path)
			}
			for _, pattern := range []string{".Feishu().Reply", ".Feishu().Send", ".Feishu().Patch", ".FeishuClient().Reply", ".FeishuClient().Send", ".FeishuClient().Patch"} {
				if strings.Contains(source, pattern) {
					t.Errorf("%s calls transport outbound method %s directly", path, pattern)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestApplicationDoesNotCallSynchronousOutboundPorts(t *testing.T) {
	root := repositoryRoot(t)
	base := filepath.Join(root, "internal", "application")
	forbidden := []string{
		".ReplyCard(",
		".SendCard(",
		".PatchCard(",
		".ReplyMessage(",
		".SendMessage(",
		".PatchMessage(",
	}
	err := filepath.Walk(base, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		source := string(data)
		for _, pattern := range forbidden {
			if strings.Contains(source, pattern) {
				t.Errorf("%s calls synchronous outbound method %s; emit an application effect instead", path, pattern)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestApplicationBackendEventServiceUsesOneSinkPort(t *testing.T) {
	root := repositoryRoot(t)
	path := filepath.Join(root, "internal", "application", "backendevents", "service.go")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	source := string(data)
	if strings.Contains(source, "ItemStarted          func(") || strings.Contains(source, "InteractionRequested func(") {
		t.Fatalf("backend event service reintroduced per-event callback fields")
	}
	if !strings.Contains(source, "type EventSink interface") || !strings.Contains(source, "Sink EventSink") {
		t.Fatalf("backend event service must expose one explicit EventSink port")
	}
}

func TestApplicationContinuationUsesOneDependencyCarrier(t *testing.T) {
	root := repositoryRoot(t)
	path := filepath.Join(root, "internal", "application", "continuation", "service.go")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	source := string(data)
	if !strings.Contains(source, "type Dependencies struct") || !strings.Contains(source, "type Service struct{ Deps Dependencies }") {
		t.Fatalf("reply continuation must expose one explicit dependency carrier")
	}
	if strings.Contains(source, "type Service struct {") {
		t.Fatalf("reply continuation reintroduced a flat callback service carrier")
	}
}

func TestApplicationLifecycleServicesUseDependencyCarriers(t *testing.T) {
	root := repositoryRoot(t)
	checks := []struct {
		path string
		want string
	}{
		{"internal/application/compaction/service.go", "type Service struct{ Deps Dependencies }"},
		{"internal/application/conversation/service.go", "type Service struct{ Deps Dependencies }"},
		{"internal/application/interaction/service.go", "type Service struct{ Deps Dependencies }"},
		{"internal/application/asyncinput/service.go", "type Service struct{ Deps Dependencies }"},
	}
	for _, check := range checks {
		data, err := os.ReadFile(filepath.Join(root, check.path))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), check.want) {
			t.Fatalf("%s must expose a grouped Dependencies carrier", check.path)
		}
	}
}

func TestBackendEventsExposeTypedSemanticPayloads(t *testing.T) {
	root := repositoryRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "internal", "application", "input.go"))
	if err != nil {
		t.Fatal(err)
	}
	source := string(data)
	if strings.Contains(source, "Payload any") {
		t.Fatal("BackendEvent must not expose an untyped payload escape hatch")
	}
	for _, field := range []string{"Item", "Usage", "Goal"} {
		if !strings.Contains(source, field) {
			t.Fatalf("BackendEvent missing typed semantic field %s", field)
		}
	}
	for _, typ := range []string{"*turn.ProtocolItem", "*turn.ThreadTokenUsage", "*conversation.ThreadGoal"} {
		if !strings.Contains(source, typ) {
			t.Fatalf("BackendEvent missing typed semantic payload type %s", typ)
		}
	}
}

func TestApplicationCardActionsDoNotExposeSDKMaps(t *testing.T) {
	root := repositoryRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "internal", "application", "input.go"))
	if err != nil {
		t.Fatal(err)
	}
	source := string(data)
	if strings.Contains(source, "ActionValue map[string]any") || strings.Contains(source, "FormValue   map[string]any") {
		t.Fatal("application CardAction must use typed Values instead of SDK maps")
	}
	if !strings.Contains(source, "ActionValue Values") || !strings.Contains(source, "FormValue   Values") {
		t.Fatal("application CardAction must expose typed callback values")
	}
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	// The test package lives at internal/architecture.
	return filepath.Clean(filepath.Join(root, "..", ".."))
}

func importsUnder(root, relative string, banned []string) ([]string, error) {
	var violations []string
	base := filepath.Join(root, relative)
	err := filepath.Walk(base, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		fset := token.NewFileSet()
		file, parseErr := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if parseErr != nil {
			return parseErr
		}
		for _, spec := range file.Imports {
			importPath, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				return err
			}
			for _, prefix := range banned {
				if importPath == prefix || strings.HasPrefix(importPath, prefix+"/") {
					violations = append(violations, fmt.Sprintf("%s imports %s", path, importPath))
					break
				}
			}
		}
		return nil
	})
	sort.Strings(violations)
	return violations, err
}
