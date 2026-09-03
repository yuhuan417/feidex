package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	appworkspacecmd "feidex/internal/app/workspacecmd"
	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

func TestGroupMessagePolicyRoutesPrimaryMentionsAndReplies(t *testing.T) {
	store, err := state.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("state.Open() error = %v", err)
	}
	cfg := config.Default()
	cfg.Feishu.GroupAtOnly = true
	a := &App{
		cfg:        cfg,
		store:      store,
		frontendID: "frontend-a",
		feishu:     wrapFeishuClient(&fakeFeishuClient{botOpenID: "bot-a-open"}),
	}
	if err := a.State().SaveAgentBinding(&state.AgentBinding{
		ID:       "binding-primary",
		ChatID:   "chat-1",
		ChatType: "group",
		Status:   state.AgentBindingStatusActive.String(),
	}); err != nil {
		t.Fatalf("SaveAgentBinding(primary) error = %v", err)
	}
	if _, err := setGroupPrimary(a, "group", "chat-1", true); err != nil {
		t.Fatalf("setGroupPrimary() error = %v", err)
	}

	if !shouldAcceptGroupMessage(a, "chat-1", "", "", false, false) {
		t.Fatal("primary binding rejected an unmentioned group message")
	}
	defaultedTopLevel := &feishu.InboundMessage{MessageID: "top-1", RootMessageID: "top-1", ChatType: "group", ChatID: "chat-1"}
	if got := groupPolicyRootMessageID(defaultedTopLevel); got != "" {
		t.Fatalf("groupPolicyRootMessageID(defaulted top-level) = %q, want empty", got)
	}
	if !shouldAcceptGroupMessage(a, "chat-1", groupPolicyRootMessageID(defaultedTopLevel), defaultedTopLevel.ParentMessageID, false, false) {
		t.Fatal("primary binding rejected an unmentioned top-level group message with defaulted root")
	}
	if !shouldAcceptGroupMessage(a, "chat-1", "", "", true, true) {
		t.Fatal("primary binding rejected a direct self mention")
	}
	if shouldAcceptGroupMessage(a, "chat-1", "", "", false, true) {
		t.Fatal("primary binding accepted a mention of another bot")
	}

	if err := store.UpsertMessageLink(&state.MessageLink{
		FrontendID: "frontend-a",
		MessageID:  "bot-reply-1",
		SessionKey: "feishu:frontend:frontend-a:chat:chat-1",
	}); err != nil {
		t.Fatalf("UpsertMessageLink() error = %v", err)
	}
	if !shouldAcceptGroupMessage(a, "chat-1", "bot-reply-1", "user-reply-1", false, false) {
		t.Fatal("primary binding rejected a reply to its own message")
	}
	reply := &feishu.InboundMessage{MessageID: "reply-1", RootMessageID: "bot-reply-1", ParentMessageID: "bot-reply-1", ChatType: "group", ChatID: "chat-1"}
	if got := groupPolicyRootMessageID(reply); got != "bot-reply-1" {
		t.Fatalf("groupPolicyRootMessageID(reply) = %q, want bot-reply-1", got)
	}
	if shouldAcceptGroupMessage(a, "chat-1", "other-bot-reply", "user-reply-2", false, false) {
		t.Fatal("primary binding accepted a reply without a local message link")
	}
	if err := store.UpsertMessageLink(&state.MessageLink{
		FrontendID: "frontend-a",
		MessageID:  "bot-parent-1",
		SessionKey: "feishu:frontend:frontend-a:chat:chat-1",
	}); err != nil {
		t.Fatalf("UpsertMessageLink(parent) error = %v", err)
	}
	if !shouldAcceptGroupMessage(a, "chat-1", "original-root-1", "bot-parent-1", false, false) {
		t.Fatal("primary binding rejected a reply with only a parent message link")
	}

	if err := a.State().SaveAgentBinding(&state.AgentBinding{
		ID:       "binding-pending",
		ChatID:   "chat-2",
		ChatType: "group",
		Status:   state.AgentBindingStatusPending.String(),
	}); err != nil {
		t.Fatalf("SaveAgentBinding(pending) error = %v", err)
	}
	if !shouldAcceptGroupMessage(a, "chat-2", "", "", true, true) {
		t.Fatal("pending binding rejected direct onboarding mention")
	}
	if shouldAcceptGroupMessage(a, "chat-2", "", "", false, false) {
		t.Fatal("pending binding accepted an unmentioned message")
	}
	if err := a.State().SaveAgentBinding(&state.AgentBinding{
		ID:       "binding-pending-primary",
		ChatID:   "chat-3",
		ChatType: "group",
		Status:   state.AgentBindingStatusPending.String(),
	}); err != nil {
		t.Fatalf("SaveAgentBinding(pending primary) error = %v", err)
	}
	if _, err := setGroupPrimary(a, "group", "chat-3", true); err != nil {
		t.Fatalf("setGroupPrimary(chat-3) error = %v", err)
	}
	if !shouldAcceptGroupMessage(a, "chat-3", "", "", false, false) {
		t.Fatal("pending primary binding rejected an unmentioned message")
	}
}

