package app

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"

	"feidex/internal/codexrpc"
	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/logcontrol"
	"feidex/internal/state"
)

func testCodexConfig() *config.Config {
	cfg := config.Default()
	cfg.Feishu.Backend = backendCodex
	return cfg
}

func TestCommandNewRejectsRunningTurn(t *testing.T) {
	store, err := state.Open(t.TempDir() + "/state.json")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	a := &App{store: store}
	if err := a.store.UpsertSession(&state.Session{
		Key:            "feishu:p2p:chat:user",
		WorkspaceID:    "default",
		ActiveThreadID: "thread-1",
		ActiveTurnID:   "turn-1",
	}); err != nil {
		t.Fatalf("upsert session: %v", err)
	}

	err = newThreadService(a).CommandThreadsNew(&feishu.InboundMessage{
		ChatID:   "chat",
		ChatType: "p2p",
		UserID:   "user",
	})
	if err == nil {
		t.Fatal("expected running turn to block /new")
	}
	if !strings.Contains(err.Error(), "仍在运行") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHandleCommandStopClearsQueuedInputsBeforeInterrupt(t *testing.T) {
	store, err := state.Open(t.TempDir() + "/state.json")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	a := &App{store: store, codex: codexrpc.New(config.CodexConfig{}), cfg: testCodexConfig()}
	if err := a.store.UpsertSession(&state.Session{
		Key:            "feishu:p2p:chat:user",
		WorkspaceID:    "default",
		ActiveThreadID: "thread-1",
		ActiveTurnID:   "turn-1",
		Queue:          []string{"sub-queued"},
	}); err != nil {
		t.Fatalf("upsert session: %v", err)
	}
	if _, err := a.store.CreateSubmission(&state.Submission{
		ID:               "sub-queued",
		SessionKey:       "feishu:p2p:chat:user",
		WorkspaceID:      "default",
		TriggerMessageID: "msg-queued",
		SourceMessageIDs: []string{"msg-queued"},
		Status:           "queued",
	}); err != nil {
		t.Fatalf("create submission: %v", err)
	}

	err = handleCommand(a, &feishu.InboundMessage{
		ChatID:   "chat",
		ChatType: "p2p",
		UserID:   "user",
	}, "/stop")
	if err == nil {
		t.Fatal("expected /stop to route to interrupt command")
	}
	if !strings.Contains(err.Error(), "client not started") {
		t.Fatalf("unexpected /stop error: %v", err)
	}
	sess := a.store.GetSession("feishu:p2p:chat:user")
	if sess == nil {
		t.Fatal("expected session to remain")
	}
	if len(sess.Queue) != 0 {
		t.Fatalf("expected /stop to clear queued inputs, got %#v", sess.Queue)
	}
}

func TestHandleCommandBlockedWhileBackendSwitching(t *testing.T) {
	a, _, _ := newTestApp(t)
	newRuntimeStateService(a).beginBackendSwitchState(backendCodex)

	msg := &feishu.InboundMessage{
		MessageID: "msg-1",
		ChatID:    "chat-1",
		ChatType:  "p2p",
		UserID:    "user-1",
		Text:      "/quiet",
	}
	err := handleCommand(a, msg, "/quiet")
	if err == nil || !strings.Contains(err.Error(), "当前正在切换到 Codex backend") {
		t.Fatalf("handleCommand() error = %v, want backend switch block", err)
	}
}

func TestIsLocalCommand(t *testing.T) {
	cases := map[string]bool{
		"/menu":                     true,
		"/help":                     true,
		"/backend":                  true,
		"/history":                  true,
		"/history detail 1":         true,
		"/skills":                   true,
		"/skills reload":            true,
		"/model":                    true,
		"/model set gpt-5":          true,
		"/model set default":        true,
		"/model effort high":        true,
		"/model effort default":     true,
		"/effort":                   true,
		"/effort high":              true,
		"/effort default":           true,
		"/quiet":                    true,
		"/quiet config":             true,
		"/debug":                    true,
		"/debug on":                 true,
		"/debug logs":               true,
		"/download":                 true,
		"/compact":                  true,
		"/fork":                     true,
		"/new":                      true,
		"/threads":                  true,
		"/thread":                   true,
		"/thread list":              true,
		"/thread new":               true,
		"/thread resume thread-1":   true,
		"/thread sandbox read-only": true,
		"/thread policy never":      true,
		"/interrupt":                true,
		"/stop":                     true,
		"/workspace":                true,
		"/workspace delete":         true,
		"/workspace delete default": true,
		"/workspace clone https://github.com/example/repo.git":                         true,
		"/workspace clone git@github.com:example/repo.git repo-copy":                   true,
		"/workspace clone https://github.com/example/repo.git --parent /home/yuhuan":   true,
		"/workspace clone https://github.com/example/repo.git repo-copy --parent /tmp": true,
		"/workspace sandbox workspace-write":                                           true,
		"/workspace policy never":                                                      true,
		"/status":                                                                      true,
		"/upgrade":                                                                     true,
		"/upgrade dev":                                                                 true,
		"/fast config":                                                                 true,
		"/upgrade local":                                                               true,
		"/upgrade path ./dist/feidex-linux-amd64":                                      true,
		"/upgrade v0.3.0":                                                              true,
		"/review custom 请重点看并发安全":                                                      true,
		"/append hello":                                                                false,
		"/history detail":                                                              false,
		"/new 和 /fork 之后能不能跑 /review 你不用管":                                             false,
		"/review 你不用管":                                                                 false,
		"/thread new please":                                                           false,
		"/thread resume":                                                               false,
		"/workspace use default extra":                                                 false,
		"/workspace delete default extra":                                              false,
		"/workspace clone https://github.com/example/repo.git repo-copy extra": false,
		"/workspace clone https://github.com/example/repo.git --parent":        false,
		"/workspace clone https://github.com/example/repo.git repo-copy /tmp":  false,
		"/upgrade dev please": false,
		"/effort high extra":  false,
		"/quiet 随便说说":         false,
		"/debug hello":        false,
		"/":                   false,
		"/unknown value":      false,
	}
	for input, want := range cases {
		if got := isLocalCommand(input); got != want {
			t.Fatalf("isLocalCommand(%q) = %v, want %v", input, got, want)
		}
	}
}

func TestIsLocalCommandForClaudeBackend(t *testing.T) {
	cases := map[string]bool{
		"/history":                           true,
		"/compact":                           true,
		"/session":                           true,
		"/session list":                      true,
		"/session list all":                  true,
		"/session new":                       true,
		"/session fork":                      true,
		"/session resume session-1":          true,
		"/session permissions":               true,
		"/session permissions default":       true,
		"/workspace permissions":             true,
		"/workspace permissions inherit":     true,
		"/review":                            false,
		"/review custom 请重点看":                false,
		"/skills":                            false,
		"/skills reload":                     false,
		"/fast":                              false,
		"/fast config":                       false,
		"/thread":                            false,
		"/thread list":                       false,
		"/thread sandbox":                    false,
		"/thread sandbox read-only":          false,
		"/thread policy":                     false,
		"/thread policy never":               false,
		"/workspace sandbox":                 true,
		"/workspace sandbox workspace-write": true,
		"/workspace policy":                  true,
		"/workspace policy never":            true,
		"/workspace use default extra":       false,
	}
	for input, want := range cases {
		if got := isLocalCommandForBackend(backendClaude, input); got != want {
			t.Fatalf("isLocalCommandForBackend(claude, %q) = %v, want %v", input, got, want)
		}
	}
}

func TestRenderHelpBodyFromRegistryForClaudeBackend(t *testing.T) {
	body := renderHelpBodyFromRegistry(backendClaude)
	for _, banned := range []string{
		"/review",
		"/skills",
		"/fast",
		"/thread",
		"/threads",
	} {
		if strings.Contains(body, banned) {
			t.Fatalf("Claude help body should hide %q, got %q", banned, body)
		}
	}
	for _, want := range []string{
		"/history",
		"/compact",
		"/session fork",
		"/session resume SESSION_ID",
		"/session permissions",
		"/workspace permissions",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("Claude help body = %q, want %q", body, want)
		}
	}
}

