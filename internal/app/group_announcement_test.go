package app

import (
	"context"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

func newGroupAnnouncementTestApp(t *testing.T, store *state.Store, ff *fakeFeishuClient, frontendID string) *App {
	t.Helper()
	cfg := config.Default()
	cfg.Feishu.Backend = backendCodex
	cfg.Workspaces[0].Cwd = t.TempDir()
	return &App{
		cfg:         cfg,
		store:       store,
		frontendID:  strings.TrimSpace(frontendID),
		feishu:      wrapFeishuClient(ff),
		started:     time.Now(),
		liveThreads: newLiveThreadTracker(),
		trackers: appTrackers{
			groupAnnouncements: newGroupAnnouncementTracker(),
		},
	}
}

func newGroupAnnouncementStore(t *testing.T) *state.Store {
	t.Helper()
	store, err := state.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("state.Open() error = %v", err)
	}
	return store
}

func seedGroupAnnouncementBinding(t *testing.T, a *App, chatID string) {
	t.Helper()
	if err := a.State().SaveAgentBinding(&state.AgentBinding{
		ID:          defaultBindingID(a.FrontendID(), "group", chatID),
		FrontendID:  a.FrontendID(),
		ChatType:    "group",
		ChatID:      chatID,
		WorkspaceID: a.cfg.Workspaces[0].ID,
		Status:      state.AgentBindingStatusActive.String(),
	}); err != nil {
		t.Fatalf("SaveAgentBinding() error = %v", err)
	}
}

func seedGroupAnnouncementSession(t *testing.T, a *App, chatID, threadID string) {
	t.Helper()
	key := makeSessionKey(a, &feishu.InboundMessage{ChatType: "group", ChatID: chatID})
	if err := a.State().SaveSession(&state.Session{
		Key:            key,
		WorkspaceID:    a.cfg.Workspaces[0].ID,
		ActiveThreadID: threadID,
		ChatType:       "group",
		ChatID:         chatID,
		Status:         state.SessionStatusIdle.String(),
		UpdatedAt:      time.Now().Unix(),
	}); err != nil {
		t.Fatalf("SaveSession() error = %v", err)
	}
}

func TestGroupAnnouncementRefreshCreatesBlockAndSkipsStableContent(t *testing.T) {
	store := newGroupAnnouncementStore(t)
	ff := &fakeFeishuClient{botOpenID: "bot-open", botName: "luban-feidex"}
	a := newGroupAnnouncementTestApp(t, store, ff, "bot-a")
	seedGroupAnnouncementBinding(t, a, "chat-1")
	seedGroupAnnouncementSession(t, a, "chat-1", "thread-1")

	if err := refreshGroupAnnouncementStatusNow(context.Background(), a, "chat-1"); err != nil {
		t.Fatalf("refreshGroupAnnouncementStatusNow() error = %v", err)
	}
	if len(ff.announcementListCalls) != 1 || len(ff.announcementCreateCalls) != 1 || len(ff.announcementUpdateCalls) != 0 {
		t.Fatalf("announcement calls list/create/update = %d/%d/%d", len(ff.announcementListCalls), len(ff.announcementCreateCalls), len(ff.announcementUpdateCalls))
	}
	content := ff.announcementCreateCalls[0].content
	for _, want := range []string{
		"feidex-status-region:bot-a:bot-open",
		"Bot: luban-feidex",
		"Machine IP: ",
		"Workspace: " + a.cfg.Workspaces[0].Cwd,
		"Backend: codex",
		"Thread: thread-1",
		"Updated: ",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("created content missing %q:\n%s", want, content)
		}
	}
	record := a.State().GroupAnnouncementBlock("group", "chat-1")
	if record == nil || record.BlockID != "announcement-block-created" || record.BotOpenID != "bot-open" || record.LastContentHash == "" {
		t.Fatalf("persisted announcement record = %+v", record)
	}

	if err := refreshGroupAnnouncementStatusNow(context.Background(), a, "chat-1"); err != nil {
		t.Fatalf("second refreshGroupAnnouncementStatusNow() error = %v", err)
	}
	if len(ff.announcementListCalls) != 1 || len(ff.announcementCreateCalls) != 1 || len(ff.announcementUpdateCalls) != 0 {
		t.Fatalf("stable refresh should skip API, calls list/create/update = %d/%d/%d", len(ff.announcementListCalls), len(ff.announcementCreateCalls), len(ff.announcementUpdateCalls))
	}
}

