// Package maintenance provides runtime maintenance services extracted from the
// app god package. This file contains the runtime maintenance service that
// handles attachment cleanup, artifact GC, upgrade checks, and startup recovery.
package maintenance

import (
	"context"
	"feidex/internal/application/upgrade"
	"feidex/internal/domain/conversation"
	"feidex/internal/runtime/maintenance"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	appattachments "feidex/internal/adapter/feishu/attachments"

	appfeishuwrap "feidex/internal/adapter/feishu/feishuwrap"
	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

// AttachmentRetention is how long attachment directories are kept before GC.
const AttachmentRetention = 7 * 24 * time.Hour

// ArtifactRetention is how long drive artifacts are kept before GC.
const ArtifactRetention = 3 * 24 * time.Hour

// ArtifactGCTimeout is the maximum duration for a background Drive artifact
// cleanup pass. Deleting Drive folders can be slow because Feishu handles
// folder deletion asynchronously server-side.
const ArtifactGCTimeout = 5 * time.Minute

// FrontendCardNotificationKindFeishuPermissionIssue is the notification kind
// for Feishu permission diagnostic cards.
const FrontendCardNotificationKindFeishuPermissionIssue = "feishu_permission_issue"

// ---------------------------------------------------------------------------
// PermissionIssueDiagnosticSender — local interface for direct notifications
// ---------------------------------------------------------------------------

// PermissionIssueDiagnosticSender is the interface for sending permission issue
// diagnostics directly to specific chats (as opposed to queuing them).
type PermissionIssueDiagnosticSender interface {
	NotifyPermissionIssue(target appfeishuwrap.NotifyTarget, err error)
}

// ---------------------------------------------------------------------------
// Service
// ---------------------------------------------------------------------------

// RuntimeMaintenanceService manages background maintenance tasks such as
// attachment cleanup, drive artifact GC, and upgrade status polling.
type Dependencies struct {
	Context           func() context.Context
	Workspaces        func() []config.Workspace
	Poller            upgrade.Poller
	Repository        maintenance.StateProvider
	ArtifactClient    ArtifactClient
	Outbound          Outbound
	Renderer          CardRenderer
	PermissionNotify  PermissionIssueDiagnosticSender
	MenuBody          func(string, string) string
	QueueNotification func(state.FrontendCardNotification)
	ReadyChatIDs      func([]*conversation.Session) []string
	RunAsync          func(func())
}
type ArtifactClient interface {
	CleanupArtifactsBefore(context.Context, time.Time) (feishu.PreviewDriveCleanupResult, error)
}
type Outbound interface {
	PatchCard(context.Context, string, map[string]any) error
}
type CardRenderer interface {
	SimpleStatusCard(string, string, string, []feishu.Button) map[string]any
}
type RuntimeMaintenanceService struct{ deps Dependencies }

func NewRuntimeMaintenanceService(deps Dependencies) RuntimeMaintenanceService {
	return RuntimeMaintenanceService{deps: deps}
}
func (s RuntimeMaintenanceService) context() context.Context {
	if s.deps.Context != nil {
		return s.deps.Context()
	}
	return context.Background()
}

// ---------------------------------------------------------------------------
// Attachment cleanup
// ---------------------------------------------------------------------------

// CleanupExpiredAttachments removes attachment directories older than
// AttachmentRetention across all configured workspaces.
func (s RuntimeMaintenanceService) CleanupExpiredAttachments() {
	for _, ws := range s.deps.Workspaces() {
		root := filepath.Join(ws.Cwd, appattachments.AttachmentsDirName)
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			s.CleanupAttachmentDir(filepath.Join(root, entry.Name()))
		}
	}
}

// CleanupAttachmentDir removes files in the given directory that are older than
// AttachmentRetention.
func (s RuntimeMaintenanceService) CleanupAttachmentDir(root string) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	threshold := time.Now().Add(-AttachmentRetention)
	for _, entry := range entries {
		path := filepath.Join(root, entry.Name())
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(threshold) {
			_ = os.RemoveAll(path)
		}
	}
}

// ---------------------------------------------------------------------------
// Drive artifact GC loop
// ---------------------------------------------------------------------------

// StartDriveArtifactGCLoop launches a background goroutine that runs drive
// artifact garbage collection on startup and then every 24 hours.

// RunDriveArtifactGC performs a single artifact GC pass, deleting drive files
// older than ArtifactRetention.
func (s RuntimeMaintenanceService) RunDriveArtifactGC(source string) {
	feishuClient := s.deps.ArtifactClient
	if feishuClient == nil {
		return
	}
	ctx, cancel := context.WithTimeout(s.context(), ArtifactGCTimeout)
	defer cancel()
	result, err := feishuClient.CleanupArtifactsBefore(ctx, time.Now().Add(-ArtifactRetention))
	if err != nil {
		s.NotifyDriveArtifactGCPermissionIssue(source, err)
		slog.Warn("artifact gc failed", "source", source, "error", err)
		return
	}
	if result.DeletedFileCount == 0 {
		return
	}
	slog.Debug("artifact gc complete",
		"source", source,
		"deleted_file_count", result.DeletedFileCount,
		"deleted_estimated_bytes", result.DeletedEstimatedBytes,
	)
}

