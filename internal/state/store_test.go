package state

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()

	store, err := Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	return store
}

func TestOpenCreatesDefaultSnapshotAndFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "state.json")

	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	if store.path != path {
		t.Fatalf("store.path = %q, want %q", store.path, path)
	}
	if store.data.Version != currentSnapshotVersion {
		t.Fatalf("store.data.Version = %d, want %d", store.data.Version, currentSnapshotVersion)
	}
	if len(store.data.Sessions) != 0 {
		t.Fatalf("expected empty sessions snapshot, got %+v", store.data.Sessions)
	}
	if store.runtime.Counters.NextSubmission != 1 || store.runtime.Counters.NextLocalID != 1 {
		t.Fatalf("unexpected default runtime counters: %+v", store.runtime.Counters)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if len(content) == 0 {
		t.Fatal("expected store file to be created")
	}
	if strings.Contains(string(content), "submissions") {
		t.Fatalf("persisted snapshot should not contain runtime fields:\n%s", string(content))
	}

	var snapshot Snapshot
	if err := json.Unmarshal(content, &snapshot); err != nil {
		t.Fatalf("persisted snapshot is invalid JSON: %v", err)
	}
	if snapshot.Version != currentSnapshotVersion {
		t.Fatalf("persisted version = %d, want %d", snapshot.Version, currentSnapshotVersion)
	}
}

func TestOpenHandlesEmptyLegacyAndInvalidFiles(t *testing.T) {
	dir := t.TempDir()

	emptyPath := filepath.Join(dir, "empty.json")
	if err := os.WriteFile(emptyPath, nil, 0o644); err != nil {
		t.Fatalf("WriteFile(empty) error = %v", err)
	}
	emptyStore, err := Open(emptyPath)
	if err != nil {
		t.Fatalf("Open(empty) error = %v", err)
	}
	if emptyStore.data.Sessions == nil || emptyStore.data.Version != currentSnapshotVersion {
		t.Fatalf("unexpected defaults from empty file: %+v", emptyStore.data)
	}

	legacyPath := filepath.Join(dir, "legacy.json")
	legacy := `{
  "sessions": {
    "session-1": {
      "key": "session-1",
      "active_thread_id": "thread-1",
      "active_thread_service_tier": "flex",
      "updated_at": 1
    }
  },
  "submissions": {"sub-1": {"id": "sub-1"}},
  "pending_requests": {"pending-1": {"id": "pending-1"}},
  "message_links": {"msg-1": {"message_id": "msg-1"}},
  "inbound_dedup": {"msg-2": 1},
  "counters": {"next_submission": 99, "next_local_id": 42}
}`
	if err := os.WriteFile(legacyPath, []byte(legacy), 0o644); err != nil {
		t.Fatalf("WriteFile(legacy) error = %v", err)
	}
	legacyStore, err := Open(legacyPath)
	if err != nil {
		t.Fatalf("Open(legacy) error = %v", err)
	}
	sess := legacyStore.GetSession("session-1")
	if sess == nil {
		t.Fatal("expected legacy session to be loaded")
	}
	if sess.ActiveThreadServiceTier != "" {
		t.Fatalf("service tier = %q, want repaired empty value", sess.ActiveThreadServiceTier)
	}

	content, err := os.ReadFile(legacyPath)
	if err != nil {
		t.Fatalf("ReadFile(legacy) error = %v", err)
	}
	text := string(content)
	for _, forbidden := range []string{"submissions", "pending_requests", "message_links", "inbound_dedup", "next_submission"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("persisted snapshot should drop legacy runtime field %q:\n%s", forbidden, text)
		}
	}
	if !strings.Contains(text, `"version": 2`) {
		t.Fatalf("persisted snapshot should be rewritten to current version:\n%s", text)
	}

	invalidPath := filepath.Join(dir, "invalid.json")
	if err := os.WriteFile(invalidPath, []byte("{"), 0o644); err != nil {
		t.Fatalf("WriteFile(invalid) error = %v", err)
	}
	if _, err := Open(invalidPath); err == nil {
		t.Fatal("expected invalid JSON to return an error")
	}
}

