package clauderuntime

import (
	"context"
	"feidex/internal/claudecli"
	"feidex/internal/config"
	"feidex/internal/textutil"
	"fmt"
	"strings"
	"time"
)

type Maintenance struct {
	Smoke   func(context.Context) error
	Active  func() bool
	Current func() interface{ Close() error }
	Create  func()
}

func (s Maintenance) Refresh(ctx context.Context) (bool, error) {
	if err := s.Smoke(ctx); err != nil {
		return false, err
	}
	if !s.Active() {
		return false, nil
	}
	if core := s.Current(); core != nil {
		if err := core.Close(); err != nil {
			return false, fmt.Errorf("切换 runtime 失败: %w", err)
		}
	} else {
		s.Create()
	}
	return true, nil
}

func Smoke(ctx, lifetime context.Context, cfg config.ClaudeConfig, workdir string) error {
	opts := []claudecli.SessionOption{
		claudecli.WithCLIPath(textutil.FirstNonEmpty(strings.TrimSpace(cfg.Command), "claude")),
		claudecli.WithWorkDir(workdir),
		claudecli.WithPermissionMode(PermissionModeValue(cfg.PermissionMode)),
		claudecli.WithEventBufferSize(16),
	}
	if cfg.DangerouslySkipPermissions {
		opts = append(opts, claudecli.WithDangerouslySkipPermissions())
	}
	if model := strings.TrimSpace(cfg.Model); model != "" {
		opts = append(opts, claudecli.WithModel(model))
	}
	if effort := strings.TrimSpace(cfg.Effort); effort != "" {
		opts = append(opts, claudecli.WithEffort(effort))
	}
	if cfg.DisablePlugins {
		opts = append(opts, claudecli.WithDisablePlugins())
	}
	if prompt := strings.TrimSpace(cfg.SystemPrompt); prompt != "" {
		opts = append(opts, claudecli.WithSystemPrompt(prompt))
	}
	if cfg.PermissionPromptToolStdio {
		opts = append(opts, claudecli.WithPermissionPromptToolStdio())
	}
	session := claudecli.NewSession(opts...)
	sessionCtx, cancel := context.WithCancel(lifetime)
	defer cancel()
	if err := session.Start(sessionCtx); err != nil {
		return err
	}
	defer session.Stop()
	if err := session.Initialize(ctx); err != nil {
		return err
	}
	return waitForSmokeStable(ctx, session, time.Second)
}

func waitForSmokeStable(ctx context.Context, session *claudecli.Session, grace time.Duration) error {
	timer := time.NewTimer(grace)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			return session.ExitError()
		case event, ok := <-session.Events():
			if !ok {
				if err := session.ExitError(); err != nil {
					return err
				}
				return fmt.Errorf("claude session exited after initialize")
			}
			switch value := event.(type) {
			case claudecli.ReadyEvent:
				return nil
			case claudecli.ErrorEvent:
				return smokeEventError(session, value)
			}
		}
	}
}

func smokeEventError(session *claudecli.Session, event claudecli.ErrorEvent) error {
	if event.Error == nil {
		return fmt.Errorf("claude session startup failed after initialize")
	}
	if event.Context == "stdout_eof" || (event.Context == "read_line" && strings.Contains(strings.ToLower(event.Error.Error()), "file already closed")) {
		if err := session.ExitError(); err != nil {
			return fmt.Errorf("claude session exited after initialize: %w", err)
		}
		return fmt.Errorf("claude session exited after initialize")
	}
	return event.Error
}
