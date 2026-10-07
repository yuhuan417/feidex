package feishuapp

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	domainbackend "feidex/internal/domain/backend"

	"feidex/internal/application/features"
	"feidex/internal/codexrpc"
	"feidex/internal/config"
	"feidex/internal/daemon"
	"feidex/internal/domain/conversation"
	catalog "feidex/internal/domain/modelconfig"
	"feidex/internal/domain/skill"
	"feidex/internal/feishu"
	"feidex/internal/release"
	"feidex/internal/state"
)

// This guard walks the whole menu from /menu through real rendered cards and
// enforces the menu contracts documented in DEVELOPER.md:
//
//  1. every action value a rendered card emits has a registered handler (no
//     dead buttons);
//  2. every menu.* action a card emits is declared in the features registry as
//     a menu node (a page) or a menu item (a listed capability), so the menu
//     stays a tree rooted at /menu with declared parents;
//  3. every declared menu node renders a page during the walk (chat scopes and
//     backends are walked; a node missing everywhere fails);
//  4. every page claims its own declared breadcrumb ("当前位置：…") and carries
//     exactly one 返回上一级 control as the final button. Cards that carry a
//     breadcrumb must claim a declared path.
//
// A page can surface in two places when opened from a card: the dispatch
// response (direct handlers) or a fresh command reply card (command-bridge
// handlers patch the tapped card with the parent page and deliver the real
// page as a new message). The walk harvests both, plus select_static options,
// so parameterized pages render exactly like production.

// allRegisteredCardActionKeysForTest merges every card action handler set and
// returns action -> handler-set name.
func allRegisteredCardActionKeysForTest() map[string]string {
	registered := map[string]string{}
	for _, set := range allCardActionPortSetsForTest() {
		for action := range set.handlers {
			registered[action] = set.name
		}
	}
	return registered
}

// dispatchMenuActionValueForTest taps a card control: the emitted value map is
// dispatched through the real card action dispatcher.
func dispatchMenuActionValueForTest(a *Frontend, msg *feishu.InboundMessage, value map[string]any, option string) (map[string]any, bool) {
	actionValue := make(map[string]any, len(value)+1)
	for key, item := range value {
		actionValue[key] = item
	}
	actionValue["session_key"] = a.configView().makeSessionKey(msg)
	// An empty MessageID keeps page opens on the synchronous path so the walk
	// sees the final card instead of an async working placeholder.
	resp, err := newCardActionService(a).dispatch(&feishu.CardAction{
		ActionValue: actionValue, UserID: msg.UserID, ChatID: msg.ChatID, Option: option,
	})
	if err != nil || resp == nil || resp.Card == nil {
		return nil, false
	}
	card, ok := resp.Card.Data.(map[string]any)
	if !ok {
		return nil, false
	}
	return card, true
}

// extractCardActionEdges collects every action a card can emit together with
// the full value map that control would dispatch.
func extractCardActionEdges(value any, actions map[string]bool, edges map[string]map[string]any) {
	switch typed := value.(type) {
	case map[string]any:
		if action, ok := typed["action"].(string); ok && strings.TrimSpace(action) != "" {
			action = strings.TrimSpace(action)
			actions[action] = true
			if _, seen := edges[action]; !seen {
				edges[action] = typed
			}
		}
		for key, child := range typed {
			if key == "action" {
				continue
			}
			extractCardActionEdges(child, actions, edges)
		}
	case []any:
		for _, child := range typed {
			extractCardActionEdges(child, actions, edges)
		}
	case []map[string]any:
		for _, child := range typed {
			extractCardActionEdges(child, actions, edges)
		}
	}
}

