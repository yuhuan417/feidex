// Package maintenance provides runtime maintenance services extracted from the
// app god package. This file contains the runtime maintenance service that
// handles attachment cleanup, artifact GC, upgrade checks, and startup recovery.
package maintenance

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"feidex/internal/app/appcore"
	appattachments "feidex/internal/app/attachments"
	appfeishuwrap "feidex/internal/app/feishuwrap"
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

// FrontendCardNotificationKindFeishuPermissionIssue is the notification kind
// for Feishu permission diagnostic cards.
const FrontendCardNotificationKindFeishuPermissionIssue = "feishu_permission_issue"

// ---------------------------------------------------------------------------
// App interface — what the service needs from the host application
// ---------------------------------------------------------------------------

// App defines the interface the runtime maintenance service requires from the
// host application. It embeds appcore.AppConfig so that appcore helpers like
// FeishuConfig, ConfiguredBackend, etc. can be called directly.
type App interface {
	appcore.AppConfig

	// Feishu returns the Feishu bot client.
	Feishu() appcore.FeishuClient
	// AppState returns the narrowed app state provider.
	MaintenanceAppState() AppStateProvider
	// RuntimeState returns the narrowed runtime state provider.
	MaintenanceRuntimeState() RuntimeStateProvider
	// MenuCardBody formats a menu card body with breadcrumb navigation.
	MenuCardBody(action, body string) string
	// AppStartupReadyChatIDs returns chat IDs for sessions that belong to the
	// current frontend and are ready to receive notifications.
	AppStartupReadyChatIDs(sessions []*state.Session) []string
	// QueueFrontendCardNotification queues a card notification for later delivery.
	QueueFrontendCardNotification(note state.FrontendCardNotification)
}

// ---------------------------------------------------------------------------
// Narrow provider interfaces
// ---------------------------------------------------------------------------

// AppStateProvider narrows app state access to the methods used by the service.
type AppStateProvider interface {
	// PendingRequests returns all pending requests in the store.
	PendingRequests() []*state.PendingRequest
	// UpdatePending applies a mutation to a pending request by ID.
	UpdatePending(id string, mutate func(*state.PendingRequest)) error
	// DeleteMessageLinks removes message links matching the predicate.
	DeleteMessageLinks(match func(*state.MessageLink) bool)
	// DeletePendingRequests removes pending requests matching the predicate.
	DeletePendingRequests(match func(*state.PendingRequest) bool)
	// DeleteSubmission removes a submission by ID.
	DeleteSubmission(id string)
	// Sessions returns all sessions in the store.
	Sessions() []*state.Session
}

// RuntimeStateProvider narrows runtime state access to the methods used by
// the service for turn binding and item state cleanup.
type RuntimeStateProvider interface {
	// ClearTurnBinding removes the turn binding for the given turn ID.
	ClearTurnBinding(turnID string)
	// ClearTurnItemStates removes all turn item states for the given turn ID.
	ClearTurnItemStates(turnID string)
	// ClearPendingTurnBindingForSubmission removes pending turn bindings for the
	// given thread/submission pair.
	ClearPendingTurnBindingForSubmission(threadID, submissionID string)
}

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
type RuntimeMaintenanceService struct {
	app App
}

// NewRuntimeMaintenanceService creates a new maintenance service bound to the
// given app.
func NewRuntimeMaintenanceService(app App) RuntimeMaintenanceService {
	return RuntimeMaintenanceService{app: app}
}

// ---------------------------------------------------------------------------
// Startup: expire pending requests
// ---------------------------------------------------------------------------

