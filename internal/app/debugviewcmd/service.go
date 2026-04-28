// Package debugviewcmd provides the debug log, usage display, and download
// command services extracted from the app god package.
package debugviewcmd

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	appcore "feidex/internal/app/appcore"
	apputil "feidex/internal/app/apputil"
	appcards "feidex/internal/app/cards"
	appclauderuntime "feidex/internal/app/clauderuntime"
	appdebugview "feidex/internal/app/debugview"
	appdelivery "feidex/internal/app/delivery"
	apppathpick "feidex/internal/app/pathpick"
	appthreadmenu "feidex/internal/app/threadmenu"
	turnbinding "feidex/internal/app/turnbinding"
	turnitem "feidex/internal/app/turnitem"
	appusageview "feidex/internal/app/usageview"
	appworkspace "feidex/internal/app/workspace"
	"feidex/internal/claudecli"
	"feidex/internal/codexrpc"
	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/logcontrol"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

// ---------------------------------------------------------------------------
// Narrow interfaces — what the services need from the host application
// ---------------------------------------------------------------------------

// FeishuClient is the narrow interface for the Feishu bot client methods
// used by these services.
type FeishuClient interface {
	ReplyCard(ctx context.Context, messageID string, card map[string]any, inThread bool) (string, error)
	ReplyText(ctx context.Context, messageID string, text string, inThread bool) error
	PatchCard(ctx context.Context, messageID string, card map[string]any) error
	ShareLocalFile(ctx context.Context, req feishu.SharedFileRequest) (feishu.SharedFileResult, error)
	SimpleStatusCard(title, color, body string, buttons []feishu.Button) map[string]any
}

// AppStateProvider narrows app state access to the session and pending
// request operations used by these services.
type AppStateProvider interface {
	Session(sessionKey string) *state.Session
	NextLocalID(prefix string) (string, error)
	SavePending(req *state.PendingRequest) error
	UpdatePending(id string, mutate func(*state.PendingRequest)) error
}

// RuntimeStateProvider narrows runtime state access to the turn binding
// tracker operations used by the usage service.
type RuntimeStateProvider interface {
	TurnBindingTracker() TurnBindingTracker
	CurrentThreadUsage(threadID string) (codexrpc.ThreadTokenUsage, bool)
}

// TurnBindingTracker is the narrow interface for thread usage tracking.
type TurnBindingTracker interface {
	SetClaudeThreadUsage(threadID string, snapshot turnbindingSnapshot)
	GetClaudeThreadUsage(threadID string) (turnbindingSnapshot, bool)
}

// turnbindingSnapshot is a local alias to avoid importing the full turnbinding
// package in the interface signature.
type turnbindingSnapshot = turnbindingClaudeSnapshot

// turnbindingClaudeSnapshot aliases the turnbinding.ClaudeThreadUsageSnapshot type.
type turnbindingClaudeSnapshot = turnbinding.ClaudeThreadUsageSnapshot

// ConversationBackendProvider narrows conversation backend access to the
// usage body rendering method.
type ConversationBackendProvider interface {
	RenderUsageBody(sess *state.Session) string
}

// WorkspaceConfigProvider narrows workspace config access.
type WorkspaceConfigProvider interface {
	CurrentWorkspaceForMessage(msg *feishu.InboundMessage) (sessionKey string, sess *state.Session, ws *config.Workspace)
}

// WorkspaceRenderProvider narrows workspace render access.
type WorkspaceRenderProvider interface {
	RenderPathPickerCard(requestID string, payload appworkspace.PathPickerPayload) (map[string]any, error)
}

// ---------------------------------------------------------------------------
// App interface — what the services require from the host application
// ---------------------------------------------------------------------------