// extractCardSelects collects every select_static element's dispatch value and
// selectable option values, so the walk can simulate selections.
func extractCardSelects(value any, out *[]selectEdgeForTest) {
	switch typed := value.(type) {
	case map[string]any:
		if tag, _ := typed["tag"].(string); tag == "select_static" {
			edge := selectEdgeForTest{}
			if behaviors, ok := typed["behaviors"].([]any); ok && len(behaviors) > 0 {
				if behavior, ok := behaviors[0].(map[string]any); ok {
					edge.value, _ = behavior["value"].(map[string]any)
				}
			}
			if behaviors, ok := typed["behaviors"].([]map[string]any); ok && len(behaviors) > 0 {
				edge.value, _ = behaviors[0]["value"].(map[string]any)
			}
			if edge.value != nil {
				if action, ok := edge.value["action"].(string); ok && strings.TrimSpace(action) != "" {
					edge.action = strings.TrimSpace(action)
					if options, ok := typed["options"].([]any); ok {
						for _, option := range options {
							optionMap, ok := option.(map[string]any)
							if !ok {
								continue
							}
							if value, ok := optionMap["value"].(string); ok && strings.TrimSpace(value) != "" {
								edge.options = append(edge.options, strings.TrimSpace(value))
							}
						}
					}
					if options, ok := typed["options"].([]map[string]any); ok {
						for _, option := range options {
							if value, ok := option["value"].(string); ok && strings.TrimSpace(value) != "" {
								edge.options = append(edge.options, strings.TrimSpace(value))
							}
						}
					}
					*out = append(*out, edge)
				}
			}
		}
		for key, child := range typed {
			if key == "action" || key == "behaviors" || key == "options" {
				continue
			}
			extractCardSelects(child, out)
		}
	case []any:
		for _, child := range typed {
			extractCardSelects(child, out)
		}
	case []map[string]any:
		for _, child := range typed {
			extractCardSelects(child, out)
		}
	}
}

type selectEdgeForTest struct {
	action  string
	value   map[string]any
	options []string
}

func backControlViolation(name string, card map[string]any) string {
	labels := buttonLabelsForTest(card)
	if len(labels) == 0 {
		return fmt.Sprintf("%s: page card has no buttons", name)
	}
	backCount := 0
	for i, label := range labels {
		if !feishu.IsMenuBackButtonText(label) {
			continue
		}
		backCount++
		if i != len(labels)-1 {
			return fmt.Sprintf("%s: back control at index %d of %d, want final", name, i, len(labels)-1)
		}
	}
	if backCount != 1 {
		return fmt.Sprintf("%s: back control count = %d, want 1 (labels %q)", name, backCount, labels)
	}
	return ""
}

func cardBreadcrumbLine(card map[string]any) string {
	for _, elem := range cardElementsForTest(card) {
		for _, candidate := range []any{elem["content"], func() any {
			text, ok := elem["text"].(map[string]any)
			if !ok {
				return nil
			}
			return text["content"]
		}()} {
			if content, ok := candidate.(string); ok && strings.HasPrefix(content, "当前位置：") {
				return strings.SplitN(content, "\n", 2)[0]
			}
		}
	}
	return ""
}

func cardIsAsyncPlaceholder(card map[string]any) bool {
	for _, elem := range cardElementsForTest(card) {
		if content, ok := elem["content"].(string); ok && strings.Contains(content, "请稍候") {
			return true
		}
	}
	return false
}

func menuGraphCallStub(method string, out any) error {
	switch method {
	case "model/list":
		*(out.(*catalog.ModelListResult)) = catalog.ModelListResult{Data: []catalog.ModelListEntry{{
			ID: "gpt-5", DisplayName: "GPT-5", DefaultReasoningEffort: "medium", IsDefault: true,
		}}}
		return nil
	case "collaborationMode/list":
		*(out.(*catalog.CollaborationModeListResponse)) = catalog.CollaborationModeListResponse{}
		return nil
	case "skills/list":
		*(out.(*skill.SkillsListResult)) = skill.SkillsListResult{}
		return nil
	case "thread/list":
		*(out.(*codexrpc.ThreadListResult)) = codexrpc.ThreadListResult{}
		return nil
	case "thread/goal/get":
		*(out.(*codexrpc.ThreadGoalGetResponse)) = codexrpc.ThreadGoalGetResponse{}
		return nil
	case "thread/read":
		*(out.(*codexrpc.ThreadReadResult)) = codexrpc.ThreadReadResult{Thread: codexrpc.ThreadReadThread{
			ID: "thread-1",
			Turns: []codexrpc.ThreadReadTurn{{
				ID:     "turn-1",
				Status: "completed",
				Items: []codexrpc.ThreadReadItem{
					{Type: "userMessage", Content: []byte(`[{"type":"text","text":"hello"}]`)},
				},
			}},
		}}
		return nil
	default:
		return errors.New("menu graph walk: unexpected rpc " + method)
	}
}

