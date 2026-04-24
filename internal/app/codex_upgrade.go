package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"feidex/internal/codexinstall"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

const (
	codexUpgradePendingKind  = "codex_npm_upgrade"
	codexUpgradeCommandUsage = "usage: /codex | /codex check | /codex upgrade | /codex restart"
)

type codexUpgradePendingPayload struct {
	CurrentVersion string `json:"current_version"`
	TargetVersion  string `json:"target_version"`
	Command        string `json:"command"`
	CommandPath    string `json:"command_path"`
	NPMPath        string `json:"npm_path"`
}

type codexUpgradeView struct {
	Probe         codexinstall.Probe
	LatestVersion string
	LatestError   string
	BusyReason    string
	Snapshot      codexUpgradeSnapshot
	Restart       codexRestartSnapshot
}

func (a *App) commandCodex(msg *feishu.InboundMessage, args []string) error {
	if msg == nil {
		return nil
	}
	if len(args) > 1 {
		return fmt.Errorf(codexUpgradeCommandUsage)
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
			return a.startCodexRestartFromMessage(msg)
		default:
			return fmt.Errorf(codexUpgradeCommandUsage)
		}
	}
	sessionKey := a.makeSessionKey(msg)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	view, err := a.loadCodexUpgradeView(ctx, includeLatest)
	if err != nil {
		return err
	}
	if !prepareUpgrade {
		card := a.renderCodexUpgradeStatusCard(sessionKey, view, includeLatest)
		_, err = a.feishu.ReplyCard(context.Background(), msg.MessageID, card, a.replyInThreadEnabled(msg.ChatType))
		return err
	}
	card, pendingID, err := a.prepareCodexUpgradeCard(sessionKey, msg.UserID, view)
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

func (a *App) loadCodexUpgradeView(ctx context.Context, includeLatest bool) (codexUpgradeView, error) {
	manager := newCodexInstallManager(a.cfg.Codex.Command)
	probe, err := manager.Probe(ctx)
	if err != nil {
		return codexUpgradeView{}, err
	}
	view := codexUpgradeView{
		Probe:      probe,
		BusyReason: newMaintenanceStateService(a).codexUpgradeRuntimeBusyReason(),
		Snapshot:   newMaintenanceStateService(a).codexUpgradeState(),
		Restart:    newMaintenanceStateService(a).codexRestartState(),
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