// App defines the interface the debugviewcmd services require from the host
// application. It embeds appcore.AppConfig so that appcore helpers like
// ConfiguredBackend, DebugAllowFrom, etc. can be called directly.
type App interface {
	appcore.AppConfig

	// DebugFeishu returns the Feishu bot client.
	DebugFeishu() FeishuClient
	// DebugAppState returns the narrowed app state provider.
	DebugAppState() AppStateProvider
	// DebugRuntimeState returns the narrowed runtime state provider.
	DebugRuntimeState() RuntimeStateProvider
	// DebugConversationBackend returns the narrowed conversation backend
	// provider for usage rendering.
	DebugConversationBackend() ConversationBackendProvider
	// DebugWorkspaceConfig returns the narrowed workspace config provider.
	DebugWorkspaceConfig() WorkspaceConfigProvider
	// DebugWorkspaceRender returns the narrowed workspace render provider.
	DebugWorkspaceRender() WorkspaceRenderProvider
	// DebugMakeSessionKey builds a session key from an inbound message.
	DebugMakeSessionKey(msg *feishu.InboundMessage) string
	// DebugReplyInThreadEnabled reports whether reply-in-thread is enabled
	// for the given chat type.
	DebugReplyInThreadEnabled(chatType string) bool
	// DebugCompleteMenuCommand dispatches a menu command from a card action.
	DebugCompleteMenuCommand(action *feishu.CardAction, sessionKey, rawCommand, parentAction string) (*callback.CardActionTriggerResponse, error)
	// DebugMenuCardBody formats a menu card body with breadcrumb navigation.
	DebugMenuCardBody(action, body string) string
	// DebugMenuBreadcrumbLabels returns breadcrumb labels for a menu action.
	DebugMenuBreadcrumbLabels(action string) []string
	// DebugCommandLabel formats a command label with its slash command.
	DebugCommandLabel(label, slash string) string
	// DebugCurrentThreadLabel returns the display label for the active thread.
	DebugCurrentThreadLabel(sess *state.Session) string
	// DebugPrimaryConversationMissingLabel returns the label for a missing
	// conversation for the given backend.
	DebugPrimaryConversationMissingLabel(backend string) string
	// DebugDefaultWorkspaceID returns the default workspace ID.
	DebugDefaultWorkspaceID() string
	// DebugConfigPath returns the config file path.
	DebugConfigPath() string
}

// ---------------------------------------------------------------------------
// Exported type aliases for sub-package types
// ---------------------------------------------------------------------------

// PathPickerPayload aliases appworkspace.PathPickerPayload.
type PathPickerPayload = appworkspace.PathPickerPayload

// PathPickerModeFile is the file picker mode constant.
const PathPickerModeFile = appworkspace.PathPickerModeFile

// PathPickerStyleDropdown is the dropdown picker style constant.
const PathPickerStyleDropdown = appworkspace.PathPickerStyleDropdown

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	// DebugLogRecentLimit is the max number of recent log lines to show.
	DebugLogRecentLimit = 200
	// DebugLogCardMaxChars is the max characters for the debug log card.
	DebugLogCardMaxChars = 12000
	// DebugLogPreviewAction is the menu action for the debug log preview.
	DebugLogPreviewAction = "menu.debug.logs"
)

// DebugAccessUnauthorizedText is the text shown when debug access is denied.
const DebugAccessUnauthorizedText = "当前用户无权使用 debug 功能"

const downloadFilePendingKind = "download_file"

const DownloadFilePendingKind = downloadFilePendingKind

// ---------------------------------------------------------------------------
// Helper functions (local to avoid importing app/)
// ---------------------------------------------------------------------------

// RawCard wraps a card map for CardActionTriggerResponse.
func RawCard(card map[string]any) *callback.Card {
	return &callback.Card{Type: "raw", Data: card}
}

// MustJSON marshals v to JSON string, ignoring errors.
func MustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// FirstNonEmpty returns the first non-empty string.
func FirstNonEmpty(values ...string) string {
	return apputil.FirstNonEmpty(values...)
}

// MarkdownCodeBlockWithLang formats a code block with language tag.
func MarkdownCodeBlockWithLang(lang, s string) string {
	return turnitem.MarkdownCodeBlockWithLang(lang, s)
}

// ---------------------------------------------------------------------------
// Exported variables re-exporting sub-package functions
// ---------------------------------------------------------------------------

// RuntimeLogLevelText returns the current runtime log level text.
var RuntimeLogLevelText = appdebugview.RuntimeLogLevelText

// DesiredDebugEnabled parses args to determine if debug should be enabled.
var DesiredDebugEnabled = appdebugview.DesiredDebugEnabled