func TestGroupMessagePolicyKeepsNonPrimaryRepliesLocal(t *testing.T) {
	store, err := state.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("state.Open() error = %v", err)
	}
	cfg := config.Default()
	a := &App{cfg: cfg, store: store, frontendID: "frontend-b", feishu: wrapFeishuClient(&fakeFeishuClient{botOpenID: "bot-b-open"})}
	if err := a.State().SaveAgentBinding(&state.AgentBinding{
		ID:       "binding-client",
		ChatID:   "chat-1",
		ChatType: "group",
		Status:   state.AgentBindingStatusActive.String(),
	}); err != nil {
		t.Fatalf("SaveAgentBinding() error = %v", err)
	}
	if _, err := setGroupPrimaryOwner(a, "group", "chat-1", "bot-a-open"); err != nil {
		t.Fatalf("setGroupPrimaryOwner() error = %v", err)
	}
	if shouldAcceptGroupMessage(a, "chat-1", "", "", false, false) {
		t.Fatal("non-primary binding accepted an unmentioned group message")
	}
	if !shouldAcceptGroupMessage(a, "chat-1", "", "", true, true) {
		t.Fatal("non-primary binding rejected direct mention")
	}
	if err := store.UpsertMessageLink(&state.MessageLink{
		FrontendID: "frontend-b",
		MessageID:  "client-reply",
		SessionKey: "feishu:frontend:frontend-b:chat:chat-1",
	}); err != nil {
		t.Fatalf("UpsertMessageLink() error = %v", err)
	}
	if !shouldAcceptGroupMessage(a, "chat-1", "client-reply", "user-reply", false, false) {
		t.Fatal("non-primary binding rejected reply to its own message")
	}
}

