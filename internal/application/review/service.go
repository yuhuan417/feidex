package review

import (
	"context"
	interactionapp "feidex/internal/application/interaction"
	"fmt"
	"strings"

	"feidex/internal/domain/conversation"
	"feidex/internal/domain/review"
	"feidex/internal/domain/submission"
	"feidex/internal/textutil"
)

type Workspace struct{ ID, CWD string }
type Input struct{ SessionKey, UserID, ChatID, ChatType, MessageID, RootMessageID, SubmissionID string }
type Repository interface {
	Session(string) *conversation.Session
	SaveSession(*conversation.Session) error
	CreateSubmission(*submission.Submission) (string, error)
	QueueSubmission(string, string) error
	Submission(string) *submission.Submission
}
type TargetResolver interface {
	Resolve(string, review.TargetSpec) (review.TargetSpec, error)
}
type Dispatcher interface {
	StartNext(string) error
	MarkQueued(*submission.Submission)
	Notify(context.Context, *submission.Submission)
}
type Dependencies struct {
	Forms      *interactionapp.FormService
	Delivery   *interactionapp.DeliveryService
	Options    Options
	Gateway    func() (Gateway, error)
	Repository Repository
	Resolver   TargetResolver
	Dispatcher Dispatcher
}

type Service Dependencies

func NewService(deps Dependencies) Service { return Service(deps) }

func (s Service) Start(ctx context.Context, input Input, ws Workspace, target review.TargetSpec) (review.TargetSpec, error) {
	if strings.TrimSpace(input.MessageID) == "" || strings.TrimSpace(ws.ID) == "" {
		return review.TargetSpec{}, fmt.Errorf("review input is incomplete")
	}
	sess := s.Repository.Session(input.SessionKey)
	if sess == nil {
		sess = &conversation.Session{Key: input.SessionKey, WorkspaceID: ws.ID, OwnerUserID: input.UserID, ChatID: input.ChatID, ChatType: input.ChatType, RootMessageID: input.RootMessageID, Status: conversation.SessionStatusIdle.String()}
		if err := s.Repository.SaveSession(sess); err != nil {
			return review.TargetSpec{}, err
		}
	}
	if conversation.HasActiveWork(sess) {
		return review.TargetSpec{}, fmt.Errorf("当前任务仍在运行，请先等待结束或中断")
	}
	if len(sess.Queue) > 0 || len(sess.StagedImages) > 0 {
		return review.TargetSpec{}, fmt.Errorf("当前仍有待处理输入")
	}
	resolved, err := s.Resolver.Resolve(ws.CWD, target)
	if err != nil {
		return review.TargetSpec{}, err
	}
	has := conversation.HasInFlightSubmission(sess)
	queued := len(sess.Queue) > 0 || has
	if queued {
		sess.Status = conversation.SessionStatusQueued.String()
		if err := s.Repository.SaveSession(sess); err != nil {
			return review.TargetSpec{}, err
		}
	}
	sub := &submission.Submission{ID: input.SubmissionID, SessionKey: input.SessionKey, WorkspaceID: ws.ID, ThreadID: strings.TrimSpace(sess.ActiveThreadID), UserID: input.UserID, ChatID: input.ChatID, TriggerMessageID: input.MessageID, SourceMessageIDs: []string{input.MessageID}, SourceRootMessageIDs: []string{first(input.RootMessageID, input.MessageID)}, InputText: SubmissionInputText(resolved), Kind: "review", ReviewTargetType: resolved.Type, ReviewBranch: resolved.Branch, ReviewCommitSHA: resolved.CommitSHA, ReviewCommitTitle: resolved.CommitTitle, ReviewInstructions: resolved.Instructions, Status: submission.SubmissionStatusQueued.String(), WaitedInQueue: queued}
	id, err := s.Repository.CreateSubmission(sub)
	if err != nil {
		return review.TargetSpec{}, err
	}
	sub.ID = id
	if err := s.Repository.QueueSubmission(input.SessionKey, id); err != nil {
		return review.TargetSpec{}, err
	}
	if !has {
		if err := s.Dispatcher.StartNext(input.SessionKey); err != nil {
			return review.TargetSpec{}, err
		}
		return resolved, nil
	}
	s.Dispatcher.MarkQueued(sub)
	s.Dispatcher.Notify(ctx, sub)
	return resolved, nil
}
func first(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}
func SubmissionInputText(target review.TargetSpec) string {
	switch strings.TrimSpace(target.Type) {
	case review.TargetUncommitted:
		return "Review: uncommitted changes"
	case review.TargetBaseBranch:
		return "Review: base branch " + strings.TrimSpace(target.Branch)
	case review.TargetCommit:
		label := strings.TrimSpace(target.CommitSHA)
		if len(label) > 12 {
			label = label[:12]
		}
		if strings.TrimSpace(target.CommitTitle) != "" {
			label += " " + strings.TrimSpace(target.CommitTitle)
		}
		return "Review: commit " + strings.TrimSpace(label)
	case review.TargetCustom:
		return "Review: " + textutil.Truncate(strings.TrimSpace(target.Instructions), 80)
	default:
		return "Review"
	}
}
