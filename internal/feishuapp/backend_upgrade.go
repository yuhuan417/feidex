package feishuapp

import (
	"context"
	"errors"
	"strings"
	"time"

	feishuoutbound "feidex/internal/adapter/feishu/outbound"
	"feidex/internal/adapter/feishu/upgraderender"
	"feidex/internal/application"
	"feidex/internal/application/backendmaintenance"
	"feidex/internal/application/interaction"
	"feidex/internal/domain/identity"
	"feidex/internal/feishu"
	"feidex/internal/runtime"
	codexruntime "feidex/internal/runtime/codex"
	"feidex/internal/runtime/maintenance"
)

// backendUpgradeService is the shared entry point for the /claude and /codex
// upgrade commands.
type backendUpgradeService struct {
	sessionKey         func(*feishu.InboundMessage) string
	replyInThread      func() bool
	frontendID         identity.FrontendID
	runner             runtime.EffectRunner
	lifecycle          *runtime.FrontendRuntime
	asyncRunner        func(func())
	backendMaintenance map[string]*backendmaintenance.Service
	maintenanceRunners map[string]maintenance.OperationRunner
	maintenance        backendmaintenance.MaintenanceStateService
	presentation       upgradeRenderService
	forms              *interaction.FormService
	codexUpgrade       codexruntime.UpgradeService
}

type BackendUpgradeInputs struct {
	SessionKey         func(*feishu.InboundMessage) string
	ReplyInThread      func() bool
	FrontendID         identity.FrontendID
	Runner             runtime.EffectRunner
	Lifecycle          *runtime.FrontendRuntime
	AsyncRunner        func(func())
	BackendMaintenance map[string]*backendmaintenance.Service
	MaintenanceRunners map[string]maintenance.OperationRunner
	Maintenance        backendmaintenance.MaintenanceStateService
	Presentation       upgradeRenderService
	Forms              *interaction.FormService
	CodexUpgrade       codexruntime.UpgradeService
}

func BuildBackendUpgrades(inputs BackendUpgradeInputs) backendUpgradeService {
	return backendUpgradeService{
		sessionKey: inputs.SessionKey, replyInThread: inputs.ReplyInThread,
		frontendID: inputs.FrontendID, runner: inputs.Runner,
		lifecycle: inputs.Lifecycle, asyncRunner: inputs.AsyncRunner,
		backendMaintenance: inputs.BackendMaintenance, maintenanceRunners: inputs.MaintenanceRunners,
		maintenance: inputs.Maintenance, presentation: inputs.Presentation,
		forms: inputs.Forms, codexUpgrade: inputs.CodexUpgrade,
	}
}

func (s backendUpgradeService) replyCard(ctx context.Context, messageID string, card map[string]any, inThread bool) (string, error) {
	return s.runner.RunSendCard(ctx, application.SendCard{
		Frontend:       s.frontendID,
		ReplyMessageID: messageID,
		View:           feishuoutbound.Card(card),
		InThread:       inThread,
	})
}

func (s backendUpgradeService) replyCardWithID(ctx context.Context, parentMessageID string, card map[string]any, inThread bool) (string, error) {
	return s.runner.RunSendCard(ctx, application.SendCard{
		Frontend: s.frontendID, ReplyMessageID: parentMessageID,
		View: feishuoutbound.Card(card), InThread: inThread,
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
			return s.startClaudeRestartFromMessage(msg)
		default:
			return errors.New(claudeUpgradeCommandUsage)
		}
	}
	sessionKey := s.sessionKey(msg)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	view, err := s.loadClaudeUpgradeView(ctx, includeLatest)
	if err != nil {
		return err
	}
	if !prepareUpgrade {
		card := s.presentation.renderUpgradeStatusCard(upgraderender.ClaudeSpec, sessionKey, view, includeLatest)
		_, err = s.replyCard(context.Background(), msg.MessageID, card, s.replyInThread())
		return err
	}
	card, pendingID, err := s.presentation.prepareUpgradeCard(upgraderender.ClaudeSpec, claudeUpgradePendingKind, "claude-upgrade", sessionKey, msg.UserID, view)
	if err != nil {
		return err
	}
	msgID, err := s.replyCard(context.Background(), msg.MessageID, card, s.replyInThread())
	if err != nil {
		return err
	}
	if strings.TrimSpace(pendingID) != "" {
		if err := s.forms.SaveDraft(pendingID, nil, "", 0, msgID); err != nil {
			return err
		}
	}
	return nil
}

func (s backendUpgradeService) loadClaudeUpgradeView(ctx context.Context, includeLatest bool) (upgraderender.UpgradeView, error) {
	view, err := s.backendMaintenance["claude"].View(ctx, includeLatest)
	return upgraderender.UpgradeView{Probe: view.Probe, BusyReason: view.BusyReason, Snapshot: view.Snapshot, Restart: view.Restart, LatestVersion: view.LatestVersion, LatestError: view.LatestError}, err
}

func (s backendUpgradeService) loadCodexUpgradeView(ctx context.Context, includeLatest bool) (upgraderender.UpgradeView, error) {
	view, err := s.backendMaintenance["codex"].View(ctx, includeLatest)
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
			return s.startCodexRestartFromMessage(msg)
		default:
			return errors.New(codexUpgradeCommandUsage)
		}
	}
	sessionKey := s.sessionKey(msg)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	view, err := s.loadCodexUpgradeView(ctx, includeLatest)
	if err != nil {
		return err
	}
	if !prepareUpgrade {
		card := s.presentation.renderUpgradeStatusCard(upgraderender.CodexSpec, sessionKey, view, includeLatest)
		_, err = s.replyCard(context.Background(), msg.MessageID, card, s.replyInThread())
		return err
	}
	card, pendingID, err := s.presentation.prepareUpgradeCard(upgraderender.CodexSpec, codexUpgradePendingKind, "codex-upgrade", sessionKey, msg.UserID, view)
	if err != nil {
		return err
	}
	msgID, err := s.replyCard(context.Background(), msg.MessageID, card, s.replyInThread())
	if err != nil {
		return err
	}
	if strings.TrimSpace(pendingID) != "" {
		if err := s.forms.SaveDraft(pendingID, nil, "", 0, msgID); err != nil {
			return err
		}
	}
	return nil
}
