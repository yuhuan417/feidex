// Package threadmenu provides the thread/session menu service extracted from
// the app god package. It handles /thread and /session command routing, menu
// rendering, interrupt/append commands, and card-action completers.
package threadmenu

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	appcore "feidex/internal/app/appcore"
	appconvbackend "feidex/internal/app/convbackend"
	appworkspace "feidex/internal/app/workspace"
	appthreadview "feidex/internal/app/threadview"
	appsessionctx "feidex/internal/app/sessionctx"
	backendcaps "feidex/internal/app/backendcaps"
	appruntime "feidex/internal/app/runtime"
	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	ThreadCommandUsage        = "/thread | /thread list [all] | /thread new | /thread fork | /thread resume THREAD_ID | /thread sandbox [MODE] | /thread policy [POLICY]"
	ClaudeSessionCommandUsage = "/session | /session list [all] | /session new | /session fork | /session resume SESSION_ID | /session permissions [MODE|inherit]"
)

// ---------------------------------------------------------------------------
// App interface — what the service needs from the host application
// ---------------------------------------------------------------------------

// App defines the interface the thread-menu service requires from the host
// application. It embeds appcore.AppConfig so that appcore helpers like
// ConfiguredBackend, DefaultWorkspaceID, MakeSessionKey, etc. can be called
// directly.
type App interface {
	appcore.AppConfig

	// Feishu returns the Feishu bot client.
	Feishu() appcore.FeishuClient

	// ThreadMenuAppState returns the narrowed app state provider.
	ThreadMenuAppState() AppStateProvider
	// ThreadMenuConversationBackend returns the narrowed conversation backend provider.
	ThreadMenuConversationBackend() ConversationBackendProvider
	// ThreadMenuBackendRuntime returns the narrowed backend runtime provider.
	ThreadMenuBackendRuntime() BackendRuntimeProvider
	// ThreadMenuPendingQueue returns the narrowed pending queue provider.
	ThreadMenuPendingQueue() PendingQueueProvider
	// ThreadMenuWorkspaceThread returns the narrowed workspace thread provider.
	ThreadMenuWorkspaceThread() WorkspaceThreadProvider
	// ThreadMenuWorkspaceConfig returns the narrowed workspace config provider.
	ThreadMenuWorkspaceConfig() WorkspaceConfigProvider
	// ThreadMenuBackendActions returns the narrowed backend action provider (may be nil).
	ThreadMenuBackendActions() BackendActionProvider

	// SessionHasActiveWork reports whether the session has active work.
	SessionHasActiveWork(sess *state.Session) bool
	// CancelAutoRetry cancels auto-retry for a session.
	CancelAutoRetry(sessionKey string, keepUntilTerminal bool, notice string) bool
	// ReplyCommandActionResponse replies to a command message with a card action response.
	ReplyCommandActionResponse(msg *feishu.InboundMessage, resp *callback.CardActionTriggerResponse) error
	// CommandFork handles the /fork command.
	CommandFork(msg *feishu.InboundMessage, args []string) error
	// CompleteMenuCommand runs a slash command from a card action and returns the response.
	CompleteMenuCommand(action *feishu.CardAction, sessionKey, rawCommand, parentAction string) (*callback.CardActionTriggerResponse, error)
	// ActionStringValue extracts a string value from a card action's action value map.
	ActionStringValue(action *feishu.CardAction, key string) string
	// MenuCardBody formats a menu card body with breadcrumb navigation.
	MenuCardBody(action, body string) string
	// MenuCardBodyForBackend formats a menu card body with backend-specific breadcrumbs.
	MenuCardBodyForBackend(backend, action, body string) string
	// CommandLabel formats a command label with its slash command.
	CommandLabel(label, slash string) string

	// Claude permission helpers
	NormalizeRequestedClaudePermissionMode(ctx context.Context, raw string) (string, string, error)
	ApplyClaudePermissionModeToRuntime(sessionKey, mode string) error
	RenderClaudeSessionPermissionMenuCard(sessionKey string) (map[string]any, error)
	ShowClaudeSessionPermissionMenuFromApp(msg *feishu.InboundMessage) error
}

// ---------------------------------------------------------------------------
// Narrow provider interfaces
// ---------------------------------------------------------------------------

// AppStateProvider narrows app state access to the methods used by the service.
type AppStateProvider interface {
	Session(key string) *state.Session
	SaveSession(sess *state.Session) error
}

// ConversationBackendProvider narrows conversation backend access to the
// methods used by the service.
type ConversationBackendProvider interface {
	RenderThreadsCard(sessionKey string, includeAll bool) (map[string]any, error)
	InterruptActiveTurn(ctx context.Context, sessionKey string, sess *state.Session) error
	ContinueActiveTurn(sessionKey string, text string) error
	ResumeSelectedThread(sessionKey string, sess *state.Session, ws *config.Workspace, selection ThreadResumeSelection) (*ThreadBinding, error)
	ForkReplyMessage(forkedID string) string
}

