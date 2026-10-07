// Package debugviewcmd provides the debug log, usage display, and download
// command services extracted from the app god package.
package debugviewcmd

import (
	"context"
	"encoding/json"
	"errors"
	"feidex/internal/application/fileshare"
	"feidex/internal/application/runtimeconfig"
	"feidex/internal/application/workspace"
	domainbackend "feidex/internal/domain/backend"
	"feidex/internal/domain/conversation"
	domainturn "feidex/internal/domain/turn"
	domainworkspace "feidex/internal/domain/workspace"
	"feidex/internal/textutil"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"

	appdebugview "feidex/internal/adapter/feishu/debugview"
	appdelivery "feidex/internal/adapter/feishu/delivery"
	menuutil "feidex/internal/adapter/feishu/menuutil"
	appthreadview "feidex/internal/adapter/feishu/threadview"
	turnitem "feidex/internal/adapter/feishu/turnitem"
	apppathpick "feidex/internal/adapter/filesystem/pathpicker"
	appusageview "feidex/internal/application/presentation/usageview"
	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/logcontrol"
	turnbinding "feidex/internal/runtime/turnbinding"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

// ---------------------------------------------------------------------------
// Narrow interfaces — what the services need from the host application
// ---------------------------------------------------------------------------

type Outbound interface {
	ReplyCard(ctx context.Context, messageID string, card map[string]any, inThread bool) (string, error)
	ReplyText(ctx context.Context, messageID string, text string, inThread bool) error
	PatchCard(ctx context.Context, messageID string, card map[string]any) error
}

type ArtifactSharer interface {
	ShareLocalFile(ctx context.Context, req feishu.SharedFileRequest) (feishu.SharedFileResult, error)
}

type CardRenderer interface {
	SimpleStatusCard(title, color, body string, buttons []feishu.Button) map[string]any
}

// StateProvider narrows app state access to the session and pending
// request operations used by these services.
type StateProvider interface {
	Session(sessionKey string) *conversation.Session
}

// RuntimeStateProvider narrows runtime state access to the turn binding
// tracker operations used by the usage service.
type RuntimeStateProvider interface {
	TurnBindingTracker() TurnBindingTracker
	CurrentThreadUsage(threadID string) (domainturn.ThreadTokenUsage, bool)
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
	RenderUsageBody(sess *conversation.Session) string
}

// WorkspaceConfigProvider narrows workspace config access.
type WorkspaceConfigProvider interface {
	CurrentWorkspaceForMessage(msg *feishu.InboundMessage) (sessionKey string, sess *conversation.Session, ws *config.Workspace)
}

// WorkspaceRenderProvider narrows workspace render access.
type WorkspaceRenderProvider interface {
	RenderPathPickerCard(requestID string, payload domainworkspace.PathPickerPayload) (map[string]any, error)
}

// ---------------------------------------------------------------------------
// Dependencies interface — what the services require from the host application
// ---------------------------------------------------------------------------

// Dependencies is the explicit debug/usage capability set assembled by the
// composition root. The service does not depend on the application root.
type Dependencies struct {
	ConfigProvider interface {
		Config() *config.Config
		ConfigMu() *sync.RWMutex
		Backend() string
		FrontendID() string
		FrontendConfigIndex() int
		Store() *state.Store
		WorkspaceSelection() workspace.SelectionService
	}
	Outbound                          Outbound
	FileSharing                       *fileshare.Service
	CardRenderer                      CardRenderer
	StateProvider                     StateProvider
	RuntimeStateProvider              RuntimeStateProvider
	RuntimeConfigRepository           runtimeconfig.Repository
	ConversationBackendProvider       ConversationBackendProvider
	WorkspaceConfigProvider           WorkspaceConfigProvider
	WorkspaceRenderProvider           WorkspaceRenderProvider
	MakeSessionKeyFn                  func(*feishu.InboundMessage) string
	ReplyInThreadEnabledFn            func(string) bool
	CompleteMenuCommandFn             func(*feishu.CardAction, string, string, string) (*callback.CardActionTriggerResponse, error)
	MenuCardBodyFn                    func(string, string) string
	MenuBreadcrumbLabelsFn            func(string) []string
	CommandLabelFn                    func(string, string) string
	CurrentThreadLabelFn              func(*conversation.Session) string
	PrimaryConversationMissingLabelFn func(string) string
	DefaultWorkspaceIDFn              func() string
	ConfigPathFn                      func() string
	ContextProvider                   interface{ Context() context.Context }
}

func (d Dependencies) Config() *config.Config {
	if d.ConfigProvider == nil {
		return nil
	}
	return d.ConfigProvider.Config()
}
func (d Dependencies) ConfigMu() *sync.RWMutex {
	if d.ConfigProvider == nil {
		return nil
	}
	return d.ConfigProvider.ConfigMu()
}
func (d Dependencies) Backend() string {
	if d.ConfigProvider == nil {
		return ""
	}
	return d.ConfigProvider.Backend()
}
func (d Dependencies) FrontendID() string {
	if d.ConfigProvider == nil {
		return ""
	}
	return d.ConfigProvider.FrontendID()
}
func (d Dependencies) FrontendConfigIndex() int {
	if d.ConfigProvider == nil {
		return -1
	}
	return d.ConfigProvider.FrontendConfigIndex()
}
func (d Dependencies) Store() *state.Store {
	if d.ConfigProvider == nil {
		return nil
	}
	return d.ConfigProvider.Store()
}
func (d Dependencies) Context() context.Context {
	if d.ContextProvider != nil {
		if ctx := d.ContextProvider.Context(); ctx != nil {
			return ctx
		}
	}
	return context.Background()
}
func (d Dependencies) DebugOutbound() Outbound                      { return d.Outbound }
func (d Dependencies) DebugRenderer() CardRenderer                  { return d.CardRenderer }
func (d Dependencies) DebugAppState() StateProvider                 { return d.StateProvider }
func (d Dependencies) DebugRuntimeState() RuntimeStateProvider      { return d.RuntimeStateProvider }
func (d Dependencies) DebugRuntimeConfig() runtimeconfig.Repository { return d.RuntimeConfigRepository }
func (d Dependencies) DebugConversationBackend() ConversationBackendProvider {
	return d.ConversationBackendProvider
}
func (d Dependencies) DebugWorkspaceConfig() WorkspaceConfigProvider {
	return d.WorkspaceConfigProvider
}
func (d Dependencies) DebugWorkspaceRender() WorkspaceRenderProvider {
	return d.WorkspaceRenderProvider
}
func (d Dependencies) DebugMakeSessionKey(m *feishu.InboundMessage) string {
	if d.MakeSessionKeyFn == nil {
		return ""
	}
	return d.MakeSessionKeyFn(m)
}
func (d Dependencies) DebugReplyInThreadEnabled(v string) bool {
	return d.ReplyInThreadEnabledFn != nil && d.ReplyInThreadEnabledFn(v)
}
func (d Dependencies) DebugCompleteMenuCommand(a *feishu.CardAction, s, r, p string) (*callback.CardActionTriggerResponse, error) {
	if d.CompleteMenuCommandFn == nil {
		return nil, fmt.Errorf("menu command unavailable")
	}
	return d.CompleteMenuCommandFn(a, s, r, p)
}
func (d Dependencies) DebugMenuCardBody(a, b string) string {
	if d.MenuCardBodyFn == nil {
		return b
	}
	return d.MenuCardBodyFn(a, b)
}
func (d Dependencies) DebugMenuBreadcrumbLabels(a string) []string {
	if d.MenuBreadcrumbLabelsFn == nil {
		return nil
	}
	return d.MenuBreadcrumbLabelsFn(a)
}
func (d Dependencies) DebugCommandLabel(a, b string) string {
	if d.CommandLabelFn == nil {
		return a
	}
	return d.CommandLabelFn(a, b)
}
func (d Dependencies) DebugCurrentThreadLabel(s *conversation.Session) string {
	if d.CurrentThreadLabelFn == nil {
		return ""
	}
	return d.CurrentThreadLabelFn(s)
}
func (d Dependencies) DebugPrimaryConversationMissingLabel(v string) string {
	if d.PrimaryConversationMissingLabelFn == nil {
		return ""
	}
	return d.PrimaryConversationMissingLabelFn(v)
}
func (d Dependencies) DebugDefaultWorkspaceID() string {
	if d.DefaultWorkspaceIDFn == nil {
		return "default"
	}
	return d.DefaultWorkspaceIDFn()
}
func (d Dependencies) DebugConfigPath() string {
	if d.ConfigPathFn == nil {
		return ""
	}
	return d.ConfigPathFn()
}

// ---------------------------------------------------------------------------
// Exported type aliases for sub-package types
// ---------------------------------------------------------------------------

// PathPickerPayload aliases domainworkspace.PathPickerPayload.
type PathPickerPayload = domainworkspace.PathPickerPayload

// PathPickerModeFile is the file picker mode constant.
const PathPickerModeFile = domainworkspace.PathPickerModeFile

// PathPickerStyleDropdown is the dropdown picker style constant.
const PathPickerStyleDropdown = domainworkspace.PathPickerStyleDropdown

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
	return textutil.FirstNonEmpty(values...)
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
var SessionCurrentThreadLabel = func(sess *conversation.Session) string {
	if sess == nil {
		return "-"
	}
	return appthreadview.CurrentThreadLabel(sess.ActiveThreadName, sess.ActiveThreadPreview, sess.ActiveThreadID)
}

// ConfiguredBackend returns the configured backend name.
func ConfiguredBackend(source Dependencies) string {
	if source.ConfigProvider == nil {
		return ""
	}
	if value := strings.TrimSpace(source.ConfigProvider.Backend()); value != "" {
		return domainbackend.NormalizeBackend(value)
	}
	cfg := source.ConfigProvider.Config()
	if cfg == nil {
		return ""
	}
	index := source.ConfigProvider.FrontendConfigIndex()
	if index >= 0 && index < len(cfg.Frontends) {
		return domainbackend.NormalizeBackend(cfg.Frontends[index].FeishuConfig.Backend)
	}
	return domainbackend.NormalizeBackend(cfg.Feishu.Backend)
}

// DebugAllowFrom returns the debug allow list from config.
func DebugAllowFrom(source Dependencies) []string {
	if source.ConfigProvider == nil || source.ConfigProvider.Config() == nil {
		return nil
	}
	cfg := source.ConfigProvider.Config()
	index := source.ConfigProvider.FrontendConfigIndex()
	if index >= 0 && index < len(cfg.Frontends) {
		return cfg.Frontends[index].FeishuConfig.DebugAllowFrom
	}
	return cfg.Feishu.DebugAllowFrom
}

// ---------------------------------------------------------------------------
// DebugService — manages /debug command actions
// ---------------------------------------------------------------------------

// DebugService provides debug log viewing and access control.
type DebugService struct {
	app Dependencies
}

// NewDebugService creates a new debug service bound to the given app.
func NewDebugService(app Dependencies) DebugService {
	return DebugService{app: app}
}

func (s DebugService) CommandDownload(msg *feishu.InboundMessage, args []string) error {
	return CommandDownload(s.app, msg, args)
}

// SetRuntimeDebug sets the runtime debug log level and updates config.
func (s DebugService) SetRuntimeDebug(enabled bool) string {
	level := logcontrol.SetDebug(enabled)
	if repository := s.app.DebugRuntimeConfig(); repository != nil {
		if err := (runtimeconfig.Service{Repository: repository}).SetLogLevel(level); err != nil {
			slog.Warn("persist runtime debug level failed", "error", err)
		}
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
		_, err := s.app.DebugOutbound().ReplyCard(s.app.Context(), msg.MessageID, card, s.app.DebugReplyInThreadEnabled(msg.ChatType))
		return err
	}
	enabled, err := DesiredDebugEnabled(args)
	if err != nil {
		return err
	}
	level := NewDebugService(s.app).SetRuntimeDebug(enabled)
	return s.app.DebugOutbound().ReplyText(s.app.Context(), msg.MessageID, "服务端 slog 日志级别已切换为 `"+level+"`。", s.app.DebugReplyInThreadEnabled(msg.ChatType))
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
		_, err := s.app.DebugOutbound().ReplyCard(s.app.Context(), msg.MessageID, card, s.app.DebugReplyInThreadEnabled(msg.ChatType))
		return err
	}
	card := NewDebugService(s.app).RenderDebugLogsCard(s.app.DebugMakeSessionKey(msg))
	_, err := s.app.DebugOutbound().ReplyCard(s.app.Context(), msg.MessageID, card, s.app.DebugReplyInThreadEnabled(msg.ChatType))
	return err
}

