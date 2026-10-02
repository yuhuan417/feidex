// Package maintenance provides runtime maintenance services extracted from the
// app god package. This file contains the runtime maintenance service that
// handles attachment cleanup, artifact GC, upgrade checks, and startup recovery.
package maintenance

import (
	"context"
	"encoding/json"
	"feidex/internal/domain/conversation"
	"feidex/internal/runtime/maintenance"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	appattachments "feidex/internal/app/attachments"

	appfeishuwrap "feidex/internal/app/feishuwrap"
	"feidex/internal/config"
	"feidex/internal/daemon"
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
// UpgradePendingPayload mirrors the app-local type used by upgrade checks
// ---------------------------------------------------------------------------

// UpgradePendingPayload is the JSON payload stored in a "upgrading" pending
// request. It is defined here to avoid importing the parent app package.
type UpgradePendingPayload struct {
	CurrentVersion string `json:"current_version"`
	TargetVersion  string `json:"target_version"`
	ReleaseTag     string `json:"release_tag"`
	BinaryPath     string `json:"binary_path"`
	DownloadURL    string `json:"download_url"`
	SourcePath     string `json:"source_path"`
	SourceKind     string `json:"source_kind"`
	SourceName     string `json:"source_name"`
	SourceSize     int64  `json:"source_size"`
	SourceCommit   string `json:"source_commit"`
	ExpectedSHA256 string `json:"expected_sha256"`
	ReleaseURL     string `json:"release_url"`
	UnitName       string `json:"unit_name,omitempty"`
	ChatID         string `json:"chat_id,omitempty"`
	FeishuMsgID    string `json:"feishu_msg_id,omitempty"`
}

// ---------------------------------------------------------------------------
// Service
// ---------------------------------------------------------------------------

// RuntimeMaintenanceService manages background maintenance tasks such as
// attachment cleanup, drive artifact GC, and upgrade status polling.
type Dependencies struct {
	Context           func() context.Context
	Workspaces        func() []config.Workspace
	Store             *state.Store
	Repository        maintenance.StateProvider
	Client            Client
	MenuBody          func(string, string) string
	QueueNotification func(state.FrontendCardNotification)
	ReadyChatIDs      func([]*conversation.Session) []string
	RunAsync          func(func())
}
type Client interface {
	CleanupArtifactsBefore(context.Context, time.Time) (feishu.PreviewDriveCleanupResult, error)
	SimpleStatusCard(string, string, string, []feishu.Button) map[string]any
	PatchCard(context.Context, string, map[string]any) error
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
// Startup: expire pending requests
// ---------------------------------------------------------------------------

// ExpirePendingRequestsOnStartup marks all pending/replied requests as expired
// so that stale state from a previous run does not leak into the new session.
func (s RuntimeMaintenanceService) ExpirePendingRequestsOnStartup() {
	store := s.deps.Store
	if store == nil {
		return
	}
	for _, req := range store.AllPendingRequests() {
		if req == nil {
			continue
		}
		status := state.NormalizePendingRequestStatus(req.Status)
		if status != state.PendingRequestStatusPending && status != state.PendingRequestStatusReplied {
			continue
		}
		_ = store.UpdateScopedPending(req.FrontendID, req.ID, func(p *state.PendingRequest) {
			p.Status = state.PendingRequestStatusExpired.String()
			if p.ExpiresAt < time.Now().Unix() {
				return
			}
			p.ExpiresAt = time.Now().Unix()
		})
	}
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
func (s RuntimeMaintenanceService) StartDriveArtifactGCLoop(ctx context.Context) {
	feishuClient := s.deps.Client
	if feishuClient == nil {
		return
	}
	s.deps.RunAsync(func() { s.RunDriveArtifactGC("startup") })
	s.deps.RunAsync(func() {
		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.RunDriveArtifactGC("ticker")
			}
		}
	})
}

// RunDriveArtifactGC performs a single artifact GC pass, deleting drive files
// older than ArtifactRetention.
func (s RuntimeMaintenanceService) RunDriveArtifactGC(source string) {
	feishuClient := s.deps.Client
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
	feishuClient := s.deps.Client
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
	notifier, _ := feishuClient.(PermissionIssueDiagnosticSender)
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
func (s RuntimeMaintenanceService) StartUpgradeCheckLoop(ctx context.Context) {
	feishuClient := s.deps.Client
	if feishuClient == nil {
		return
	}
	s.deps.RunAsync(func() { s.CheckPendingUpgrades("startup") })
	s.deps.RunAsync(func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.CheckPendingUpgrades("ticker")
			}
		}
	})
}