func TestHandleCommandPassthroughsUnsupportedLocalCommandsToClaude(t *testing.T) {
	for _, raw := range []string{
		"/review",
		"/skills",
		"/fast config",
		"/thread",
		"/thread sandbox read-only",
		"/thread policy never",
	} {
		t.Run(raw, func(t *testing.T) {
			a, _, _ := newTestApp(t)
			a.cfg.Feishu.Backend = backendClaude
			a.codex = nil
			claude := &fakeClaudeCore{}
			a.claude = claude

			msg := &feishu.InboundMessage{
				MessageID: "m-1",
				ChatID:    "chat",
				ChatType:  "p2p",
				UserID:    "user",
				Text:      raw,
			}
			if err := handleCommand(a, msg, raw); err != nil {
				t.Fatalf("handleCommand(%q) error = %v", raw, err)
			}
			if len(claude.startTurnCalls) != 1 {
				t.Fatalf("Claude startTurn calls = %#v, want 1", claude.startTurnCalls)
			}
			if got := claude.startTurnCalls[0].prompt; got != raw {
				t.Fatalf("Claude passthrough prompt = %q, want %q", got, raw)
			}
			sess := a.store.GetSession("feishu:p2p:chat:user")
			if sess == nil || strings.TrimSpace(sess.ActiveSubmissionID) == "" {
				t.Fatalf("session after Claude passthrough = %+v", sess)
			}
			sub := a.store.GetSubmission(strings.TrimSpace(sess.ActiveSubmissionID))
			if sub == nil || sub.InputText != raw {
				t.Fatalf("Claude passthrough submission = %+v, want input %q", sub, raw)
			}
		})
	}
}

