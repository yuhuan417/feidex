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

func TestAutoRetryPolicyHasOneApplicationOwner(t *testing.T) {
	root := repositoryRoot(t)
	for _, path := range []string{"internal/runtime/autoretry/engine.go", "internal/runtime/autoretry/state.go"} {
		if _, err := os.Stat(filepath.Join(root, path)); !os.IsNotExist(err) {
			t.Fatalf("removed retry policy returned: %s", path)
		}
	}
	violations, err := importsUnder(root, "internal/application/autoretry", []string{
		modulePath + "/internal/runtime", modulePath + "/internal/adapter",
		modulePath + "/internal/config", modulePath + "/internal/state",
		modulePath + "/internal/feishu", modulePath + "/internal/feishuapp",
		modulePath + "/internal/codexrpc", modulePath + "/internal/claudecli",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) > 0 {
		t.Fatalf("retry policy imports concrete boundary: %v", violations)
	}
	file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(root, "internal/adapter/feishu/autoretry/service.go"), nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	ast.Inspect(file, func(node ast.Node) bool {
		if call, ok := node.(*ast.CallExpr); ok {
			if selector, ok := call.Fun.(*ast.SelectorExpr); ok && selector.Sel.Name == "CancelAllAutoRetry" {
				t.Error("retry adapter must delegate disable/cancel policy to the application owner")
			}
		}
		return true
	})
}

func TestRemovedRuntimeAndServiceFacadesCannotReturn(t *testing.T) {
	root := repositoryRoot(t)
	for _, path := range []string{"internal/runtime/registry.go", "internal/feishuapp/backend_state_service.go", "internal/feishuapp/turn_binding.go", "internal/feishuapp/turn_item_state.go", "internal/adapter/feishu/backend/failure.go", "internal/adapter/feishu/backend/transition_state.go"} {
		if _, err := os.Stat(filepath.Join(root, path)); !os.IsNotExist(err) {
			t.Fatalf("removed facade returned: %s", path)
		}
	}
	entries, err := filepath.Glob(filepath.Join(root, "internal/feishuapp/*.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range entries {
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range file.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok {
				for _, forbidden := range []string{"newRuntimeStateService", "newSubmissionQueueServiceFromApp", "newConversationService", "newTurnLifecycleService", "newPendingQueueService", "newTurnStreamService", "newBackendSelectionService"} {
					if fn.Name.Name == forbidden {
						t.Fatalf("%s reintroduced %s", path, forbidden)
					}
				}
			}
			gen, ok := decl.(*ast.GenDecl)
			if !ok {
				continue
			}
			for _, spec := range gen.Specs {
				typ, ok := spec.(*ast.TypeSpec)
				if !ok || typ.Name.Name != "App" {
					continue
				}
				structure, ok := typ.Type.(*ast.StructType)
				if !ok {
					continue
				}
				for _, field := range structure.Fields.List {
					for _, name := range field.Names {
						if name.Name == "backend" || name.Name == "switchState" || name.Name == "trackers" {
							t.Fatalf("App holds runtime state: %s", name.Name)
						}
					}
				}
			}
		}
	}
}

func TestMigratedLocalFormsUseInteractionOwner(t *testing.T) {
	root := repositoryRoot(t)
	for _, path := range []string{"internal/feishuapp/binding_workspace_actions.go", "internal/feishuapp/path_picker_actions.go", "internal/feishuapp/upgrade_actions.go", "internal/feishuapp/backend_upgrade.go", "internal/adapter/feishu/workspacecmd/management_service.go"} {
		file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(root, path), nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			if call, ok := n.(*ast.CallExpr); ok {
				if selector, ok := call.Fun.(*ast.SelectorExpr); ok && selector.Sel.Name == "UpdatePending" {
					t.Errorf("%s bypasses local form owner", path)
				}
			}
			return true
		})
	}
}

func TestNewBusinessOwnersRemainProtocolIndependent(t *testing.T) {
	root := repositoryRoot(t)
	for _, path := range []string{"internal/application/backendfailure", "internal/application/backendselection", "internal/application/plan", "internal/application/review", "internal/application/goal", "internal/application/inbound", "internal/application/interaction", "internal/application/workspace"} {
		violations, err := importsUnder(root, path, []string{modulePath + "/internal/adapter", modulePath + "/internal/feishu", modulePath + "/internal/feishuapp", modulePath + "/internal/codexrpc", modulePath + "/internal/runtime", modulePath + "/internal/state"})
		if err != nil {
			t.Fatal(err)
		}
		if len(violations) > 0 {
			t.Errorf("business owner imports concrete boundary: %v", violations)
		}
	}
}

