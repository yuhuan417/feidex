package interaction

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	domain "feidex/internal/domain/interaction"
)

type FormRepository interface {
	Pending(string) *domain.PendingRequest
	SavePending(*domain.PendingRequest) error
	NextLocalID(string) (string, error)
	UpdatePending(string, func(*domain.PendingRequest)) error
}

type FormService struct{ Repository FormRepository }

func (s FormService) Open(prefix string, request domain.PendingRequest, payload any, lifetime time.Duration) (*domain.PendingRequest, error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	request.ID, err = s.Repository.NextLocalID(prefix)
	if err != nil {
		return nil, err
	}
	if lifetime <= 0 {
		lifetime = 30 * time.Minute
	}
	now := time.Now()
	request.PayloadJSON, request.Status = string(encoded), "pending"
	request.CreatedAt, request.ExpiresAt = now.Unix(), now.Add(lifetime).Unix()
	if err := s.Repository.SavePending(&request); err != nil {
		return nil, err
	}
	return s.Repository.Pending(request.ID), nil
}

func (s FormService) Authorize(id, kind, userID string) (*domain.PendingRequest, error) {
	request := s.Repository.Pending(strings.TrimSpace(id))
	if request == nil || request.Kind != kind || request.Status != "pending" || (request.ExpiresAt > 0 && request.ExpiresAt <= time.Now().Unix()) {
		return nil, fmt.Errorf("请求已过期")
	}
	if request.OwnerUserID != "" && request.OwnerUserID != userID {
		return nil, fmt.Errorf("你没有权限处理这个请求")
	}
	return request, nil
}

// Transition validates ownership and phase in the same repository transaction.
func (s FormService) Transition(id, kind, userID, expected, next, messageID string) (*domain.PendingRequest, error) {
	var result *domain.PendingRequest
	var validationErr error
	err := s.Repository.UpdatePending(strings.TrimSpace(id), func(current *domain.PendingRequest) {
		if current == nil || current.Kind != kind || strings.TrimSpace(current.Status) != expected || (current.ExpiresAt > 0 && current.ExpiresAt <= time.Now().Unix()) {
			validationErr = fmt.Errorf("请求已过期或正在处理")
			return
		}
		if current.OwnerUserID != "" && current.OwnerUserID != userID {
			validationErr = fmt.Errorf("你没有权限处理这个请求")
			return
		}
		if domain.IsServerResolvedPendingKind(current.Kind) || domain.IsClaudeInteractiveKind(current.Kind) || current.Kind == "async_user_input" {
			validationErr = fmt.Errorf("backend interaction cannot enter local form workflow")
			return
		}
		if next != "processing" && next != "resolved" {
			validationErr = fmt.Errorf("invalid form transition: %s", next)
			return
		}
		copy := *current
		result = &copy
		current.Status = next
		if current.FeishuMsgID == "" {
			current.FeishuMsgID = messageID
		}
	})
	if err != nil {
		return nil, err
	}
	if validationErr != nil {
		return nil, validationErr
	}
	if result == nil {
		return nil, fmt.Errorf("请求不存在")
	}
	return result, nil
}

// SaveDraft owns local form updates; backend approvals use Service's distinct
// replied/resolved transitions and never pass through this form workflow.
func (s FormService) SaveDraft(id string, payload any, status string, lifetime time.Duration, messageID string) error {
	var encoded []byte
	var err error
	if payload != nil {
		encoded, err = json.Marshal(payload)
		if err != nil {
			return err
		}
	}
	var transitionErr error
	err = s.Repository.UpdatePending(id, func(current *domain.PendingRequest) {
		if current == nil {
			return
		}
		if domain.IsServerResolvedPendingKind(current.Kind) || domain.IsClaudeInteractiveKind(current.Kind) || current.Kind == "async_user_input" {
			transitionErr = fmt.Errorf("backend interaction cannot enter local form workflow")
			return
		}
		phase := strings.TrimSpace(current.Status)
		if current.ExpiresAt > 0 && current.ExpiresAt <= time.Now().Unix() && status != "expired" {
			transitionErr = fmt.Errorf("form is expired")
			return
		}
		if phase == "resolved" || phase == "expired" {
			transitionErr = fmt.Errorf("form is already closed")
			return
		}
		if status == "processing" && phase != "pending" && phase != "" {
			transitionErr = fmt.Errorf("form is already processing")
			return
		}
		if status == "cancelling" && phase != "processing" {
			transitionErr = fmt.Errorf("form is not processing")
			return
		}
		if status != "" && status != "pending" && status != "processing" && status != "cancelling" && status != "resolved" && status != "expired" {
			transitionErr = fmt.Errorf("invalid form transition: %s", status)
			return
		}
		if payload != nil {
			current.PayloadJSON = string(encoded)
		}
		if status != "" {
			current.Status = status
		}
		if lifetime > 0 {
			current.ExpiresAt = time.Now().Add(lifetime).Unix()
		}
		if current.FeishuMsgID == "" && messageID != "" {
			current.FeishuMsgID = messageID
		}
	})
	if err != nil {
		return err
	}
	return transitionErr
}
