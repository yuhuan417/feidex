package feishuapp

import (
	"feidex/internal/application/interaction"
	domain "feidex/internal/domain/interaction"
)

type PendingReplyAdapter struct {
	Service    *interaction.Service
	Repository interface {
		Pending(string) *domain.PendingRequest
	}
}

func (p PendingReplyAdapter) Finalize(pending *domain.PendingRequest) *domain.PendingRequest {
	if pending == nil {
		return nil
	}
	request, err := p.Service.FinalizeReply(pending.ID)
	if err != nil || request == nil {
		return nil
	}
	return p.Repository.Pending(request.ID)
}
func (p PendingReplyAdapter) Resolve(id string) *domain.PendingRequest {
	request, err := p.Service.Resolve(id)
	if err != nil || request == nil {
		return nil
	}
	return p.Repository.Pending(request.ID)
}
func (p PendingReplyAdapter) Resume(pending *domain.PendingRequest) {
	if pending != nil {
		_ = p.Service.ResumeSubmission(&domain.Request{ID: pending.ID, ThreadID: pending.ThreadID, TurnID: pending.TurnID})
	}
}