// DebugUserAllowed checks if a user is in the debug allow list.
var DebugUserAllowed = appdebugview.DebugUserAllowed

// ActionUserID extracts the user ID from a card action.
var ActionUserID = appdebugview.ActionUserID

// RenderRuntimeLogLevelValue renders the runtime log level value.
var RenderRuntimeLogLevelValue = appdebugview.RenderRuntimeLogLevelValue

// CompactDebugLogText compacts debug log text to fit within maxChars.
var CompactDebugLogText = appdebugview.CompactDebugLogText

// DebugLogPlainTextBlock creates a plain text block for debug logs.
var DebugLogPlainTextBlock = appdebugview.DebugLogPlainTextBlock

// FormatUsageInt formats an integer usage value.
var FormatUsageInt = appusageview.FormatUsageInt

// FormatUsageRatio formats a usage ratio.
var FormatUsageRatio = appusageview.FormatUsageRatio

// FormatUsageCost formats a cost value.
var FormatUsageCost = appusageview.FormatUsageCost

// FormatTurnUsageLine formats a turn usage line.
var FormatTurnUsageLine = appusageview.FormatTurnUsageLine

// FormatTurnElapsedLine formats a turn elapsed time line.
var FormatTurnElapsedLine = appusageview.FormatTurnElapsedLine

// FormatContextLeftLine formats a context left line.
var FormatContextLeftLine = appusageview.FormatContextLeftLine

// FormatContextUsedLine formats a context used line.
var FormatContextUsedLine = appusageview.FormatContextUsedLine

// RenderThreadUsageCardBody renders thread usage card body.
var RenderThreadUsageCardBody = appusageview.RenderThreadUsageCardBody

// RenderDownloadDisplayPath renders the display path for a download.
var RenderDownloadDisplayPath = appdelivery.RenderDownloadDisplayPath

// FormatDownloadSize formats a download file size.
var FormatDownloadSize = appdelivery.FormatDownloadSize

// ResolvePathPickerRoot resolves the root path for a path picker.
var ResolvePathPickerRoot = apppathpick.ResolvePathPickerRoot

// SessionCurrentThreadLabel returns the display label for the active thread.
var SessionCurrentThreadLabel = appthreadmenu.SessionCurrentThreadLabel

// TurnContextUsagePercent calculates the context window usage percentage.
var TurnContextUsagePercent = appclauderuntime.TurnContextUsagePercent

// ConfiguredBackend returns the configured backend name.
var ConfiguredBackend = appcore.ConfiguredBackend

// DebugAllowFrom returns the debug allow list from config.
var DebugAllowFrom = appcore.DebugAllowFrom

// ---------------------------------------------------------------------------
// DebugService — manages /debug command actions
// ---------------------------------------------------------------------------

// DebugService provides debug log viewing and access control.
type DebugService struct {
	app App
}

// NewDebugService creates a new debug service bound to the given app.
func NewDebugService(app App) DebugService {
	return DebugService{app: app}
}

// SetRuntimeDebug sets the runtime debug log level and updates config.
func (s DebugService) SetRuntimeDebug(enabled bool) string {
	level := logcontrol.SetDebug(enabled)
	if s.app != nil && s.app.Config() != nil {
		s.app.ConfigMu().Lock()
		s.app.Config().Log.Level = level
		s.app.ConfigMu().Unlock()
	}
	return level
}

// CommandDebug handles the /debug command.
func (s DebugService) CommandDebug(msg *feishu.InboundMessage, args []string) error {
	if msg == nil {
		return nil
	}
	if len(args) > 0 && strings.TrimSpace(args[0]) == "logs" {
		return NewDebugService(s.app).CommandDebugLogs(msg, args[1:])
	}
	if !NewDebugService(s.app).DebugAccessAllowed(msg.UserID) {
		card := NewDebugService(s.app).RenderDebugAccessDeniedCard(s.app.DebugMakeSessionKey(msg), msg.UserID)
		_, err := s.app.DebugFeishu().ReplyCard(context.Background(), msg.MessageID, card, s.app.DebugReplyInThreadEnabled(msg.ChatType))
		return err
	}
	enabled, err := DesiredDebugEnabled(args)
	if err != nil {
		return err
	}
	level := NewDebugService(s.app).SetRuntimeDebug(enabled)
	return s.app.DebugFeishu().ReplyText(context.Background(), msg.MessageID, "服务端 slog 日志级别已切换为 `"+level+"`。", s.app.DebugReplyInThreadEnabled(msg.ChatType))
}

