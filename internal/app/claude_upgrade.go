package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"feidex/internal/claudeinstall"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

const (
	claudeUpgradePendingKind  = "claude_npm_upgrade"
	claudeUpgradeCommandUsage = "usage: /claude | /claude check | /claude upgrade | /claude restart"
)


type claudeUpgradeView struct {
	Probe         claudeinstall.Probe
	LatestVersion string
	LatestError   string
	BusyReason    string
	Snapshot      backendUpgradeSnapshot
	Restart       backendRestartSnapshot
}

func (s backendUpgradeService) commandClaude(msg *feishu.InboundMessage, args []string) error {
	if msg == nil {
		return nil
	}
	if len(args) > 1 {
		return fmt.Errorf(claudeUpgradeCommandUsage)
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
			return fmt.Errorf(claudeUpgradeCommandUsage)
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
		card := newUpgradeRenderService(s.app).renderClaudeUpgradeStatusCard(sessionKey, view, includeLatest)
		_, err = s.app.feishu.ReplyCard(context.Background(), msg.MessageID, card, replyInThreadEnabled(s.app, msg.ChatType))
		return err
	}
	card, pendingID, err := newUpgradeRenderService(s.app).prepareClaudeUpgradeCard(sessionKey, msg.UserID, view)
	if err != nil {
		return err
	}
	msgID, err := s.app.feishu.ReplyCard(context.Background(), msg.MessageID, card, replyInThreadEnabled(s.app, msg.ChatType))
	if err != nil {
		return err
	}
	if strings.TrimSpace(pendingID) != "" {
		_ = appState(s.app).updatePending(pendingID, func(req *state.PendingRequest) {
			req.FeishuMsgID = msgID
		})
	}
	return nil
}

func (s backendUpgradeService) loadClaudeUpgradeView(ctx context.Context, includeLatest bool) (claudeUpgradeView, error) {
	manager := newClaudeInstallManager(s.app.cfg.Claude.Command)
	probe, err := manager.Probe(ctx)
	if err != nil {
		return claudeUpgradeView{}, err
	}
	view := claudeUpgradeView{
		Probe:      probe,
		BusyReason: newMaintenanceStateService(s.app).ClaudeUpgradeRuntimeBusyReason(),
		Snapshot:   newMaintenanceStateService(s.app).ClaudeUpgradeState(),
		Restart:    newMaintenanceStateService(s.app).ClaudeRestartState(),
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