func newMenuGraphWalkApp(t *testing.T, chatType, backend string) (*Frontend, *fakeFeishuClient, *feishu.InboundMessage) {
	t.Helper()
	origManager, origRelease, origVersion, origGOARCH := newDaemonManager, newReleaseClient, currentVersion, currentGOARCH
	t.Cleanup(func() {
		newDaemonManager, newReleaseClient, currentVersion, currentGOARCH = origManager, origRelease, origVersion, origGOARCH
	})
	newDaemonManager = func(string) (daemon.Manager, error) {
		// The PID must match the test process so the upgrade path treats the
		// walk as an in-daemon run instead of rejecting remote upgrades.
		return &fakeDaemonManagerForApp{status: &daemon.Status{Installed: true, Running: true, PID: os.Getpid()}}, nil
	}
	newReleaseClient = func() releaseClient {
		return &fakeReleaseClient{info: &release.ReleaseInfo{Version: "v9.9.9", BinaryURL: "https://download.test/feidex", ExpectedSHA256: "abc123", HTMLURL: "https://example.test/releases/v9.9.9"}}
	}
	currentVersion = func() string { return "v0.1.0" }
	currentGOARCH = func() string { return "amd64" }

	a, ff, fc := newTestApp(t)
	a.frontendID = "bot-menu-graph"
	a.cfg.Feishu.Backend = backend
	a.cfg.Feishu.DebugAllowFrom = []string{"user-1"}
	// A second, inactive workspace gives the workspace delete page a
	// deletable entry; the default workspace is always active.
	a.cfg.Workspaces = append(a.cfg.Workspaces, config.Workspace{ID: "drop", Cwd: t.TempDir()})
	recomposeTestApp(a)
	msg := &feishu.InboundMessage{ChatID: "chat-menu-graph-" + chatType, ChatType: chatType, UserID: "user-1", RootMessageID: "root-1"}
	sessionKey := a.configView().makeSessionKey(msg)
	if err := a.State().SaveSession(&conversation.Session{
		Key: sessionKey, ChatID: msg.ChatID, ChatType: chatType,
		WorkspaceID: "default", ActiveThreadID: "thread-1", ActiveThreadWorkspaceID: "default",
	}); err != nil {
		t.Fatalf("SaveSession(): %v", err)
	}
	if chatType == "group" {
		if err := a.State().SaveAgentBinding(&state.AgentBinding{
			ID: defaultBindingID(a.frontendID, chatType, msg.ChatID), FrontendID: a.frontendID,
			ChatID: msg.ChatID, ChatType: chatType, WorkspaceID: "default",
			Status: state.AgentBindingStatusActive.String(),
		}); err != nil {
			t.Fatalf("SaveAgentBinding(): %v", err)
		}
	}
	fc.callHook = func(_ context.Context, method string, _ any, out any) error {
		return menuGraphCallStub(method, out)
	}
	return a, ff, msg
}