func TestDeletedHostBridgesCannotReturn(t *testing.T) {
	root := repositoryRoot(t)
	for _, relative := range []string{"internal/app/appcore", "internal/app/workspace", "internal/app/commandmatch", "internal/app/skillscmd"} {
		if _, err := os.Stat(filepath.Join(root, relative)); !os.IsNotExist(err) {
			t.Fatalf("deleted host bridge must remain absent: %s", relative)
		}
		violations, err := importsUnder(root, "internal", []string{modulePath + "/" + relative})
		if err != nil {
			t.Fatal(err)
		}
		if len(violations) != 0 {
			t.Fatalf("deleted host bridge dependencies must not return: %v", violations)
		}
	}
}

func TestInternalAppIsOnlyTheFeishuThinBoundary(t *testing.T) {
	root := repositoryRoot(t)
	appRoot := filepath.Join(root, "internal", "app")
	entries, err := os.ReadDir(appRoot)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			continue
		}
		if entry.Name() != "doc.go" && entry.Name() != "entrypoint.go" {
			t.Fatalf("internal/app must remain a thin Feishu boundary; found %s", entry.Name())
		}
	}
	violations, err := importsUnder(root, "internal/app", []string{
		modulePath + "/internal/application",
		modulePath + "/internal/config",
		modulePath + "/internal/runtime",
		modulePath + "/internal/state",
		modulePath + "/internal/codexrpc",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) != 0 {
		t.Fatalf("internal/app must not own application/runtime construction: %v", violations)
	}
}

func TestModelSettingsRendererDoesNotReadConfigurationOrSessionState(t *testing.T) {
	violations, err := importsUnder(repositoryRoot(t), "internal/adapter/feishu/modelsettings", []string{
		modulePath + "/internal/config", modulePath + "/internal/state", modulePath + "/internal/adapter/storage",
		modulePath + "/internal/domain/conversation", modulePath + "/internal/domain/routing",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) != 0 {
		t.Fatalf("model renderer must consume application views: %v", violations)
	}
}

func TestHistoryAdapterAndUseCaseBoundaries(t *testing.T) {
	root := repositoryRoot(t)
	violations, err := importsUnder(root, "internal/adapter/feishu/history", []string{
		modulePath + "/internal/adapter/backend",
		modulePath + "/internal/codexrpc",
		modulePath + "/internal/runtime",
		modulePath + "/internal/state",
		modulePath + "/internal/app",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) != 0 {
		t.Fatalf("history Feishu adapter must remain protocol and storage independent: %v", violations)
	}
	violations, err = importsUnder(root, "internal/application/history", []string{
		modulePath + "/internal/adapter",
		modulePath + "/internal/codexrpc",
		modulePath + "/internal/runtime",
		modulePath + "/internal/state",
		modulePath + "/internal/app",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) != 0 {
		t.Fatalf("history use case must consume semantic ports: %v", violations)
	}
}

func TestHistoryBindingsDoNotReintroduceRecursiveRenderCallbacks(t *testing.T) {
	root := repositoryRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "internal", "feishuapp", "history_bindings.go"))
	if err != nil {
		t.Fatal(err)
	}
	source := string(data)
	for _, forbidden := range []string{"RenderHistory", "RenderDetail", "HistoryIndex", "return newHistoryService(app)"} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("history composition still contains recursive callback %q", forbidden)
		}
	}
}

func TestBackendConfigurationDoesNotDependOnTransitionalConfigurationHelpers(t *testing.T) {
	root := repositoryRoot(t)
	violations, err := importsUnder(root, "internal/adapter/feishu/backend", []string{modulePath + "/internal/app/"})
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) != 0 {
		t.Fatalf("backend package must not depend on transitional app packages: %v", violations)
	}
}

func TestWorkspaceCommandCompositionHasNoIndirectInitBridge(t *testing.T) {
	root := repositoryRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "internal", "feishuapp", "workspacecmd_bindings.go"))
	if err != nil {
		t.Fatal(err)
	}
	source := string(data)
	for _, forbidden := range []string{"func init()", "indirectCompleteMenuCommand", "indirectReplyCommandActionResponse"} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("workspace command composition still uses implicit bridge %q", forbidden)
		}
	}
}