func TestSendCommandMenuListsTopLevelCommands(t *testing.T) {
	a := &App{feishu: feishu.New(config.Default().Feishu)}
	msg := &feishu.InboundMessage{MessageID: "m1", ChatType: "p2p", ChatID: "chat", UserID: "user"}
	card := a.feishu.SimpleStatusCard("命令菜单", "blue", "选择命令执行。", nil)
	elements := cardElementsForTest(card)
	if len(elements) == 0 {
		t.Fatalf("unexpected card elements: %#v", card)
	}
	body, _ := elements[0]["content"].(string)
	for _, alias := range []string{"/menu", "/help", "/history", "/download", "/compact", "/fork", "/new", "/stop", "/model", "/quiet", "/debug", "/fast", "/thread", "/threads", "/interrupt", "/status", "/workspace", "/upgrade"} {
		if strings.Contains(body, alias) {
			t.Fatalf("expected menu body to omit command text %q, got %q", alias, body)
		}
	}
	_ = msg
}

func TestStartupReadyChatIDsDeduplicatesChats(t *testing.T) {
	ids := startupReadyChatIDs([]*state.Session{
		{ChatID: "chat-b"},
		{ChatID: "chat-a"},
		{ChatID: "chat-b"},
		{ChatID: ""},
		nil,
	})
	if len(ids) != 2 {
		t.Fatalf("unexpected chat id count: %#v", ids)
	}
	if ids[0] != "chat-a" || ids[1] != "chat-b" {
		t.Fatalf("unexpected sorted chat ids: %#v", ids)
	}
}

func TestCommandFastTogglesAndSupportsConfigCard(t *testing.T) {
	store, err := state.Open(t.TempDir() + "/state.json")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	ff := &fakeFeishuClient{}
	a := &App{store: store, feishu: ff}
	if err := a.store.UpsertSession(&state.Session{
		Key:                     "feishu:p2p:chat:user",
		WorkspaceID:             "default",
		ActiveThreadID:          "thread-1",
		ActiveThreadWorkspaceID: "default",
	}); err != nil {
		t.Fatalf("upsert session: %v", err)
	}
	msg := &feishu.InboundMessage{MessageID: "m-1", ChatID: "chat", ChatType: "p2p", UserID: "user"}
	if err := commandFast(a, msg, nil); err != nil {
		t.Fatalf("commandFast(toggle to fast) error = %v", err)
	}
	sess := a.store.GetSession("feishu:p2p:chat:user")
	if sess == nil || sess.ActiveThreadServiceTier != "fast" {
		t.Fatalf("expected service tier fast, got %#v", sess)
	}
	if err := commandFast(a, msg, []string{"config"}); err != nil {
		t.Fatalf("commandFast(config) error = %v", err)
	}
	if len(ff.replyCards) != 1 {
		t.Fatalf("reply card count = %d, want 1", len(ff.replyCards))
	}
	if err := commandFast(a, msg, []string{"default"}); err != nil {
		t.Fatalf("commandFast(set default) error = %v", err)
	}
	sess = a.store.GetSession("feishu:p2p:chat:user")
	if sess == nil || sess.ActiveThreadServiceTier != "" {
		t.Fatalf("expected service tier default, got %#v", sess)
	}
}

