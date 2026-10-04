package feishuapp

import (
	"context"
	"errors"
	"strings"
	"time"

	feishuoutbound "feidex/internal/adapter/feishu/outbound"
	"feidex/internal/adapter/feishu/upgraderender"
	"feidex/internal/application"
	"feidex/internal/domain/identity"
	"feidex/internal/feishu"
	"feidex/internal/runtime"
)

// backendUpgradeService is the shared entry point for the /claude and /codex
// upgrade commands.
type backendUpgradeService struct {
	app    *App
	runner runtime.EffectRunner
}

func BuildBackendUpgrades(app *App) backendUpgradeService {
	return backendUpgradeService{app: app, runner: newEffectRunner(app.runtimeOwner)}
}

func (s backendUpgradeService) replyCard(ctx context.Context, messageID string, card map[string]any, inThread bool) (string, error) {
	if s.app == nil {
		return "", errors.New("application is not initialized")
	}
	return s.runner.RunSendCard(ctx, application.SendCard{
		Frontend:       identity.FrontendID(s.app.FrontendID()),
		ReplyMessageID: messageID,
		View:           feishuoutbound.Card(card),
		InThread:       inThread,
	})
}

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
			return s.app.bindings.BackendUpgrades.startClaudeRestartFromMessage(msg)
		default:
			return errors.New(claudeUpgradeCommandUsage)
		}
	}
	sessionKey := makeSessionKey(s.app, msg)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	view, err := s.app.bindings.BackendUpgrades.loadClaudeUpgradeView(ctx, includeLatest)
	if err != nil {
		return err
	}
	if !prepareUpgrade {
		card := s.app.bindings.UpgradePresentation.renderUpgradeStatusCard(upgraderender.ClaudeSpec, sessionKey, view, includeLatest)
		_, err = s.replyCard(context.Background(), msg.MessageID, card, replyInThreadEnabled(s.app, msg.ChatType))
		return err
	}
	card, pendingID, err := s.app.bindings.UpgradePresentation.prepareUpgradeCard(upgraderender.ClaudeSpec, claudeUpgradePendingKind, "claude-upgrade", sessionKey, msg.UserID, view)
	if err != nil {
		return err
	}
	msgID, err := s.replyCard(context.Background(), msg.MessageID, card, replyInThreadEnabled(s.app, msg.ChatType))
	if err != nil {
		return err
	}
	if strings.TrimSpace(pendingID) != "" {
		if err := s.app.bindings.Forms.SaveDraft(pendingID, nil, "", 0, msgID); err != nil {
			return err
		}
	}
	return nil
}

func (s backendUpgradeService) loadClaudeUpgradeView(ctx context.Context, includeLatest bool) (upgraderender.UpgradeView, error) {
	view, err := s.app.bindings.BackendMaintenance["claude"].View(ctx, includeLatest)
	return upgraderender.UpgradeView{Probe: view.Probe, BusyReason: view.BusyReason, Snapshot: view.Snapshot, Restart: view.Restart, LatestVersion: view.LatestVersion, LatestError: view.LatestError}, err
}

func (s backendUpgradeService) loadCodexUpgradeView(ctx context.Context, includeLatest bool) (upgraderender.UpgradeView, error) {
	view, err := s.app.bindings.BackendMaintenance["codex"].View(ctx, includeLatest)
	return upgraderender.UpgradeView{Probe: view.Probe, BusyReason: view.BusyReason, Snapshot: view.Snapshot, Restart: view.Restart, LatestVersion: view.LatestVersion, LatestError: view.LatestError}, err
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
			return s.app.bindings.BackendUpgrades.startCodexRestartFromMessage(msg)
		default:
			return errors.New(codexUpgradeCommandUsage)
		}
	}
	sessionKey := makeSessionKey(s.app, msg)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	view, err := s.app.bindings.BackendUpgrades.loadCodexUpgradeView(ctx, includeLatest)
	if err != nil {
		return err
	}
	if !prepareUpgrade {
		card := s.app.bindings.UpgradePresentation.renderUpgradeStatusCard(upgraderender.CodexSpec, sessionKey, view, includeLatest)
		_, err = s.replyCard(context.Background(), msg.MessageID, card, replyInThreadEnabled(s.app, msg.ChatType))
		return err
	}
	card, pendingID, err := s.app.bindings.UpgradePresentation.prepareUpgradeCard(upgraderender.CodexSpec, codexUpgradePendingKind, "codex-upgrade", sessionKey, msg.UserID, view)
	if err != nil {
		return err
	}
	msgID, err := s.replyCard(context.Background(), msg.MessageID, card, replyInThreadEnabled(s.app, msg.ChatType))
	if err != nil {
		return err
	}
	if strings.TrimSpace(pendingID) != "" {
		if err := s.app.bindings.Forms.SaveDraft(pendingID, nil, "", 0, msgID); err != nil {
			return err
		}
	}
	return nil
}