// CompleteMenuDebugLogs handles the debug logs menu card action.
func (s DebugService) CompleteMenuDebugLogs(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return s.app.DebugCompleteMenuCommand(action, sessionKey, "/debug logs", "menu.group.system")
}

// DebugAccessAllowed checks if the given user is allowed to use debug.
func (s DebugService) DebugAccessAllowed(userID string) bool {
	if s.app.ConfigProvider == nil || s.app.Config() == nil {
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
	return s.app.DebugRenderer().SimpleStatusCard("Debug 权限不足", "orange", strings.Join(bodyLines, "\n"), []feishu.Button{
		{Text: feishu.MenuBackButtonText, Type: "default", Value: map[string]any{"action": "menu.group.system", "session_key": sessionKey}},
	})
}

// RenderDebugLogsCard renders the debug logs card.
func (s DebugService) RenderDebugLogsCard(sessionKey string) map[string]any {
	lines := logcontrol.RecentLines(DebugLogRecentLimit)
	var logBlock map[string]any
	summaryLines := []string{
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
	elements := []map[string]any{DebugLogPlainTextBlock(strings.Join(summaryLines, "\n"), true)}
	if logBlock != nil {
		elements = append(elements, logBlock)
	}

	return menuutil.MarkdownPageCard{
		Node: "menu.debug.logs", SessionKey: sessionKey, Title: "调试日志", Color: "blue",
		Elements: elements,
		Buttons: []feishu.Button{{
			Text:  s.app.DebugCommandLabel("刷新日志", "/debug logs"),
			Type:  "default",
			Value: map[string]any{"action": "menu.debug.logs", "session_key": sessionKey},
		}},
	}.Render()
}

// ---------------------------------------------------------------------------
// UsageService — manages /usage command actions
// ---------------------------------------------------------------------------

// UsageService provides token usage display and tracking.
type UsageService struct {
	app Dependencies
}

// NewUsageService creates a new usage service bound to the given app.
func NewUsageService(app Dependencies) UsageService {
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
func (s UsageService) RecordClaudeThreadUsage(threadID string, usage domainturn.ClaudeThreadUsage) {
	if s.app.ConfigProvider == nil {
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
	if percentage, ok := claudeContextUsagePercent(usage); ok {
		snapshot.ContextUsagePercent = percentage
		snapshot.HasContextUsagePercent = true
	}

	tracker := s.app.DebugRuntimeState().TurnBindingTracker()
	tracker.SetClaudeThreadUsage(threadID, snapshot)
}

func claudeContextUsagePercent(usage domainturn.ClaudeThreadUsage) (float64, bool) {
	if usage.ContextWindow <= 0 {
		return 0, false
	}
	used := usage.InputTokens + usage.CacheReadTokens + usage.CacheCreationTokens
	if used < 0 {
		used = 0
	}
	percentage := float64(used) * 100 / float64(usage.ContextWindow)
	if percentage > 100 {
		percentage = 100
	}
	return percentage, true
}

// CurrentClaudeThreadUsage returns the current Claude thread usage.
func (s UsageService) CurrentClaudeThreadUsage(threadID string) (turnbindingClaudeSnapshot, bool) {
	if s.app.ConfigProvider == nil {
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
	_, err := s.app.DebugOutbound().ReplyCard(s.app.Context(), msg.MessageID, card, s.app.DebugReplyInThreadEnabled(msg.ChatType))
	return err
}

// RenderUsageCard renders the usage card.
func (s UsageService) RenderUsageCard(sessionKey string) map[string]any {
	sess := s.app.DebugAppState().Session(sessionKey)
	body := s.app.DebugPrimaryConversationMissingLabel(ConfiguredBackend(s.app)) + "。"
	if sess != nil && strings.TrimSpace(sess.ActiveThreadID) != "" {
		body = s.app.DebugConversationBackend().RenderUsageBody(sess)
	}
	return menuutil.PageCard{
		Node: "menu.usage", SessionKey: sessionKey, Title: "Token Usage", Color: "blue",
		Body: body,
	}.Render()
}

// RenderClaudeUsageBody renders the Claude usage body.
func (s UsageService) RenderClaudeUsageBody(sess *conversation.Session) string {
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
func (s UsageService) RenderCodexUsageBody(sess *conversation.Session) string {
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
func CommandDownload(a Dependencies, msg *feishu.InboundMessage, args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("usage: /download")
	}
	if msg == nil {
		return nil
	}
	sessionKey, _, ws := a.DebugWorkspaceConfig().CurrentWorkspaceForMessage(msg)
	payload, err := NewDownloadPathPickerPayload(ws)
	if err != nil {
		return err
	}
	pending, err := a.FileSharing.Open(sessionKey, msg.UserID, payload)
	if err != nil {
		return err
	}
	card, err := a.DebugWorkspaceRender().RenderPathPickerCard(pending.ID, payload)
	if err != nil {
		return err
	}
	msgID, err := a.DebugOutbound().ReplyCard(a.Context(), msg.MessageID, card, a.DebugReplyInThreadEnabled(msg.ChatType))
	if err != nil {
		return err
	}
	return a.FileSharing.Forms.SaveDraft(pending.ID, nil, "", 0, msgID)
}

// CompleteMenuDownload handles the download menu card action.
func CompleteMenuDownload(a Dependencies, action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
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

func (s DebugService) CompleteDownloadFileConfirm(action *feishu.CardAction, pending *state.PendingRequest, payload PathPickerPayload, selectedPath string) (*callback.CardActionTriggerResponse, error) {
	return CompleteDownloadFileConfirm(s.app, action, pending, payload, selectedPath)
}

// CompleteDownloadFileConfirm handles the download file confirm card action.
func CompleteDownloadFileConfirm(a Dependencies, action *feishu.CardAction, pending *state.PendingRequest, payload PathPickerPayload, selectedPath string) (*callback.CardActionTriggerResponse, error) {
	if pending == nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "下载请求已过期"}}, nil
	}
	if state.NormalizePendingRequestStatus(pending.Status) == state.PendingRequestStatusProcessing {
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "info", Content: "正在生成下载链接，请稍候"},
			Card:  RawCard(RenderDownloadPreparingCard(a, selectedPath, payload.RootPath)),
		}, nil
	}
	execution, err := a.FileSharing.Confirm(pending.ID, action.UserID, action.ChatID, action.MessageID, selectedPath, payload)
	if err != nil {
		kind := "warning"
		if errors.Is(err, fileshare.ErrProcessing) {
			kind = "info"
		}
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: kind, Content: err.Error()}}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "info", Content: "正在生成下载链接"},
		Card:  RawCard(RenderDownloadPreparingCard(a, selectedPath, execution.WorkspaceCWD)),
	}, nil
}