func TestGroupMessagePolicyDeliversUnknownTopLevelForPrimaryAutoInit(t *testing.T) {
	store, err := state.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("state.Open() error = %v", err)
	}
	cfg := config.Default()
	cfg.Feishu.GroupAtOnly = true
	a := &App{cfg: cfg, store: store, frontendID: "frontend-auto"}

	if shouldAcceptGroupMessage(a, "chat-new", "", "", false, false) {
		t.Fatal("app policy accepted unmentioned message before primary init")
	}
	if !shouldDeliverGroupMessageToApp(a, feishu.GroupMessagePolicyInput{ChatID: "chat-new"}) {
		t.Fatal("adapter policy rejected top-level message needed for primary init")
	}
	if shouldDeliverGroupMessageToApp(a, feishu.GroupMessagePolicyInput{ChatID: "chat-new", RootMessageID: "root-1", ParentMessageID: "parent-1"}) {
		t.Fatal("adapter policy delivered unrelated reply for primary init")
	}
	if shouldDeliverGroupMessageToApp(a, feishu.GroupMessagePolicyInput{ChatID: "chat-new", Text: "@bot-other hello", MentionedOpenIDs: []string{"bot-other"}}) {
		t.Fatal("adapter policy delivered explicit mention of another bot for primary init")
	}
	if shouldDeliverGroupMessageToApp(a, feishu.GroupMessagePolicyInput{ChatID: "chat-new", Text: "@unknown hello", MentionedAny: true}) {
		t.Fatal("adapter policy delivered mention event without current bot mention")
	}
	if !shouldDeliverGroupMessageToApp(a, feishu.GroupMessagePolicyInput{ChatID: "chat-new", Text: "@bot-b /primary on", MentionedOpenIDs: []string{"bot-b-open"}}) {
		t.Fatal("adapter policy rejected explicit primary owner assignment")
	}
	if shouldDeliverGroupMessageToApp(a, feishu.GroupMessagePolicyInput{ChatID: "chat-new", Text: "@bot-b", MentionedOpenIDs: []string{"bot-b-open"}}) {
		t.Fatal("adapter policy delivered mention-only primary owner assignment")
	}
	if shouldDeliverGroupMessageToApp(a, feishu.GroupMessagePolicyInput{ChatID: "chat-new", Text: "@bot-b hello", MentionedOpenIDs: []string{"bot-b-open"}}) {
		t.Fatal("adapter policy delivered ordinary explicit mention of another bot")
	}

	if _, err := setGroupPrimaryOwner(a, "group", "chat-new", "bot-other-open"); err != nil {
		t.Fatalf("setGroupPrimaryOwner(other) error = %v", err)
	}
	if shouldDeliverGroupMessageToApp(a, feishu.GroupMessagePolicyInput{ChatID: "chat-new"}) {
		t.Fatal("adapter policy delivered top-level message after non-primary owner state was initialized")
	}
}

func TestBindingUsesChatScopedGroupSessionKey(t *testing.T) {
	store, err := state.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("state.Open() error = %v", err)
	}
	cfg := config.Default()
	a := &App{cfg: cfg, store: store, frontendID: "frontend-a"}
	if err := a.State().SaveAgentBinding(&state.AgentBinding{
		ID:       "binding-server",
		ChatID:   "chat-1",
		ChatType: "group",
		Status:   state.AgentBindingStatusPending.String(),
	}); err != nil {
		t.Fatalf("SaveAgentBinding() error = %v", err)
	}
	first := makeSessionKey(a, &feishu.InboundMessage{ChatType: "group", ChatID: "chat-1", MessageID: "m-1"})
	second := makeSessionKey(a, &feishu.InboundMessage{ChatType: "group", ChatID: "chat-1", MessageID: "m-2", RootMessageID: "root-2"})
	firstWant := "feishu:frontend:frontend-a:chat:chat-1"
	secondWant := "feishu:frontend:frontend-a:chat:chat-1"
	if first != firstWant || second != secondWant || first != second {
		t.Fatalf("binding session keys = %q / %q, want %q / %q", first, second, firstWant, secondWant)
	}
	frontendID, chatType, chatID, rootID, userID := parseSessionKey(first)
	if frontendID != "frontend-a" || chatType != "" || chatID != "chat-1" || rootID != "" || userID != "" {
		t.Fatalf("parsed group session key = %q %q %q %q %q", frontendID, chatType, chatID, rootID, userID)
	}
}

func TestPrimaryBindingHandlesUnmentionedSlashCommand(t *testing.T) {
	a, ff, _ := newTestApp(t)
	a.frontendID = "bot-a"
	if err := a.State().SaveAgentBinding(&state.AgentBinding{
		ID:          "binding-primary-slash",
		FrontendID:  "bot-a",
		ChatID:      "chat-slash",
		ChatType:    "group",
		WorkspaceID: "default",
		Status:      state.AgentBindingStatusActive.String(),
	}); err != nil {
		t.Fatalf("SaveAgentBinding() error = %v", err)
	}
	ff.botOpenID = "bot-a-open"
	if _, err := setGroupPrimary(a, "group", "chat-slash", true); err != nil {
		t.Fatalf("setGroupPrimary(chat-slash) error = %v", err)
	}

	a.HandleFeishuMessage(&feishu.InboundMessage{
		MessageID:     "slash-1",
		ChatID:        "chat-slash",
		ChatType:      "group",
		UserID:        "user-1",
		Text:          "/menu",
		RootMessageID: "slash-1",
	})

	cards := ff.replyCardsSnapshot()
	if len(cards) != 1 {
		t.Fatalf("reply cards = %d, want 1", len(cards))
	}
	if body := cardMarkdownContent(t, cards[0]); !strings.Contains(body, "当前位置：主菜单") {
		t.Fatalf("/menu reply body = %q, want main menu", body)
	}
}

