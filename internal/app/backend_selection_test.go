package app

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

func TestHandleFeishuMessageWithoutConfiguredBackendPromptsSelection(t *testing.T) {
	store, err := state.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("Open(store) error = %v", err)
	}
	ff := &fakeFeishuClient{}
	a := &App{
		cfg:     config.Default(),
		store:   store,
		feishu:  ff,
		started: time.Now(),
	}

	origLookPath := backendLookPath
	backendLookPath = func(file string) (string, error) {
		if file == "codex" {
			return "/usr/bin/codex", nil
		}
		return "", errors.New("not found")
	}
	defer func() {
		backendLookPath = origLookPath
	}()

	msg := &feishu.InboundMessage{MessageID: "m-1", ChatID: "chat-1", ChatType: "p2p", UserID: "user-1", Text: "hello"}
	a.handleFeishuMessage(msg)

	if len(ff.replyCards) != 1 {
		t.Fatalf("replyCards = %d, want 1 backend selection card", len(ff.replyCards))
	}
	body := cardMarkdownContent(t, ff.replyCards[0])
	if !strings.Contains(body, "当前 frontend 还没有设置 backend") || !strings.Contains(body, "`codex`") {
		t.Fatalf("backend selection body = %q", body)
	}
	if sess := store.GetSession(a.makeSessionKey(msg)); sess != nil {
		t.Fatalf("unexpected session persisted while backend unset: %+v", sess)
	}
}

func TestCommandBackendShowsOnlyAvailableBackends(t *testing.T) {
	ff := &fakeFeishuClient{}
	a := &App{cfg: config.Default(), feishu: ff}

	origLookPath := backendLookPath
	backendLookPath = func(file string) (string, error) {
		if file == "codex" {
			return "/usr/bin/codex", nil
		}
		return "", errors.New("not found")
	}
	defer func() {
		backendLookPath = origLookPath
	}()

	msg := &feishu.InboundMessage{MessageID: "m-1", ChatID: "chat-1", ChatType: "p2p", UserID: "user-1", Text: "/backend"}
	if err := a.commandBackend(msg, nil); err != nil {
		t.Fatalf("commandBackend() error = %v", err)
	}

	if len(ff.replyCards) != 1 {
		t.Fatalf("replyCards = %d, want 1", len(ff.replyCards))
	}
	body := cardMarkdownContent(t, ff.replyCards[0])
	if !strings.Contains(body, "`codex`") || strings.Contains(body, "`claude`") {
		t.Fatalf("backend selection body = %q", body)
	}
	buttons := cardButtonsForTest(ff.replyCards[0])
	if len(buttons) == 0 || !cardHasButtonText(ff.replyCards[0], "Codex") {
		t.Fatalf("backend selection buttons = %#v", buttons)
	}
}

func TestHandleCardActionMenuGroupBackendOpensBackendMenuCard(t *testing.T) {
	a, _, _ := newTestApp(t)

	resp, err := a.handleCardAction(&feishu.CardAction{
		ActionValue: map[string]any{"action": "menu.group.backend", "session_key": "sess-1"},
		UserID:      "user-1",
		ChatID:      "chat-1",
	})
	if err != nil {
		t.Fatalf("handleCardAction(menu.group.backend) error = %v", err)
	}
	if resp == nil || resp.Card == nil {
		t.Fatalf("handleCardAction(menu.group.backend) = %#v, want card response", resp)
	}
	cardData, _ := resp.Card.Data.(map[string]any)
	body := cardMarkdownContent(t, cardData)
	if !strings.Contains(body, "当前位置：主菜单 / 系统运维 / 后端选择") {
		t.Fatalf("backend menu body = %q", body)
	}
}

func TestBackendSelectionCardsUseBackendSwitchPath(t *testing.T) {
	a, _, _ := newTestApp(t)

	selection := a.renderBackendSelectionCard("sess-1", "")
	if body := cardMarkdownContent(t, selection); !strings.Contains(body, "当前位置：主菜单 / 系统运维 / 后端选择 / 切换后端") {
		t.Fatalf("backend selection body = %q", body)
	}
	foundBack := false
	for _, button := range cardButtonsForTest(selection) {
		value, _ := button["value"].(map[string]any)
		if len(value) == 0 {
			behaviors, _ := button["behaviors"].([]map[string]any)
			if len(behaviors) > 0 {
				value, _ = behaviors[0]["value"].(map[string]any)
			}
		}
		if action, _ := value["action"].(string); action == "menu.group.backend" {
			foundBack = true
			break
		}
	}
	if !foundBack {
		t.Fatalf("backend selection buttons = %#v, want return to menu.group.backend", cardButtonsForTest(selection))
	}

	switching := a.renderBackendSwitchingCard("sess-1", backendClaude)
	if body := cardMarkdownContent(t, switching); !strings.Contains(body, "当前位置：主菜单 / 系统运维 / 后端选择 / 切换后端") {
		t.Fatalf("backend switching body = %q", body)
	}
	buttons := cardButtonsForTest(switching)
	if len(buttons) != 1 {
		t.Fatalf("backend switching buttons = %#v, want 1", buttons)
	}
	value, _ := buttons[0]["value"].(map[string]any)
	if len(value) == 0 {
		behaviors, _ := buttons[0]["behaviors"].([]map[string]any)
		if len(behaviors) > 0 {
			value, _ = behaviors[0]["value"].(map[string]any)
		}
	}
	if got, _ := value["action"].(string); got != "menu.backend.switch" {
		t.Fatalf("backend switching action = %q, want menu.backend.switch", got)
	}
}