// CompleteMenuDebug handles the debug menu card action.
func (s DebugService) CompleteMenuDebug(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return s.app.DebugCompleteMenuCommand(action, sessionKey, "/debug", "menu.group.system")
}

// CommandDebugLogs handles the /debug logs command.
func (s DebugService) CommandDebugLogs(msg *feishu.InboundMessage, args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("usage: /debug logs")
	}
	if msg == nil {
		return nil
	}
	if !NewDebugService(s.app).DebugAccessAllowed(msg.UserID) {
		card := NewDebugService(s.app).RenderDebugAccessDeniedCard(s.app.DebugMakeSessionKey(msg), msg.UserID)
		_, err := s.app.DebugFeishu().ReplyCard(context.Background(), msg.MessageID, card, s.app.DebugReplyInThreadEnabled(msg.ChatType))
		return err
	}
	card := NewDebugService(s.app).RenderDebugLogsCard(s.app.DebugMakeSessionKey(msg))
	_, err := s.app.DebugFeishu().ReplyCard(context.Background(), msg.MessageID, card, s.app.DebugReplyInThreadEnabled(msg.ChatType))
	return err
}

// CompleteMenuDebugLogs handles the debug logs menu card action.
func (s DebugService) CompleteMenuDebugLogs(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return s.app.DebugCompleteMenuCommand(action, sessionKey, "/debug logs", "menu.group.system")
}

// DebugAccessAllowed checks if the given user is allowed to use debug.
func (s DebugService) DebugAccessAllowed(userID string) bool {
	if s.app == nil || s.app.Config() == nil {
		return false
	}
	return DebugUserAllowed(userID, DebugAllowFrom(s.app))
}

// RenderDebugAccessDeniedCard renders the debug access denied card.
func (s DebugService) RenderDebugAccessDeniedCard(sessionKey, userID string) map[string]any {
	bodyLines := []string{
		"当前用户无权使用 debug 功能。",
		"",
		"当前用户 OpenID: `" + FirstNonEmpty(strings.TrimSpace(userID), "-") + "`",
	}
	if cfgPath := strings.TrimSpace(s.app.DebugConfigPath()); cfgPath != "" {
		bodyLines = append(bodyLines, "配置文件: `"+cfgPath+"`")
	}
	bodyLines = append(bodyLines,
		"",
		"请把该用户加入 `[feishu].debug_allow_from`，然后重启服务。",
		"",
		"示例配置：",
		MarkdownCodeBlockWithLang("toml", strings.Join([]string{
			"[feishu]",
			"debug_allow_from = [\"" + FirstNonEmpty(strings.TrimSpace(userID), "ou_xxx") + "\"]",
		}, "\n")),
	)
	return s.app.DebugFeishu().SimpleStatusCard("Debug 权限不足", "orange", strings.Join(bodyLines, "\n"), []feishu.Button{
		{Text: "返回上一级", Type: "default", Value: map[string]any{"action": "menu.group.system", "session_key": sessionKey}},
	})
}