func TestGroupAnnouncementBotNameFallbacks(t *testing.T) {
	store := newGroupAnnouncementStore(t)
	ff := &fakeFeishuClient{botOpenID: "bot-open"}
	a := newGroupAnnouncementTestApp(t, store, ff, "bot-a")

	status := buildGroupAnnouncementStatus(a, "chat-1", time.Unix(1700000000, 0))
	if !strings.Contains(status.content, "Bot: bot-open") {
		t.Fatalf("status content = %q, want bot open id fallback", status.content)
	}

	ff.botOpenID = ""
	status = buildGroupAnnouncementStatus(a, "chat-1", time.Unix(1700000000, 0))
	if !strings.Contains(status.content, "Bot: bot-a") {
		t.Fatalf("status content = %q, want frontend id fallback", status.content)
	}
}

func TestGroupAnnouncementRefreshRecoversExistingBlockByMarker(t *testing.T) {
	store := newGroupAnnouncementStore(t)
	ff := &fakeFeishuClient{
		botOpenID: "bot-open",
		announcementBlocks: []feishu.AnnouncementBlock{{
			BlockID: "existing-block",
			Text:    "old\n" + groupAnnouncementMarker("bot-a", "bot-open"),
		}},
	}
	a := newGroupAnnouncementTestApp(t, store, ff, "bot-a")
	seedGroupAnnouncementBinding(t, a, "chat-1")

	if err := refreshGroupAnnouncementStatusNow(context.Background(), a, "chat-1"); err != nil {
		t.Fatalf("refreshGroupAnnouncementStatusNow() error = %v", err)
	}
	if len(ff.announcementListCalls) != 1 || len(ff.announcementCreateCalls) != 0 || len(ff.announcementUpdateCalls) != 1 {
		t.Fatalf("announcement calls list/create/update = %d/%d/%d", len(ff.announcementListCalls), len(ff.announcementCreateCalls), len(ff.announcementUpdateCalls))
	}
	if got := ff.announcementUpdateCalls[0].blockID; got != "existing-block" {
		t.Fatalf("updated block id = %q, want existing-block", got)
	}
	if record := a.State().GroupAnnouncementBlock("group", "chat-1"); record == nil || record.BlockID != "existing-block" {
		t.Fatalf("persisted recovered record = %+v", record)
	}
}

func TestGroupAnnouncementRefreshSwallowsRateLimitWithoutRetry(t *testing.T) {
	store := newGroupAnnouncementStore(t)
	ff := &fakeFeishuClient{
		botOpenID:             "bot-open",
		announcementCreateErr: &feishu.AnnouncementAPIError{HTTPStatus: http.StatusTooManyRequests},
	}
	a := newGroupAnnouncementTestApp(t, store, ff, "bot-a")
	seedGroupAnnouncementBinding(t, a, "chat-1")

	if err := refreshGroupAnnouncementStatusNow(context.Background(), a, "chat-1"); err != nil {
		t.Fatalf("refreshGroupAnnouncementStatusNow() error = %v", err)
	}
	if len(ff.announcementListCalls) != 1 || len(ff.announcementCreateCalls) != 1 || len(ff.announcementUpdateCalls) != 0 {
		t.Fatalf("rate-limited refresh should not retry, calls list/create/update = %d/%d/%d", len(ff.announcementListCalls), len(ff.announcementCreateCalls), len(ff.announcementUpdateCalls))
	}
	if record := a.State().GroupAnnouncementBlock("group", "chat-1"); record != nil {
		t.Fatalf("rate-limited create persisted record = %+v, want nil", record)
	}
}

