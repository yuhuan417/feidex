package review

import (
	"context"
	"encoding/json"
	"feidex/internal/application/backendops"
	"feidex/internal/domain/interaction"
	domain "feidex/internal/domain/review"
	"feidex/internal/domain/submission"
	"fmt"
	"strings"
	"time"
)

const FormKind = "review_form"

type FormPayload struct {
	Mode         string `json:"mode"`
	Branch       string `json:"branch,omitempty"`
	CommitSHA    string `json:"commit_sha,omitempty"`
	CommitTitle  string `json:"commit_title,omitempty"`
	Instructions string `json:"instructions,omitempty"`
}
type BranchOption struct {
	Name             string
	UpdatedAt        int64
	Current, Default bool
}
type CommitOption struct{ SHA, ShortSHA, Date, Subject string }
type Options interface {
	Branches(string) ([]BranchOption, error)
	Commits(string, int) ([]CommitOption, error)
}
type Gateway interface {
	StartReview(context.Context, backendops.ReviewRequest) (backendops.ReviewResult, error)
}

func (s Service) OpenForm(input Input, ws Workspace, mode string) (*interaction.PendingRequest, FormPayload, error) {
	payload := FormPayload{Mode: strings.TrimSpace(mode)}
	switch payload.Mode {
	case "base":
		options, err := s.Options.Branches(ws.CWD)
		if err != nil {
			return nil, payload, err
		}
		if len(options) == 0 {
			return nil, payload, fmt.Errorf("当前仓库没有可选 branch")
		}
		payload.Branch = options[0].Name
	case "commit":
		options, err := s.Options.Commits(ws.CWD, 100)
		if err != nil {
			return nil, payload, err
		}
		if len(options) == 0 {
			return nil, payload, fmt.Errorf("当前仓库没有可选 commit")
		}
		payload.CommitSHA, payload.CommitTitle = options[0].SHA, options[0].Subject
	case "custom":
	default:
		return nil, payload, fmt.Errorf("unsupported review form mode %q", mode)
	}
	req, err := s.Forms.Open("review", interaction.PendingRequest{Kind: FormKind, SessionKey: input.SessionKey, OwnerUserID: input.UserID}, payload, 10*time.Minute)
	return req, payload, err
}

func (s Service) Form(id, userID, mode string) (*interaction.PendingRequest, FormPayload, error) {
	req, err := s.Forms.Authorize(id, FormKind, userID)
	if err != nil {
		return nil, FormPayload{}, err
	}
	var payload FormPayload
	if err := json.Unmarshal([]byte(req.PayloadJSON), &payload); err != nil {
		return nil, payload, err
	}
	if mode != "" && payload.Mode != mode {
		return nil, payload, fmt.Errorf("review 请求类型不匹配")
	}
	return req, payload, nil
}
func (s Service) Select(id, userID, mode, value string, ws Workspace) (*interaction.PendingRequest, FormPayload, error) {
	req, payload, err := s.Form(id, userID, mode)
	if err != nil {
		return nil, payload, err
	}
	value = strings.TrimSpace(value)
	switch mode {
	case "base":
		options, err := s.Options.Branches(ws.CWD)
		if err != nil {
			return nil, payload, err
		}
		found := false
		for _, option := range options {
			if option.Name == value {
				found = true
			}
		}
		if !found {
			return nil, payload, fmt.Errorf("未收到有效 branch")
		}
		payload.Branch = value
	case "commit":
		options, err := s.Options.Commits(ws.CWD, 100)
		if err != nil {
			return nil, payload, err
		}
		found := false
		for _, option := range options {
			if option.SHA == value {
				payload.CommitSHA, payload.CommitTitle, found = value, option.Subject, true
			}
		}
		if !found {
			return nil, payload, fmt.Errorf("未收到有效 commit")
		}
	default:
		return nil, payload, fmt.Errorf("unknown review form")
	}
	err = s.Forms.SaveDraft(id, payload, "", 0, "")
	return req, payload, err
}
func (s Service) SubmitForm(ctx context.Context, id string, input Input, ws Workspace, instructions *string) (domain.TargetSpec, error) {
	req, payload, err := s.Form(id, input.UserID, "")
	if err != nil {
		return domain.TargetSpec{}, err
	}
	if instructions != nil {
		payload.Instructions = *instructions
	}
	var target domain.TargetSpec
	switch payload.Mode {
	case "base":
		target = domain.TargetSpec{Type: domain.TargetBaseBranch, Branch: payload.Branch}
	case "commit":
		target = domain.TargetSpec{Type: domain.TargetCommit, CommitSHA: payload.CommitSHA, CommitTitle: payload.CommitTitle}
	case "custom":
		target = domain.TargetSpec{Type: domain.TargetCustom, Instructions: payload.Instructions}
	default:
		return target, fmt.Errorf("未知 review 表单")
	}
	if err := s.Forms.SaveDraft(id, payload, "processing", 0, ""); err != nil {
		return target, err
	}
	input.SessionKey, input.SubmissionID = req.SessionKey, "review-"+id
	resolved, startErr := s.Start(ctx, input, ws, target)
	// Admission is irreversible for this form even if backend startup fails.
	status := "pending"
	if s.Repository.Submission(input.SubmissionID) != nil {
		status = "resolved"
	}
	if err := s.Forms.SaveDraft(id, payload, status, 0, ""); err != nil {
		return resolved, err
	}
	return resolved, startErr
}
func (s Service) StartSubmission(ctx context.Context, threadID string, sub *submission.Submission) (string, error) {
	if sub == nil {
		return "", fmt.Errorf("nil submission")
	}
	if strings.TrimSpace(threadID) == "" {
		return "", fmt.Errorf("review requires an active thread")
	}
	gateway, err := s.Gateway()
	if err != nil {
		return "", err
	}
	result, err := gateway.StartReview(ctx, backendops.ReviewRequest{ThreadID: threadID, Target: domain.TargetSpec{Type: sub.ReviewTargetType, Branch: sub.ReviewBranch, CommitSHA: sub.ReviewCommitSHA, CommitTitle: sub.ReviewCommitTitle, Instructions: sub.ReviewInstructions}})
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(result.Turn.ID) == "" {
		return "", fmt.Errorf("review/start returned empty turn id")
	}
	return strings.TrimSpace(result.Turn.ID), nil
}