// RenderDebugLogsCard renders the debug logs card.
func (s DebugService) RenderDebugLogsCard(sessionKey string) map[string]any {
	lines := logcontrol.RecentLines(DebugLogRecentLimit)
	card := appcards.NewMarkdownBodyCard("调试日志", "blue")
	var logBlock map[string]any
	summaryLines := []string{
		"当前位置：" + strings.Join(s.app.DebugMenuBreadcrumbLabels(DebugLogPreviewAction), " / "),
		"",
		fmt.Sprintf("最近服务端 slog 日志（内存缓冲，最新 %d 条）。", DebugLogRecentLimit),
		"当前日志级别: " + RuntimeLogLevelText(),
	}
	if len(lines) == 0 {
		summaryLines = append(summaryLines, "", "当前还没有可展示的日志。")
	} else {
		logText, shown, truncated := CompactDebugLogText(lines, DebugLogCardMaxChars)
		switch {
		case truncated:
			summaryLines = append(summaryLines, fmt.Sprintf("显示范围: 最新 %d/%d 条", shown, len(lines)))
			summaryLines = append(summaryLines, "说明: 卡片内容过长，已截断为最新尾部。")
		default:
			summaryLines = append(summaryLines, fmt.Sprintf("显示范围: %d 条", shown))
		}
		logBlock = DebugLogPlainTextBlock(logText, false)
	}
	appcards.AppendMarkdownBodyCardElement(card, DebugLogPlainTextBlock(strings.Join(summaryLines, "\n"), true))
	if logBlock != nil {
		appcards.AppendMarkdownBodyCardElement(card, logBlock)
	}

	buttons := []feishu.Button{
		{Text: s.app.DebugCommandLabel("刷新日志", "/debug logs"), Type: "default", Value: map[string]any{"action": "menu.debug.logs", "session_key": sessionKey}},
		{Text: "返回上一级", Type: "default", Value: map[string]any{"action": "menu.group.system", "session_key": sessionKey}},
	}
	for _, btn := range buttons {
		appcards.AppendMarkdownBodyCardElement(card, appcards.BuildMarkdownBodyCardActionElement([]feishu.Button{btn}))
	}
	return card
}

// ---------------------------------------------------------------------------
// UsageService — manages /usage command actions
// ---------------------------------------------------------------------------

// UsageService provides token usage display and tracking.
type UsageService struct {
	app App
}

// NewUsageService creates a new usage service bound to the given app.
func NewUsageService(app App) UsageService {
	return UsageService{app: app}
}

// RenderClaudeThreadUsageCardBody renders the Claude thread usage card body.
func RenderClaudeThreadUsageCardBody(threadLabel, threadID string, usage turnbindingClaudeSnapshot) string {
	totalTokens := usage.TotalInputTokens + usage.TotalCacheReadTokens + usage.TotalCacheCreationTokens + usage.TotalOutputTokens
	lines := []string{
		"当前会话: " + FirstNonEmpty(strings.TrimSpace(threadLabel), "-"),
		"session: `" + FirstNonEmpty(strings.TrimSpace(threadID), "-") + "`",
		"",
		"累计 token usage (`modelUsage`):",
		"- total: `" + FormatUsageInt(totalTokens) + "`",
		"- input: `" + FormatUsageInt(usage.TotalInputTokens) + "`",
		"- cache read: `" + FormatUsageInt(usage.TotalCacheReadTokens) + "`",
		"- cache write: `" + FormatUsageInt(usage.TotalCacheCreationTokens) + "`",
		"- output: `" + FormatUsageInt(usage.TotalOutputTokens) + "`",
		"- cost: `" + FormatUsageCost(usage.TotalCostUSD) + "`",
	}
	if usage.HasContextUsagePercent {
		lines = append(lines, "", FormatContextUsedLine(usage.ContextUsagePercent))
	}
	return strings.Join(lines, "\n")
}

// RecordClaudeThreadUsage records Claude thread usage from a turn.
func (s UsageService) RecordClaudeThreadUsage(threadID string, usage claudecli.TurnUsage) {
	if s.app == nil {
		return
	}
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return
	}
	snapshot := turnbindingClaudeSnapshot{
		TotalCostUSD:  usage.CostUSD,
		ContextWindow: int64(usage.ContextWindow),
	}
	if usage.HasCumulativeUsage {
		snapshot.TotalInputTokens = int64(usage.CumulativeInputTokens)
		snapshot.TotalOutputTokens = int64(usage.CumulativeOutputTokens)
		snapshot.TotalCacheReadTokens = int64(usage.CumulativeCacheReadTokens)
		snapshot.TotalCacheCreationTokens = int64(usage.CumulativeCacheCreationTokens)
	} else {
		snapshot.TotalInputTokens = int64(usage.InputTokens)
		snapshot.TotalOutputTokens = int64(usage.OutputTokens)
		snapshot.TotalCacheReadTokens = int64(usage.CacheReadTokens)
		snapshot.TotalCacheCreationTokens = int64(usage.CacheCreationTokens)
	}
	if percentage, ok := TurnContextUsagePercent(usage); ok {
		snapshot.ContextUsagePercent = percentage
		snapshot.HasContextUsagePercent = true
	}

	tracker := s.app.DebugRuntimeState().TurnBindingTracker()
	tracker.SetClaudeThreadUsage(threadID, snapshot)
}

