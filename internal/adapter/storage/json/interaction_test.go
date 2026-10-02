package json

import (
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	applicationinteraction "feidex/internal/application/interaction"
	"feidex/internal/state"
)

func TestInteractionRepositoryResolutionIsAtomicAndFrontendScoped(t *testing.T) {
	store, err := state.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, frontend := range []string{"a", "b"} {
		if err := store.UpsertPending(&state.PendingRequest{ID: "r1", FrontendID: frontend, Kind: "command", Status: "pending", ThreadID: "thread", TurnID: "turn", PayloadJSON: "payload"}); err != nil {
			t.Fatal(err)
		}
	}
	svc := applicationinteraction.Service{Repository: InteractionRepository{Store: store, FrontendID: "a"}}
	if _, err := svc.ReplyAccepted("r1"); err != nil {
		t.Fatal(err)
	}
	if !svc.HasOpenRequest("thread", "turn", "") {
		t.Fatal("reply incorrectly removed the resume blocker")
	}
	var resolved atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req, err := svc.Resolve("r1")
			if err != nil {
				t.Error(err)
			} else if req != nil {
				resolved.Add(1)
			}
		}()
	}
	wg.Wait()
	if resolved.Load() != 1 || svc.HasOpenRequest("thread", "turn", "") {
		t.Fatalf("resolution count = %d; request must resolve once", resolved.Load())
	}
	if got := store.PendingByScopedID("a", "r1"); got.Status != "resolved" || got.PayloadJSON != "payload" {
		t.Fatalf("transition corrupted payload: %+v", got)
	}
	if got := store.PendingByScopedID("b", "r1"); got.Status != "pending" {
		t.Fatalf("other frontend changed: %+v", got)
	}
}