func TestCommandCompactCallsThreadCompactStart(t *testing.T) {
	store, err := state.Open(t.TempDir() + "/state.json")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	fc := &fakeCodexClient{}
	ff := &fakeFeishuClient{}
	a := &App{store: store, codex: fc, feishu: ff, cfg: testCodexConfig()}
	if err := a.store.UpsertSession(&state.Session{
		Key:            "feishu:p2p:chat:user",
		WorkspaceID:    "default",
		ActiveThreadID: "thread-1",
	}); err != nil {
		t.Fatalf("upsert session: %v", err)
	}

	var gotMethod string
	var gotThreadID string
	fc.callHook = func(_ context.Context, method string, params any, _ any) error {
		gotMethod = method
		raw, _ := params.(map[string]any)
		gotThreadID, _ = raw["threadId"].(string)
		return nil
	}

	msg := &feishu.InboundMessage{MessageID: "m-1", ChatID: "chat", ChatType: "p2p", UserID: "user"}
	if err := commandCompact(a, msg, nil); err != nil {
		t.Fatalf("commandCompact() error = %v", err)
	}
	if gotMethod != "thread/compact/start" || gotThreadID != "thread-1" {
		t.Fatalf("compact call = %q %q, want thread/compact/start thread-1", gotMethod, gotThreadID)
	}
	if len(ff.replyTexts) == 0 || !strings.Contains(ff.replyTexts[0], "压缩当前线程上下文") {
		t.Fatalf("compact reply = %#v, want success text", ff.replyTexts)
	}
	sess := a.store.GetSession("feishu:p2p:chat:user")
	if sess == nil || sess.Status != sessionStatusCompacting {
		t.Fatalf("session after /compact = %+v, want compacting", sess)
	}
}

func TestCommandCompactRestoresSessionWhenRPCFails(t *testing.T) {
	store, err := state.Open(t.TempDir() + "/state.json")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	fc := &fakeCodexClient{callErr: context.DeadlineExceeded}
	a := &App{store: store, codex: fc, feishu: &fakeFeishuClient{}, cfg: testCodexConfig()}
	if err := a.store.UpsertSession(&state.Session{
		Key:            "feishu:p2p:chat:user",
		WorkspaceID:    "default",
		ActiveThreadID: "thread-1",
		Status:         "idle",
	}); err != nil {
		t.Fatalf("upsert session: %v", err)
	}

	msg := &feishu.InboundMessage{MessageID: "m-1", ChatID: "chat", ChatType: "p2p", UserID: "user"}
	if err := commandCompact(a, msg, nil); err == nil {
		t.Fatal("expected commandCompact() to fail")
	}
	sess := a.store.GetSession("feishu:p2p:chat:user")
	if sess == nil || sess.Status != "idle" || sess.ActiveTurnID != "" {
		t.Fatalf("session after failed /compact = %+v, want idle without turn", sess)
	}
}

func TestHandleCommandCompactPassthroughsToClaude(t *testing.T) {
	store, err := state.Open(t.TempDir() + "/state.json")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	cfg := testCodexConfig()
	cfg.Feishu.Backend = backendClaude
	claude := &fakeClaudeCore{}
	a := &App{store: store, claude: claude, feishu: &fakeFeishuClient{}, cfg: cfg}

	msg := &feishu.InboundMessage{
		MessageID: "m-1",
		ChatID:    "chat",
		ChatType:  "p2p",
		UserID:    "user",
		Text:      "/compact",
	}
	if err := handleCommand(a, msg, "/compact"); err != nil {
		t.Fatalf("handleCommand(/compact) error = %v", err)
	}
	if len(claude.startTurnCalls) != 1 || !strings.Contains(claude.startTurnCalls[0].prompt, "/compact") {
		t.Fatalf("Claude startTurn calls = %#v", claude.startTurnCalls)
	}
	sess := a.store.GetSession("feishu:p2p:chat:user")
	if sess == nil || strings.TrimSpace(sess.ActiveThreadID) == "" || strings.TrimSpace(sess.ActiveSubmissionID) == "" {
		t.Fatalf("session after Claude /compact = %+v", sess)
	}
	sub := a.store.GetSubmission(strings.TrimSpace(sess.ActiveSubmissionID))
	if sub == nil || sub.InputText != "/compact" {
		t.Fatalf("Claude compact submission = %+v", sub)
	}
}

