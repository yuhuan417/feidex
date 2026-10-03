package history

import (
	"context"
	"errors"
	"testing"

	"feidex/internal/application/backendops"
	"feidex/internal/domain/conversation"
	domainhistory "feidex/internal/domain/history"
	"feidex/internal/domain/identity"
)

type fakeRepository struct{ session *conversation.Session }

func (r fakeRepository) Session(string) *conversation.Session { return r.session }

type fakeReader struct {
	reads int
	value backendops.ThreadHistory
}

func (r *fakeReader) ReadConversationHistory(ctx context.Context, _ string) (backendops.ThreadHistory, error) {
	r.reads++
	if err := ctx.Err(); err != nil {
		return backendops.ThreadHistory{}, err
	}
	return r.value, nil
}

func TestSnapshotRejectsForeignFrontendBeforeReading(t *testing.T) {
	reader := &fakeReader{value: backendops.ThreadHistory{ID: "thread"}}
	svc := Service{Frontend: "frontend-a", Repository: fakeRepository{session: &conversation.Session{ActiveThreadID: "thread"}}, Backend: func() string { return "codex" }, Readers: map[string]Reader{"codex": reader}}
	if _, err := svc.Snapshot(context.Background(), "feishu:frontend:frontend-b:chat:chat"); err == nil {
		t.Fatal("expected foreign frontend rejection")
	}
	if reader.reads != 0 {
		t.Fatalf("reader called %d times before scope rejection", reader.reads)
	}
}

func TestSnapshotDoesNotMutateReaderHistory(t *testing.T) {
	reader := &fakeReader{value: backendops.ThreadHistory{ID: "thread", Turns: []backendops.HistoryTurn{{ID: "turn", Records: []domainhistory.Record{{Details: []string{"detail"}}}}}}}
	svc := Service{Frontend: "frontend-a", Repository: fakeRepository{session: &conversation.Session{ActiveThreadID: "thread", ActiveTurnID: "turn"}}, Backend: func() string { return "codex" }, Readers: map[string]Reader{"codex": reader}}
	first, err := svc.Snapshot(context.Background(), "feishu:frontend:frontend-a:chat:chat")
	if err != nil {
		t.Fatal(err)
	}
	first.Turns[0].Records[0].Details[0] = "changed"
	second, err := svc.Snapshot(context.Background(), "feishu:frontend:frontend-a:chat:chat")
	if err != nil {
		t.Fatal(err)
	}
	if second.Turns[0].Records[0].Details[0] != "detail" {
		t.Fatalf("reader-owned history was mutated: %+v", second.Turns[0].Records[0])
	}
}

func TestSnapshotCancellationAvoidsRepositoryAndReader(t *testing.T) {
	reader := &fakeReader{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	svc := Service{Frontend: identity.FrontendID("frontend-a"), Repository: fakeRepository{session: &conversation.Session{ActiveThreadID: "thread"}}, Backend: func() string { return "codex" }, Readers: map[string]Reader{"codex": reader}}
	if _, err := svc.Snapshot(ctx, "feishu:frontend:frontend-a:chat:chat"); !errors.Is(err, context.Canceled) {
		t.Fatalf("Snapshot() error = %v, want context canceled", err)
	}
	if reader.reads != 0 {
		t.Fatalf("reader called %d times after cancellation", reader.reads)
	}
}

func TestPageClampsNegativeAndHugeNumbers(t *testing.T) {
	turns := make([]backendops.HistoryTurn, 51)
	for i := range turns {
		turns[i] = backendops.HistoryTurn{Ordinal: i + 1, ID: "turn"}
	}
	reader := &fakeReader{value: backendops.ThreadHistory{ID: "thread", Turns: turns}}
	svc := Service{Frontend: "frontend-a", Repository: fakeRepository{session: &conversation.Session{ActiveThreadID: "thread"}}, Backend: func() string { return "codex" }, Readers: map[string]Reader{"codex": reader}}
	page, err := svc.Page(context.Background(), "feishu:frontend:frontend-a:chat:chat", 1<<62)
	if err != nil {
		t.Fatal(err)
	}
	if page.Number != 1 || page.Start != 50 || page.End != 51 {
		t.Fatalf("huge page = %+v, want final page", page)
	}
	page, err = svc.Page(context.Background(), "feishu:frontend:frontend-a:chat:chat", -10)
	if err != nil || page.Number != 0 || page.Start != 0 {
		t.Fatalf("negative page = %+v, err=%v", page, err)
	}
}

func TestDetailForOrdinalUsesOneSnapshot(t *testing.T) {
	reader := &fakeReader{value: backendops.ThreadHistory{ID: "thread", Turns: []backendops.HistoryTurn{{Ordinal: 2, ID: "second"}, {Ordinal: 1, ID: "first"}}}}
	svc := Service{Frontend: "frontend-a", Repository: fakeRepository{session: &conversation.Session{ActiveThreadID: "thread"}}, Backend: func() string { return "codex" }, Readers: map[string]Reader{"codex": reader}}
	detail, err := svc.DetailForOrdinal(context.Background(), "feishu:frontend:frontend-a:chat:chat", 1)
	if err != nil || detail.Index != 1 || reader.reads != 1 {
		t.Fatalf("detail = %+v, err=%v, reads=%d", detail, err, reader.reads)
	}
}