// BackendRuntimeProvider narrows backend runtime access to the methods used by
// the service.
type BackendRuntimeProvider interface {
	ReconcileCompletedTurnFromFinalOutput(sessionKey string, sess *state.Session) *state.Session
}

// PendingQueueProvider narrows pending queue access to the methods used by the
// service.
type PendingQueueProvider interface {
	DiscardSessionPendingInputs(sessionKey string) int
}

// WorkspaceThreadProvider narrows workspace thread access to the methods used
// by the service.
type WorkspaceThreadProvider interface {
	StartWorkspaceThread(sessionKey string, sess *state.Session, ws *config.Workspace) (*ThreadBinding, error)
}

// WorkspaceConfigProvider narrows workspace config access to the methods used
// by the service.
type WorkspaceConfigProvider interface {
	CurrentThreadForMessage(msg *feishu.InboundMessage) (sessionKey string, sess *state.Session, ws *config.Workspace, threadID string, err error)
}

// BackendActionProvider narrows backend action access to the methods used by
// the service.
type BackendActionProvider interface {
	CompleteMenuInterrupt(action *feishu.CardAction, sessionKey, targetTurnID string) (*callback.CardActionTriggerResponse, error)
}

// ---------------------------------------------------------------------------
// Type definitions
// ---------------------------------------------------------------------------

// ThreadResumeSelection describes a thread resume selection from the UI.
type ThreadResumeSelection struct {
	ThreadID string
	Name     string
	Preview  string
	Cwd      string
}

// ThreadBinding is an alias for the workspace thread binding type.
type ThreadBinding = appworkspace.ThreadBinding

// ---------------------------------------------------------------------------
// Local helpers
// ---------------------------------------------------------------------------

// uiWarningError is a sentinel error type for UI warning messages.
type uiWarningError struct{ message string }

func (e uiWarningError) Error() string { return e.message }

func isUIWarningError(err error) bool {
	var target uiWarningError
	if errors.As(err, &target) {
		return true
	}
	return appconvbackend.IsUIWarningError(err)
}

// NewUIWarningError creates a new UI warning error.
func NewUIWarningError(message string) error {
	return uiWarningError{message: message}
}

// RawCard wraps a card map in a callback.Card for card action responses.
func RawCard(card map[string]any) *callback.Card {
	return &callback.Card{Type: "raw", Data: card}
}

// ActionSessionKey extracts the "session_key" value from a card action.
func ActionSessionKey(action *feishu.CardAction) string {
	if action == nil {
		return ""
	}
	value, _ := action.ActionValue["session_key"].(string)
	return strings.TrimSpace(value)
}

// CommandActionFromMessage builds a CardAction from an inbound message and
// optional action value overrides.
func CommandActionFromMessage(msg *feishu.InboundMessage, actionValue map[string]any) *feishu.CardAction {
	if actionValue == nil {
		actionValue = map[string]any{}
	}
	if msg == nil {
		return &feishu.CardAction{ActionValue: actionValue}
	}
	return &feishu.CardAction{
		ActionValue: actionValue,
		UserID:      strings.TrimSpace(msg.UserID),
		ChatID:      strings.TrimSpace(msg.ChatID),
		MessageID:   strings.TrimSpace(msg.MessageID),
	}
}

// ActionStringValue extracts a string value from a card action's action value map.
func ActionStringValue(action *feishu.CardAction, key string) string {
	if action == nil {
		return ""
	}
	v, _ := action.ActionValue[key].(string)
	return strings.TrimSpace(v)
}

// ThreadView var aliases — re-exported for convenience.
var (
	RenderThreadSettingValue    = appthreadview.RenderThreadSettingValue
	CurrentThreadLabel          = appthreadview.CurrentThreadLabel
	RenderThreadButtonLabel     = appthreadview.RenderThreadButtonLabel
	RenderThreadListEntry       = appthreadview.RenderThreadListEntry
	RenderThreadListEntryBase   = appthreadview.RenderThreadListEntryBase
	ShortThreadID               = appthreadview.ShortThreadID
	FilterThreadsByWorkspaceCWD = appthreadview.FilterThreadsByWorkspaceCWD
	SameWorkspaceCWD            = appthreadview.SameWorkspaceCWD
)

// Pure helpers copied from the app package.

func primaryConversationSlash(backend string) string {
	return backendcaps.ForKind(backend).Conversation.Slash
}

func primaryConversationNoun(backend string) string {
	return backendcaps.ForKind(backend).Conversation.Noun
}

