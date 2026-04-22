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

type claudeUpgradePendingPayload struct {
	CurrentVersion string `json:"current_version"`
	TargetVersion  string `json:"target_version"`
	Command        string `json:"command"`
	CommandPath    string `json:"command_path"`
	NPMPath        string `json:"npm_path"`
}

type claudeUpgradeView struct {
	Probe         claudeinstall.Probe
	LatestVersion string
	LatestError   string
	BusyReason    string
	Snapshot      claudeUpgradeSnapshot
	Restart       claudeRestartSnapshot
}

func (a *App) commandClaude(msg *feishu.InboundMessage, args []string) error {
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
			return a.startClaudeRestartFromMessage(msg)
		default:
			return fmt.Errorf(claudeUpgradeCommandUsage)
		}
	}
	sessionKey := a.makeSessionKey(msg)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	view, err := a.loadClaudeUpgradeView(ctx, includeLatest)
	if err != nil {
		return err
	}
	if !prepareUpgrade {
		card := a.renderClaudeUpgradeStatusCard(sessionKey, view, includeLatest)
		_, err = a.feishu.ReplyCard(context.Background(), msg.MessageID, card, a.replyInThreadEnabled(msg.ChatType))
		return err
	}
	card, pendingID, err := a.prepareClaudeUpgradeCard(sessionKey, msg.UserID, view)
	if err != nil {
		return err
	}
	msgID, err := a.feishu.ReplyCard(context.Background(), msg.MessageID, card, a.replyInThreadEnabled(msg.ChatType))
	if err != nil {
		return err
	}
	if strings.TrimSpace(pendingID) != "" {
		_ = a.appState().updatePending(pendingID, func(req *state.PendingRequest) {
			req.FeishuMsgID = msgID
		})
	}
	return nil
}

func (a *App) loadClaudeUpgradeView(ctx context.Context, includeLatest bool) (claudeUpgradeView, error) {
	manager := newClaudeInstallManager(a.cfg.Claude.Command)
	probe, err := manager.Probe(ctx)
	if err != nil {
		return claudeUpgradeView{}, err
	}
	view := claudeUpgradeView{
		Probe:      probe,
		BusyReason: a.claudeUpgradeRuntimeBusyReason(),
		Snapshot:   a.claudeUpgradeState(),
		Restart:    a.claudeRestartState(),
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