// ExpirePendingRequestsOnStartup marks all pending/replied requests as expired
// so that stale state from a previous run does not leak into the new session.
func (s RuntimeMaintenanceService) ExpirePendingRequestsOnStartup() {
	store := s.app.Store()
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
	cfg := s.app.Config()
	if cfg == nil {
		return
	}
	for _, ws := range cfg.Workspaces {
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
// Submission runtime state cleanup
// ---------------------------------------------------------------------------

// CleanupSubmissionRuntimeState removes message links, pending requests, turn
// bindings, and turn item states associated with the given submission.
func (s RuntimeMaintenanceService) CleanupSubmissionRuntimeState(sub *state.Submission) {
	if sub == nil {
		return
	}
	stateProvider := s.app.MaintenanceAppState()
	runtimeProvider := s.app.MaintenanceRuntimeState()
	if stateProvider == nil || runtimeProvider == nil {
		return
	}
	submissionID := strings.TrimSpace(sub.ID)
	turnID := strings.TrimSpace(sub.TurnID)
	threadID := strings.TrimSpace(sub.ThreadID)
	stateProvider.DeleteMessageLinks(func(link *state.MessageLink) bool {
		if link == nil {
			return false
		}
		if submissionID != "" && strings.TrimSpace(link.SubmissionID) == submissionID {
			return true
		}
		if turnID != "" && strings.TrimSpace(link.TurnID) == turnID {
			return true
		}
		return false
	})
	if turnID != "" {
		stateProvider.DeletePendingRequests(func(req *state.PendingRequest) bool {
			return req != nil && strings.TrimSpace(req.TurnID) == turnID
		})
	}
	if submissionID != "" {
		stateProvider.DeleteSubmission(submissionID)
	}
	if turnID != "" {
		runtimeProvider.ClearTurnBinding(turnID)
		runtimeProvider.ClearTurnItemStates(turnID)
	}
	if submissionID != "" && threadID != "" {
		runtimeProvider.ClearPendingTurnBindingForSubmission(threadID, submissionID)
	}
}

// ---------------------------------------------------------------------------
// Drive artifact GC loop
// ---------------------------------------------------------------------------

// StartDriveArtifactGCLoop launches a background goroutine that runs drive
// artifact garbage collection on startup and then every 24 hours.
func (s RuntimeMaintenanceService) StartDriveArtifactGCLoop(ctx context.Context) {
	feishuClient := s.app.Feishu()
	if feishuClient == nil {
		return
	}
	go s.RunDriveArtifactGC("startup")
	go func() {
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
	}()
}

// RunDriveArtifactGC performs a single artifact GC pass, deleting drive files
// older than ArtifactRetention.
func (s RuntimeMaintenanceService) RunDriveArtifactGC(source string) {
	feishuClient := s.app.Feishu()
	if feishuClient == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
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
	feishuClient := s.app.Feishu()
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
	chatIDs := s.app.AppStartupReadyChatIDs(s.app.MaintenanceAppState().Sessions())
	if len(chatIDs) == 0 {
		s.app.QueueFrontendCardNotification(state.FrontendCardNotification{
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
		s.app.QueueFrontendCardNotification(state.FrontendCardNotification{
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
	feishuClient := s.app.Feishu()
	if feishuClient == nil {
		return
	}
	go s.CheckPendingUpgrades("startup")
	go func() {
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
	}()
}

// CheckPendingUpgrades scans all pending requests for "upgrading" status and
// checks each one.
func (s RuntimeMaintenanceService) CheckPendingUpgrades(source string) {
	store := s.app.Store()
	if store == nil {
		return
	}
	pendings := s.app.MaintenanceAppState().PendingRequests()
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
		s.app.MaintenanceAppState().UpdatePending(pending.ID, func(req *state.PendingRequest) { req.Status = state.PendingRequestStatusResolved.String() })
		return
	}
	unitName := strings.TrimSpace(payload.UnitName)
	if unitName == "" {
		slog.Warn("upgrade check: missing unit name", "request_id", pending.ID)
		s.app.MaintenanceAppState().UpdatePending(pending.ID, func(req *state.PendingRequest) { req.Status = state.PendingRequestStatusResolved.String() })
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
		s.app.MaintenanceAppState().UpdatePending(pending.ID, func(req *state.PendingRequest) { req.Status = state.PendingRequestStatusResolved.String() })
		return
	}
	if st.ActiveState == "active" || st.ActiveState == "activating" {
		return // still running
	}

	// Unit has exited — patch card and clean up
	s.app.MaintenanceAppState().UpdatePending(pending.ID, func(req *state.PendingRequest) { req.Status = state.PendingRequestStatusResolved.String() })
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

	feishuClient := s.app.Feishu()
	var card map[string]any
	if st.Result == "success" {
		slog.Info("upgrade unit succeeded", "unit", unitName, "source", source)
		body := "升级已完成，服务已重启。"
		card = feishuClient.SimpleStatusCard("升级成功", "green", s.app.MenuCardBody("menu.upgrade", body), []feishu.Button{
			{Text: "返回上一级", Type: "default", Value: map[string]any{"action": "menu.group.system", "session_key": sessionKey}},
		})
	} else {
		errMsg := ExtractUpgradeErrorFromJournal(st.JournalTail)
		slog.Warn("upgrade unit failed", "unit", unitName, "result", st.Result, "error", errMsg, "source", source)
		body := "升级失败。"
		if errMsg != "" {
			body += "\n\n错误: " + errMsg
		}
		card = feishuClient.SimpleStatusCard("升级失败", "red", s.app.MenuCardBody("menu.upgrade", body), []feishu.Button{
			{Text: "返回上一级", Type: "default", Value: map[string]any{"action": "menu.group.system", "session_key": sessionKey}},
		})
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
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