func primaryConversationSummaryLabel(backend string) string {
	return backendcaps.ForKind(backend).Conversation.SummaryLabel
}

func effectiveThreadSandboxMode(sess *state.Session, ws *config.Workspace) string {
	return appsessionctx.EffectiveSandboxMode(sess, ws)
}

func effectiveThreadApprovalPolicy(sess *state.Session, ws *config.Workspace) string {
	return appsessionctx.EffectiveApprovalPolicy(sess, ws)
}

func sessionHasInFlightSubmission(sess *state.Session) bool {
	return appsessionctx.HasInFlightSubmission(sess)
}

func normalizeClaudePermissionModeValue(value string) string {
	switch strings.TrimSpace(value) {
	case "", "default":
		return string(appruntime.ClaudePermissionModeDefault)
	case string(appruntime.ClaudePermissionModeAcceptEdits):
		return string(appruntime.ClaudePermissionModeAcceptEdits)
	case string(appruntime.ClaudePermissionModeBypass):
		return string(appruntime.ClaudePermissionModeBypass)
	case string(appruntime.ClaudePermissionModePlan):
		return string(appruntime.ClaudePermissionModePlan)
	default:
		return strings.TrimSpace(value)
	}
}

func normalizeClaudePermissionOverrideValue(raw string) (string, bool) {
	switch strings.TrimSpace(raw) {
	case "", "inherit", "follow", "workspace", "global":
		return "", true
	default:
		return "", false
	}
}

func effectiveClaudePermissionMode(sess *state.Session, ws *config.Workspace, cfg config.ClaudeConfig) string {
	if sess != nil && strings.TrimSpace(sess.ActiveClaudePermissionMode) != "" {
		return normalizeClaudePermissionModeValue(sess.ActiveClaudePermissionMode)
	}
	if ws != nil && strings.TrimSpace(ws.ClaudePermissionMode) != "" {
		return normalizeClaudePermissionModeValue(ws.ClaudePermissionMode)
	}
	return normalizeClaudePermissionModeValue(cfg.PermissionMode)
}

func workspaceSandboxOptions() []appworkspace.SettingOption {
	return appworkspace.SandboxOptions()
}

func workspaceApprovalPolicyOptions() []appworkspace.SettingOption {
	return appworkspace.ApprovalPolicyOptions()
}

// ---------------------------------------------------------------------------
// Service — manages thread/session menu actions
// ---------------------------------------------------------------------------

// Service manages thread/session menu actions for a single app instance.
type Service struct {
	app App
}

// NewService creates a new thread-menu service bound to the given app.
func NewService(app App) *Service {
	return &Service{app: app}
}

// ---------------------------------------------------------------------------
// Thread listing and creation
// ---------------------------------------------------------------------------

// StartFreshThread creates a new workspace thread for the session.
func (s *Service) StartFreshThread(sessionKey, userID, chatID, chatType string) (int, *ThreadBinding, error) {
	if s.app == nil || s.app.Store() == nil {
		return 0, nil, fmt.Errorf("store not initialized")
	}
	appState := s.app.ThreadMenuAppState()
	defaultWorkspaceID := appcore.DefaultWorkspaceID(s.app)
	sess := appState.Session(sessionKey)
	if sess != nil && s.app.SessionHasActiveWork(sess) {
		return 0, nil, fmt.Errorf("当前任务仍在运行，请先等待结束或中断")
	}
	if sess == nil {
		sess = &state.Session{
			Key:         sessionKey,
			WorkspaceID: defaultWorkspaceID,
			ChatID:      chatID,
			ChatType:    chatType,
			OwnerUserID: userID,
		}
	}
	if strings.TrimSpace(sess.WorkspaceID) == "" {
		sess.WorkspaceID = defaultWorkspaceID
	}
	discarded := s.app.ThreadMenuPendingQueue().DiscardSessionPendingInputs(sessionKey)
	sess = appState.Session(sessionKey)
	if sess == nil {
		sess = &state.Session{
			Key:         sessionKey,
			WorkspaceID: defaultWorkspaceID,
			ChatID:      chatID,
			ChatType:    chatType,
			OwnerUserID: userID,
		}
	}
	if strings.TrimSpace(sess.OwnerUserID) == "" {
		sess.OwnerUserID = userID
	}
	if strings.TrimSpace(sess.ChatID) == "" {
		sess.ChatID = chatID
	}
	if strings.TrimSpace(sess.ChatType) == "" {
		sess.ChatType = chatType
	}
	workspaceID := appcore.FirstNonEmpty(strings.TrimSpace(sess.WorkspaceID), defaultWorkspaceID)
	ws := config.FindWorkspace(s.app.Config(), workspaceID)
	if ws == nil {
		return discarded, nil, fmt.Errorf("workspace %q not found", workspaceID)
	}
	binding, err := s.app.ThreadMenuWorkspaceThread().StartWorkspaceThread(sessionKey, sess, ws)
	if err != nil {
		return discarded, nil, err
	}
	return discarded, binding, nil
}

