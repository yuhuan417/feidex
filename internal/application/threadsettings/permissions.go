package threadsettings

import (
	"context"
	"fmt"
	"strings"
	"time"

	"feidex/internal/domain/conversation"
	"feidex/internal/domain/routing"
	"feidex/internal/domain/workspace"
)

type PermissionRevision struct {
	Session     *conversation.Session
	Binding     *routing.AgentBinding
	Profile     *routing.BotProfile
	Workspace   *workspace.Workspace
	Default     string
	AllowBypass bool
}

type PermissionSource interface {
	PermissionRevision(string) PermissionRevision
}
type PermissionRuntime interface {
	ApplyPermission(context.Context, string, string) error
}
type PermissionTasks interface{ Run(string, func()) bool }
type PermissionFailure interface{ PermissionFailed(string, string, error) }

type PermissionService struct {
	Settings Service
	Source   PermissionSource
	Runtime  PermissionRuntime
	Tasks    PermissionTasks
	Failure  PermissionFailure
	Context  func() context.Context
}

func (s PermissionService) Set(key, threadID, raw, messageID string, async bool) (*conversation.Session, error) {
	value := strings.TrimSpace(raw)
	switch value {
	case "inherit", "follow", "clear", "unset", "workspace", "global":
		value = ""
	case "", "default", "acceptEdits":
	case "bypassPermissions":
		if !s.Source.PermissionRevision(key).AllowBypass {
			return nil, fmt.Errorf("当前未启用 `claude.dangerously_skip_permissions`，不能切到 `bypassPermissions`")
		}
	default:
		return nil, fmt.Errorf("不支持的 Claude 权限模式 `%s`", value)
	}
	sess, err := s.Settings.Set(key, threadID, routing.Permissions, value)
	if err != nil {
		return nil, err
	}
	if !async {
		return sess, s.apply(key, threadID)
	}
	if !s.Tasks.Run(key, func() {
		if err := s.apply(key, threadID); err != nil {
			s.Failure.PermissionFailed(messageID, key, err)
		}
	}) {
		s.Failure.PermissionFailed(messageID, key, fmt.Errorf("frontend is stopping"))
	}
	return sess, nil
}

func (s PermissionService) apply(key, threadID string) error {
	revision := s.Source.PermissionRevision(key)
	if revision.Session == nil || revision.Session.ActiveThreadID != threadID {
		return nil
	}
	mode := conversation.ResolveSettings(revision.Session, revision.Binding, revision.Profile, revision.Workspace, revision.Default).ClaudePermissionMode
	ctx, cancel := context.WithTimeout(s.Context(), 5*time.Second)
	defer cancel()
	return s.Runtime.ApplyPermission(ctx, key, mode)
}