func TestSessionQueueAndCloneBehavior(t *testing.T) {
	store := openTestStore(t)

	if got := store.GetSession("missing"); got != nil {
		t.Fatalf("GetSession(missing) = %+v, want nil", got)
	}
	if err := store.UpsertSession(nil); err != nil {
		t.Fatalf("UpsertSession(nil) error = %v", err)
	}

	original := &Session{
		Key:          "session-1",
		Status:       "busy",
		Queue:        []string{"sub-1"},
		StagedImages: []SessionStagedImage{{SourceMessageID: "m-1", Name: "shot.png"}},
	}
	if err := store.UpsertSession(original); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}

	original.Queue[0] = "mutated"
	original.StagedImages[0].Name = "changed.png"

	saved := store.GetSession("session-1")
	if saved == nil {
		t.Fatal("GetSession() returned nil")
	}
	if saved.Queue[0] != "sub-1" {
		t.Fatalf("stored queue was mutated through caller slice: %+v", saved.Queue)
	}
	if saved.StagedImages[0].Name != "shot.png" {
		t.Fatalf("stored staged image was mutated through caller slice: %+v", saved.StagedImages)
	}

	saved.Queue[0] = "changed-locally"
	all := store.AllSessions()
	if len(all) != 1 {
		t.Fatalf("AllSessions() len = %d, want 1", len(all))
	}
	if all[0].Queue[0] != "sub-1" {
		t.Fatalf("AllSessions() returned shared slice: %+v", all[0].Queue)
	}

	if err := store.QueueSubmission("session-1", "sub-2"); err != nil {
		t.Fatalf("QueueSubmission() error = %v", err)
	}
	if err := store.QueueSubmission("session-1", "sub-2"); err != nil {
		t.Fatalf("QueueSubmission(duplicate) error = %v", err)
	}

	session := store.GetSession("session-1")
	if got, want := session.Queue, []string{"sub-1", "sub-2"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("queue = %+v, want %+v", got, want)
	}

	next, err := store.DequeueSubmission("session-1")
	if err != nil {
		t.Fatalf("DequeueSubmission() error = %v", err)
	}
	if next != "sub-1" {
		t.Fatalf("DequeueSubmission() = %q, want sub-1", next)
	}

	next, err = store.DequeueSubmission("session-1")
	if err != nil {
		t.Fatalf("DequeueSubmission(second) error = %v", err)
	}
	if next != "sub-2" {
		t.Fatalf("DequeueSubmission(second) = %q, want sub-2", next)
	}

	next, err = store.DequeueSubmission("session-1")
	if err != nil {
		t.Fatalf("DequeueSubmission(empty) error = %v", err)
	}
	if next != "" {
		t.Fatalf("DequeueSubmission(empty) = %q, want empty", next)
	}

	autoCreated, err := store.DequeueSubmission("new-session")
	if err != nil {
		t.Fatalf("DequeueSubmission(new session) error = %v", err)
	}
	if autoCreated != "" {
		t.Fatalf("DequeueSubmission(new session) = %q, want empty", autoCreated)
	}
	if got := store.GetSession("new-session"); got == nil || got.Status != "idle" {
		t.Fatalf("expected ensureSessionLocked to create idle session, got %+v", got)
	}

	if cloneSession(nil) != nil {
		t.Fatal("cloneSession(nil) should return nil")
	}

	content, err := os.ReadFile(store.path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	text := string(content)
	for _, forbidden := range []string{`"queue"`, `"staged_images"`, `"active_turn_id"`, `"active_submission_id"`, `"chat_id"`, `"chat_type"`, `"root_message_id"`} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("persisted session should omit runtime field %s:\n%s", forbidden, text)
		}
	}

	reopened, err := Open(store.path)
	if err != nil {
		t.Fatalf("Open(reopen) error = %v", err)
	}
	reopenedSession := reopened.GetSession("session-1")
	if reopenedSession == nil {
		t.Fatal("expected reopened session")
	}
	if reopenedSession.Status != "idle" || reopenedSession.ActiveTurnID != "" || reopenedSession.ActiveSubmissionID != "" || len(reopenedSession.Queue) != 0 || len(reopenedSession.StagedImages) != 0 {
		t.Fatalf("reopened session should only contain persistent state, got %+v", reopenedSession)
	}
}

func TestSessionContextIsRecoveredFromSessionKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	groupKey := "feishu:group:chat-1:root:root-1"
	if err := store.UpsertSession(&Session{
		Key:                      groupKey,
		WorkspaceID:              "ws",
		ActiveThreadID:           "thread-1",
		ActiveThreadWorkspaceID:  "ws",
		ActiveThreadPreview:      "preview",
		ChatID:                   "chat-1",
		ChatType:                 "group",
		RootMessageID:            "root-1",
		ActiveTurnID:             "turn-1",
		ActiveSubmissionID:       "sub-1",
		Status:                   "turn_in_progress",
	}); err != nil {
		t.Fatalf("UpsertSession(group) error = %v", err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatalf("Open(reopen) error = %v", err)
	}
	groupSess := reopened.GetSession(groupKey)
	if groupSess == nil {
		t.Fatal("expected reopened group session")
	}
	if groupSess.ChatID != "chat-1" || groupSess.ChatType != "group" || groupSess.RootMessageID != "root-1" {
		t.Fatalf("reopened group session context = %+v, want chat/root reconstructed from key", groupSess)
	}

	p2pKey := "feishu:p2p:chat-2:user-9"
	if err := reopened.UpsertSession(&Session{
		Key:       p2pKey,
		ChatID:    "chat-2",
		ChatType:  "p2p",
	}); err != nil {
		t.Fatalf("UpsertSession(p2p) error = %v", err)
	}
	reopenedAgain, err := Open(path)
	if err != nil {
		t.Fatalf("Open(reopen again) error = %v", err)
	}
	p2pSess := reopenedAgain.GetSession(p2pKey)
	if p2pSess == nil {
		t.Fatal("expected reopened p2p session")
	}
	if p2pSess.ChatID != "chat-2" || p2pSess.ChatType != "p2p" || p2pSess.RootMessageID != "" {
		t.Fatalf("reopened p2p session context = %+v, want chat reconstructed and empty root", p2pSess)
	}
}