// CurrentClaudeThreadUsage returns the current Claude thread usage.
func (s UsageService) CurrentClaudeThreadUsage(threadID string) (turnbindingClaudeSnapshot, bool) {
	if s.app == nil {
		return turnbindingClaudeSnapshot{}, false
	}
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return turnbindingClaudeSnapshot{}, false
	}
	tracker := s.app.DebugRuntimeState().TurnBindingTracker()
	return tracker.GetClaudeThreadUsage(threadID)
}

// CommandUsage handles the /usage command.
func (s UsageService) CommandUsage(msg *feishu.InboundMessage, args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("usage: /usage")
	}
	card := NewUsageService(s.app).RenderUsageCard(s.app.DebugMakeSessionKey(msg))
	_, err := s.app.DebugFeishu().ReplyCard(context.Background(), msg.MessageID, card, s.app.DebugReplyInThreadEnabled(msg.ChatType))
	return err
}

// RenderUsageCard renders the usage card.
func (s UsageService) RenderUsageCard(sessionKey string) map[string]any {
	sess := s.app.DebugAppState().Session(sessionKey)
	body := s.app.DebugPrimaryConversationMissingLabel(ConfiguredBackend(s.app)) + "。"
	if sess != nil && strings.TrimSpace(sess.ActiveThreadID) != "" {
		body = s.app.DebugConversationBackend().RenderUsageBody(sess)
	}
	return s.app.DebugFeishu().SimpleStatusCard("Token Usage", "blue", s.app.DebugMenuCardBody("menu.usage", body), []feishu.Button{
		{Text: "返回上一级", Type: "default", Value: map[string]any{"action": "menu.tools", "session_key": sessionKey}},
	})
}

// RenderClaudeUsageBody renders the Claude usage body.
func (s UsageService) RenderClaudeUsageBody(sess *state.Session) string {
	if sess == nil || strings.TrimSpace(sess.ActiveThreadID) == "" {
		return s.app.DebugPrimaryConversationMissingLabel("claude") + "。"
	}
	body := "当前会话暂无 Claude usage 数据。"
	if usage, ok := NewUsageService(s.app).CurrentClaudeThreadUsage(sess.ActiveThreadID); ok {
		body = RenderClaudeThreadUsageCardBody(s.app.DebugCurrentThreadLabel(sess), sess.ActiveThreadID, usage)
	}
	return body
}

// RenderCodexUsageBody renders the Codex usage body.
func (s UsageService) RenderCodexUsageBody(sess *state.Session) string {
	if sess == nil || strings.TrimSpace(sess.ActiveThreadID) == "" {
		return s.app.DebugPrimaryConversationMissingLabel("codex") + "。"
	}
	body := "当前线程暂无 token usage 数据。"
	if usage, ok := s.app.DebugRuntimeState().CurrentThreadUsage(sess.ActiveThreadID); ok {
		contextLine := ""
		if usage.ModelContextWindow != nil {
			contextLine = FormatContextLeftLine(usage.Last.InputTokens, *usage.ModelContextWindow)
		}
		body = RenderThreadUsageCardBody(s.app.DebugCurrentThreadLabel(sess), sess.ActiveThreadID, usage, contextLine)
	}
	return body
}

// ---------------------------------------------------------------------------
// Download functions
// ---------------------------------------------------------------------------