func TestCommandForkCallsThreadForkAndSwitchesSession(t *testing.T) {
	store, err := state.Open(t.TempDir() + "/state.json")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	fc := &fakeCodexClient{}
	ff := &fakeFeishuClient{}
	cfg := testCodexConfig()
	a := &App{store: store, codex: fc, feishu: ff, cfg: cfg}
	if err := a.store.UpsertSession(&state.Session{
		Key:                        "feishu:p2p:chat:user",
		WorkspaceID:                "default",
		ActiveThreadID:             "thread-1",
		ActiveThreadWorkspaceID:    "default",
		ActiveThreadApprovalPolicy: "never",
		ActiveThreadSandboxMode:    "read-only",
		ActiveThreadServiceTier:    serviceTierFast,
		Status:                     "idle",
	}); err != nil {
		t.Fatalf("upsert session: %v", err)
	}

	var gotMethod string
	var gotParams map[string]any
	fc.callHook = func(_ context.Context, method string, params any, out any) error {
		gotMethod = method
		gotParams, _ = params.(map[string]any)
		result := out.(*codexrpc.ThreadStartResult)
		result.Thread.ID = "thread-forked"
		result.Thread.Name = "Forked Thread"
		result.Thread.Preview = "fork preview"
		return nil
	}

	msg := &feishu.InboundMessage{MessageID: "m-fork", ChatID: "chat", ChatType: "p2p", UserID: "user"}
	if err := commandFork(a, msg, nil); err != nil {
		t.Fatalf("commandFork() error = %v", err)
	}
	if gotMethod != "thread/fork" {
		t.Fatalf("fork call method = %q, want thread/fork", gotMethod)
	}
	if got, _ := gotParams["threadId"].(string); got != "thread-1" {
		t.Fatalf("fork threadId = %q, want thread-1", got)
	}
	if got, _ := gotParams["cwd"].(string); got != cfg.Workspaces[0].Cwd {
		t.Fatalf("fork cwd = %q, want %q", got, cfg.Workspaces[0].Cwd)
	}
	if got, _ := gotParams["approvalPolicy"].(string); got != "never" {
		t.Fatalf("fork approvalPolicy = %q, want never", got)
	}
	if got, _ := gotParams["sandbox"].(string); got != "read-only" {
		t.Fatalf("fork sandbox = %q, want read-only", got)
	}
	if got, _ := gotParams["serviceTier"].(string); got != serviceTierFast {
		t.Fatalf("fork serviceTier = %q, want %q", got, serviceTierFast)
	}
	sess := a.store.GetSession("feishu:p2p:chat:user")
	if sess == nil || sess.ActiveThreadID != "thread-forked" || sess.ActiveThreadName != "Forked Thread" || sess.Status != "idle" {
		t.Fatalf("session after /fork = %+v", sess)
	}
	if len(ff.replyTexts) == 0 || !strings.Contains(ff.replyTexts[0], "forked current thread") {
		t.Fatalf("fork reply = %#v, want success text", ff.replyTexts)
	}
}