func TestGroupWorkspaceCloneWithoutURLIsHandledAsLocalCommand(t *testing.T) {
	a, ff, _ := newTestApp(t)
	a.frontendID = "bot-a"
	if err := a.State().SaveAgentBinding(&state.AgentBinding{
		ID:          "binding-workspace-clone",
		FrontendID:  "bot-a",
		ChatID:      "chat-clone",
		ChatType:    "group",
		WorkspaceID: "default",
		Status:      state.AgentBindingStatusActive.String(),
	}); err != nil {
		t.Fatalf("SaveAgentBinding() error = %v", err)
	}

	a.HandleFeishuMessage(&feishu.InboundMessage{
		MessageID:     "clone-1",
		ChatID:        "chat-clone",
		ChatType:      "group",
		UserID:        "user-1",
		Text:          "/workspace clone",
		RootMessageID: "clone-1",
		MentionedSelf: true,
	})

	cards := ff.replyCardsSnapshot()
	if len(cards) != 1 {
		t.Fatalf("reply cards = %d, want clone form card", len(cards))
	}
	if body := cardMarkdownContent(t, cards[0]); !strings.Contains(body, "从仓库创建") || !strings.Contains(body, "Git 地址") {
		t.Fatalf("/workspace clone reply body = %q, want clone form", body)
	}
	foundPending := false
	for _, pending := range a.State().PendingRequests() {
		if pending.Kind == "workspace_clone" && strings.Contains(pending.SessionKey, "chat-clone") {
			foundPending = true
		}
	}
	if !foundPending {
		t.Fatalf("missing workspace_clone pending request: %+v", a.State().PendingRequests())
	}
}

func TestGroupWorkspaceCloneWithoutURLInNewGroupDoesNotUseDefaultWorkspace(t *testing.T) {
	a, ff, _ := newTestApp(t)
	a.frontendID = "bot-a"
	defaultParent := filepath.Dir(a.cfg.Workspaces[0].Cwd)
	configParent := filepath.Dir(a.cfgPath)

	a.HandleFeishuMessage(&feishu.InboundMessage{
		MessageID:     "clone-new-group-1",
		ChatID:        "chat-new-clone",
		ChatType:      "group",
		UserID:        "user-1",
		Text:          "/workspace clone",
		RootMessageID: "clone-new-group-1",
		MentionedSelf: true,
	})

	binding := agentBindingForChat(a, "group", "chat-new-clone")
	if binding == nil || strings.TrimSpace(binding.WorkspaceID) != "" || binding.Status != state.AgentBindingStatusPending.String() {
		t.Fatalf("new group binding = %+v, want pending with empty workspace", binding)
	}
	cards := ff.replyCardsSnapshot()
	if len(cards) != 1 {
		t.Fatalf("reply cards = %d, want clone form card", len(cards))
	}
	body := cardMarkdownContent(t, cards[0])
	if !strings.Contains(body, "当前工作区: (未配置)") {
		t.Fatalf("clone form body = %q, want unconfigured current workspace", body)
	}
	if strings.Contains(body, "当前工作区: `default`") || strings.Contains(body, "已选父目录: `"+defaultParent+"`") {
		t.Fatalf("clone form leaked default workspace context: %q", body)
	}
	if !strings.Contains(body, "已选父目录: `"+configParent+"`") {
		t.Fatalf("clone form body = %q, want config directory parent %q", body, configParent)
	}

	foundPending := false
	for _, pending := range a.State().PendingRequests() {
		if pending.Kind != "workspace_clone" || !strings.Contains(pending.SessionKey, "chat-new-clone") {
			continue
		}
		foundPending = true
		var payload appworkspacecmd.ClonePayload
		if err := json.Unmarshal([]byte(pending.PayloadJSON), &payload); err != nil {
			t.Fatalf("clone payload unmarshal error = %v", err)
		}
		if strings.TrimSpace(payload.SelectedParentDir) != configParent {
			t.Fatalf("clone payload selected parent = %q, want config directory parent %q", payload.SelectedParentDir, configParent)
		}
	}
	if !foundPending {
		t.Fatalf("missing workspace_clone pending request: %+v", a.State().PendingRequests())
	}
}