func TestTurnCompositionDoesNotPassAppAggregateToUseCase(t *testing.T) {
	root := repositoryRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "internal", "feishuapp", "turn_lifecycle.go"))
	if err != nil {
		t.Fatal(err)
	}
	source := string(data)
	if strings.Contains(source, "Runtime: app") || strings.Contains(source, "Continuations: app") || strings.Contains(source, "Delivery: app") || strings.Contains(source, "Diagnostics: app") {
		t.Fatal("turn composition passes the App aggregate instead of narrow ports")
	}
}

func TestFrontendRuntimeOwnerConstructionStaysInComposition(t *testing.T) {
	root := repositoryRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "internal", "feishuapp", "app.go"))
	if err != nil {
		t.Fatal(err)
	}
	source := string(data)
	for _, forbidden := range []string{
		"frontendruntime.NewFrontendOwner()",
		"frontendruntime.NewSessionActors()",
		"frontendruntime.NewLiveThreads()",
		"appautoretry.NewTracker()",
		"appcodexruntime.NewRecoveryState()",
	} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("frontend construction must stay behind composition owner factory: %q", forbidden)
		}
	}
	if strings.Contains(source, "NewFrontendOwner()") {
		t.Fatal("frontend runtime owner must be injected by composition")
	}
	if strings.Contains(source, "func NewFrontend(") {
		t.Fatal("production frontend constructor must live in internal/composition")
	}
}

func TestRuntimeOwnerDoesNotDependOnTransitionalApp(t *testing.T) {
	violations, err := importsUnder(repositoryRoot(t), "internal/runtime", []string{modulePath + "/internal/app"})
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) != 0 {
		t.Fatalf("runtime owner must not depend on transitional app: %v", violations)
	}
}

func TestPlanModeDoesNotDependOnAppCoreOrAppWorkspace(t *testing.T) {
	root := repositoryRoot(t)
	violations, err := importsUnder(root, "internal/adapter/feishu/planmode", []string{
		modulePath + "/internal/app/appcore",
		modulePath + "/internal/app/workspace",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) != 0 {
		t.Fatalf("plan mode must consume explicit capabilities and domain values: %v", violations)
	}
}

func TestReviewCommandUsesExplicitContextAndConfigurationCapabilities(t *testing.T) {
	root := repositoryRoot(t)
	violations, err := importsUnder(root, "internal/adapter/feishu/reviewcmd", []string{modulePath + "/internal/app/appcore"})
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) != 0 {
		t.Fatalf("review command must not depend on appcore aggregation: %v", violations)
	}
}

func TestGoalCommandDoesNotDependOnAppCore(t *testing.T) {
	root := repositoryRoot(t)
	violations, err := importsUnder(root, "internal/adapter/feishu/goalcmd", []string{modulePath + "/internal/app/appcore"})
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) != 0 {
		t.Fatalf("goal command must use explicit context and value helpers: %v", violations)
	}
}

func TestThreadMenuDoesNotDependOnLegacyWorkspaceOrAppCoreHelpers(t *testing.T) {
	root := repositoryRoot(t)
	violations, err := importsUnder(root, "internal/adapter/feishu/threadmenu", []string{
		modulePath + "/internal/app/appcore",
		modulePath + "/internal/app/workspace",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) != 0 {
		t.Fatalf("thread menu must use explicit capabilities and domain values: %v", violations)
	}
}

func TestWorkspaceCommandDoesNotDependOnAppCore(t *testing.T) {
	root := repositoryRoot(t)
	violations, err := importsUnder(root, "internal/adapter/feishu/workspacecmd", []string{modulePath + "/internal/app/appcore"})
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) != 0 {
		t.Fatalf("workspace command must use explicit configuration and identity capabilities: %v", violations)
	}
}