func TestGroupAnnouncementRefreshCreatesSeparateBlocksPerFrontend(t *testing.T) {
	store := newGroupAnnouncementStore(t)
	ff := &fakeFeishuClient{botOpenID: "bot-open"}
	a := newGroupAnnouncementTestApp(t, store, ff, "bot-a")
	b := newGroupAnnouncementTestApp(t, store, ff, "bot-b")
	seedGroupAnnouncementBinding(t, a, "chat-1")
	seedGroupAnnouncementBinding(t, b, "chat-1")

	if err := refreshGroupAnnouncementStatusNow(context.Background(), a, "chat-1"); err != nil {
		t.Fatalf("refresh bot-a error = %v", err)
	}
	if err := refreshGroupAnnouncementStatusNow(context.Background(), b, "chat-1"); err != nil {
		t.Fatalf("refresh bot-b error = %v", err)
	}
	if len(ff.announcementCreateCalls) != 2 || len(ff.announcementUpdateCalls) != 0 {
		t.Fatalf("create/update calls = %d/%d", len(ff.announcementCreateCalls), len(ff.announcementUpdateCalls))
	}
	contents := []string{ff.announcementCreateCalls[0].content, ff.announcementCreateCalls[1].content}
	if !slices.ContainsFunc(contents, func(content string) bool { return strings.Contains(content, "feidex-status-region:bot-a:bot-open") }) {
		t.Fatalf("missing bot-a region in contents: %#v", contents)
	}
	if !slices.ContainsFunc(contents, func(content string) bool { return strings.Contains(content, "feidex-status-region:bot-b:bot-open") }) {
		t.Fatalf("missing bot-b region in contents: %#v", contents)
	}
	if record := a.State().GroupAnnouncementBlock("group", "chat-1"); record == nil || record.BlockID == "" {
		t.Fatalf("bot-a record = %+v", record)
	}
	if record := b.State().GroupAnnouncementBlock("group", "chat-1"); record == nil || record.BlockID == "" {
		t.Fatalf("bot-b record = %+v", record)
	}
}

func TestKnownGroupAnnouncementChatIDsDoNotTreatUnknownCanonicalSessionAsGroup(t *testing.T) {
	store := newGroupAnnouncementStore(t)
	ff := &fakeFeishuClient{botOpenID: "bot-open"}
	a := newGroupAnnouncementTestApp(t, store, ff, "bot-a")
	if err := a.State().SaveSession(&state.Session{
		Key:            "feishu:frontend:bot-a:chat:chat-p2p",
		ChatID:         "chat-p2p",
		ChatType:       "p2p",
		ActiveThreadID: "thread-p2p",
		Status:         state.SessionStatusIdle.String(),
	}); err != nil {
		t.Fatalf("SaveSession(p2p) error = %v", err)
	}
	if err := a.State().SaveSession(&state.Session{
		Key:            "feishu:frontend:bot-a:chat:chat-unknown",
		ChatID:         "chat-unknown",
		ActiveThreadID: "thread-unknown",
		Status:         state.SessionStatusIdle.String(),
	}); err != nil {
		t.Fatalf("SaveSession(unknown) error = %v", err)
	}
	if got := knownGroupAnnouncementChatIDs(a); len(got) != 0 {
		t.Fatalf("knownGroupAnnouncementChatIDs() = %#v, want none", got)
	}

	seedGroupAnnouncementBinding(t, a, "chat-unknown")
	if got := knownGroupAnnouncementChatIDs(a); len(got) != 1 || got[0] != "chat-unknown" {
		t.Fatalf("knownGroupAnnouncementChatIDs(with binding) = %#v, want chat-unknown", got)
	}
}