// CommandDownload handles the /download command.
func CommandDownload(a App, msg *feishu.InboundMessage, args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("usage: /download")
	}
	if msg == nil {
		return nil
	}
	sessionKey, _, ws := a.DebugWorkspaceConfig().CurrentWorkspaceForMessage(msg)
	appState := a.DebugAppState()
	payload, err := NewDownloadPathPickerPayload(ws)
	if err != nil {
		return err
	}
	requestID, err := appState.NextLocalID("download")
	if err != nil {
		return err
	}
	card, err := a.DebugWorkspaceRender().RenderPathPickerCard(requestID, payload)
	if err != nil {
		return err
	}
	msgID, err := a.DebugFeishu().ReplyCard(context.Background(), msg.MessageID, card, a.DebugReplyInThreadEnabled(msg.ChatType))
	if err != nil {
		return err
	}
	return appState.SavePending(&state.PendingRequest{
		ID:          requestID,
		Kind:        downloadFilePendingKind,
		SessionKey:  sessionKey,
		OwnerUserID: msg.UserID,
		FeishuMsgID: msgID,
		PayloadJSON: MustJSON(payload),
		Status:      state.PendingRequestStatusPending.String(),
		CreatedAt:   time.Now().Unix(),
		ExpiresAt:   time.Now().Add(10 * time.Minute).Unix(),
	})
}

// CompleteMenuDownload handles the download menu card action.
func CompleteMenuDownload(a App, action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return a.DebugCompleteMenuCommand(action, sessionKey, "/download", "menu.tools")
}

// NewDownloadPathPickerPayload creates a path picker payload for download.
func NewDownloadPathPickerPayload(ws *config.Workspace) (PathPickerPayload, error) {
	root, err := ResolvePathPickerRoot(ws)
	if err != nil {
		return PathPickerPayload{}, err
	}
	return PathPickerPayload{
		Mode:        PathPickerModeFile,
		Style:       PathPickerStyleDropdown,
		RootPath:    root,
		CurrentPath: root,
	}, nil
}

// CompleteDownloadFileConfirm handles the download file confirm card action.
func CompleteDownloadFileConfirm(a App, action *feishu.CardAction, pending *state.PendingRequest, payload PathPickerPayload, selectedPath string) (*callback.CardActionTriggerResponse, error) {
	if pending == nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "下载请求已过期"}}, nil
	}
	if state.NormalizePendingRequestStatus(pending.Status) == state.PendingRequestStatusProcessing {
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "info", Content: "正在生成下载链接，请稍候"},
			Card:  RawCard(RenderDownloadPreparingCard(a, selectedPath, payload.RootPath)),
		}, nil
	}
	appState := a.DebugAppState()
	sess := appState.Session(pending.SessionKey)
	chatID := strings.TrimSpace(action.ChatID)
	userID := strings.TrimSpace(action.UserID)
	messageID := FirstNonEmpty(strings.TrimSpace(pending.FeishuMsgID), strings.TrimSpace(action.MessageID))
	workspaceCWD := strings.TrimSpace(payload.RootPath)
	if sess != nil {
		chatID = FirstNonEmpty(chatID, sess.ChatID)
		workspaceID := FirstNonEmpty(strings.TrimSpace(sess.WorkspaceID), a.DebugDefaultWorkspaceID())
		if ws := config.FindWorkspace(a.Config(), workspaceID); ws != nil {
			workspaceCWD = FirstNonEmpty(workspaceCWD, strings.TrimSpace(ws.Cwd))
		}
	}
	_ = appState.UpdatePending(pending.ID, func(req *state.PendingRequest) {
		req.Status = state.PendingRequestStatusProcessing.String()
		req.PayloadJSON = MustJSON(payload)
		if strings.TrimSpace(req.FeishuMsgID) == "" {
			req.FeishuMsgID = messageID
		}
	})
	go FinishDownloadFileShare(a, pending.ID, messageID, payload, selectedPath, workspaceCWD, feishu.SharedFileRequest{
		LocalPath: selectedPath,
		ChatID:    chatID,
		UserID:    FirstNonEmpty(userID, pending.OwnerUserID),
	})
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "info", Content: "正在生成下载链接"},
		Card:  RawCard(RenderDownloadPreparingCard(a, selectedPath, workspaceCWD)),
	}, nil
}

