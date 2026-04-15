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

	err = a.commandNew(&feishu.InboundMessage{
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

	a := &App{store: store, codex: codexrpc.New(config.CodexConfig{})}
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

	err = a.handleCommand(&feishu.InboundMessage{
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

func TestIsLocalCommand(t *testing.T) {
	cases := map[string]bool{
		"/menu":          true,
		"/help":          true,
		"/history":       true,
		"/model":         true,
		"/quiet":         true,
		"/quiet config":  true,
		"/debug":         true,
		"/debug on":      true,
		"/debug logs":    true,
		"/download":      true,
		"/compact":       true,
		"/fork":          true,
		"/new":           true,
		"/threads":       true,
		"/thread":        true,
		"/thread list":   true,
		"/thread new":    true,
		"/interrupt":     true,
		"/stop":          true,
		"/workspace":     true,
		"/status":        true,
		"/upgrade":       true,
		"/upgrade dev":   true,
		"/fast config":   true,
		"/upgrade local": true,
		"/upgrade path ./dist/feidex-linux-amd64": true,
		"/upgrade v0.3.0":                         true,
		"/append hello":                           false,
		"/model list":                             false,
		"/":                                       false,
		"/unknown value":                          false,
	}
	for input, want := range cases {
		if got := isLocalCommand(input); got != want {
			t.Fatalf("isLocalCommand(%q) = %v, want %v", input, got, want)
		}
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
	if err := a.commandFast(msg, nil); err != nil {
		t.Fatalf("commandFast(toggle to fast) error = %v", err)
	}
	sess := a.store.GetSession("feishu:p2p:chat:user")
	if sess == nil || sess.ActiveThreadServiceTier != "fast" {
		t.Fatalf("expected service tier fast, got %#v", sess)
	}
	if err := a.commandFast(msg, []string{"config"}); err != nil {
		t.Fatalf("commandFast(config) error = %v", err)
	}
	if len(ff.replyCards) != 1 {
		t.Fatalf("reply card count = %d, want 1", len(ff.replyCards))
	}
	if err := a.commandFast(msg, []string{"default"}); err != nil {
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
	a := &App{store: store, codex: fc, feishu: ff, cfg: config.Default()}
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
	if err := a.commandCompact(msg, nil); err != nil {
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
	a := &App{store: store, codex: fc, feishu: &fakeFeishuClient{}, cfg: config.Default()}
	if err := a.store.UpsertSession(&state.Session{
		Key:            "feishu:p2p:chat:user",
		WorkspaceID:    "default",
		ActiveThreadID: "thread-1",
		Status:         "idle",
	}); err != nil {
		t.Fatalf("upsert session: %v", err)
	}

	msg := &feishu.InboundMessage{MessageID: "m-1", ChatID: "chat", ChatType: "p2p", UserID: "user"}
	if err := a.commandCompact(msg, nil); err == nil {
		t.Fatal("expected commandCompact() to fail")
	}
	sess := a.store.GetSession("feishu:p2p:chat:user")
	if sess == nil || sess.Status != "idle" || sess.ActiveTurnID != "" {
		t.Fatalf("session after failed /compact = %+v, want idle without turn", sess)
	}
}

func TestCommandForkCallsThreadForkAndSwitchesSession(t *testing.T) {
	store, err := state.Open(t.TempDir() + "/state.json")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	fc := &fakeCodexClient{}
	ff := &fakeFeishuClient{}
	cfg := config.Default()
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
	if err := a.commandFork(msg, nil); err != nil {
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
	if len(ff.replyTexts) == 0 || !strings.Contains(ff.replyTexts[0], "fork 当前线程") {
		t.Fatalf("fork reply = %#v, want success text", ff.replyTexts)
	}
}

func TestCommandDebugTogglesRuntimeLogLevel(t *testing.T) {
	ff := &fakeFeishuClient{}
	a := &App{feishu: ff, cfg: config.Default()}
	a.cfg.Feishu.DebugAllowFrom = []string{"user"}
	prev := runtimeLogLevelText()
	t.Cleanup(func() {
		_ = logcontrol.SetName(prev)
		a.cfg.Log.Level = runtimeLogLevelText()
	})

	msg := &feishu.InboundMessage{MessageID: "m-debug", ChatID: "chat", ChatType: "p2p", UserID: "user"}
	a.setRuntimeDebug(false)
	if err := a.commandDebug(msg, nil); err != nil {
		t.Fatalf("commandDebug(toggle on) error = %v", err)
	}
	if got := runtimeLogLevelText(); got != "debug" {
		t.Fatalf("runtimeLogLevelText() = %q, want debug", got)
	}
	if err := a.commandDebug(msg, []string{"off"}); err != nil {
		t.Fatalf("commandDebug(off) error = %v", err)
	}
	if got := runtimeLogLevelText(); got != "info" {
		t.Fatalf("runtimeLogLevelText() = %q, want info", got)
	}
	if err := a.commandDebug(msg, []string{"bad"}); err == nil {
		t.Fatal("expected invalid /debug arg to fail")
	}
	if len(ff.replyTexts) < 2 || !strings.Contains(ff.replyTexts[0], "`debug`") || !strings.Contains(ff.replyTexts[1], "`info`") {
		t.Fatalf("debug replies = %#v", ff.replyTexts)
	}
}

func TestCommandDebugLogsShowsRecentLogContent(t *testing.T) {
	ff := &fakeFeishuClient{}
	a := &App{feishu: ff, cfg: config.Default()}
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
	if err := a.commandDebug(msg, []string{"logs"}); err != nil {
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
	a := &App{feishu: ff, cfg: config.Default(), cfgPath: "/etc/feidex/config.toml"}
	a.cfg.Feishu.DebugAllowFrom = []string{"allowed-user"}

	msg := &feishu.InboundMessage{MessageID: "m-logs", ChatID: "chat", ChatType: "p2p", UserID: "blocked-user"}
	if err := a.commandDebug(msg, []string{"logs"}); err != nil {
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
	a := &App{cfg: config.Default(), feishu: &fakeFeishuClient{}, cfgPath: "/etc/feidex/config.toml"}
	a.cfg.Feishu.DebugAllowFrom = []string{"allowed-user"}

	resp, err := a.completeMenuDebugLogs(&feishu.CardAction{UserID: "blocked-user"}, "sess-1")
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
	a := &App{feishu: ff, cfg: config.Default(), cfgPath: "/etc/feidex/config.toml"}
	a.cfg.Feishu.DebugAllowFrom = []string{"allowed-user"}

	msg := &feishu.InboundMessage{MessageID: "m-debug", ChatID: "chat", ChatType: "p2p", UserID: "blocked-user"}
	if err := a.commandDebug(msg, nil); err != nil {
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
	a := &App{cfg: config.Default(), feishu: &fakeFeishuClient{}, cfgPath: "/etc/feidex/config.toml"}
	a.cfg.Feishu.DebugAllowFrom = []string{"allowed-user"}

	resp, err := a.completeMenuDebug(&feishu.CardAction{UserID: "blocked-user"}, "sess-1")
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
