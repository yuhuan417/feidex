// Package asyncinput owns non-blocking question claims and answer routing.
package asyncinput

import (
	"fmt"
	"strings"

	"feidex/internal/application"
	"feidex/internal/domain/conversation"
	"feidex/internal/domain/identity"
	"feidex/internal/domain/interaction"
)

type Repository interface {
	Session(string) *conversation.Session
	UpdatePending(string, func(*interaction.PendingRequest)) error
}
type Service struct {
	Repository Repository
	Backend    string
}

func (s Service) Session(p *interaction.PendingRequest) (*conversation.Session, error) {
	if p == nil {
		return nil, fmt.Errorf("请求已过期")
	}
	sess := s.Repository.Session(p.SessionKey)
	if sess == nil || sess.ActiveThreadID != p.ThreadID || s.Backend != p.Backend {
		return nil, fmt.Errorf("会话已切换，请在当前会话中回答")
	}
	return sess, nil
}
func (s Service) Validate(p *interaction.PendingRequest, userID, messageID, chatID string, cancel bool) error {
	if p == nil || p.Kind != "async_user_input" || strings.TrimSpace(p.Status) != "pending" {
		return fmt.Errorf("请求已处理或过期")
	}
	if p.OwnerUserID != "" && p.OwnerUserID != userID {
		return fmt.Errorf("你没有权限回答这个问题")
	}
	if messageID != "" && messageID != p.FeishuMsgID {
		return fmt.Errorf("问题卡片不匹配")
	}
	if cancel {
		return nil
	}
	sess, err := s.Session(p)
	if err != nil {
		return err
	}
	if chatID != "" && chatID != sess.ChatID {
		return fmt.Errorf("问题会话不匹配")
	}
	return nil
}

// Claim is atomic and does no transport I/O; the frontend can acknowledge now.
func (s Service) Claim(p *interaction.PendingRequest, cancel bool) error {
	claimed := false
	err := s.Repository.UpdatePending(p.ID, func(current *interaction.PendingRequest) {
		if strings.TrimSpace(current.Status) != "pending" {
			return
		}
		claimed = true
		current.Status = "replied"
		if cancel {
			current.Status = "resolved"
		}
	})
	if err != nil {
		return err
	}
	if !claimed {
		return fmt.Errorf("请求已处理或正在提交")
	}
	return nil
}

// AnswerEffect chooses steer versus queue from the current session. Async
// questions have no server-request reply or authoritative resolved boundary.
func (s Service) AnswerEffect(p *interaction.PendingRequest, userID, text string) (application.Effect, error) {
	sess, err := s.Session(p)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(sess.ActiveTurnID) != "" {
		return application.SteerTurn{Frontend: identity.FrontendID(p.FrontendID), SessionKey: p.SessionKey, ThreadID: sess.ActiveThreadID, ExpectedTurnID: sess.ActiveTurnID, Text: text}, nil
	}
	return application.EnqueueInput{Frontend: identity.FrontendID(p.FrontendID), SessionKey: p.SessionKey, BindOnlyCurrentRoot: true, Message: application.InboundMessage{SessionKey: p.SessionKey, MessageID: p.FeishuMsgID, ParentMessageID: p.FeishuMsgID, RootMessageID: p.FeishuMsgID, ChatID: sess.ChatID, ChatType: sess.ChatType, UserID: userID, Text: text}}, nil
}
func (s Service) Complete(p *interaction.PendingRequest, accepted bool) error {
	return s.Repository.UpdatePending(p.ID, func(current *interaction.PendingRequest) {
		current.Status = "pending"
		if accepted {
			current.Status = "resolved"
		}
	})
}
