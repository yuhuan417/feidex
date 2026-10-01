package app

import (
	"context"
	"errors"
	"strings"
	"time"

	appbackend "feidex/internal/app/backend"
	"feidex/internal/app/upgraderender"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

// backendUpgradeService is the shared entry point for the /claude and /codex
// upgrade commands.
type backendUpgradeService struct {
	app *App
}

func newBackendUpgradeService(app *App) backendUpgradeService {
	return serviceFor(app, "backendUpgradeService", func() backendUpgradeService {
		return backendUpgradeService{app: app}
	})
}

// backendUpgradeView is the upgrade snapshot both backends render from.
type backendUpgradeView = upgraderender.UpgradeView

const (
	claudeUpgradePendingKind  = "claude_self_upgrade"
	codexUpgradePendingKind   = "codex_self_upgrade"
	claudeUpgradeCommandUsage = "usage: /claude | /claude check | /claude upgrade | /claude restart"
	codexUpgradeCommandUsage  = "usage: /codex | /codex check | /codex upgrade | /codex restart"
)

func (s backendUpgradeService) commandClaude(msg *feishu.InboundMessage, args []string) error {
	if msg == nil {
		return nil
	}
	if len(args) > 1 {
		return errors.New(claudeUpgradeCommandUsage)
	}
	includeLatest := false
	prepareUpgrade := false
	if len(args) == 1 {
		switch strings.TrimSpace(args[0]) {
		case "check":
			includeLatest = true
		case "upgrade":
			includeLatest = true
			prepareUpgrade = true
		case "restart":
			return newBackendUpgradeService(s.app).startClaudeRestartFromMessage(msg)
		default:
			return errors.New(claudeUpgradeCommandUsage)
		}
	}
	sessionKey := makeSessionKey(s.app, msg)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	view, err := newBackendUpgradeService(s.app).loadClaudeUpgradeView(ctx, includeLatest)
	if err != nil {
		return err
	}
	if !prepareUpgrade {
		card := newUpgradeRenderService(s.app).renderUpgradeStatusCard(upgraderender.ClaudeSpec, sessionKey, view, includeLatest)
		_, err = s.app.feishu.ReplyCard(context.Background(), msg.MessageID, card, replyInThreadEnabled(s.app, msg.ChatType))
		return err
	}
	card, pendingID, err := newUpgradeRenderService(s.app).prepareUpgradeCard(upgraderender.ClaudeSpec, claudeUpgradePendingKind, "claude-upgrade", sessionKey, msg.UserID, view)
	if err != nil {
		return err
	}
	msgID, err := s.app.feishu.ReplyCard(context.Background(), msg.MessageID, card, replyInThreadEnabled(s.app, msg.ChatType))
	if err != nil {
		return err
	}
	if strings.TrimSpace(pendingID) != "" {
		_ = s.app.State().UpdatePending(pendingID, func(req *state.PendingRequest) {
			req.FeishuMsgID = msgID
		})
	}
	return nil
}

func (s backendUpgradeService) loadClaudeUpgradeView(ctx context.Context, includeLatest bool) (backendUpgradeView, error) {
	manager := newClaudeInstallManager(s.app.cfg.Claude.Command)
	probe, err := manager.Probe(ctx)
	if err != nil {
		return backendUpgradeView{}, err
	}
	view := backendUpgradeView{
		Probe:      probe,
		BusyReason: appbackend.NewMaintenanceStateService(s.app).ClaudeUpgradeRuntimeBusyReason(),
		Snapshot:   appbackend.NewMaintenanceStateService(s.app).ClaudeUpgradeState(),
		Restart:    appbackend.NewMaintenanceStateService(s.app).ClaudeRestartState(),
	}
	if includeLatest && probe.Supported && !view.Snapshot.Running && !view.Restart.Running {
		latest, latestErr := manager.LatestVersion(ctx)
		if latestErr != nil {
			view.LatestError = latestErr.Error()
		} else {
			view.LatestVersion = strings.TrimSpace(latest)
		}
	}
	return view, nil
}

func (s backendUpgradeService) loadCodexUpgradeView(ctx context.Context, includeLatest bool) (backendUpgradeView, error) {
	manager := newCodexInstallManager(s.app.cfg.Codex.Command)
	probe, err := manager.Probe(ctx)
	if err != nil {
		return backendUpgradeView{}, err
	}
	view := backendUpgradeView{
		Probe:      probe,
		BusyReason: appbackend.NewMaintenanceStateService(s.app).CodexUpgradeRuntimeBusyReason(),
		Snapshot:   appbackend.NewMaintenanceStateService(s.app).CodexUpgradeState(),
		Restart:    appbackend.NewMaintenanceStateService(s.app).CodexRestartState(),
	}
	if includeLatest && probe.Supported && !view.Snapshot.Running && !view.Restart.Running {
		latest, latestErr := manager.LatestVersion(ctx)
		if latestErr != nil {
			view.LatestError = latestErr.Error()
		} else {
			view.LatestVersion = strings.TrimSpace(latest)
		}
	}
	return view, nil
}

func (s backendUpgradeService) commandCodex(msg *feishu.InboundMessage, args []string) error {
	if msg == nil {
		return nil
	}
	if len(args) > 1 {
		return errors.New(codexUpgradeCommandUsage)
	}
	includeLatest := false
	prepareUpgrade := false
	if len(args) == 1 {
		switch strings.TrimSpace(args[0]) {
		case "check":
			includeLatest = true
		case "upgrade":
			includeLatest = true
			prepareUpgrade = true
		case "restart":
			return newBackendUpgradeService(s.app).startCodexRestartFromMessage(msg)
		default:
			return errors.New(codexUpgradeCommandUsage)
		}
	}
	sessionKey := makeSessionKey(s.app, msg)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	view, err := newBackendUpgradeService(s.app).loadCodexUpgradeView(ctx, includeLatest)
	if err != nil {
		return err
	}
	if !prepareUpgrade {
		card := newUpgradeRenderService(s.app).renderUpgradeStatusCard(upgraderender.CodexSpec, sessionKey, view, includeLatest)
		_, err = s.app.feishu.ReplyCard(context.Background(), msg.MessageID, card, replyInThreadEnabled(s.app, msg.ChatType))
		return err
	}
	card, pendingID, err := newUpgradeRenderService(s.app).prepareUpgradeCard(upgraderender.CodexSpec, codexUpgradePendingKind, "codex-upgrade", sessionKey, msg.UserID, view)
	if err != nil {
		return err
	}
	msgID, err := s.app.feishu.ReplyCard(context.Background(), msg.MessageID, card, replyInThreadEnabled(s.app, msg.ChatType))
	if err != nil {
		return err
	}
	if strings.TrimSpace(pendingID) != "" {
		_ = s.app.State().UpdatePending(pendingID, func(req *state.PendingRequest) {
			req.FeishuMsgID = msgID
		})
	}
	return nil
}