// CommandThreadsNew handles /thread new or /session new.
func (s *Service) CommandThreadsNew(msg *feishu.InboundMessage) error {
	sessionKey := appcore.MakeSessionKey(s.app, msg)
	discarded, binding, err := s.StartFreshThread(sessionKey, msg.UserID, msg.ChatID, msg.ChatType)
	if err != nil {
		return err
	}
	backend := appcore.ConfiguredBackend(s.app)
	noun := primaryConversationNoun(backend)
	reply := "已创建新" + noun + "并切换过去。"
	if binding != nil && strings.TrimSpace(binding.ThreadID) != "" {
		reply += " " + primaryConversationSummaryLabel(backend) + ": `" + binding.ThreadID + "`。"
	}
	if discarded > 0 {
		reply += fmt.Sprintf(" 已丢弃 %d 条排队或暂存输入。", discarded)
	}
	return s.app.Feishu().ReplyText(context.Background(), msg.MessageID, reply, appcore.ReplyInThreadEnabled(s.app, msg.ChatType))
}

// CommandThreads handles /thread list or /session list.
func (s *Service) CommandThreads(msg *feishu.InboundMessage, includeAll bool) error {
	card, err := s.app.ThreadMenuConversationBackend().RenderThreadsCard(appcore.MakeSessionKey(s.app, msg), includeAll)
	if err != nil {
		return err
	}
	_, err = s.app.Feishu().ReplyCard(context.Background(), msg.MessageID, card, appcore.ReplyInThreadEnabled(s.app, msg.ChatType))
	return err
}

// CommandThread handles the /thread command with subcommands.
func (s *Service) CommandThread(msg *feishu.InboundMessage, args []string) error {
	if len(args) == 0 {
		return s.CommandThreads(msg, false)
	}
	sessionKey := appcore.MakeSessionKey(s.app, msg)
	switch strings.TrimSpace(args[0]) {
	case "list":
		includeAll := false
		if len(args) > 2 {
			return fmt.Errorf("usage: %s", ThreadCommandUsage)
		}
		if len(args) == 2 {
			if strings.TrimSpace(args[1]) != "all" {
				return fmt.Errorf("usage: %s", ThreadCommandUsage)
			}
			includeAll = true
		}
		return s.CommandThreads(msg, includeAll)
	case "new":
		if len(args) != 1 {
			return fmt.Errorf("usage: /thread new")
		}
		return s.CommandThreadsNew(msg)
	case "fork":
		if len(args) != 1 {
			return fmt.Errorf("usage: /thread fork")
		}
		return s.app.CommandFork(msg, nil)
	case "resume":
		if len(args) != 2 {
			return fmt.Errorf("usage: /thread resume THREAD_ID")
		}
		resp, err := s.CompleteThreadResume(CommandActionFromMessage(msg, nil), sessionKey, strings.TrimSpace(args[1]))
		if err != nil {
			return err
		}
		return s.app.ReplyCommandActionResponse(msg, resp)
	case "sandbox":
		if len(args) == 1 {
			return s.ShowThreadSandboxMenu(msg)
		}
		if len(args) != 2 {
			return fmt.Errorf("usage: /thread sandbox [MODE]")
		}
		_, _, _, threadID, err := s.app.ThreadMenuWorkspaceConfig().CurrentThreadForMessage(msg)
		if err != nil {
			return err
		}
		resp, err := s.CompleteThreadSandboxSet(CommandActionFromMessage(msg, nil), sessionKey, threadID, strings.TrimSpace(args[1]))
		if err != nil {
			return err
		}
		return s.app.ReplyCommandActionResponse(msg, resp)
	case "policy":
		if len(args) == 1 {
			return s.ShowThreadPolicyMenu(msg)
		}
		if len(args) != 2 {
			return fmt.Errorf("usage: /thread policy [POLICY]")
		}
		_, _, _, threadID, err := s.app.ThreadMenuWorkspaceConfig().CurrentThreadForMessage(msg)
		if err != nil {
			return err
		}
		resp, err := s.CompleteThreadPolicySet(CommandActionFromMessage(msg, nil), sessionKey, threadID, strings.TrimSpace(args[1]))
		if err != nil {
			return err
		}
		return s.app.ReplyCommandActionResponse(msg, resp)
	default:
		return fmt.Errorf("usage: %s", ThreadCommandUsage)
	}
}