func TestDebugCommandDoesNotDependOnLegacyAppHelpers(t *testing.T) {
	root := repositoryRoot(t)
	violations, err := importsUnder(root, "internal/adapter/feishu/debugviewcmd", []string{
		modulePath + "/internal/app/appcore",
		modulePath + "/internal/adapter/feishu/threadmenu",
		modulePath + "/internal/app/workspace",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) != 0 {
		t.Fatalf("debug command must use explicit runtime/configuration capabilities: %v", violations)
	}
}

func TestModelSettingsEntrypointsDoNotMutateBusinessState(t *testing.T) {
	root := repositoryRoot(t)
	for _, relative := range []string{"internal/feishuapp/bot_profile.go", "internal/feishuapp/binding_scoped_commands.go", "internal/feishuapp/binding_model_actions.go"} {
		file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(root, relative), nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			assignment, ok := node.(*ast.AssignStmt)
			if !ok {
				return true
			}
			for _, lhs := range assignment.Lhs {
				selector, ok := lhs.(*ast.SelectorExpr)
				if !ok {
					continue
				}
				name := selector.Sel.Name
				if strings.HasSuffix(name, "Override") || name == "AppliedModelConfig" || name == "ActiveTurnID" {
					t.Errorf("%s mutates model/turn state directly: %s", relative, name)
				}
			}
			return true
		})
	}
}

func TestModelConsumersUseSnapshotPorts(t *testing.T) {
	root := repositoryRoot(t)
	for _, relative := range []string{"internal/application/conversation/service.go", "internal/application/submission/queue.go"} {
		file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(root, relative), nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if ok && (function.Name.Name == "effectiveCodexModel" || function.Name.Name == "effectiveClaudeModel" || function.Name.Name == "effectiveCodexReasoningEffort") {
				t.Errorf("%s reintroduces model precedence outside domain: %s", relative, function.Name.Name)
			}
		}
		ast.Inspect(file, func(node ast.Node) bool {
			field, ok := node.(*ast.Field)
			if !ok {
				return true
			}
			for _, name := range field.Names {
				switch name.Name {
				case "ResolveModel", "ResolveModelConfig", "ConfiguredCodexModel", "ConfiguredCodexReasoningEffort", "ConfiguredClaudeModel":
					t.Errorf("%s reintroduces host model callback %s", relative, name.Name)
				}
			}
			return true
		})
	}
}

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

func TestBackendProtocolAssemblyStaysBehindAdapters(t *testing.T) {
	root := repositoryRoot(t)
	checks := []struct {
		name     string
		relative string
		banned   []string
	}{
		{
			name: "app thread wire params", relative: "internal/app",
			banned: []string{"codexrpc.ThreadStartParams", "codexrpc.ThreadResumeParams", "codexrpc.ThreadForkParams"},
		},
		{
			name: "workspace command backend protocol", relative: "internal/adapter/feishu/workspacecmd",
			banned: []string{"internal/codexrpc", "internal/claudecli", "CodexRPCClient"},
		},
		{
			name: "debug command Claude protocol", relative: "internal/adapter/feishu/debugviewcmd",
			banned: []string{"internal/claudecli", "claudecli.TurnUsage"},
		},
	}
	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			violations, err := textMatches(root, check.relative, check.banned)
			if err != nil {
				t.Fatal(err)
			}
			if len(violations) != 0 {
				t.Fatalf("backend protocol assembly crossed its boundary: %v", violations)
			}
		})
	}
}

func TestStartupRecoveryIsApplicationOwned(t *testing.T) {
	root := repositoryRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "internal/application/conversation/recovery.go"))
	if err != nil {
		t.Fatal(err)
	}
	source := string(data)
	for _, forbidden := range []string{"internal/codexrpc", "internal/adapter", "internal/runtime", "client.Call("} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("startup recovery crossed its semantic boundary: %q", forbidden)
		}
	}
	for _, required := range []string{"endpoint.Gateway.Start(", "endpoint.Gateway.Resume(", "endpoint.Current()"} {
		if !strings.Contains(source, required) {
			t.Fatalf("startup recovery must call %s", required)
		}
	}
}