func TestSubmissionPendingAndMessageLinksStayInMemory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	sub := &Submission{
		SessionKey:       "session-1",
		SourceMessageIDs: []string{"m-1", "m-2"},
		Attachments:      []SubmissionAttachment{{Kind: "file", Name: "doc.txt", LocalPath: "/tmp/doc.txt"}},
		FinalMessageIDs:  []string{"final-1"},
	}
	id, err := store.CreateSubmission(sub)
	if err != nil {
		t.Fatalf("CreateSubmission() error = %v", err)
	}
	if !strings.HasPrefix(id, "sub-") {
		t.Fatalf("generated submission id = %q, want sub-*", id)
	}

	sub.SourceMessageIDs[0] = "mutated"
	sub.Attachments[0].Name = "changed.txt"
	sub.FinalMessageIDs[0] = "changed"

	saved := store.GetSubmission(id)
	if saved == nil {
		t.Fatal("GetSubmission() returned nil")
	}
	if saved.SourceMessageIDs[0] != "m-1" || saved.Attachments[0].Name != "doc.txt" || saved.FinalMessageIDs[0] != "final-1" {
		t.Fatalf("stored submission was mutated through caller slices: %+v", saved)
	}
	if saved.CreatedAt == 0 || saved.UpdatedAt == 0 {
		t.Fatalf("expected timestamps to be set, got %+v", saved)
	}

	before := saved.UpdatedAt
	time.Sleep(1100 * time.Millisecond)
	if err := store.UpdateSubmission(id, func(s *Submission) {
		s.Status = "done"
		s.FinalMessageIDs = append(s.FinalMessageIDs, "final-2")
	}); err != nil {
		t.Fatalf("UpdateSubmission() error = %v", err)
	}

	updated := store.GetSubmission(id)
	if updated.Status != "done" {
		t.Fatalf("updated.Status = %q, want done", updated.Status)
	}
	if len(updated.FinalMessageIDs) != 2 {
		t.Fatalf("updated.FinalMessageIDs = %+v, want appended value", updated.FinalMessageIDs)
	}
	if updated.UpdatedAt <= before {
		t.Fatalf("UpdatedAt did not move forward: before=%d after=%d", before, updated.UpdatedAt)
	}
	if err := store.UpdateSubmission("missing", func(*Submission) {}); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("UpdateSubmission(missing) error = %v, want %v", err, os.ErrNotExist)
	}
	if got := store.GetSubmission("missing"); got != nil {
		t.Fatalf("GetSubmission(missing) = %+v, want nil", got)
	}

	req := &PendingRequest{ID: "pending-1", Kind: "approval", Status: "open"}
	if err := store.UpsertPending(req); err != nil {
		t.Fatalf("UpsertPending() error = %v", err)
	}
	req.Status = "mutated"

	savedReq := store.PendingByID("pending-1")
	if savedReq == nil || savedReq.Status != "open" {
		t.Fatalf("PendingByID() = %+v, want open copy", savedReq)
	}
	savedReq.Status = "changed"
	if store.PendingByID("pending-1").Status != "open" {
		t.Fatal("PendingByID() returned shared pointer")
	}

	if err := store.UpdatePending("pending-1", func(p *PendingRequest) {
		p.Status = "done"
	}); err != nil {
		t.Fatalf("UpdatePending() error = %v", err)
	}
	if got := store.PendingByID("pending-1"); got == nil || got.Status != "done" {
		t.Fatalf("updated pending request = %+v, want status done", got)
	}
	if err := store.UpdatePending("missing", func(*PendingRequest) {}); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("UpdatePending(missing) error = %v, want %v", err, os.ErrNotExist)
	}
	store.DeletePendingRequests(func(p *PendingRequest) bool { return p != nil && p.Status == "done" })
	if got := store.PendingByID("pending-1"); got != nil {
		t.Fatalf("DeletePendingRequests() should remove request, got %+v", got)
	}

	link := &MessageLink{MessageID: "msg-1", Kind: "submission"}
	if err := store.UpsertMessageLink(link); err != nil {
		t.Fatalf("UpsertMessageLink() error = %v", err)
	}
	savedLink := store.GetMessageLink("msg-1")
	if savedLink == nil || savedLink.CreatedAt == 0 {
		t.Fatalf("GetMessageLink() = %+v, want populated timestamp", savedLink)
	}
	store.DeleteMessageLinks(func(link *MessageLink) bool { return link != nil && link.Kind == "submission" })
	if got := store.GetMessageLink("msg-1"); got != nil {
		t.Fatalf("DeleteMessageLinks() should remove link, got %+v", got)
	}

	local1, err := store.NextLocalID("")
	if err != nil {
		t.Fatalf("NextLocalID(empty) error = %v", err)
	}
	local2, err := store.NextLocalID(" chat ")
	if err != nil {
		t.Fatalf("NextLocalID(prefix) error = %v", err)
	}
	if !strings.HasPrefix(local1, "local-") || !strings.HasPrefix(local2, "chat-") || local1 == local2 {
		t.Fatalf("unexpected local ids: %q %q", local1, local2)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if strings.Contains(string(content), id) || strings.Contains(string(content), "pending-1") || strings.Contains(string(content), "msg-1") {
		t.Fatalf("runtime-only data should not be persisted:\n%s", string(content))
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatalf("Open(reopen) error = %v", err)
	}
	if got := reopened.GetSubmission(id); got != nil {
		t.Fatalf("submission should not survive reopen, got %+v", got)
	}
	if got := reopened.PendingByID("pending-1"); got != nil {
		t.Fatalf("pending request should not survive reopen, got %+v", got)
	}
	if got := reopened.GetMessageLink("msg-1"); got != nil {
		t.Fatalf("message link should not survive reopen, got %+v", got)
	}

	if cloneSubmission(nil) != nil {
		t.Fatal("cloneSubmission(nil) should return nil")
	}
	if got := strconvFormat(42); got != "000042" {
		t.Fatalf("strconvFormat(42) = %q, want 000042", got)
	}
	if got := formatID(7); !strings.HasSuffix(got, "-000007") {
		t.Fatalf("formatID(7) = %q, want suffix -000007", got)
	}
}