// DownloadPresentation renders the persisted application outcome.
type DownloadPresentation struct{ Dependencies Dependencies }

func (p DownloadPresentation) Completed(input fileshare.Execution, result fileshare.Result, cause error) {
	a := p.Dependencies
	if strings.TrimSpace(input.MessageID) == "" {
		return
	}
	var card map[string]any
	if cause != nil {
		slog.Warn("download share failed", "request_id", input.ID, "error", cause)
		var err error
		card, err = a.DebugWorkspaceRender().RenderPathPickerCard(input.ID, input.Payload)
		if err != nil {
			card = RenderDownloadFailedCard(a, input.Request.LocalPath, input.WorkspaceCWD, cause.Error())
		}
	} else {
		card = RenderDownloadReadyCard(a, input.Request.LocalPath, input.WorkspaceCWD, feishu.SharedFileResult{FileName: result.FileName, URL: result.URL, SizeBytes: result.SizeBytes})
	}
	_ = a.DebugOutbound().PatchCard(a.Context(), input.MessageID, card)
}

// RenderDownloadPreparingCard renders the download preparing card.
func RenderDownloadPreparingCard(a Dependencies, selectedPath, workspaceCWD string) map[string]any {
	displayPath := RenderDownloadDisplayPath(selectedPath, workspaceCWD)
	lines := []string{
		"正在生成文件下载链接（飞书云盘中转）。",
		"",
		"文件: `" + filepath.Base(selectedPath) + "`",
		"路径: `" + displayPath + "`",
		"",
		"请稍候，这张卡片会自动刷新。",
	}
	return a.DebugRenderer().SimpleStatusCard("文件下载", "blue", strings.Join(lines, "\n"), nil)
}

// RenderDownloadReadyCard renders the download ready card.
func RenderDownloadReadyCard(a Dependencies, selectedPath, workspaceCWD string, result feishu.SharedFileResult) map[string]any {
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
	return a.DebugRenderer().SimpleStatusCard("文件下载", "green", strings.Join(lines, "\n"), nil)
}

// RenderDownloadFailedCard renders the download failed card.
func RenderDownloadFailedCard(a Dependencies, selectedPath, workspaceCWD, errText string) map[string]any {
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
	return a.DebugRenderer().SimpleStatusCard("文件下载", "orange", strings.Join(lines, "\n"), nil)
}

func (d Dependencies) WorkspaceSelection() workspace.SelectionService {
	if d.ConfigProvider == nil {
		return workspace.SelectionService{}
	}
	return d.ConfigProvider.WorkspaceSelection()
}