func TestSemanticEffectsExposeRetryIdentityWithoutTransportTypes(t *testing.T) {
	root := repositoryRoot(t)
	effectPath := filepath.Join(root, "internal", "application", "effect.go")
	data, err := os.ReadFile(effectPath)
	if err != nil {
		t.Fatal(err)
	}
	source := string(data)
	for _, required := range []string{"IdempotencyKey string", "func EffectIdentity(", "func StableEffectKey("} {
		if !strings.Contains(source, required) {
			t.Fatalf("application effects must expose %s", required)
		}
	}
	for _, forbidden := range []string{"internal/adapter/", "internal/feishu/", "map[string]any"} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("semantic effects must not expose transport type %q", forbidden)
		}
	}
	runner, err := os.ReadFile(filepath.Join(root, "internal", "runtime", "effect_runner.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(runner), "EffectDeduper") || !strings.Contains(string(runner), "runValue(") {
		t.Fatal("effect runner must execute retryable effects through the deduper")
	}
}

func TestApplicationPresentationStaysDetachedFromFeishuSDK(t *testing.T) {
	violations, err := importsUnder(repositoryRoot(t), "internal/application/presentation", []string{
		modulePath + "/internal/feishu", modulePath + "/internal/adapter", modulePath + "/internal/app",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) != 0 {
		t.Fatalf("application presentation must remain detached: %v", violations)
	}
}

func TestSessionOwnedAsyncPathsUseActorAwarePorts(t *testing.T) {
	root := repositoryRoot(t)
	checks := []struct {
		path string
		want []string
		bad  []string
	}{
		{"internal/application/submission/queue.go", []string{"RunSessionAsync", "runSessionAsync("}, nil},
		{"internal/application/turn/service.go", []string{"RunSessionAsync"}, nil},
		{"internal/feishuapp/session_async.go", []string{"runSessionOnActor("}, nil},
		{"internal/adapter/feishu/backend/selection.go", nil, []string{"go func()"}},
		{"internal/runtime/codex/recovery.go", []string{"RunSessionAsync"}, nil},
		{"internal/application/backendfailure/service.go", []string{"RunSessionAsync"}, nil},
	}
	for _, check := range checks {
		data, err := os.ReadFile(filepath.Join(root, check.path))
		if err != nil {
			t.Fatal(err)
		}
		source := string(data)
		for _, required := range check.want {
			if !strings.Contains(source, required) {
				t.Fatalf("%s must contain actor-aware path %q", check.path, required)
			}
		}
		for _, forbidden := range check.bad {
			if strings.Contains(source, forbidden) {
				t.Fatalf("%s must not bypass injected async runner with %q", check.path, forbidden)
			}
		}
	}
}

func TestStateRepositoryDoesNotPerformExternalEffects(t *testing.T) {
	violations, err := importsUnder(repositoryRoot(t), "internal/state", []string{
		modulePath + "/internal/feishu", modulePath + "/internal/adapter", modulePath + "/internal/runtime", modulePath + "/internal/claudecli", modulePath + "/internal/codexrpc",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) != 0 {
		t.Fatalf("state repository must remain a pure persistence owner: %v", violations)
	}
}

func TestArchitectureDocumentsDoNotReferenceRemovedBoundaries(t *testing.T) {
	root := repositoryRoot(t)
	docs := []string{
		"docs/architecture.md",
		"docs/app-package-boundaries.md",
		"docs/backend-layering.md",
		"docs/codex-app-server-state-machine-audit.md",
		"docs/feishu-card-callback-latency-audit.md",
	}
	forbidden := []string{
		"internal/app/appcore",
		"internal/app/appstate",
		"internal/app/convbackend",
		"internal/app/backend/",
		"internal/app/serverrequest/",
		"internal/app/submission_queue.go",
		"internal/app/goalcmd/",
		"internal/app/planmode/",
		"internal/app/skillscmd/",
		"internal/app/skills/",
		"internal/app/apphistory/",
	}
	for _, relative := range docs {
		data, err := os.ReadFile(filepath.Join(root, relative))
		if err != nil {
			t.Fatal(err)
		}
		source := string(data)
		for _, path := range forbidden {
			if strings.Contains(source, path) {
				t.Fatalf("%s references removed boundary %q", relative, path)
			}
		}
	}
}

func TestCardCallbackAndSessionActorContractsRemainDocumented(t *testing.T) {
	root := repositoryRoot(t)
	latency, err := os.ReadFile(filepath.Join(root, "docs", "feishu-card-callback-latency-audit.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"fast callback ack", "async work", "card patch", "card.action.trigger"} {
		if !strings.Contains(string(latency), required) {
			t.Fatalf("callback latency audit must retain %q", required)
		}
	}
	architecture, err := os.ReadFile(filepath.Join(root, "docs", "architecture.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"SessionActors", "FrontendOwner", "RunSessionAsync", "SaveState"} {
		if !strings.Contains(string(architecture), required) {
			t.Fatalf("architecture guide must document %q", required)
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
		"internal/adapter/feishu/goalcmd",
		"internal/adapter/feishu/skills",
		"internal/adapter/feishu/reviewcmd",
		"internal/adapter/feishu/planmode",
		"internal/adapter/feishu/upgradecmd",
		"internal/adapter/feishu/debugviewcmd",
		"internal/adapter/feishu/threadmenu",
		"internal/adapter/feishu/workspacecmd",
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

func TestWorkspaceRendererDoesNotDependOnAppCoreHelpers(t *testing.T) {
	root := repositoryRoot(t)
	violations, err := importsUnder(root, "internal/adapter/feishu/workspace", []string{
		modulePath + "/internal/app", modulePath + "/internal/config", modulePath + "/internal/state",
		modulePath + "/internal/adapter/storage", modulePath + "/internal/domain/conversation", modulePath + "/internal/domain/routing",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) != 0 {
		t.Fatalf("workspace renderer must consume application views: %v", violations)
	}
	if _, err := os.Stat(filepath.Join(root, "internal/adapter/feishu/workspacecmd/render.go")); !os.IsNotExist(err) {
		t.Fatal("legacy workspace renderer must not be reintroduced")
	}
	if _, err := os.Stat(filepath.Join(root, "internal/app/appcore/workspace_selection.go")); !os.IsNotExist(err) {
		t.Fatal("workspace selection host bridge must not be reintroduced")
	}
}

func TestPureCardRenderersDoNotImportFilesystemOrHostState(t *testing.T) {
	root := repositoryRoot(t)
	for _, pkg := range []string{"internal/adapter/feishu/pathpicker", "internal/adapter/feishu/workspace"} {
		violations, err := importsUnder(root, pkg, []string{
			modulePath + "/internal/adapter/filesystem", modulePath + "/internal/config",
			modulePath + "/internal/state", modulePath + "/internal/app",
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(violations) != 0 {
			t.Fatalf("%s imports filesystem or host state: %v", pkg, violations)
		}
	}
}

func TestModelCatalogPolicyLivesInApplication(t *testing.T) {
	root := repositoryRoot(t)
	violations, err := importsUnder(root, "internal/adapter/feishu/modelconfig", []string{modulePath + "/internal/app"})
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) != 0 {
		t.Fatalf("model config adapter must not depend on app orchestration: %v", violations)
	}
	data, err := os.ReadFile(filepath.Join(root, "internal", "adapter", "feishu", "modelconfig", "modelconfig.go"))
	if err != nil {
		t.Fatal(err)
	}
	source := string(data)
	for _, signature := range []string{
		"func DefaultModelEntry(",
		"func LookupModelEntry(",
		"func FindModelEntry(",
		"func ModelSupportsEffort(",
	} {
		if strings.Contains(source, signature) {
			t.Fatalf("transitional modelconfig package reintroduced catalog policy %s", signature)
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

func TestApplicationBackendEventServiceUsesExplicitOwners(t *testing.T) {
	root := repositoryRoot(t)
	path := filepath.Join(root, "internal", "application", "backendevents", "service.go")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	source := string(data)
	if strings.Contains(source, "EventSink") || strings.Contains(source, "*App") {
		t.Fatal("backend events must coordinate owner ports in application")
	}
	for _, owner := range []string{"Lifecycle", "Items", "Presentation", "Compaction", "Submissions", "Usage", "Goals", "Interactions"} {
		if !strings.Contains(source, owner+" interface") {
			t.Errorf("backend event service has no explicit %s owner port", owner)
		}
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

func TestApplicationBackendRepliesUseOpaqueJSON(t *testing.T) {
	root := repositoryRoot(t)
	checks := []struct {
		path string
		bad  []string
		good string
	}{
		{"internal/application/backendops/request.go", []string{"Payload any"}, "Payload json.RawMessage"},
		{"internal/application/interaction/reply.go", []string{"replyPayload any"}, "replyPayload json.RawMessage"},
	}
	for _, check := range checks {
		data, err := os.ReadFile(filepath.Join(root, check.path))
		if err != nil {
			t.Fatal(err)
		}
		source := string(data)
		for _, forbidden := range check.bad {
			if strings.Contains(source, forbidden) {
				t.Fatalf("%s still exposes untyped backend reply value %q", check.path, forbidden)
			}
		}
		if !strings.Contains(source, check.good) {
			t.Fatalf("%s must expose %s", check.path, check.good)
		}
	}
}

func TestPresentationActionsDoNotExposePlatformMaps(t *testing.T) {
	root := repositoryRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "internal", "application", "presentation", "card.go"))
	if err != nil {
		t.Fatal(err)
	}
	source := string(data)
	if strings.Contains(source, "Value       map[string]any") {
		t.Fatal("presentation Action must not expose a platform map")
	}
	if !strings.Contains(source, "Value json.RawMessage") {
		t.Fatal("presentation Action must use opaque JSON")
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

// TestFeishuAppAggregateDoesNotGrow pins the remaining *App coupling in
// internal/feishuapp. The package is the transitional home of the Feishu
// frontend implementation; the migration that removes the aggregate proceeds
// one capability at a time (see
// docs/feishuapp-app-aggregate-removal.md and
// docs/feishuapp-construction-cycle-breaking.md).
//
// The budget only ratchets down. Converting a function or struct to take the
// narrow values it uses should lower the number here in the same commit; if a
// change needs a higher number, it is adding coupling rather than removing it.
func TestFeishuAppAggregateDoesNotGrow(t *testing.T) {
	const budget = 121

	root := repositoryRoot(t)
	entries, err := filepath.Glob(filepath.Join(root, "internal/feishuapp/*.go"))
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, path := range entries {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatal(readErr)
		}
		count += strings.Count(string(data), "*App")
	}
	if count > budget {
		t.Fatalf("internal/feishuapp has %d *App references, budget is %d; "+
			"narrow the new code to the values it uses instead of adding coupling", count, budget)
	}
	if count < budget {
		t.Fatalf("internal/feishuapp has %d *App references but the budget is still %d; "+
			"lower the budget in the same commit", count, budget)
	}
}

// TestFeishuAppLazyBindingReadsDoesNotGrow pins the number of backend-binding
// reads that sit inside closures in functions taking *App.
//
// A read inside a closure happens when the closure runs, not when the factory
// is called, which hides the dependency from the type system and from static
// analysis. It also lets two construction entry points disagree about the
// order services are built without anything failing — converting these to
// construction-time reads surfaced two such disagreements.
//
// The goal is zero. The budget only ratchets down; lower it in the same
// commit that removes reads.
func TestFeishuAppLazyBindingReadsDoesNotGrow(t *testing.T) {
	const budget = 0 // goal: 0

	root := repositoryRoot(t)
	entries, err := filepath.Glob(filepath.Join(root, "internal/feishuapp/*.go"))
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	count := 0
	locations := []string{}
	for _, path := range entries {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		file, parseErr := parser.ParseFile(fset, path, nil, 0)
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil || fn.Type.Params == nil {
				continue
			}
			var appVar string
			for _, prm := range fn.Type.Params.List {
				star, ok := prm.Type.(*ast.StarExpr)
				if !ok {
					continue
				}
				if id, ok := star.X.(*ast.Ident); ok && id.Name == "App" && len(prm.Names) > 0 {
					appVar = prm.Names[0].Name
				}
			}
			if appVar == "" {
				continue
			}
			count += lazyBindingReads(fn.Body, appVar, func(pos token.Pos) {
				locations = append(locations, fset.Position(pos).String())
			})
		}
	}
	if count != budget {
		t.Fatalf("internal/feishuapp has %d lazy backend-binding reads, budget is %d; "+
			"read the value once at construction instead, or lower the budget in the same commit; reads: %s", count, budget, strings.Join(locations, ", "))
	}
}

// lazyBindingReads counts appVar.bindings.X reads that appear inside a func
// literal, and reads of appVar itself inside one (which forwards the whole
// aggregate).
func lazyBindingReads(body *ast.BlockStmt, appVar string, onRead func(token.Pos)) int {
	count := 0
	var walk func(n ast.Node, lazy bool)
	walk = func(n ast.Node, lazy bool) {
		ast.Inspect(n, func(m ast.Node) bool {
			switch x := m.(type) {
			case *ast.FuncLit:
				walk(x.Body, true)
				return false
			case *ast.SelectorExpr:
				if !lazy {
					return true
				}
				inner, ok := x.X.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				if id, ok := inner.X.(*ast.Ident); ok && id.Name == appVar && inner.Sel.Name == "bindings" {
					count++
					if onRead != nil {
						onRead(x.Pos())
					}
				}
			}
			return true
		})
	}
	walk(body, false)
	return count
}