// ---------------------------------------------------------------------------
// Permission issue notification for artifact GC failures
// ---------------------------------------------------------------------------

// NotifyDriveArtifactGCPermissionIssue sends a diagnostic notification when
// artifact GC fails due to a Feishu permission issue.
func (s RuntimeMaintenanceService) NotifyDriveArtifactGCPermissionIssue(source string, err error) {
	feishuClient := s.deps.ArtifactClient
	if feishuClient == nil || err == nil {
		return
	}
	issue, ok := feishu.PermissionIssueFromError(err)
	if !ok || issue == nil {
		return
	}
	body := feishu.RenderPermissionIssueBody(issue)
	if body == "" {
		return
	}
	notifier := s.deps.PermissionNotify
	appState := s.deps.Repository
	if appState == nil {
		return
	}
	chatIDs := s.deps.ReadyChatIDs(appState.Sessions())
	if len(chatIDs) == 0 {
		s.deps.QueueNotification(state.FrontendCardNotification{
			Kind:        FrontendCardNotificationKindFeishuPermissionIssue,
			CollapseKey: FrontendCardNotificationKindFeishuPermissionIssue,
			Title:       "飞书权限错误",
			Color:       "red",
			Body:        body,
		})
		slog.Debug("artifact gc permission diagnostic queued",
			"source", source,
			"reason", "no_known_chats",
			"api", strings.TrimSpace(issue.API),
		)
		return
	}
	if notifier == nil {
		s.deps.QueueNotification(state.FrontendCardNotification{
			Kind:        FrontendCardNotificationKindFeishuPermissionIssue,
			CollapseKey: FrontendCardNotificationKindFeishuPermissionIssue,
			Title:       "飞书权限错误",
			Color:       "red",
			Body:        body,
		})
		slog.Debug("artifact gc permission diagnostic queued",
			"source", source,
			"reason", "feishu_notifier_unavailable",
			"api", strings.TrimSpace(issue.API),
		)
		return
	}
	for _, chatID := range chatIDs {
		notifier.NotifyPermissionIssue(appfeishuwrap.NotifyTarget{ChatID: chatID}, err)
	}
}

// ---------------------------------------------------------------------------
// Upgrade check loop
// ---------------------------------------------------------------------------

// StartUpgradeCheckLoop launches a background goroutine that polls for pending
// upgrades on startup and then every 30 seconds.

// CheckPendingUpgrades scans all pending requests for "upgrading" status and
// checks each one.
func (s RuntimeMaintenanceService) CheckPendingUpgrades(source string) {
	pendings := s.deps.Repository.PendingRequests()
	for _, pending := range pendings {
		if upgrade.IsRunning(pending) {
			s.CheckOneUpgrade(source, pending)
		}
	}
}

// CheckOneUpgrade inspects a single upgrade request, queries the systemd unit
// status, and patches the Feishu card accordingly.
func (s RuntimeMaintenanceService) CheckOneUpgrade(source string, pending *state.PendingRequest) {
	ctx, cancel := context.WithTimeout(s.context(), 10*time.Second)
	defer cancel()
	outcome, err := s.deps.Poller.Check(ctx, pending)
	if err != nil {
		slog.Warn("upgrade check failed", "source", source, "error", err)
		return
	}
	if outcome == nil {
		return
	}
	if outcome.UnitName != "" {
		if err := s.deps.Poller.Units.Cleanup(ctx, outcome.UnitName); err != nil {
			slog.Warn("upgrade unit cleanup failed", "error", err)
		}
	}
	sessionKey, feishuMsgID, unitName := outcome.SessionKey, outcome.MessageID, outcome.UnitName
	if feishuMsgID == "" {
		return
	}
	cardRenderer := s.deps.Renderer
	var card map[string]any
	if outcome.Success {
		slog.Info("upgrade unit succeeded", "unit", unitName, "source", source)
		body := "升级已完成，服务已重启。"
		card = cardRenderer.SimpleStatusCard("升级成功", "green", s.deps.MenuBody("menu.upgrade", body), []feishu.Button{
			{Text: feishu.MenuBackButtonText, Type: "default", Value: map[string]any{"action": "menu.group.system", "session_key": sessionKey}},
		})
	} else {
		errMsg := outcome.Error
		slog.Warn("upgrade unit failed", "unit", unitName, "error", errMsg, "source", source)
		body := "升级失败。"
		if errMsg != "" {
			body += "\n\n错误: " + errMsg
		}
		card = cardRenderer.SimpleStatusCard("升级失败", "red", s.deps.MenuBody("menu.upgrade", body), []feishu.Button{
			{Text: feishu.MenuBackButtonText, Type: "default", Value: map[string]any{"action": "menu.group.system", "session_key": sessionKey}},
		})
	}

	if err := s.deps.Outbound.PatchCard(ctx, feishuMsgID, card); err != nil {
		slog.Error("upgrade check: patch card failed", "unit", unitName, "msg_id", feishuMsgID, "error", err)
	}
}

// ---------------------------------------------------------------------------
// Pure helpers
// ---------------------------------------------------------------------------

// ExtractUpgradeErrorFromJournal scans the journal tail for an error line,
// falling back to the last non-empty line.
func ExtractUpgradeErrorFromJournal(journal string) string { return upgrade.ExtractError(journal) }