// CheckPendingUpgrades scans all pending requests for "upgrading" status and
// checks each one.
func (s RuntimeMaintenanceService) CheckPendingUpgrades(source string) {
	store := s.deps.Store
	if store == nil {
		return
	}
	pendings := s.deps.Repository.PendingRequests()
	for _, pending := range pendings {
		if pending != nil && state.NormalizePendingRequestStatus(pending.Status) == state.PendingRequestStatusUpgrading {
			s.CheckOneUpgrade(source, pending)
		}
	}
}

// CheckOneUpgrade inspects a single upgrade request, queries the systemd unit
// status, and patches the Feishu card accordingly.
func (s RuntimeMaintenanceService) CheckOneUpgrade(source string, pending *state.PendingRequest) {
	if pending == nil {
		return
	}
	var payload UpgradePendingPayload
	if err := json.Unmarshal([]byte(pending.PayloadJSON), &payload); err != nil {
		slog.Warn("upgrade check: bad payload", "request_id", pending.ID, "error", err)
		s.deps.Repository.UpdatePending(pending.ID, func(req *state.PendingRequest) { req.Status = state.PendingRequestStatusResolved.String() })
		return
	}
	unitName := strings.TrimSpace(payload.UnitName)
	if unitName == "" {
		slog.Warn("upgrade check: missing unit name", "request_id", pending.ID)
		s.deps.Repository.UpdatePending(pending.ID, func(req *state.PendingRequest) { req.Status = state.PendingRequestStatusResolved.String() })
		return
	}

	st, err := daemon.QueryUpgradeUnitStatus(unitName)
	if err != nil {
		slog.Debug("upgrade check: query failed", "unit", unitName, "error", err)
		return // transient, retry next tick
	}
	if st == nil {
		// unit not found (collected or never existed)
		slog.Warn("upgrade check: unit not found, marking resolved", "unit", unitName, "source", source)
		s.deps.Repository.UpdatePending(pending.ID, func(req *state.PendingRequest) { req.Status = state.PendingRequestStatusResolved.String() })
		return
	}
	if st.ActiveState == "active" || st.ActiveState == "activating" {
		return // still running
	}

	// Unit has exited — patch card and clean up
	s.deps.Repository.UpdatePending(pending.ID, func(req *state.PendingRequest) { req.Status = state.PendingRequestStatusResolved.String() })
	daemon.CleanupUpgradeUnit(unitName)

	sessionKey := strings.TrimSpace(pending.SessionKey)
	if sessionKey == "" {
		sessionKey = payload.ChatID
	}
	feishuMsgID := strings.TrimSpace(pending.FeishuMsgID)
	if feishuMsgID == "" {
		feishuMsgID = payload.FeishuMsgID
	}
	if feishuMsgID == "" {
		slog.Warn("upgrade check: no feishu msg id to patch", "unit", unitName, "request_id", pending.ID)
		return
	}

	feishuClient := s.deps.Client
	var card map[string]any
	if st.Result == "success" {
		slog.Info("upgrade unit succeeded", "unit", unitName, "source", source)
		body := "升级已完成，服务已重启。"
		card = feishuClient.SimpleStatusCard("升级成功", "green", s.deps.MenuBody("menu.upgrade", body), []feishu.Button{
			{Text: feishu.MenuBackButtonText, Type: "default", Value: map[string]any{"action": "menu.group.system", "session_key": sessionKey}},
		})
	} else {
		errMsg := ExtractUpgradeErrorFromJournal(st.JournalTail)
		slog.Warn("upgrade unit failed", "unit", unitName, "result", st.Result, "error", errMsg, "source", source)
		body := "升级失败。"
		if errMsg != "" {
			body += "\n\n错误: " + errMsg
		}
		card = feishuClient.SimpleStatusCard("升级失败", "red", s.deps.MenuBody("menu.upgrade", body), []feishu.Button{
			{Text: feishu.MenuBackButtonText, Type: "default", Value: map[string]any{"action": "menu.group.system", "session_key": sessionKey}},
		})
	}

	ctx, cancel := context.WithTimeout(s.context(), 10*time.Second)
	defer cancel()
	if err := feishuClient.PatchCard(ctx, feishuMsgID, card); err != nil {
		slog.Error("upgrade check: patch card failed", "unit", unitName, "msg_id", feishuMsgID, "error", err)
	}
}

// ---------------------------------------------------------------------------
// Pure helpers
// ---------------------------------------------------------------------------

// ExtractUpgradeErrorFromJournal scans the journal tail for an error line,
// falling back to the last non-empty line.
func ExtractUpgradeErrorFromJournal(journal string) string {
	lines := strings.Split(journal, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if strings.Contains(line, "error") || strings.Contains(line, "Error") || strings.Contains(line, "failed") || strings.Contains(line, "mismatch") {
			return line
		}
	}
	// fallback: last non-empty line
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.TrimSpace(lines[i]) != "" {
			return strings.TrimSpace(lines[i])
		}
	}
	return ""
}
