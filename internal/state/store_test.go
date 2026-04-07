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
	if store.data.Counters.NextSubmission != 1 || store.data.Counters.NextLocalID != 1 {
		t.Fatalf("unexpected default counters: %+v", store.data.Counters)
	}
	if len(store.data.Sessions) != 0 || len(store.data.Submissions) != 0 || len(store.data.PendingRequests) != 0 {
		t.Fatalf("expected empty default snapshot, got %+v", store.data)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if len(content) == 0 {
		t.Fatal("expected store file to be created")
	}

	var snapshot Snapshot
	if err := json.Unmarshal(content, &snapshot); err != nil {
		t.Fatalf("persisted snapshot is invalid JSON: %v", err)
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
	if emptyStore.data.Sessions == nil || emptyStore.data.Counters.NextSubmission != 1 {
		t.Fatalf("unexpected defaults from empty file: %+v", emptyStore.data)
	}

	legacyPath := filepath.Join(dir, "legacy.json")
	legacy := `{"sessions":null,"submissions":null,"pending_requests":null,"message_links":null,"inbound_dedup":null,"counters":{"next_submission":0,"next_local_id":0}}`
	if err := os.WriteFile(legacyPath, []byte(legacy), 0o644); err != nil {
		t.Fatalf("WriteFile(legacy) error = %v", err)
	}
	legacyStore, err := Open(legacyPath)
	if err != nil {
		t.Fatalf("Open(legacy) error = %v", err)
	}
	if legacyStore.data.Sessions == nil || legacyStore.data.Submissions == nil || legacyStore.data.PendingRequests == nil {
		t.Fatal("expected nil maps to be initialized")
	}
	if legacyStore.data.MessageLinks == nil || legacyStore.data.InboundDedup == nil {
		t.Fatal("expected message link and inbound maps to be initialized")
	}
	if legacyStore.data.Counters.NextSubmission != 1 || legacyStore.data.Counters.NextLocalID != 1 {
		t.Fatalf("expected counters to be repaired, got %+v", legacyStore.data.Counters)
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
}

func TestSubmissionLifecycleAndHelpers(t *testing.T) {
	store := openTestStore(t)

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

func TestPendingLinksAndInboundDedup(t *testing.T) {
	store := openTestStore(t)

	req := &PendingRequest{ID: "pending-1", Kind: "approval", Status: "open"}
	if err := store.UpsertPending(req); err != nil {
		t.Fatalf("UpsertPending() error = %v", err)
	}
	req.Status = "mutated"

	savedReq := store.PendingByID("pending-1")
	if savedReq == nil {
		t.Fatal("PendingByID() returned nil")
	}
	if savedReq.Status != "open" {
		t.Fatalf("stored pending request was mutated through caller struct: %+v", savedReq)
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

	allPending := store.AllPendingRequests()
	if len(allPending) != 1 {
		t.Fatalf("AllPendingRequests() len = %d, want 1", len(allPending))
	}
	allPending[0].Status = "changed"
	if store.PendingByID("pending-1").Status != "done" {
		t.Fatal("AllPendingRequests() returned shared pointer")
	}

	if err := store.UpsertMessageLink(nil); err != nil {
		t.Fatalf("UpsertMessageLink(nil) error = %v", err)
	}
	if err := store.UpsertMessageLink(&MessageLink{}); err != nil {
		t.Fatalf("UpsertMessageLink(empty) error = %v", err)
	}

	link := &MessageLink{MessageID: "msg-1", Kind: "submission"}
	if err := store.UpsertMessageLink(link); err != nil {
		t.Fatalf("UpsertMessageLink() error = %v", err)
	}
	if savedLink := store.data.MessageLinks["msg-1"]; savedLink == nil || savedLink.CreatedAt == 0 {
		t.Fatalf("expected message link timestamp to be populated, got %+v", savedLink)
	}

	seen, err := store.MarkInboundSeen("", 0)
	if err != nil || seen {
		t.Fatalf("MarkInboundSeen(empty) = (%v, %v), want (false, nil)", seen, err)
	}
	seen, err = store.MarkInboundSeen("msg-1", 123)
	if err != nil {
		t.Fatalf("MarkInboundSeen(first) error = %v", err)
	}
	if seen {
		t.Fatal("MarkInboundSeen(first) reported duplicate")
	}
	seen, err = store.MarkInboundSeen("msg-1", 456)
	if err != nil {
		t.Fatalf("MarkInboundSeen(second) error = %v", err)
	}
	if !seen {
		t.Fatal("MarkInboundSeen(second) did not report duplicate")
	}

	store.data.InboundDedup["old"] = 1
	store.data.InboundDedup["new"] = 100
	if err := store.CleanupInboundSeen(10); err != nil {
		t.Fatalf("CleanupInboundSeen() error = %v", err)
	}
	if _, ok := store.data.InboundDedup["old"]; ok {
		t.Fatal("CleanupInboundSeen() did not remove old entry")
	}
	if _, ok := store.data.InboundDedup["new"]; !ok {
		t.Fatal("CleanupInboundSeen() removed fresh entry")
	}
	if err := store.CleanupInboundSeen(10); err != nil {
		t.Fatalf("CleanupInboundSeen(no-op) error = %v", err)
	}
}
