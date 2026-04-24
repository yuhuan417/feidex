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

func (s backendUpgradeService) commandCodex(msg *feishu.InboundMessage, args []string) error {
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
			return newBackendUpgradeService(s.app).startCodexRestartFromMessage(msg)
		default:
			return fmt.Errorf(codexUpgradeCommandUsage)
		}
	}
	sessionKey := s.app.makeSessionKey(msg)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	view, err := newBackendUpgradeService(s.app).loadCodexUpgradeView(ctx, includeLatest)
	if err != nil {
		return err
	}
	if !prepareUpgrade {
		card := newUpgradeRenderService(s.app).renderCodexUpgradeStatusCard(sessionKey, view, includeLatest)
		_, err = s.app.feishu.ReplyCard(context.Background(), msg.MessageID, card, s.app.replyInThreadEnabled(msg.ChatType))
		return err
	}
	card, pendingID, err := newUpgradeRenderService(s.app).prepareCodexUpgradeCard(sessionKey, msg.UserID, view)
	if err != nil {
		return err
	}
	msgID, err := s.app.feishu.ReplyCard(context.Background(), msg.MessageID, card, s.app.replyInThreadEnabled(msg.ChatType))
	if err != nil {
		return err
	}
	if strings.TrimSpace(pendingID) != "" {
		_ = s.app.appState().updatePending(pendingID, func(req *state.PendingRequest) {
			req.FeishuMsgID = msgID
		})
	}
	return nil
}

func (s backendUpgradeService) loadCodexUpgradeView(ctx context.Context, includeLatest bool) (codexUpgradeView, error) {
	manager := newCodexInstallManager(s.app.cfg.Codex.Command)
	probe, err := manager.Probe(ctx)
	if err != nil {
		return codexUpgradeView{}, err
	}
	view := codexUpgradeView{
		Probe:      probe,
		BusyReason: newMaintenanceStateService(s.app).codexUpgradeRuntimeBusyReason(),
		Snapshot:   newMaintenanceStateService(s.app).codexUpgradeState(),
		Restart:    newMaintenanceStateService(s.app).codexRestartState(),
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
