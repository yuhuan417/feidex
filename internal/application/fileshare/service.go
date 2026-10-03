package fileshare

import (
	"context"
	"errors"
	interactionapp "feidex/internal/application/interaction"
	"feidex/internal/domain/conversation"
	"feidex/internal/domain/interaction"
	"feidex/internal/domain/workspace"
	"feidex/internal/textutil"
	"fmt"
	"strings"
	"time"
)

const Kind = "download_file"

var ErrProcessing = errors.New("正在生成下载链接，请稍候")

type Request struct{ LocalPath, ChatID, UserID string }
type Result struct {
	FileName, URL string
	SizeBytes     int64
}
type Execution struct {
	ID, SessionKey, MessageID, WorkspaceCWD string
	Payload                                 workspace.PathPickerPayload
	Request                                 Request
}
type Repository interface {
	Session(string) *conversation.Session
}
type Artifacts interface {
	Share(context.Context, Request) (Result, error)
}
type Presentation interface {
	Completed(Execution, Result, error)
}
type Service struct {
	Forms        *interactionapp.FormService
	Repository   Repository
	Artifacts    Artifacts
	Presentation Presentation
	Context      func() context.Context
	Run          func(string, func()) bool
}

func (s Service) Open(key, userID string, payload workspace.PathPickerPayload) (*interaction.PendingRequest, error) {
	return s.Forms.Open("download", interaction.PendingRequest{Kind: Kind, SessionKey: key, OwnerUserID: userID}, payload, 10*time.Minute)
}

func (s Service) Confirm(id, userID, chatID, messageID, path string, payload workspace.PathPickerPayload) (Execution, error) {
	pending := s.Forms.Repository.Pending(id)
	if pending == nil || pending.Kind != Kind {
		return Execution{}, fmt.Errorf("下载请求已过期")
	}
	if pending.Status == "processing" || pending.Status == "sharing" {
		return Execution{}, ErrProcessing
	}
	if _, err := s.Forms.Transition(id, Kind, userID, "pending", "processing", messageID); err != nil {
		return Execution{}, err
	}
	execution := Execution{ID: id, SessionKey: pending.SessionKey, MessageID: textutil.FirstNonEmpty(pending.FeishuMsgID, messageID), WorkspaceCWD: strings.TrimSpace(payload.RootPath), Payload: payload,
		Request: Request{LocalPath: path, ChatID: chatID, UserID: userID}}
	if sess := s.Repository.Session(pending.SessionKey); sess != nil {
		execution.Request.ChatID = textutil.FirstNonEmpty(chatID, sess.ChatID)
	}
	if !s.Run(pending.SessionKey, func() { s.Execute(execution) }) {
		if err := s.finish(id, false); err != nil {
			return Execution{}, err
		}
		return Execution{}, fmt.Errorf("frontend is shutting down")
	}
	return execution, nil
}

func (s Service) Execute(input Execution) {
	claimed := false
	if err := s.Forms.Repository.UpdatePending(input.ID, func(current *interaction.PendingRequest) {
		if current != nil && current.Kind == Kind && current.Status == "processing" {
			current.Status = "sharing"
			claimed = true
		}
	}); err != nil {
		s.Presentation.Completed(input, Result{}, err)
		return
	}
	if !claimed {
		return
	}
	ctx, cancel := context.WithTimeout(s.Context(), 30*time.Second)
	defer cancel()
	result, cause := s.Artifacts.Share(ctx, input.Request)
	if err := s.finish(input.ID, cause == nil); err != nil {
		s.Presentation.Completed(input, Result{}, err)
		return
	}
	s.Presentation.Completed(input, result, cause)
}

func (s Service) finish(id string, success bool) error {
	var validation error
	err := s.Forms.Repository.UpdatePending(id, func(current *interaction.PendingRequest) {
		if current == nil || current.Kind != Kind || (current.Status != "processing" && current.Status != "sharing") {
			validation = fmt.Errorf("下载请求已结束")
			return
		}
		current.Status = "resolved"
		if !success {
			current.Status = "pending"
			current.ExpiresAt = time.Now().Add(10 * time.Minute).Unix()
		}
	})
	if err != nil {
		return err
	}
	return validation
}