func TestCommandDebugTogglesRuntimeLogLevel(t *testing.T) {
	ff := &fakeFeishuClient{}
	a := &App{feishu: ff, cfg: testCodexConfig()}
	a.cfg.Feishu.DebugAllowFrom = []string{"user"}
	prev := runtimeLogLevelText()
	t.Cleanup(func() {
		_ = logcontrol.SetName(prev)
		a.cfg.Log.Level = runtimeLogLevelText()
	})

	msg := &feishu.InboundMessage{MessageID: "m-debug", ChatID: "chat", ChatType: "p2p", UserID: "user"}
	newDebugService(a).SetRuntimeDebug(false)
	if err := newDebugService(a).CommandDebug(msg, nil); err != nil {
		t.Fatalf("CommandDebug(toggle on) error = %v", err)
	}
	if got := runtimeLogLevelText(); got != "debug" {
		t.Fatalf("runtimeLogLevelText() = %q, want debug", got)
	}
	if err := newDebugService(a).CommandDebug(msg, []string{"off"}); err != nil {
		t.Fatalf("commandDebug(off) error = %v", err)
	}
	if got := runtimeLogLevelText(); got != "info" {
		t.Fatalf("runtimeLogLevelText() = %q, want info", got)
	}
	if err := newDebugService(a).CommandDebug(msg, []string{"bad"}); err == nil {
		t.Fatal("expected invalid /debug arg to fail")
	}
	if len(ff.replyTexts) < 2 || !strings.Contains(ff.replyTexts[0], "`debug`") || !strings.Contains(ff.replyTexts[1], "`info`") {
		t.Fatalf("debug replies = %#v", ff.replyTexts)
	}
}

func TestClaudeForkCommandsStartNewSession(t *testing.T) {
	for _, raw := range []string{"/fork", "/session fork"} {
		t.Run(raw, func(t *testing.T) {
			a, ff, _ := newTestApp(t)
			a.cfg.Feishu.Backend = backendClaude
			a.cfg.Claude.Model = "mimo-v2-pro"
			a.codex = nil
			claude := &fakeClaudeCore{
				forkSessionID:  "claude-forked",
				forkSessionSet: true,
			}
			a.claude = claude

			sessionKey := "feishu:p2p:chat:user"
			if err := a.store.UpsertSession(&state.Session{
				Key:                     sessionKey,
				WorkspaceID:             a.cfg.Workspaces[0].ID,
				ActiveThreadID:          "claude-parent",
				ActiveThreadWorkspaceID: a.cfg.Workspaces[0].ID,
				ActiveThreadName:        "Claude Parent",
				ActiveThreadPreview:     "parent preview",
				Status:                  "idle",
			}); err != nil {
				t.Fatalf("UpsertSession() error = %v", err)
			}

			msg := &feishu.InboundMessage{MessageID: "m-claude-fork", ChatID: "chat", ChatType: "p2p", UserID: "user"}
			if err := handleCommand(a, msg, raw); err != nil {
				t.Fatalf("handleCommand(%q) error = %v", raw, err)
			}
			if len(claude.forkCalls) != 1 {
				t.Fatalf("ForkSession calls = %#v, want 1", claude.forkCalls)
			}
			if got := claude.forkCalls[0].sourceSessionID; got != "claude-parent" {
				t.Fatalf("ForkSession sourceSessionID = %q, want claude-parent", got)
			}
			if got := claude.forkCalls[0].model; got != "mimo-v2-pro" {
				t.Fatalf("ForkSession model = %q, want mimo-v2-pro", got)
			}
			sess := a.store.GetSession(sessionKey)
			if sess == nil || sess.ActiveThreadID != "claude-forked" || sess.ActiveThreadName != "Claude Parent" || sess.Status != "idle" {
				t.Fatalf("session after %s = %+v", raw, sess)
			}
			if len(ff.replyTexts) == 0 || !strings.Contains(ff.replyTexts[0], "forked current session") {
				t.Fatalf("fork reply = %#v, want success text", ff.replyTexts)
			}
		})
	}
}

