package fileshare

import (
	"context"
	interactionapp "feidex/internal/application/interaction"
	"feidex/internal/domain/conversation"
	"feidex/internal/domain/interaction"
	"feidex/internal/domain/workspace"
	"sync"
	"testing"
	"time"
)

type repository struct {
	mu      sync.Mutex
	request interaction.PendingRequest
}

func (r *repository) Pending(string) *interaction.PendingRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	copy := r.request
	return &copy
}
func (r *repository) Session(string) *conversation.Session { return nil }
func (r *repository) SavePending(p *interaction.PendingRequest) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.request = *p
	return nil
}
func (r *repository) NextLocalID(string) (string, error) { return "download", nil }
func (r *repository) UpdatePending(_ string, mutate func(*interaction.PendingRequest)) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	mutate(&r.request)
	return nil
}

type shareFunc func(context.Context, Request) (Result, error)

func (f shareFunc) Share(ctx context.Context, req Request) (Result, error) { return f(ctx, req) }

type presentationFunc func(Execution, Result, error)

func (f presentationFunc) Completed(e Execution, r Result, err error) { f(e, r, err) }

func fixture() (*repository, Service) {
	r := &repository{request: interaction.PendingRequest{ID: "download", Kind: Kind, Status: "pending", OwnerUserID: "owner", ExpiresAt: time.Now().Add(time.Hour).Unix()}}
	return r, Service{Forms: &interactionapp.FormService{Repository: r}, Repository: r, Context: context.Background}
}

func TestDuplicateExecutionSharesAndPublishesOnce(t *testing.T) {
	r, s := fixture()
	var work func()
	s.Run = func(_ string, fn func()) bool { work = fn; return true }
	entered, release := make(chan struct{}), make(chan struct{})
	s.Artifacts = shareFunc(func(context.Context, Request) (Result, error) {
		close(entered)
		<-release
		return Result{URL: "url"}, nil
	})
	publications := 0
	s.Presentation = presentationFunc(func(_ Execution, _ Result, err error) {
		if err != nil {
			t.Error(err)
		}
		publications++
	})
	if _, err := s.Confirm("download", "other", "chat", "card", "/file", workspace.PathPickerPayload{}); err == nil {
		t.Fatal("foreign owner admitted")
	}
	input, err := s.Confirm("download", "owner", "chat", "card", "/file", workspace.PathPickerPayload{})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { work(); close(done) }()
	<-entered
	s.Execute(input)
	if _, err := s.Confirm("download", "owner", "chat", "card", "/file", workspace.PathPickerPayload{}); err != ErrProcessing {
		t.Fatalf("duplicate confirm: %v", err)
	}
	close(release)
	<-done
	s.Execute(input)
	if publications != 1 || r.Pending("download").Status != "resolved" {
		t.Fatalf("publication count/phase: %d/%s", publications, r.Pending("download").Status)
	}
}

func TestRejectedAdmissionRestoresPending(t *testing.T) {
	r, s := fixture()
	s.Run = func(string, func()) bool { return false }
	if _, err := s.Confirm("download", "owner", "chat", "card", "/file", workspace.PathPickerPayload{}); err == nil {
		t.Fatal("shutdown admission accepted")
	}
	if r.Pending("download").Status != "pending" {
		t.Fatal("rejected work owns request")
	}
}