// CommandSession handles the /session command with subcommands.
func (s *Service) CommandSession(msg *feishu.InboundMessage, args []string) error {
	if len(args) == 0 {
		return s.CommandThreads(msg, false)
	}
	sessionKey := appcore.MakeSessionKey(s.app, msg)
	switch strings.TrimSpace(args[0]) {
	case "list":
		includeAll := false
		if len(args) > 2 {
			return fmt.Errorf("usage: %s", ClaudeSessionCommandUsage)
		}
		if len(args) == 2 {
			if strings.TrimSpace(args[1]) != "all" {
				return fmt.Errorf("usage: %s", ClaudeSessionCommandUsage)
			}
			includeAll = true
		}
		return s.CommandThreads(msg, includeAll)
	case "new":
		if len(args) != 1 {
			return fmt.Errorf("usage: /session new")
		}
		return s.CommandThreadsNew(msg)
	case "fork":
		if len(args) != 1 {
			return fmt.Errorf("usage: /session fork")
		}
		return s.app.CommandFork(msg, nil)
	case "resume":
		if len(args) != 2 {
			return fmt.Errorf("usage: /session resume SESSION_ID")
		}
		resp, err := s.CompleteThreadResume(CommandActionFromMessage(msg, nil), sessionKey, strings.TrimSpace(args[1]))
		if err != nil {
			return err
		}
		return s.app.ReplyCommandActionResponse(msg, resp)
	case "permissions":
		if len(args) == 1 {
			return s.app.ShowClaudeSessionPermissionMenuFromApp(msg)
		}
		if len(args) != 2 {
			return fmt.Errorf("usage: /session permissions [MODE|inherit]")
		}
		_, _, _, threadID, err := s.app.ThreadMenuWorkspaceConfig().CurrentThreadForMessage(msg)
		if err != nil {
			return err
		}
		resp, err := s.CompleteClaudeSessionPermissionModeSet(CommandActionFromMessage(msg, nil), sessionKey, threadID, strings.TrimSpace(args[1]))
		if err != nil {
			return err
		}
		return s.app.ReplyCommandActionResponse(msg, resp)
	default:
		return fmt.Errorf("usage: %s", ClaudeSessionCommandUsage)
	}
}

// ---------------------------------------------------------------------------
// Interrupt and append
// ---------------------------------------------------------------------------