func TestClaudeForkCommandsPreparePendingSessionWhenIDNotReady(t *testing.T) {
	a, ff, _ := newTestApp(t)
	a.cfg.Feishu.Backend = backendClaude
	a.cfg.Claude.Model = "mimo-v2-pro"
	a.codex = nil
	claude := &fakeClaudeCore{
		forkSessionID:  "",
		forkSessionSet: true,
	}
	a.claude = claude

	sessionKey := "feishu:p2p:chat:user"
	if err := a.store.UpsertSession(&state.Session{
		Key:                     sessionKey,
		WorkspaceID:             a.cfg.Workspaces[0].ID,
		ActiveThreadID:          "claude-parent",
		ActiveThreadWorkspaceID: a.cfg.Workspaces[0].ID,
		ActiveThreadName:        "Claude Parent",
		ActiveThreadPreview:     "parent preview",
		Status:                  "idle",
	}); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}
	markSessionThreadLive(a, sessionKey, "claude-parent")

	msg := &feishu.InboundMessage{MessageID: "m-claude-fork-pending", ChatID: "chat", ChatType: "p2p", UserID: "user"}
	if err := handleCommand(a, msg, "/fork"); err != nil {
		t.Fatalf("handleCommand(/fork) error = %v", err)
	}
	if len(claude.forkCalls) != 1 {
		t.Fatalf("ForkSession calls = %#v, want 1", claude.forkCalls)
	}
	sess := a.store.GetSession(sessionKey)
	if sess == nil || sess.ActiveThreadID != "" || sess.ActiveThreadWorkspaceID != a.cfg.Workspaces[0].ID || sess.ActiveThreadName != "Claude Parent" || sess.Status != "idle" {
		t.Fatalf("session after pending /fork = %+v", sess)
	}
	if sessionHasLiveThread(a, sessionKey, "claude-parent") {
		t.Fatalf("expected old live thread binding to be cleared after pending /fork")
	}
	if len(ff.replyTexts) == 0 || !strings.Contains(ff.replyTexts[0], "next message") {
		t.Fatalf("fork reply = %#v, want pending fork text", ff.replyTexts)
	}
}

func TestCommandDebugLogsShowsRecentLogContent(t *testing.T) {
	ff := &fakeFeishuClient{}
	a := &App{feishu: ff, cfg: testCodexConfig()}
	a.cfg.Feishu.DebugAllowFrom = []string{"user"}
	prevLevel := runtimeLogLevelText()
	oldLogger := slog.Default()
	t.Cleanup(func() {
		slog.SetDefault(oldLogger)
		_ = logcontrol.SetName(prevLevel)
		a.cfg.Log.Level = runtimeLogLevelText()
	})
	_ = logcontrol.SetName("debug")
	logger := slog.New(logcontrol.NewHandler(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: logcontrol.LevelVar()})))
	slog.SetDefault(logger)
	slog.Debug("debug-log-test", "key", "value")

	msg := &feishu.InboundMessage{MessageID: "m-logs", ChatID: "chat", ChatType: "p2p", UserID: "user"}
	if err := newDebugService(a).CommandDebug(msg, []string{"logs"}); err != nil {
		t.Fatalf("commandDebug(logs) error = %v", err)
	}
	if len(ff.replyCards) == 0 {
		t.Fatal("expected debug logs card")
	}
	elements := cardElementsForTest(ff.replyCards[len(ff.replyCards)-1])
	if len(elements) < 2 || elements[0]["tag"] != "div" || elements[1]["tag"] != "div" {
		t.Fatalf("debug logs card elements = %#v, want summary/log div blocks first", elements)
	}
	buttons := cardButtonsForTest(ff.replyCards[len(ff.replyCards)-1])
	if len(buttons) != 2 {
		t.Fatalf("debug logs card buttons = %#v, want refresh + back", buttons)
	}
	body := cardMarkdownContent(t, ff.replyCards[len(ff.replyCards)-1])
	if !strings.Contains(body, "debug-log-test") || !strings.Contains(body, "key=value") {
		t.Fatalf("debug logs card body = %q", body)
	}
	if strings.Contains(body, "```") {
		t.Fatalf("debug logs card should use plain_text blocks, got %q", body)
	}
}

func TestCommandDebugLogsRejectsUnauthorizedUser(t *testing.T) {
	ff := &fakeFeishuClient{}
	a := &App{feishu: ff, cfg: testCodexConfig(), cfgPath: "/etc/feidex/config.toml"}
	a.cfg.Feishu.DebugAllowFrom = []string{"allowed-user"}

	msg := &feishu.InboundMessage{MessageID: "m-logs", ChatID: "chat", ChatType: "p2p", UserID: "blocked-user"}
	if err := newDebugService(a).CommandDebug(msg, []string{"logs"}); err != nil {
		t.Fatalf("commandDebug(logs blocked) error = %v", err)
	}
	if len(ff.replyCards) != 1 {
		t.Fatalf("replyCards = %#v, want one denied card", ff.replyCards)
	}
	body := cardMarkdownContent(t, ff.replyCards[0])
	for _, want := range []string{"blocked-user", "debug_allow_from", "/etc/feidex/config.toml"} {
		if !strings.Contains(body, want) {
			t.Fatalf("denied debug logs card body = %q, want %q", body, want)
		}
	}
}

