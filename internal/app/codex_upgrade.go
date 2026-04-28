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

type backendUpgradeService struct {
	app *App
}

func newBackendUpgradeService(app *App) backendUpgradeService {
	return backendUpgradeService{app: app}
}

const (
	codexUpgradePendingKind  = "codex_npm_upgrade"
	codexUpgradeCommandUsage = "usage: /codex | /codex check | /codex upgrade | /codex restart"
)

type codexUpgradeView struct {
	Probe         codexinstall.Probe
	LatestVersion string
	LatestError   string
	BusyReason    string
	Snapshot      backendUpgradeSnapshot
	Restart       backendRestartSnapshot
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
	sessionKey := makeSessionKey(s.app, msg)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	view, err := newBackendUpgradeService(s.app).loadCodexUpgradeView(ctx, includeLatest)
	if err != nil {
		return err
	}
	if !prepareUpgrade {
		card := newUpgradeRenderService(s.app).renderCodexUpgradeStatusCard(sessionKey, view, includeLatest)
		_, err = s.app.feishu.ReplyCard(context.Background(), msg.MessageID, card, replyInThreadEnabled(s.app, msg.ChatType))
		return err
	}
	card, pendingID, err := newUpgradeRenderService(s.app).prepareCodexUpgradeCard(sessionKey, msg.UserID, view)
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

func (s backendUpgradeService) loadCodexUpgradeView(ctx context.Context, includeLatest bool) (codexUpgradeView, error) {
	manager := newCodexInstallManager(s.app.cfg.Codex.Command)
	probe, err := manager.Probe(ctx)
	if err != nil {
		return codexUpgradeView{}, err
	}
	view := codexUpgradeView{
		Probe:      probe,
		BusyReason: newMaintenanceStateService(s.app).CodexUpgradeRuntimeBusyReason(),
		Snapshot:   newMaintenanceStateService(s.app).CodexUpgradeState(),
		Restart:    newMaintenanceStateService(s.app).CodexRestartState(),
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