// CommandInterrupt handles /stop — interrupts the active turn.
func (s *Service) CommandInterrupt(msg *feishu.InboundMessage) error {
	sessionKey := appcore.MakeSessionKey(s.app, msg)
	discarded := s.app.ThreadMenuPendingQueue().DiscardSessionPendingInputs(sessionKey)
	sess := s.app.ThreadMenuAppState().Session(sessionKey)
	if runtime := s.app.ThreadMenuBackendRuntime(); runtime != nil {
		sess = runtime.ReconcileCompletedTurnFromFinalOutput(sessionKey, sess)
	}
	if sess == nil {
		sess = s.app.ThreadMenuAppState().Session(sessionKey)
	}
	canceledRetry := s.app.CancelAutoRetry(sessionKey, sess != nil && sess.ActiveTurnID != "" && sess.ActiveThreadID != "", "已停止当前 session 的自动重试。")
	if sess == nil || sess.ActiveTurnID == "" || sess.ActiveThreadID == "" {
		if canceledRetry {
			reply := "已停止当前 session 的自动重试。"
			if discarded > 0 {
				reply += fmt.Sprintf(" 已清空 %d 条排队或暂存输入。", discarded)
			}
			return s.app.Feishu().ReplyText(context.Background(), msg.MessageID, reply, appcore.ReplyInThreadEnabled(s.app, msg.ChatType))
		}
		if discarded > 0 {
			return s.app.Feishu().ReplyText(context.Background(), msg.MessageID, fmt.Sprintf("已清空 %d 条排队或暂存输入。", discarded), appcore.ReplyInThreadEnabled(s.app, msg.ChatType))
		}
		return fmt.Errorf("当前没有运行中的任务")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := s.app.ThreadMenuConversationBackend().InterruptActiveTurn(ctx, sessionKey, sess); err != nil {
		return err
	}
	reply := "已请求中断当前任务。"
	if discarded > 0 {
		reply += fmt.Sprintf(" 已清空 %d 条排队或暂存输入。", discarded)
	}
	if canceledRetry {
		reply += " 当前 session 的自动重试也已停止。"
	}
	return s.app.Feishu().ReplyText(context.Background(), msg.MessageID, reply, appcore.ReplyInThreadEnabled(s.app, msg.ChatType))
}

// CommandAppend handles appending text to the active turn.
func (s *Service) CommandAppend(msg *feishu.InboundMessage, text string) error {
	sessionKey := appcore.MakeSessionKey(s.app, msg)
	sess := s.app.ThreadMenuAppState().Session(sessionKey)
	if sess == nil || sess.ActiveTurnID == "" || sess.ActiveThreadID == "" {
		return fmt.Errorf("当前没有可补充的任务")
	}
	return s.app.ThreadMenuConversationBackend().ContinueActiveTurn(sessionKey, text)
}

// ---------------------------------------------------------------------------
// Menu rendering
// ---------------------------------------------------------------------------

// ShowThreadSandboxMenu shows the sandbox configuration menu.
func (s *Service) ShowThreadSandboxMenu(msg *feishu.InboundMessage) error {
	card, err := s.RenderThreadSandboxMenuCard(appcore.MakeSessionKey(s.app, msg))
	if err != nil {
		return err
	}
	_, err = s.app.Feishu().ReplyCard(context.Background(), msg.MessageID, card, appcore.ReplyInThreadEnabled(s.app, msg.ChatType))
	return err
}

// RenderThreadSandboxMenuCard renders the sandbox configuration menu card.
func (s *Service) RenderThreadSandboxMenuCard(sessionKey string) (map[string]any, error) {
	sess := s.app.ThreadMenuAppState().Session(sessionKey)
	workspaceID := appcore.DefaultWorkspaceID(s.app)
	if sess != nil && strings.TrimSpace(sess.WorkspaceID) != "" {
		workspaceID = sess.WorkspaceID
	}
	ws := config.FindWorkspace(s.app.Config(), workspaceID)
	if sess == nil || strings.TrimSpace(sess.ActiveThreadID) == "" {
		return nil, fmt.Errorf("当前没有活动线程")
	}
	threadID := strings.TrimSpace(sess.ActiveThreadID)
	current := effectiveThreadSandboxMode(sess, ws)
	body := "配置当前 thread 默认 sandbox。\n\nthread: `" + threadID + "`\n当前值: `" + current + "`"
	buttons := make([]feishu.Button, 0, len(workspaceSandboxOptions())+1)
	for _, opt := range workspaceSandboxOptions() {
		btnType := "default"
		label := opt.Label
		if opt.Value == current {
			btnType = "primary"
			label = "当前 · " + label
		}
		buttons = append(buttons, feishu.Button{
			Text: label,
			Type: btnType,
			Value: map[string]any{
				"action":       "thread.sandbox.set",
				"session_key":  sessionKey,
				"thread_id":    threadID,
				"sandbox_mode": opt.Value,
			},
		})
	}
	buttons = append(buttons, feishu.Button{
		Text: s.app.CommandLabel("返回 thread", "/thread"),
		Type: "default",
		Value: map[string]any{
			"action":      "menu.thread",
			"session_key": sessionKey,
		},
	})
	return s.app.Feishu().SimpleStatusCard("配置 Thread Sandbox", "blue", s.app.MenuCardBody("thread.sandbox.menu", body), buttons), nil
}

// ShowThreadPolicyMenu shows the policy configuration menu.
func (s *Service) ShowThreadPolicyMenu(msg *feishu.InboundMessage) error {
	card, err := s.RenderThreadPolicyMenuCard(appcore.MakeSessionKey(s.app, msg))
	if err != nil {
		return err
	}
	_, err = s.app.Feishu().ReplyCard(context.Background(), msg.MessageID, card, appcore.ReplyInThreadEnabled(s.app, msg.ChatType))
	return err
}

// RenderThreadPolicyMenuCard renders the policy configuration menu card.
func (s *Service) RenderThreadPolicyMenuCard(sessionKey string) (map[string]any, error) {
	sess := s.app.ThreadMenuAppState().Session(sessionKey)
	workspaceID := appcore.DefaultWorkspaceID(s.app)
	if sess != nil && strings.TrimSpace(sess.WorkspaceID) != "" {
		workspaceID = sess.WorkspaceID
	}
	ws := config.FindWorkspace(s.app.Config(), workspaceID)
	if sess == nil || strings.TrimSpace(sess.ActiveThreadID) == "" {
		return nil, fmt.Errorf("当前没有活动线程")
	}
	threadID := strings.TrimSpace(sess.ActiveThreadID)
	current := effectiveThreadApprovalPolicy(sess, ws)
	body := "配置当前 thread 默认 approval policy。\n\nthread: `" + threadID + "`\n当前值: `" + current + "`"
	buttons := make([]feishu.Button, 0, len(workspaceApprovalPolicyOptions())+1)
	for _, opt := range workspaceApprovalPolicyOptions() {
		btnType := "default"
		label := opt.Label
		if opt.Value == current {
			btnType = "primary"
			label = "当前 · " + label
		}
		buttons = append(buttons, feishu.Button{
			Text: label,
			Type: btnType,
			Value: map[string]any{
				"action":          "thread.policy.set",
				"session_key":     sessionKey,
				"thread_id":       threadID,
				"approval_policy": opt.Value,
			},
		})
	}
	buttons = append(buttons, feishu.Button{
		Text: s.app.CommandLabel("返回 thread", "/thread"),
		Type: "default",
		Value: map[string]any{
			"action":      "menu.thread",
			"session_key": sessionKey,
		},
	})
	return s.app.Feishu().SimpleStatusCard("配置 Thread Policy", "blue", s.app.MenuCardBody("thread.policy.menu", body), buttons), nil
}

// ---------------------------------------------------------------------------
// Card action completers (from thread_feature_actions.go)
// ---------------------------------------------------------------------------

// CompleteMenuThread handles the "menu.thread" card action.
func (s *Service) CompleteMenuThread(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return s.app.CompleteMenuCommand(action, sessionKey, primaryConversationSlash(appcore.ConfiguredBackend(s.app)), "menu.root")
}

// CompleteMenuNew handles the "menu.thread.new" card action.
func (s *Service) CompleteMenuNew(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return s.app.CompleteMenuCommand(action, sessionKey, primaryConversationSlash(appcore.ConfiguredBackend(s.app))+" new", "menu.thread")
}

// CompleteMenuInterrupt handles the "menu.interrupt" card action.
func (s *Service) CompleteMenuInterrupt(action *feishu.CardAction, sessionKey, targetTurnID string) (*callback.CardActionTriggerResponse, error) {
	if strings.TrimSpace(targetTurnID) != "" {
		if sess := s.app.ThreadMenuAppState().Session(sessionKey); sess != nil && strings.TrimSpace(sess.ActiveTurnID) != "" && strings.TrimSpace(sess.ActiveTurnID) != strings.TrimSpace(targetTurnID) {
			return &callback.CardActionTriggerResponse{
				Toast: &callback.Toast{Type: "warning", Content: "这个任务已经结束或已切换到其他任务"},
			}, nil
		}
	}
	if actions := s.app.ThreadMenuBackendActions(); actions != nil {
		return actions.CompleteMenuInterrupt(action, sessionKey, targetTurnID)
	}
	return s.app.CompleteMenuCommand(action, sessionKey, "/stop", ActionStringValue(action, "parent_action"))
}

// CompleteThreadSandboxMenu handles the "thread.sandbox.menu" card action.
func (s *Service) CompleteThreadSandboxMenu(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return s.app.CompleteMenuCommand(action, sessionKey, "/thread sandbox", "menu.thread")
}

// CompleteThreadPolicyMenu handles the "thread.policy.menu" card action.
func (s *Service) CompleteThreadPolicyMenu(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return s.app.CompleteMenuCommand(action, sessionKey, "/thread policy", "menu.thread")
}

// CompleteClaudeSessionPermissionMenu handles the session permission menu card action.
func (s *Service) CompleteClaudeSessionPermissionMenu(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return s.app.CompleteMenuCommand(action, sessionKey, "/session permissions", "menu.thread")
}

// CompleteThreadSandboxSet handles the "thread.sandbox.set" card action.
func (s *Service) CompleteThreadSandboxSet(action *feishu.CardAction, sessionKey, threadID, sandboxMode string) (*callback.CardActionTriggerResponse, error) {
	appState := s.app.ThreadMenuAppState()
	valid := false
	for _, opt := range workspaceSandboxOptions() {
		if opt.Value == sandboxMode {
			valid = true
			break
		}
	}
	if !valid {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: "不支持的 sandbox"}}, nil
	}
	sess := appState.Session(sessionKey)
	if sess == nil || strings.TrimSpace(sess.ActiveThreadID) == "" || strings.TrimSpace(sess.ActiveThreadID) != strings.TrimSpace(threadID) {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "当前 thread 已失效"}}, nil
	}
	sess.ActiveThreadSandboxMode = sandboxMode
	if err := appState.SaveSession(sess); err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: err.Error()}}, nil
	}
	card, err := s.RenderThreadSandboxMenuCard(sessionKey)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已更新 thread sandbox"},
		Card:  RawCard(card),
	}, nil
}

