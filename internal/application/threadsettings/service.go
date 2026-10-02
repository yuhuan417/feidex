// Package threadsettings owns thread-scoped settings shared by command and card inputs.
package threadsettings

import (
	"feidex/internal/domain/conversation"
	"fmt"
	"strings"
)

type Repository interface {
	Session(string) *conversation.Session
	SaveSession(*conversation.Session) error
}
type Service struct{ Repository Repository }

func (s Service) SetThreadServiceTier(key, threadID, tier string) (*conversation.Session, error) {
	sess := s.Repository.Session(key)
	if sess == nil || strings.TrimSpace(sess.ActiveThreadID) == "" {
		return nil, fmt.Errorf("当前没有活动线程，无法切换 service tier")
	}
	if strings.TrimSpace(threadID) != "" && strings.TrimSpace(sess.ActiveThreadID) != strings.TrimSpace(threadID) {
		return nil, fmt.Errorf("当前 thread 已失效")
	}
	sess.ActiveThreadServiceTier = conversation.NormalizeServiceTier(tier)
	if err := s.Repository.SaveSession(sess); err != nil {
		return nil, err
	}
	return sess, nil
}
func (s Service) Toggle(key string) (*conversation.Session, error) {
	sess := s.Repository.Session(key)
	if sess == nil || strings.TrimSpace(sess.ActiveThreadID) == "" {
		return nil, fmt.Errorf("当前没有活动线程，无法切换 service tier")
	}
	tier := "fast"
	if conversation.NormalizeServiceTier(sess.ActiveThreadServiceTier) == "fast" {
		tier = ""
	}
	return s.SetThreadServiceTier(key, sess.ActiveThreadID, tier)
}
