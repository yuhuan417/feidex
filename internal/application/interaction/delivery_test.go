package interaction

import (
	"context"
	"errors"
	"testing"
	"time"

	"feidex/internal/domain/conversation"
	domain "feidex/internal/domain/interaction"
)

type deliveryRepository struct {
	req                                         *domain.PendingRequest
	failSave, failWaiting, failUpdate, failLink bool
	links                                       int
}

func (r *deliveryRepository) NextLocalID(string) (string, error) { return "request-1", nil }

func (r *deliveryRepository) Pending(string) *domain.PendingRequest {
	if r.req == nil {
		return nil
	}
	cp := *r.req
	return &cp
}
func (r *deliveryRepository) SavePending(req *domain.PendingRequest) error {
	if r.failSave {
		return errors.New("save failed")
	}
	cp := *req
	r.req = &cp
	return nil
}
func (r *deliveryRepository) UpdatePending(_ string, fn func(*domain.PendingRequest)) error {
	if r.failUpdate {
		return errors.New("update failed")
	}
	fn(r.req)
	return nil
}
func (r *deliveryRepository) SetSubmissionStatus(string, string) error {
	if r.failWaiting {
		return errors.New("waiting failed")
	}
	return nil
}
func (r *deliveryRepository) SaveMessageLink(*conversation.MessageLink) error {
	if r.failLink {
		return errors.New("link failed")
	}
	r.links++
	return nil
}

type deliveryPresenter struct {
	calls  int
	input  DeliveryInput
	cached string
	fail   bool
}

func (p *deliveryPresenter) DeliverInteraction(_ context.Context, input DeliveryInput) (string, error) {
	p.input = input
	p.calls++
	if p.fail {
		return "", errors.New("send failed")
	}
	if p.cached == "" {
		p.cached = "card-1"
	}
	return p.cached, nil
}
func deliveryInput() DeliveryInput {
	return DeliveryInput{Request: domain.PendingRequest{ID: "request-1", PayloadJSON: "original", SessionKey: "session-1", ThreadID: "thread-1", TurnID: "turn-1"}, SubmissionID: "submission-1", WaitingStatus: "waiting_approval", TTL: time.Minute}
}

func TestDeliveryPersistsBeforeSending(t *testing.T) {
	for _, stage := range []string{"request", "waiting"} {
		t.Run(stage, func(t *testing.T) {
			repo := &deliveryRepository{failSave: stage == "request", failWaiting: stage == "waiting"}
			presenter := &deliveryPresenter{}
			if err := (DeliveryService{Repository: repo}).Open(context.Background(), deliveryInput(), presenter); err == nil {
				t.Fatal("expected persistence failure")
			}
			if presenter.calls != 0 {
				t.Fatal("sent before persistence succeeded")
			}
		})
	}
}
func TestDeliveryRetryRetainsPayloadAndLifetime(t *testing.T) {
	repo := &deliveryRepository{}
	presenter := &deliveryPresenter{fail: true}
	svc := DeliveryService{Repository: repo}
	if err := svc.Open(context.Background(), deliveryInput(), presenter); err == nil {
		t.Fatal("expected send failure")
	}
	before := *repo.req
	next := deliveryInput()
	next.Request.PayloadJSON = "replacement"
	next.TTL = time.Hour
	presenter.fail = false
	if err := svc.Open(context.Background(), next, presenter); err != nil {
		t.Fatal(err)
	}
	if presenter.input.Request.CreatedAt != before.CreatedAt || presenter.input.Request.ExpiresAt != before.ExpiresAt || presenter.input.Request.PayloadJSON != before.PayloadJSON {
		t.Fatalf("retry replaced request: %+v", presenter.input.Request)
	}
}
func TestDeliveryLinkRepairDoesNotSendAgain(t *testing.T) {
	repo := &deliveryRepository{failLink: true}
	presenter := &deliveryPresenter{}
	svc := DeliveryService{Repository: repo}
	if err := svc.Open(context.Background(), deliveryInput(), presenter); err == nil {
		t.Fatal("expected link failure")
	}
	repo.failLink = false
	if err := svc.Open(context.Background(), deliveryInput(), presenter); err != nil {
		t.Fatal(err)
	}
	if presenter.calls != 1 || repo.links != 1 {
		t.Fatalf("calls=%d links=%d", presenter.calls, repo.links)
	}
}
func TestTerminalDeliveryNeverReopensRequest(t *testing.T) {
	for _, status := range []string{"replied", "resolved", "expired"} {
		t.Run(status, func(t *testing.T) {
			repo := &deliveryRepository{req: &domain.PendingRequest{ID: "request-1", Status: status}}
			presenter := &deliveryPresenter{}
			if err := (DeliveryService{Repository: repo}).Open(context.Background(), deliveryInput(), presenter); err != nil {
				t.Fatal(err)
			}
			if presenter.calls != 0 || repo.req.Status != status {
				t.Fatal("terminal request reopened")
			}
		})
	}
}
func TestLocalFormCannotReopenOrDuplicateProcessing(t *testing.T) {
	for _, test := range []struct{ kind, status, next string }{{"command", "pending", "resolved"}, {"workspace_clone", "resolved", "pending"}, {"workspace_clone", "processing", "processing"}} {
		t.Run(test.kind+test.status, func(t *testing.T) {
			repo := &deliveryRepository{req: &domain.PendingRequest{Kind: test.kind, Status: test.status, PayloadJSON: "original"}}
			if err := (FormService{Repository: repo}).SaveDraft("request-1", map[string]string{"new": "value"}, test.next, time.Hour, ""); err == nil {
				t.Fatal("expected transition rejection")
			}
			if repo.req.PayloadJSON != "original" || repo.req.Status != test.status {
				t.Fatal("rejected transition mutated form")
			}
		})
	}
}