// CompleteThreadPolicySet handles the "thread.policy.set" card action.
func (s *Service) CompleteThreadPolicySet(action *feishu.CardAction, sessionKey, threadID, approvalPolicy string) (*callback.CardActionTriggerResponse, error) {
	appState := s.app.ThreadMenuAppState()
	valid := false
	for _, opt := range workspaceApprovalPolicyOptions() {
		if opt.Value == approvalPolicy {
			valid = true
			break
		}
	}
	if !valid {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: "不支持的 policy"}}, nil
	}
	sess := appState.Session(sessionKey)
	if sess == nil || strings.TrimSpace(sess.ActiveThreadID) == "" || strings.TrimSpace(sess.ActiveThreadID) != strings.TrimSpace(threadID) {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "当前 thread 已失效"}}, nil
	}
	sess.ActiveThreadApprovalPolicy = approvalPolicy
	if err := appState.SaveSession(sess); err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: err.Error()}}, nil
	}
	card, err := s.RenderThreadPolicyMenuCard(sessionKey)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已更新 thread policy"},
		Card:  RawCard(card),
	}, nil
}

// CompleteThreadResume handles resuming a previously created thread.
func (s *Service) CompleteThreadResume(action *feishu.CardAction, sessionKey, threadID string) (*callback.CardActionTriggerResponse, error) {
	appState := s.app.ThreadMenuAppState()
	sess := appState.Session(sessionKey)
	if sess == nil {
		sess = &state.Session{Key: sessionKey, OwnerUserID: action.UserID, ChatID: action.ChatID}
	}
	if sessionHasInFlightSubmission(sess) {
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "warning", Content: "当前任务仍在运行，请先等待结束或中断"},
		}, nil
	}
	if strings.TrimSpace(sess.OwnerUserID) == "" {
		sess.OwnerUserID = action.UserID
	}
	if strings.TrimSpace(sess.ChatID) == "" {
		sess.ChatID = action.ChatID
	}
	if strings.TrimSpace(sess.WorkspaceID) == "" {
		sess.WorkspaceID = appcore.DefaultWorkspaceID(s.app)
	}
	ws := config.FindWorkspace(s.app.Config(), sess.WorkspaceID)
	if ws == nil {
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "error", Content: "workspace not found"},
		}, nil
	}
	selectedName, _ := action.ActionValue["thread_name"].(string)
	selectedPreview, _ := action.ActionValue["thread_preview"].(string)
	selectedCWD, _ := action.ActionValue["thread_cwd"].(string)
	if _, err := s.app.ThreadMenuConversationBackend().ResumeSelectedThread(sessionKey, sess, ws, ThreadResumeSelection{
		ThreadID: threadID,
		Name:     selectedName,
		Preview:  selectedPreview,
		Cwd:      selectedCWD,
	}); err != nil {
		toastType := "error"
		if isUIWarningError(err) {
			toastType = "warning"
		}
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: toastType, Content: err.Error()}}, nil
	}
	includeAll, _ := action.ActionValue["include_all"].(bool)
	card, err := s.app.ThreadMenuConversationBackend().RenderThreadsCard(sessionKey, includeAll)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "success", Content: "已恢复" + primaryConversationNoun(appcore.ConfiguredBackend(s.app))}}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已恢复" + primaryConversationNoun(appcore.ConfiguredBackend(s.app))},
		Card:  RawCard(card),
	}, nil
}

