package state

import (
	"feidex/internal/domain/conversation"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
)

func TestSessionUpdateDiskFailureDoesNotPublishMutation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	key := "feishu:frontend:a:chat:private"
	if err := store.UpsertSession(&conversation.Session{Key: key, ActiveThreadID: "thread", RecentWorkspaceIDs: []string{"original"}}); err != nil {
		t.Fatal(err)
	}
	before := store.GetSession(key)
	if err := os.Mkdir(path+".tmp", 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdateSession(key, func(sess *conversation.Session) {
		sess.RecentWorkspaceIDs[0] = "mutated"
		sess.ActiveThreadID = "replacement"
	}); err == nil {
		t.Fatal("expected write failure")
	}
	if !reflect.DeepEqual(before, store.GetSession(key)) {
		t.Fatal("failed session write published mutable snapshot")
	}
	if err := os.Remove(path + ".tmp"); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := reopened.GetSession(key); got == nil || len(got.RecentWorkspaceIDs) != 1 || got.RecentWorkspaceIDs[0] != "original" {
		t.Fatal("failed session write changed disk")
	}
}

func TestUpdateSessionSerializesConcurrentMutationsAndPersistsOwnerState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	const sessionKey = "feishu:frontend:frontend-a:chat:chat-1"
	if err := store.UpsertSession(&conversation.Session{Key: sessionKey, WorkspaceID: "workspace-a"}); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}

	const updates = 24
	var wg sync.WaitGroup
	for i := 0; i < updates; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if _, err := store.UpdateSession(sessionKey, func(sess *conversation.Session) {
				sess.RecentWorkspaceIDs = append(sess.RecentWorkspaceIDs, fmt.Sprintf("workspace-%d", i))
			}); err != nil {
				t.Errorf("UpdateSession(%d) error = %v", i, err)
			}
		}(i)
	}
	wg.Wait()

	sess := store.GetSession(sessionKey)
	if sess == nil || len(sess.RecentWorkspaceIDs) != updates {
		t.Fatalf("concurrent session state = %+v, want %d updates", sess, updates)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatalf("Open(reopened) error = %v", err)
	}
	persisted := reopened.GetSession(sessionKey)
	if persisted == nil || len(persisted.RecentWorkspaceIDs) != updates {
		t.Fatalf("persisted session state = %+v, want %d updates", persisted, updates)
	}
}

func TestFrontendScopedRecordsDoNotCrossOwners(t *testing.T) {
	store := openTestStore(t)
	for _, record := range []*AgentBinding{
		{ID: "binding-a", FrontendID: "frontend-a", ChatID: "chat-a", ChatType: "group", WorkspaceID: "workspace-a"},
		{ID: "binding-b", FrontendID: "frontend-b", ChatID: "chat-b", ChatType: "group", WorkspaceID: "workspace-b"},
	} {
		if err := store.UpsertScopedAgentBinding(record.FrontendID, record); err != nil {
			t.Fatalf("UpsertScopedAgentBinding(%s) error = %v", record.FrontendID, err)
		}
	}
	if got := store.GetScopedAgentBinding("frontend-a", "binding-b"); got != nil {
		t.Fatalf("frontend-a observed frontend-b binding: %+v", got)
	}
	if got := store.GetScopedAgentBinding("frontend-b", "binding-a"); got != nil {
		t.Fatalf("frontend-b observed frontend-a binding: %+v", got)
	}
}
