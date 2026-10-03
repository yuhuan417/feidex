// Package threadsettings owns thread-scoped settings shared by command and card inputs.
package threadsettings

import (
	"errors"
	"feidex/internal/domain/conversation"
	"feidex/internal/domain/routing"
	"feidex/internal/domain/workspace"
	"fmt"
	"os"
	"strings"
)

type Repository interface {
	Session(string) *conversation.Session
	UpdateSession(string, func(*conversation.Session)) (*conversation.Session, error)
}
type Service struct{ Repository Repository }

func (s Service) Set(key, threadID string, setting routing.Setting, value string) (*conversation.Session, error) {
	value = strings.TrimSpace(value)
	var options []workspace.SettingOption
	switch setting {
	case routing.Sandbox:
		options = workspace.SandboxOptions()
	case routing.ApprovalPolicy:
		options = workspace.ApprovalPolicyOptions()
	case routing.MultiAgent:
		options = workspace.MultiAgentModeOptions()
	case routing.Permissions:
		if value != "" && value != "default" && value != "acceptEdits" && value != "bypassPermissions" {
			return nil, fmt.Errorf("不支持的权限模式")
		}
	default:
		return nil, fmt.Errorf("unsupported thread setting %q", setting)
	}
	if setting != routing.Permissions {
		valid := value == "" && setting != routing.MultiAgent
		for _, opt := range options {
			if opt.Value == value {
				valid = true
			}
		}
		if !valid {
			return nil, fmt.Errorf("不支持的 %s", setting)
		}
	}
	return s.update(key, threadID, false, func(sess *conversation.Session) {
		switch setting {
		case routing.Sandbox:
			sess.ActiveThreadSandboxMode = value
		case routing.ApprovalPolicy:
			sess.ActiveThreadApprovalPolicy = value
		case routing.MultiAgent:
			sess.ActiveThreadMultiAgentMode = value
		case routing.Permissions:
			sess.ActiveClaudePermissionMode = value
		}
	})
}

func (s Service) SetThreadServiceTier(key, threadID, tier string) (*conversation.Session, error) {
	return s.update(key, threadID, true, func(sess *conversation.Session) {
		sess.ActiveThreadServiceTier = conversation.NormalizeServiceTier(tier)
	})
}
func (s Service) Toggle(key string) (*conversation.Session, error) {
	return s.update(key, "", true, func(sess *conversation.Session) {
		tier := "fast"
		if conversation.NormalizeServiceTier(sess.ActiveThreadServiceTier) == "fast" {
			tier = ""
		}
		sess.ActiveThreadServiceTier = tier
	})
}

func (s Service) update(key, threadID string, tier bool, mutate func(*conversation.Session)) (*conversation.Session, error) {
	var invalid error
	sess, err := s.Repository.UpdateSession(key, func(current *conversation.Session) {
		if current == nil || strings.TrimSpace(current.ActiveThreadID) == "" {
			invalid = fmt.Errorf("当前 thread 已失效")
			if tier {
				invalid = fmt.Errorf("当前没有活动线程，无法切换 service tier")
			}
			return
		}
		if (threadID != "" || !tier) && strings.TrimSpace(current.ActiveThreadID) != strings.TrimSpace(threadID) {
			invalid = fmt.Errorf("当前 thread 已失效")
			return
		}
		mutate(current)
	})
	if errors.Is(err, os.ErrNotExist) || (err == nil && sess == nil) {
		if tier {
			return nil, fmt.Errorf("当前没有活动线程，无法切换 service tier")
		}
		return nil, fmt.Errorf("当前 thread 已失效")
	}
	if err != nil {
		return nil, err
	}
	if invalid != nil {
		return nil, invalid
	}
	return sess, nil
}