// ---------------------------------------------------------------------------
// Claude session permission mode (from claude_permission_config.go)
// ---------------------------------------------------------------------------

// CompleteClaudeSessionPermissionModeSet handles setting the Claude session permission mode.
func (s *Service) CompleteClaudeSessionPermissionModeSet(action *feishu.CardAction, sessionKey, threadID, rawMode string) (*callback.CardActionTriggerResponse, error) {
	appState := s.app.ThreadMenuAppState()
	sess := appState.Session(sessionKey)
	if sess == nil || strings.TrimSpace(sess.ActiveThreadID) == "" || strings.TrimSpace(sess.ActiveThreadID) != strings.TrimSpace(threadID) {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "当前会话已失效"}}, nil
	}
	mode := ""
	warning := ""
	if override, ok := normalizeClaudePermissionOverrideValue(rawMode); ok {
		mode = override
	} else {
		var err error
		mode, warning, err = s.app.NormalizeRequestedClaudePermissionMode(context.Background(), rawMode)
		if err != nil {
			return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
		}
	}
	sess.ActiveClaudePermissionMode = mode
	if err := appState.SaveSession(sess); err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: err.Error()}}, nil
	}
	effective := effectiveClaudePermissionMode(sess, config.FindWorkspace(s.app.Config(), sess.WorkspaceID), s.app.Config().Claude)
	if err := s.app.ApplyClaudePermissionModeToRuntime(sessionKey, effective); err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: err.Error()}}, nil
	}
	card, err := s.app.RenderClaudeSessionPermissionMenuCard(sessionKey)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	content := "已更新 Claude 会话权限模式"
	if warning != "" {
		content = warning
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: content},
		Card:  RawCard(card),
	}, nil
}

// ---------------------------------------------------------------------------
// Convenience: expose CurrentThreadLabel for callers in the parent package
// ---------------------------------------------------------------------------

// SessionCurrentThreadLabel returns the current thread label for a session.
func SessionCurrentThreadLabel(sess *state.Session) string {
	if sess == nil {
		return "-"
	}
	return appthreadview.CurrentThreadLabel(sess.ActiveThreadName, sess.ActiveThreadPreview, sess.ActiveThreadID)
}