// FinishDownloadFileShare completes the download file sharing workflow.
func FinishDownloadFileShare(a App, requestID, messageID string, payload PathPickerPayload, selectedPath, workspaceCWD string, req feishu.SharedFileRequest) {
	appState := a.DebugAppState()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	slog.Debug("download share started",
		"request_id", requestID,
		"message_id", messageID,
		"path", selectedPath,
	)
	result, err := a.DebugFeishu().ShareLocalFile(ctx, req)
	if err != nil {
		slog.Warn("download share failed",
			"request_id", requestID,
			"message_id", messageID,
			"path", selectedPath,
			"error", err,
		)
		_ = appState.UpdatePending(requestID, func(p *state.PendingRequest) {
			p.Status = state.PendingRequestStatusPending.String()
			p.PayloadJSON = MustJSON(payload)
		})
		if strings.TrimSpace(messageID) == "" {
			return
		}
		card, renderErr := a.DebugWorkspaceRender().RenderPathPickerCard(requestID, payload)
		if renderErr != nil {
			slog.Error("download failure card render failed",
				"request_id", requestID,
				"message_id", messageID,
				"error", renderErr,
			)
			_ = a.DebugFeishu().PatchCard(context.Background(), messageID, RenderDownloadFailedCard(a, selectedPath, workspaceCWD, err.Error()))
			return
		}
		_ = a.DebugFeishu().PatchCard(context.Background(), messageID, card)
		return
	}
	slog.Debug("download share completed",
		"request_id", requestID,
		"message_id", messageID,
		"path", selectedPath,
		"url", result.URL,
	)
	_ = appState.UpdatePending(requestID, func(p *state.PendingRequest) {
		p.Status = state.PendingRequestStatusResolved.String()
		p.PayloadJSON = MustJSON(payload)
	})
	if strings.TrimSpace(messageID) == "" {
		return
	}
	_ = a.DebugFeishu().PatchCard(context.Background(), messageID, RenderDownloadReadyCard(a, selectedPath, workspaceCWD, result))
}

// RenderDownloadPreparingCard renders the download preparing card.
func RenderDownloadPreparingCard(a App, selectedPath, workspaceCWD string) map[string]any {
	displayPath := RenderDownloadDisplayPath(selectedPath, workspaceCWD)
	lines := []string{
		"正在生成文件下载链接（飞书云盘中转）。",
		"",
		"文件: `" + filepath.Base(selectedPath) + "`",
		"路径: `" + displayPath + "`",
		"",
		"请稍候，这张卡片会自动刷新。",
	}
	return a.DebugFeishu().SimpleStatusCard("文件下载", "blue", strings.Join(lines, "\n"), nil)
}

// RenderDownloadReadyCard renders the download ready card.
func RenderDownloadReadyCard(a App, selectedPath, workspaceCWD string, result feishu.SharedFileResult) map[string]any {
	displayPath := RenderDownloadDisplayPath(selectedPath, workspaceCWD)
	lines := []string{
		"已生成文件下载链接（飞书云盘中转）。",
		"",
		"文件: `" + FirstNonEmpty(strings.TrimSpace(result.FileName), filepath.Base(selectedPath)) + "`",
		"路径: `" + displayPath + "`",
	}
	if result.SizeBytes > 0 {
		lines = append(lines, "大小: `"+FormatDownloadSize(result.SizeBytes)+"`")
	}
	if url := strings.TrimSpace(result.URL); url != "" {
		lines = append(lines, "", "[点击下载]("+url+")", url)
	}
	return a.DebugFeishu().SimpleStatusCard("文件下载", "green", strings.Join(lines, "\n"), nil)
}

// RenderDownloadFailedCard renders the download failed card.
func RenderDownloadFailedCard(a App, selectedPath, workspaceCWD, errText string) map[string]any {
	displayPath := RenderDownloadDisplayPath(selectedPath, workspaceCWD)
	lines := []string{
		"生成下载链接失败。",
		"",
		"文件: `" + filepath.Base(selectedPath) + "`",
		"路径: `" + displayPath + "`",
	}
	if strings.TrimSpace(errText) != "" {
		lines = append(lines, "", "错误: "+strings.TrimSpace(errText))
	}
	return a.DebugFeishu().SimpleStatusCard("文件下载", "orange", strings.Join(lines, "\n"), nil)
}