func TestMenuGraphGuard(t *testing.T) {
	registered := allRegisteredCardActionKeysForTest()
	nodes := features.MenuNodes()
	declaredItems := map[string]bool{}
	nodeSlash := map[string]string{}
	for _, item := range features.MenuItemSpecs() {
		declaredItems[strings.TrimSpace(item.Action)] = true
		if strings.TrimSpace(item.Slash) != "" {
			nodeSlash[strings.TrimSpace(item.Action)] = strings.TrimSpace(item.Slash)
		}
	}
	declaredPages := map[string]bool{"menu.root": true}
	for action := range nodes {
		declaredPages[action] = true
	}

	const maxSelectOptions = 3
	pageWellFormed := map[string]bool{}
	pageSeen := map[string]bool{}

	for _, chatType := range []string{"group", "p2p"} {
		for _, backend := range []string{domainbackend.BackendCodex, domainbackend.BackendClaude} {
			a, ff, msg := newMenuGraphWalkApp(t, chatType, backend)
			// Expected breadcrumb line -> owning nodes. The walk fixture may
			// render through a runtime backend that differs from the config
			// backend, so a claim matches when it names a declared path under
			// any backend label set. Several nodes may share one breadcrumb
			// (an entry section and its page render the same card); a claim
			// then counts for every matching node.
			expectedBreadcrumbs := map[string][]string{}
			for _, labelBackend := range []string{domainbackend.BackendCodex, domainbackend.BackendClaude, ""} {
				for action := range nodes {
					line := cardBreadcrumbLine(map[string]any{"elements": []map[string]any{{"content": menuCardBodyForBackend(labelBackend, action, "")}}})
					if line == "" {
						continue
					}
					known := false
					for _, existing := range expectedBreadcrumbs[line] {
						if existing == action {
							known = true
							break
						}
					}
					if !known {
						expectedBreadcrumbs[line] = append(expectedBreadcrumbs[line], action)
					}
				}
			}
			type queueEntry struct {
				action string
				value  map[string]any
			}
			enqueued := map[string]bool{"menu.root": true}
			queue := []queueEntry{{action: "menu.root", value: map[string]any{"action": "menu.root"}}}
			// enqueuePage schedules a node for dispatch exactly once; both the
			// claim path (a rendered card claims the node's breadcrumb) and
			// the edge path (a card emits the node's action) go through it, so
			// a discovered page is always dispatched at least once.
			enqueuePage := func(action string, value map[string]any) {
				if action == "menu.root" || !declaredPages[action] || enqueued[action] {
					return
				}
				enqueued[action] = true
				queue = append(queue, queueEntry{action: action, value: value})
			}
			replied := 0
			selectBudget := 500
			simulatedSelects := map[string]bool{}
			upgradeDeferral := map[string]bool{"menu.upgrade": true, "menu.codex_upgrade": true, "menu.claude_upgrade": true}

			var inspect func(origin string, card map[string]any)
			inspect = func(origin string, card map[string]any) {
				if card == nil {
					return
				}
				if line := cardBreadcrumbLine(card); line != "" {
					if owners, ok := expectedBreadcrumbs[line]; ok {
						for _, node := range owners {
							pageSeen[node] = true
							if !cardIsAsyncPlaceholder(card) && backControlViolation(node, card) == "" {
								pageWellFormed[node] = true
							}
							enqueuePage(node, map[string]any{"action": node})
						}
					} else {
						t.Errorf("menu graph [%s/%s]: card from %s claims breadcrumb %q which is not a declared node path", chatType, backend, origin, line)
					}
				}
				actions := map[string]bool{}
				edges := map[string]map[string]any{}
				extractCardActionEdges(card, actions, edges)
				for _, action := range sortedActions(actions) {
					if _, ok := registered[action]; !ok {
						t.Errorf("menu graph [%s/%s]: card from %s emits action %q with no registered handler", chatType, backend, origin, action)
						continue
					}
					if strings.HasPrefix(action, "menu.") && !declaredPages[action] && !declaredItems[action] {
						t.Errorf("menu graph [%s/%s]: card from %s emits undeclared menu action %q; declare it as a node or menu item, or stop naming it menu.*", chatType, backend, origin, action)
					}
					enqueuePage(action, edges[action])
				}
				if selectBudget <= 0 {
					return
				}
				var selects []selectEdgeForTest
				extractCardSelects(card, &selects)
				for _, selectEdge := range selects {
					if _, ok := registered[selectEdge.action]; !ok {
						t.Errorf("menu graph [%s/%s]: card from %s emits select action %q with no registered handler", chatType, backend, origin, selectEdge.action)
						continue
					}
					options := selectEdge.options
					if len(options) > maxSelectOptions {
						options = options[:maxSelectOptions]
					}
					for _, option := range options {
						if selectBudget <= 0 {
							return
						}
						// Simulate each select choice once: re-dispatching the
						// same select on its own result would recurse forever.
						selectKey := selectEdge.action + "=" + option
						if simulatedSelects[selectKey] {
							continue
						}
						simulatedSelects[selectKey] = true
						selectBudget--
						value := map[string]any{"action": selectEdge.action}
						for key, item := range selectEdge.value {
							value[key] = item
						}
						if card, ok := dispatchMenuActionValueForTest(a, msg, value, option); ok {
							inspect(origin+"/"+selectEdge.action+"="+option, card)
						}
					}
				}
			}

			drain := func() {
				for {
					replies := ff.replyCardsSnapshot()[replied:]
					replied = len(ff.replyCardsSnapshot())
					if len(replies) == 0 {
						return
					}
					for _, reply := range replies {
						inspect("reply", reply)
					}
				}
			}
			commandMsg := *msg
			commandMsg.MessageID = "menu-graph-command"
			// Upgrade entries run last: their async version checks flip the
			// frontend into maintenance and would break model pages that open
			// through the codex gateway afterwards.
			deferred := []queueEntry{}
			for len(queue) > 0 {
				entry := queue[0]
				queue = queue[1:]
				if upgradeDeferral[entry.action] {
					deferred = append(deferred, entry)
					continue
				}
				if card, ok := dispatchMenuActionValueForTest(a, msg, entry.value, ""); ok {
					inspect("dispatch:"+entry.action, card)
				}
				// Command-bridge pages deliver their real page as a command
				// reply; open the page through the declared slash entrypoint.
				if slash, ok := nodeSlash[entry.action]; ok {
					_ = a.bindings.Commands.Handle(&commandMsg, slash)
					drain()
				}
			}
			for _, entry := range deferred {
				if card, ok := dispatchMenuActionValueForTest(a, msg, entry.value, ""); ok {
					inspect("dispatch:"+entry.action, card)
				}
				if slash, ok := nodeSlash[entry.action]; ok {
					_ = a.bindings.Commands.Handle(&commandMsg, slash)
				}
			}
			// Async command replies may land after the dispatch returns; drain
			// until no new reply cards show up.
			for i := 0; i < 5; i++ {
				before := len(ff.replyCardsSnapshot())
				time.Sleep(50 * time.Millisecond)
				drain()
				if len(ff.replyCardsSnapshot()) == before {
					break
				}
			}
		}
	}

	var violations []string
	pageActions := make([]string, 0, len(nodes))
	for action := range nodes {
		if action == "menu.root" {
			continue
		}
		pageActions = append(pageActions, action)
	}
	sort.Strings(pageActions)
	for _, action := range pageActions {
		if !pageWellFormed[action] {
			if pageSeen[action] {
				violations = append(violations, fmt.Sprintf("%s: opened from /menu but rendered no well-formed page card (own breadcrumb + single final back control)", action))
			} else {
				violations = append(violations, fmt.Sprintf("%s: declared menu node never renders a page card", action))
			}
		}
	}
	if len(violations) > 0 {
		t.Fatalf("menu graph guard found %d violations:\n  %s", len(violations), strings.Join(violations, "\n  "))
	}
}

func sortedActions(actions map[string]bool) []string {
	out := make([]string, 0, len(actions))
	for action := range actions {
		out = append(out, action)
	}
	sort.Strings(out)
	return out
}