func TestCompleteMenuDebugLogsRejectsUnauthorizedUser(t *testing.T) {
	ff := &fakeFeishuClient{}
	a := &App{cfg: testCodexConfig(), feishu: wrapFeishuClient(ff), cfgPath: "/etc/feidex/config.toml"}
	a.cfg.Feishu.DebugAllowFrom = []string{"allowed-user"}

	resp, err := newDebugService(a).CompleteMenuDebugLogs(&feishu.CardAction{UserID: "blocked-user"}, "sess-1")
	if err != nil {
		t.Fatalf("completeMenuDebugLogs(blocked) error = %v", err)
	}
	if resp == nil || resp.Toast == nil || resp.Card == nil {
		t.Fatalf("completeMenuDebugLogs(blocked) = %#v, want denied card response", resp)
	}
	if resp.Toast.Content != "已执行 /debug logs" {
		t.Fatalf("debug logs toast = %#v, want command execution toast", resp.Toast)
	}
	cardData, _ := resp.Card.Data.(map[string]any)
	cardBody := cardMarkdownContent(t, cardData)
	for _, want := range []string{"blocked-user", "debug_allow_from", "/etc/feidex/config.toml"} {
		if !strings.Contains(cardBody, want) {
			t.Fatalf("denied debug logs card body = %q, want %q", cardBody, want)
		}
	}
}

func TestCommandDebugRejectsUnauthorizedUserWithCard(t *testing.T) {
	ff := &fakeFeishuClient{}
	a := &App{feishu: ff, cfg: testCodexConfig(), cfgPath: "/etc/feidex/config.toml"}
	a.cfg.Feishu.DebugAllowFrom = []string{"allowed-user"}

	msg := &feishu.InboundMessage{MessageID: "m-debug", ChatID: "chat", ChatType: "p2p", UserID: "blocked-user"}
	if err := newDebugService(a).CommandDebug(msg, nil); err != nil {
		t.Fatalf("commandDebug(blocked) error = %v", err)
	}
	if len(ff.replyCards) != 1 {
		t.Fatalf("replyCards = %#v, want one denied card", ff.replyCards)
	}
	body := cardMarkdownContent(t, ff.replyCards[0])
	if !strings.Contains(body, "blocked-user") || !strings.Contains(body, "debug_allow_from") {
		t.Fatalf("denied debug card body = %q", body)
	}
}

func TestCompleteMenuDebugRejectsUnauthorizedUserWithCard(t *testing.T) {
	ff := &fakeFeishuClient{}
	a := &App{cfg: testCodexConfig(), feishu: wrapFeishuClient(ff), cfgPath: "/etc/feidex/config.toml"}
	a.cfg.Feishu.DebugAllowFrom = []string{"allowed-user"}

	resp, err := newDebugService(a).CompleteMenuDebug(&feishu.CardAction{UserID: "blocked-user"}, "sess-1")
	if err != nil {
		t.Fatalf("completeMenuDebug(blocked) error = %v", err)
	}
	if resp == nil || resp.Toast == nil || resp.Card == nil {
		t.Fatalf("completeMenuDebug(blocked) = %#v, want denied card response", resp)
	}
	if resp.Toast.Content != "已执行 /debug" {
		t.Fatalf("debug toast = %#v, want command execution toast", resp.Toast)
	}
	cardData, _ := resp.Card.Data.(map[string]any)
	cardBody := cardMarkdownContent(t, cardData)
	for _, want := range []string{"blocked-user", "debug_allow_from", "/etc/feidex/config.toml"} {
		if !strings.Contains(cardBody, want) {
			t.Fatalf("denied debug card body = %q, want %q", cardBody, want)
		}
	}
}