func TestSwitchBackendRestoresPerBackendThreadLineage(t *testing.T) {
	store, err := state.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("Open(store) error = %v", err)
	}

	cfg := config.Default()
	cfg.Feishu.Backend = backendCodex
	cfg.Workspaces[0].Cwd = t.TempDir()
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("Save(config) error = %v", err)
	}

	origLookPath := backendLookPath
	origNewCodex := newCodexClient
	origNewClaude := newClaudeCore
	defer func() {
		backendLookPath = origLookPath
		newCodexClient = origNewCodex
		newClaudeCore = origNewClaude
	}()

	backendLookPath = func(file string) (string, error) {
		switch file {
		case "codex":
			return "/usr/bin/codex", nil
		case "claude":
			return "/usr/bin/claude", nil
		default:
			return "", errors.New("not found")
		}
	}

	createdCodex := []*fakeCodexClient{}
	newCodexClient = func(config.CodexConfig) codexClient {
		client := &fakeCodexClient{}
		createdCodex = append(createdCodex, client)
		return client
	}
	createdClaude := []*fakeClaudeCore{}
	newClaudeCore = func(_ *App, _ config.ClaudeConfig) claudeCore {
		client := &fakeClaudeCore{}
		createdClaude = append(createdClaude, client)
		return client
	}

	a := &App{
		cfg:         cfg,
		cfgPath:     cfgPath,
		store:       store,
		backend:     backendCodex,
		codex:       &fakeCodexClient{},
		feishu:      &fakeFeishuClient{},
		liveThreads: map[string]string{},
	}

	sessionKey := "feishu:p2p:chat-1:user-1"
	if err := store.UpsertSession(&state.Session{
		Key:                     sessionKey,
		WorkspaceID:             "default",
		ActiveThreadID:          "codex-thread-1",
		ActiveThreadWorkspaceID: "default",
		ActiveThreadName:        "Codex Thread",
		ActiveThreadPreview:     "codex preview",
		Status:                  "idle",
	}); err != nil {
		t.Fatalf("UpsertSession(codex) error = %v", err)
	}

	if err := a.switchBackend(context.Background(), backendClaude); err != nil {
		t.Fatalf("switchBackend(codex->claude) error = %v", err)
	}
	if len(createdClaude) != 1 {
		t.Fatalf("newClaudeCore calls = %d, want 1", len(createdClaude))
	}
	sess := store.GetSession(sessionKey)
	if sess == nil {
		t.Fatal("expected session to exist after claude switch")
	}
	if sess.ActiveThreadID != "" {
		t.Fatalf("active thread after claude switch = %q, want cleared until claude has its own lineage", sess.ActiveThreadID)
	}
	if snapshot := sess.BackendThreads[backendCodex]; snapshot.ThreadID != "codex-thread-1" {
		t.Fatalf("stored codex snapshot = %+v", snapshot)
	}

	sess.ActiveThreadID = "claude-session-1"
	sess.ActiveThreadWorkspaceID = "default"
	sess.ActiveThreadName = "Claude Session"
	sess.ActiveThreadPreview = "claude preview"
	if err := a.appState().saveSession(sess); err != nil {
		t.Fatalf("saveSession(claude lineage) error = %v", err)
	}

	if err := a.switchBackend(context.Background(), backendCodex); err != nil {
		t.Fatalf("switchBackend(claude->codex) error = %v", err)
	}
	if len(createdCodex) != 1 {
		t.Fatalf("newCodexClient calls = %d, want 1", len(createdCodex))
	}
	if !createdCodex[0].started {
		t.Fatal("expected switched codex runtime to be started")
	}
	sess = store.GetSession(sessionKey)
	if sess == nil {
		t.Fatal("expected session after codex switch")
	}
	if sess.ActiveThreadID != "codex-thread-1" || sess.ActiveThreadName != "Codex Thread" {
		t.Fatalf("restored codex lineage = %+v", sess)
	}
	if snapshot := sess.BackendThreads[backendClaude]; snapshot.ThreadID != "claude-session-1" {
		t.Fatalf("stored claude snapshot = %+v", snapshot)
	}
}

func TestReplyRootTurnLinkIgnoresMismatchedBackend(t *testing.T) {
	store, err := state.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("Open(store) error = %v", err)
	}
	cfg := testCodexConfig()
	a := &App{cfg: cfg, store: store, backend: backendClaude}

	sessionKey := "feishu:p2p:chat-1:user-1"
	if err := store.UpsertSession(&state.Session{
		Key:            sessionKey,
		WorkspaceID:    "default",
		ActiveThreadID: "claude-thread-1",
		Status:         "idle",
	}); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}
	if err := store.UpsertMessageLink(&state.MessageLink{
		MessageID:  "root-1",
		SessionKey: sessionKey,
		ThreadID:   "codex-thread-1",
		TurnID:     "turn-1",
		Backend:    backendCodex,
	}); err != nil {
		t.Fatalf("UpsertMessageLink() error = %v", err)
	}

	msg := &feishu.InboundMessage{MessageID: "child-1", ParentMessageID: "parent-1", RootMessageID: "root-1"}
	if link := a.replyRootTurnLink(msg); link != nil {
		t.Fatalf("replyRootTurnLink(claude current) = %+v, want nil", link)
	}

	a.backend = backendCodex
	if err := store.UpsertSession(&state.Session{
		Key:            sessionKey,
		WorkspaceID:    "default",
		ActiveThreadID: "codex-thread-1",
		Status:         "idle",
	}); err != nil {
		t.Fatalf("UpsertSession(codex) error = %v", err)
	}
	if link := a.replyRootTurnLink(msg); link == nil || link.ThreadID != "codex-thread-1" {
		t.Fatalf("replyRootTurnLink(codex current) = %+v, want codex link", link)
	}
}