func TestGroupWorkspaceCloneWithURLInNewGroupUsesConfigDirParent(t *testing.T) {
	a, _, _ := newTestApp(t)
	a.frontendID = "bot-a"
	defaultParent := filepath.Dir(a.cfg.Workspaces[0].Cwd)
	configParent := filepath.Dir(a.cfgPath)
	repoURL := "git@github.com:example/repo.git"

	origClone := workspaceGitClone
	defer func() { workspaceGitClone = origClone }()
	var gotTargetDir string
	workspaceGitClone = func(_ context.Context, _, targetDir string, _ workspaceCloneProgressReporter) error {
		gotTargetDir = targetDir
		return os.MkdirAll(filepath.Join(targetDir, ".git"), 0o755)
	}

	msg := &feishu.InboundMessage{
		MessageID:     "clone-new-group-url-1",
		ChatID:        "chat-new-clone-url",
		ChatType:      "group",
		UserID:        "user-1",
		Text:          "/workspace clone " + repoURL,
		RootMessageID: "clone-new-group-url-1",
		MentionedSelf: true,
	}
	if err := newBindingService(a).commandWorkspace(msg, []string{"clone", repoURL}); err != nil {
		t.Fatalf("group commandWorkspace(/workspace clone URL) error = %v", err)
	}

	if gotTargetDir == "" {
		t.Fatal("workspaceGitClone was not called")
	}
	if filepath.Dir(gotTargetDir) != configParent {
		t.Fatalf("clone target parent = %q, want config directory parent %q", filepath.Dir(gotTargetDir), configParent)
	}
	if filepath.Dir(gotTargetDir) == defaultParent {
		t.Fatalf("clone target parent leaked default workspace parent %q", defaultParent)
	}
	binding := agentBindingForChat(a, "group", "chat-new-clone-url")
	if binding == nil || binding.WorkspaceID != "repo" || binding.Status != state.AgentBindingStatusActive.String() {
		t.Fatalf("binding after clone = %+v, want active repo", binding)
	}
}

func TestGroupWorkspaceCloneMenuActionOpensCloneForm(t *testing.T) {
	a, _, _ := newTestApp(t)
	a.frontendID = "bot-a"
	if err := a.State().SaveAgentBinding(&state.AgentBinding{
		ID:          "binding-workspace-clone-menu",
		FrontendID:  "bot-a",
		ChatID:      "chat-clone-menu",
		ChatType:    "group",
		WorkspaceID: "default",
		Status:      state.AgentBindingStatusActive.String(),
	}); err != nil {
		t.Fatalf("SaveAgentBinding() error = %v", err)
	}

	resp, err := newCardActionService(a).dispatch(&feishu.CardAction{
		ActionValue: map[string]any{"action": "workspace.clone", "session_key": "feishu:frontend:bot-a:chat:chat-clone-menu"},
		UserID:      "user-1",
		ChatID:      "chat-clone-menu",
		MessageID:   "menu-card-1",
	})
	if err != nil || resp == nil || resp.Card == nil {
		t.Fatalf("workspace.clone action = %#v, %v", resp, err)
	}
	card := callbackResponseCard(resp)
	if body := cardMarkdownContent(t, card); !strings.Contains(body, "从仓库创建") || !strings.Contains(body, "Git 地址") {
		t.Fatalf("workspace.clone action card body = %q, want clone form", body)
	}
	foundPending := false
	for _, pending := range a.State().PendingRequests() {
		if pending.Kind == "workspace_clone" && strings.Contains(pending.SessionKey, "chat-clone-menu") {
			foundPending = true
		}
	}
	if !foundPending {
		t.Fatalf("missing workspace_clone pending request: %+v", a.State().PendingRequests())
	}
}
