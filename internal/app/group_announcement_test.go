package app

import (
	"context"
	"net/http"
	"path/filepath"
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
		groupAnnouncementDivider,
		"feidex-status-region:luban-feidex:bot-open",
		groupAnnouncementField("Bot", "luban-feidex"),
		groupAnnouncementField("Machine IP", ""),
		groupAnnouncementField("Workspace", a.cfg.Workspaces[0].Cwd),
		groupAnnouncementField("Backend", "codex"),
		groupAnnouncementField("Thread", "thread-1"),
		groupAnnouncementField("Updated", ""),
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

func TestGroupAnnouncementRefreshCreatesCommonRegionAtTopForPrimary(t *testing.T) {
	store := newGroupAnnouncementStore(t)
	ff := &fakeFeishuClient{botOpenID: "bot-open", botName: "luban-feidex"}
	a := newGroupAnnouncementTestApp(t, store, ff, "bot-a")
	seedGroupAnnouncementBinding(t, a, "chat-1")
	seedGroupAnnouncementSession(t, a, "chat-1", "thread-1")
	if _, err := setGroupPrimary(a, "group", "chat-1", true); err != nil {
		t.Fatalf("setGroupPrimary() error = %v", err)
	}

	if err := refreshGroupAnnouncementStatusNow(context.Background(), a, "chat-1"); err != nil {
		t.Fatalf("refreshGroupAnnouncementStatusNow() error = %v", err)
	}
	if len(ff.announcementListCalls) != 1 || len(ff.announcementCreateCalls) != 2 || len(ff.announcementUpdateCalls) != 0 {
		t.Fatalf("announcement calls list/create/update = %d/%d/%d", len(ff.announcementListCalls), len(ff.announcementCreateCalls), len(ff.announcementUpdateCalls))
	}
	commonCreate := ff.announcementCreateCalls[0]
	if commonCreate.index == nil || *commonCreate.index != 0 {
		t.Fatalf("common create index = %v, want 0", commonCreate.index)
	}
	for _, want := range []string{
		groupAnnouncementCommonTitle,
		groupAnnouncementField("Primary Bot", "luban-feidex"),
		groupAnnouncementField("Marker", groupAnnouncementCommonMarker),
		groupAnnouncementField("Updated", ""),
	} {
		if !strings.Contains(commonCreate.content, want) {
			t.Fatalf("common content missing %q:\n%s", want, commonCreate.content)
		}
	}
	botCreate := ff.announcementCreateCalls[1]
	if botCreate.index != nil {
		t.Fatalf("bot create index = %v, want append", *botCreate.index)
	}
	if len(ff.announcementBlocks) < 2 || !strings.Contains(ff.announcementBlocks[0].Text, groupAnnouncementCommonMarker) {
		t.Fatalf("announcement block order = %+v, want common block first", ff.announcementBlocks)
	}
	if record := a.State().GroupAnnouncementBlock(groupAnnouncementCommonChatType, "chat-1"); record == nil || record.BlockID != "announcement-block-created" || record.BotOpenID != "bot-open" || record.LastContentHash == "" {
		t.Fatalf("persisted common announcement record = %+v", record)
	}
	if record := a.State().GroupAnnouncementBlock("group", "chat-1"); record == nil || record.BlockID != "announcement-block-created-next" || record.BotOpenID != "bot-open" || record.LastContentHash == "" {
		t.Fatalf("persisted bot announcement record = %+v", record)
	}

	if err := refreshGroupAnnouncementStatusNow(context.Background(), a, "chat-1"); err != nil {
		t.Fatalf("second refreshGroupAnnouncementStatusNow() error = %v", err)
	}
	if len(ff.announcementListCalls) != 2 || len(ff.announcementCreateCalls) != 2 || len(ff.announcementUpdateCalls) != 0 {
		t.Fatalf("stable common refresh should not rewrite, calls list/create/update = %d/%d/%d", len(ff.announcementListCalls), len(ff.announcementCreateCalls), len(ff.announcementUpdateCalls))
	}
}

func TestGroupAnnouncementRefreshSkipsCommonRegionForNonPrimary(t *testing.T) {
	store := newGroupAnnouncementStore(t)
	ff := &fakeFeishuClient{botOpenID: "bot-a-open", botName: "bot-a"}
	a := newGroupAnnouncementTestApp(t, store, ff, "bot-a")
	seedGroupAnnouncementBinding(t, a, "chat-1")
	if _, err := setGroupPrimaryOwner(a, "group", "chat-1", "bot-b-open"); err != nil {
		t.Fatalf("setGroupPrimaryOwner() error = %v", err)
	}

	if err := refreshGroupAnnouncementStatusNow(context.Background(), a, "chat-1"); err != nil {
		t.Fatalf("refreshGroupAnnouncementStatusNow() error = %v", err)
	}
	if len(ff.announcementCreateCalls) != 1 {
		t.Fatalf("create calls = %d, want only this bot's status block", len(ff.announcementCreateCalls))
	}
	created := ff.announcementCreateCalls[0]
	if created.index != nil || strings.Contains(created.content, groupAnnouncementCommonMarker) {
		t.Fatalf("non-primary common create = index %v content:\n%s", created.index, created.content)
	}
	if record := a.State().GroupAnnouncementBlock(groupAnnouncementCommonChatType, "chat-1"); record != nil {
		t.Fatalf("non-primary persisted common record = %+v", record)
	}
}

func TestGroupAnnouncementRefreshUpdatesExistingCommonRegionByPrimary(t *testing.T) {
	store := newGroupAnnouncementStore(t)
	ff := &fakeFeishuClient{
		botOpenID: "bot-open",
		botName:   "luban-feidex",
		announcementBlocks: []feishu.AnnouncementBlock{{
			BlockID: "common-block",
			Text: strings.Join([]string{
				groupAnnouncementCommonTitle,
				groupAnnouncementField("Primary Bot", "old-primary"),
				groupAnnouncementField("Marker", groupAnnouncementCommonMarker),
			}, "\n"),
		}},
	}
	a := newGroupAnnouncementTestApp(t, store, ff, "bot-a")
	seedGroupAnnouncementBinding(t, a, "chat-1")
	if _, err := setGroupPrimary(a, "group", "chat-1", true); err != nil {
		t.Fatalf("setGroupPrimary() error = %v", err)
	}

	if err := refreshGroupAnnouncementStatusNow(context.Background(), a, "chat-1"); err != nil {
		t.Fatalf("refreshGroupAnnouncementStatusNow() error = %v", err)
	}
	if len(ff.announcementUpdateCalls) != 1 || ff.announcementUpdateCalls[0].blockID != "common-block" {
		t.Fatalf("common update calls = %+v, want common-block", ff.announcementUpdateCalls)
	}
	if !strings.Contains(ff.announcementUpdateCalls[0].content, groupAnnouncementField("Primary Bot", "luban-feidex")) {
		t.Fatalf("common update content missing primary bot name:\n%s", ff.announcementUpdateCalls[0].content)
	}
	if record := a.State().GroupAnnouncementBlock(groupAnnouncementCommonChatType, "chat-1"); record == nil || record.BlockID != "common-block" || record.BotOpenID != "bot-open" {
		t.Fatalf("persisted common record = %+v", record)
	}
}

func TestGroupAnnouncementBotNameFallbacks(t *testing.T) {
	store := newGroupAnnouncementStore(t)
	ff := &fakeFeishuClient{botOpenID: "bot-open"}
	a := newGroupAnnouncementTestApp(t, store, ff, "bot-a")

	status := buildGroupAnnouncementStatus(a, "chat-1", time.Unix(1700000000, 0))
	if !strings.Contains(status.content, groupAnnouncementField("Bot", "bot-open")) {
		t.Fatalf("status content = %q, want bot open id fallback", status.content)
	}

	ff.botOpenID = ""
	status = buildGroupAnnouncementStatus(a, "chat-1", time.Unix(1700000000, 0))
	if strings.Contains(status.content, groupAnnouncementField("Bot", "bot-a")) || !strings.Contains(status.content, groupAnnouncementField("Bot", "unknown")) {
		t.Fatalf("status content = %q, want unknown fallback without frontend id", status.content)
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

func TestGroupAnnouncementRefreshRecoversLegacyUnknownMarker(t *testing.T) {
	store := newGroupAnnouncementStore(t)
	ff := &fakeFeishuClient{
		botOpenID: "bot-open",
		botName:   "luban-feidex",
		announcementBlocks: []feishu.AnnouncementBlock{{
			BlockID: "legacy-block",
			Text:    "old\n" + groupAnnouncementLegacyMarker("default", ""),
		}},
	}
	a := newGroupAnnouncementTestApp(t, store, ff, "default")
	seedGroupAnnouncementBinding(t, a, "chat-1")

	if err := refreshGroupAnnouncementStatusNow(context.Background(), a, "chat-1"); err != nil {
		t.Fatalf("refreshGroupAnnouncementStatusNow() error = %v", err)
	}
	if len(ff.announcementCreateCalls) != 0 || len(ff.announcementUpdateCalls) != 1 {
		t.Fatalf("create/update calls = %d/%d", len(ff.announcementCreateCalls), len(ff.announcementUpdateCalls))
	}
	updated := ff.announcementUpdateCalls[0]
	if updated.blockID != "legacy-block" {
		t.Fatalf("updated block id = %q, want legacy-block", updated.blockID)
	}
	if !strings.Contains(updated.content, "feidex-status-region:luban-feidex:bot-open") || !strings.Contains(updated.content, groupAnnouncementField("Bot", "luban-feidex")) {
		t.Fatalf("updated content did not replace legacy marker/name:\n%s", updated.content)
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

func TestGroupAnnouncementRefreshReusesBlockForSameBotIdentity(t *testing.T) {
	store := newGroupAnnouncementStore(t)
	ff := &fakeFeishuClient{botOpenID: "bot-open", botName: "luban-feidex"}
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
	if len(ff.announcementCreateCalls) != 1 || len(ff.announcementUpdateCalls) != 1 {
		t.Fatalf("create/update calls = %d/%d", len(ff.announcementCreateCalls), len(ff.announcementUpdateCalls))
	}
	if !strings.Contains(ff.announcementCreateCalls[0].content, "feidex-status-region:luban-feidex:bot-open") {
		t.Fatalf("created content missing bot identity marker:\n%s", ff.announcementCreateCalls[0].content)
	}
	if ff.announcementUpdateCalls[0].blockID != "announcement-block-created" {
		t.Fatalf("updated block id = %q, want existing same-bot block", ff.announcementUpdateCalls[0].blockID)
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

func TestKnownGroupAnnouncementChatIDsIncludesPersistedAnnouncementBlocks(t *testing.T) {
	store := newGroupAnnouncementStore(t)
	ff := &fakeFeishuClient{botOpenID: "bot-open"}
	a := newGroupAnnouncementTestApp(t, store, ff, "bot-a")
	if err := store.UpsertGroupAnnouncementBlock(&state.GroupAnnouncementBlock{
		ID:         "announcement-existing",
		FrontendID: "bot-a",
		ChatID:     "chat-from-announcement",
		ChatType:   "group",
		BlockID:    "old-block",
		Marker:     groupAnnouncementMarker("bot-open", "bot-open"),
	}); err != nil {
		t.Fatalf("UpsertGroupAnnouncementBlock() error = %v", err)
	}
	if err := store.UpsertGroupAnnouncementBlock(&state.GroupAnnouncementBlock{
		ID:         "announcement-other-frontend",
		FrontendID: "bot-b",
		ChatID:     "chat-other",
		ChatType:   "group",
		BlockID:    "other-block",
	}); err != nil {
		t.Fatalf("UpsertGroupAnnouncementBlock(other frontend) error = %v", err)
	}

	got := knownGroupAnnouncementChatIDs(a)
	if len(got) != 1 || got[0] != "chat-from-announcement" {
		t.Fatalf("knownGroupAnnouncementChatIDs() = %#v, want persisted announcement chat", got)
	}
}
