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
	appbackend "feidex/internal/app/backend"
	appconvbackend "feidex/internal/app/convbackend"
	appruntime "feidex/internal/app/runtime"
	appsessionctx "feidex/internal/app/sessionctx"
	appthreadview "feidex/internal/app/threadview"
	appworkspace "feidex/internal/app/workspace"
	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	ThreadCommandUsage        = "/thread | /thread list [all] | /thread new | /thread fork | /thread resume THREAD_ID | /thread sandbox [MODE] | /thread policy [POLICY] | /thread multiagent [MODE]"
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

type effectiveSessionKeyProvider interface {
	ThreadMenuEffectiveSessionKey(sessionKey string) string
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
	// ClearActiveOperationsAfterInterrupt clears stale active operations after
	// an interrupt request. For backends where the interrupt response is
	// asynchronous (e.g. Claude), this prevents the session from getting stuck
	// in "queuing" state if the interrupt doesn't trigger a turn completion.
	ClearActiveOperationsAfterInterrupt(sessionKey string, sess *state.Session) *state.Session
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
	return appbackend.DriverForKind(backend).Conversation().PrimarySlash()
}

func primaryConversationNoun(backend string) string {
	return appbackend.DriverForKind(backend).Conversation().Noun()
}

func primaryConversationSummaryLabel(backend string) string {
	return appbackend.DriverForKind(backend).Conversation().SummaryLabel()
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

func (s *Service) effectiveSessionKey(sessionKey string) string {
	sessionKey = strings.TrimSpace(sessionKey)
	if s == nil || s.app == nil {
		return sessionKey
	}
	provider, ok := s.app.(effectiveSessionKeyProvider)
	if !ok || provider == nil {
		return sessionKey
	}
	resolved := strings.TrimSpace(provider.ThreadMenuEffectiveSessionKey(sessionKey))
	if resolved == "" {
		return sessionKey
	}
	return resolved
}

func (s *Service) messageForThreadMenu(msg *feishu.InboundMessage) (*feishu.InboundMessage, string) {
	if msg == nil {
		return nil, ""
	}
	sessionKey := appcore.MakeSessionKey(s.app, msg)
	effectiveSessionKey := s.effectiveSessionKey(sessionKey)
	if effectiveSessionKey == "" || effectiveSessionKey == sessionKey {
		return msg, sessionKey
	}
	cp := *msg
	cp.SessionKey = effectiveSessionKey
	return &cp, effectiveSessionKey
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
	msg, sessionKey := s.messageForThreadMenu(msg)
	if msg == nil {
		return nil
	}
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
	msg, sessionKey := s.messageForThreadMenu(msg)
	if msg == nil {
		return nil
	}
	card, err := s.app.ThreadMenuConversationBackend().RenderThreadsCard(sessionKey, includeAll)
	if err != nil {
		return err
	}
	_, err = s.app.Feishu().ReplyCard(context.Background(), msg.MessageID, card, appcore.ReplyInThreadEnabled(s.app, msg.ChatType))
	return err
}

// CommandThread handles the /thread command with subcommands.
func (s *Service) CommandThread(msg *feishu.InboundMessage, args []string) error {
	msg, sessionKey := s.messageForThreadMenu(msg)
	if msg == nil {
		return nil
	}
	if len(args) == 0 {
		return s.CommandThreads(msg, false)
	}
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
	case "sandbox", "policy", "multiagent":
		return appbackend.DriverForApp(s.app).Permission().HandleConversationCommand(appbackend.ConversationPermissionCommandRequest{
			Message:    msg,
			Args:       args,
			SessionKey: sessionKey,
			CurrentThread: func(msg *feishu.InboundMessage) (string, *state.Session, *config.Workspace, string, error) {
				return s.app.ThreadMenuWorkspaceConfig().CurrentThreadForMessage(msg)
			},
			ShowConversationSandboxMenu: func(msg *feishu.InboundMessage) error {
				return s.ShowThreadSandboxMenu(msg)
			},
			ShowConversationPolicyMenu: func(msg *feishu.InboundMessage) error {
				return s.ShowThreadPolicyMenu(msg)
			},
			ShowConversationMultiAgentMenu: func(msg *feishu.InboundMessage) error {
				return s.ShowThreadMultiAgentMenu(msg)
			},
			CompleteConversationSandboxSet: func(action *feishu.CardAction, sessionKey, threadID, sandboxMode string) (*callback.CardActionTriggerResponse, error) {
				return s.CompleteThreadSandboxSet(action, sessionKey, threadID, sandboxMode)
			},
			CompleteConversationPolicySet: func(action *feishu.CardAction, sessionKey, threadID, approvalPolicy string) (*callback.CardActionTriggerResponse, error) {
				return s.CompleteThreadPolicySet(action, sessionKey, threadID, approvalPolicy)
			},
			CompleteConversationMultiAgentSet: func(action *feishu.CardAction, sessionKey, threadID, mode string) (*callback.CardActionTriggerResponse, error) {
				return s.CompleteThreadMultiAgentSet(action, sessionKey, threadID, mode)
			},
			ReplyCommandActionResponse: s.app.ReplyCommandActionResponse,
			CommandActionFromMessage:   CommandActionFromMessage,
		})
	default:
		return fmt.Errorf("usage: %s", ThreadCommandUsage)
	}
}

// CommandSession handles the /session command with subcommands.
func (s *Service) CommandSession(msg *feishu.InboundMessage, args []string) error {
	msg, sessionKey := s.messageForThreadMenu(msg)
	if msg == nil {
		return nil
	}
	if len(args) == 0 {
		return s.CommandThreads(msg, false)
	}
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
		return appbackend.DriverForApp(s.app).Permission().HandleConversationCommand(appbackend.ConversationPermissionCommandRequest{
			Message:    msg,
			Args:       args,
			SessionKey: sessionKey,
			CurrentThread: func(msg *feishu.InboundMessage) (string, *state.Session, *config.Workspace, string, error) {
				return s.app.ThreadMenuWorkspaceConfig().CurrentThreadForMessage(msg)
			},
			ShowConversationPermissionModeMenu: func(msg *feishu.InboundMessage) error {
				card, err := appbackend.DriverForApp(s.app).Permission().RenderConversationPermissionModeMenu(sessionKey, appbackend.ConversationPermissionRenderDeps{
					App:            s.app,
					Session:        s.app.ThreadMenuAppState().Session,
					FormatMenuBody: s.app.MenuCardBody,
					CommandLabel:   s.app.CommandLabel,
				})
				if err != nil {
					return err
				}
				_, err = s.app.Feishu().ReplyCard(context.Background(), msg.MessageID, card, appcore.ReplyInThreadEnabled(s.app, msg.ChatType))
				return err
			},
			CompleteConversationPermissionModeSet: func(action *feishu.CardAction, sessionKey, threadID, rawMode string) (*callback.CardActionTriggerResponse, error) {
				return s.CompleteClaudeSessionPermissionModeSet(action, sessionKey, threadID, rawMode)
			},
			ReplyCommandActionResponse: s.app.ReplyCommandActionResponse,
			CommandActionFromMessage:   CommandActionFromMessage,
		})
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
	// For backends with asynchronous interrupt responses (e.g. Claude), clear
	// stale active operations so the session doesn't get stuck in "queuing".
	if runtime := s.app.ThreadMenuBackendRuntime(); runtime != nil {
		sess = runtime.ClearActiveOperationsAfterInterrupt(sessionKey, sess)
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
	msg, sessionKey := s.messageForThreadMenu(msg)
	if msg == nil {
		return nil
	}
	card, err := s.RenderThreadSandboxMenuCard(sessionKey)
	if err != nil {
		return err
	}
	_, err = s.app.Feishu().ReplyCard(context.Background(), msg.MessageID, card, appcore.ReplyInThreadEnabled(s.app, msg.ChatType))
	return err
}

// RenderThreadSandboxMenuCard renders the sandbox configuration menu card.
func (s *Service) RenderThreadSandboxMenuCard(sessionKey string) (map[string]any, error) {
	sessionKey = s.effectiveSessionKey(sessionKey)
	return appbackend.DriverForApp(s.app).Permission().RenderConversationSandboxMenu(sessionKey, appbackend.ConversationPermissionRenderDeps{
		App:            s.app,
		Session:        s.app.ThreadMenuAppState().Session,
		FormatMenuBody: s.app.MenuCardBody,
		CommandLabel:   s.app.CommandLabel,
	})
}

// ShowThreadPolicyMenu shows the policy configuration menu.
func (s *Service) ShowThreadPolicyMenu(msg *feishu.InboundMessage) error {
	msg, sessionKey := s.messageForThreadMenu(msg)
	if msg == nil {
		return nil
	}
	card, err := s.RenderThreadPolicyMenuCard(sessionKey)
	if err != nil {
		return err
	}
	_, err = s.app.Feishu().ReplyCard(context.Background(), msg.MessageID, card, appcore.ReplyInThreadEnabled(s.app, msg.ChatType))
	return err
}

// RenderThreadPolicyMenuCard renders the policy configuration menu card.
func (s *Service) RenderThreadPolicyMenuCard(sessionKey string) (map[string]any, error) {
	sessionKey = s.effectiveSessionKey(sessionKey)
	return appbackend.DriverForApp(s.app).Permission().RenderConversationPolicyMenu(sessionKey, appbackend.ConversationPermissionRenderDeps{
		App:            s.app,
		Session:        s.app.ThreadMenuAppState().Session,
		FormatMenuBody: s.app.MenuCardBody,
		CommandLabel:   s.app.CommandLabel,
	})
}

// ShowThreadMultiAgentMenu shows the multi-agent mode configuration menu.
func (s *Service) ShowThreadMultiAgentMenu(msg *feishu.InboundMessage) error {
	msg, sessionKey := s.messageForThreadMenu(msg)
	if msg == nil {
		return nil
	}
	card, err := s.RenderThreadMultiAgentMenuCard(sessionKey)
	if err != nil {
		return err
	}
	_, err = s.app.Feishu().ReplyCard(context.Background(), msg.MessageID, card, appcore.ReplyInThreadEnabled(s.app, msg.ChatType))
	return err
}

// RenderThreadMultiAgentMenuCard renders the multi-agent mode configuration menu card.
func (s *Service) RenderThreadMultiAgentMenuCard(sessionKey string) (map[string]any, error) {
	sessionKey = s.effectiveSessionKey(sessionKey)
	return appbackend.DriverForApp(s.app).Permission().RenderConversationMultiAgentMenu(sessionKey, appbackend.ConversationPermissionRenderDeps{
		App:            s.app,
		Session:        s.app.ThreadMenuAppState().Session,
		FormatMenuBody: s.app.MenuCardBody,
		CommandLabel:   s.app.CommandLabel,
	})
}

// ---------------------------------------------------------------------------
// Card action completers (from thread_feature_actions.go)
// ---------------------------------------------------------------------------

// CompleteMenuThread handles the "menu.thread" card action.
func (s *Service) CompleteMenuThread(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	sessionKey = s.effectiveSessionKey(sessionKey)
	slash := primaryConversationSlash(appcore.ConfiguredBackend(s.app))
	if strings.TrimSpace(slash) == "" {
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "warning", Content: "当前 frontend 还没有设置 backend，请先选择。"},
		}, nil
	}
	return s.app.CompleteMenuCommand(action, sessionKey, slash, "menu.root")
}

// CompleteMenuNew handles the "menu.thread.new" card action.
func (s *Service) CompleteMenuNew(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	sessionKey = s.effectiveSessionKey(sessionKey)
	slash := primaryConversationSlash(appcore.ConfiguredBackend(s.app))
	if strings.TrimSpace(slash) == "" {
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "warning", Content: "当前 frontend 还没有设置 backend，请先选择。"},
		}, nil
	}
	return s.app.CompleteMenuCommand(action, sessionKey, slash+" new", "menu.thread")
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
	sessionKey = s.effectiveSessionKey(sessionKey)
	return s.app.CompleteMenuCommand(action, sessionKey, "/thread sandbox", "menu.thread")
}

// CompleteThreadPolicyMenu handles the "thread.policy.menu" card action.
func (s *Service) CompleteThreadPolicyMenu(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	sessionKey = s.effectiveSessionKey(sessionKey)
	return s.app.CompleteMenuCommand(action, sessionKey, "/thread policy", "menu.thread")
}

// CompleteThreadMultiAgentMenu handles the "thread.multiagent.menu" card action.
func (s *Service) CompleteThreadMultiAgentMenu(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	sessionKey = s.effectiveSessionKey(sessionKey)
	return s.app.CompleteMenuCommand(action, sessionKey, "/thread multiagent", "menu.thread")
}

// CompleteClaudeSessionPermissionMenu handles the session permission menu card action.
func (s *Service) CompleteClaudeSessionPermissionMenu(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	sessionKey = s.effectiveSessionKey(sessionKey)
	return s.app.CompleteMenuCommand(action, sessionKey, "/session permissions", "menu.thread")
}

// CompleteThreadSandboxSet handles the "thread.sandbox.set" card action.
func (s *Service) CompleteThreadSandboxSet(action *feishu.CardAction, sessionKey, threadID, sandboxMode string) (*callback.CardActionTriggerResponse, error) {
	sessionKey = s.effectiveSessionKey(sessionKey)
	return appbackend.DriverForApp(s.app).Permission().CompleteConversationSandboxSet(sessionKey, threadID, sandboxMode, appbackend.ConversationPermissionUpdateDeps{
		Session:     s.app.ThreadMenuAppState().Session,
		SaveSession: s.app.ThreadMenuAppState().SaveSession,
		RenderSandboxMenu: func(sessionKey string) (map[string]any, error) {
			return s.RenderThreadSandboxMenuCard(sessionKey)
		},
		RenderPolicyMenu: func(sessionKey string) (map[string]any, error) {
			return s.RenderThreadPolicyMenuCard(sessionKey)
		},
	})
}

// CompleteThreadPolicySet handles the "thread.policy.set" card action.
func (s *Service) CompleteThreadPolicySet(action *feishu.CardAction, sessionKey, threadID, approvalPolicy string) (*callback.CardActionTriggerResponse, error) {
	sessionKey = s.effectiveSessionKey(sessionKey)
	return appbackend.DriverForApp(s.app).Permission().CompleteConversationPolicySet(sessionKey, threadID, approvalPolicy, appbackend.ConversationPermissionUpdateDeps{
		Session:     s.app.ThreadMenuAppState().Session,
		SaveSession: s.app.ThreadMenuAppState().SaveSession,
		RenderSandboxMenu: func(sessionKey string) (map[string]any, error) {
			return s.RenderThreadSandboxMenuCard(sessionKey)
		},
		RenderPolicyMenu: func(sessionKey string) (map[string]any, error) {
			return s.RenderThreadPolicyMenuCard(sessionKey)
		},
	})
}

// CompleteThreadMultiAgentSet handles the "thread.multiagent.set" card action.
func (s *Service) CompleteThreadMultiAgentSet(action *feishu.CardAction, sessionKey, threadID, mode string) (*callback.CardActionTriggerResponse, error) {
	sessionKey = s.effectiveSessionKey(sessionKey)
	return appbackend.DriverForApp(s.app).Permission().CompleteConversationMultiAgentSet(sessionKey, threadID, mode, appbackend.ConversationPermissionUpdateDeps{
		Session:     s.app.ThreadMenuAppState().Session,
		SaveSession: s.app.ThreadMenuAppState().SaveSession,
		RenderSandboxMenu: func(sessionKey string) (map[string]any, error) {
			return s.RenderThreadSandboxMenuCard(sessionKey)
		},
		RenderPolicyMenu: func(sessionKey string) (map[string]any, error) {
			return s.RenderThreadPolicyMenuCard(sessionKey)
		},
		RenderMultiAgentMenu: func(sessionKey string) (map[string]any, error) {
			return s.RenderThreadMultiAgentMenuCard(sessionKey)
		},
	})
}

// CompleteThreadResume handles resuming a previously created thread.
func (s *Service) CompleteThreadResume(action *feishu.CardAction, sessionKey, threadID string) (*callback.CardActionTriggerResponse, error) {
	sessionKey = s.effectiveSessionKey(sessionKey)
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
	sessionKey = s.effectiveSessionKey(sessionKey)
	return appbackend.DriverForApp(s.app).Permission().CompleteConversationPermissionModeSet(sessionKey, threadID, rawMode, appbackend.ConversationPermissionModeUpdateDeps{
		App:         s.app,
		Session:     s.app.ThreadMenuAppState().Session,
		SaveSession: s.app.ThreadMenuAppState().SaveSession,
		NormalizeRequested: func(raw string) (string, string, error) {
			return s.app.NormalizeRequestedClaudePermissionMode(context.Background(), raw)
		},
		ApplyRuntime: s.app.ApplyClaudePermissionModeToRuntime,
		RenderPermissionMenu: func(sessionKey string) (map[string]any, error) {
			return appbackend.DriverForApp(s.app).Permission().RenderConversationPermissionModeMenu(sessionKey, appbackend.ConversationPermissionRenderDeps{
				App:            s.app,
				Session:        s.app.ThreadMenuAppState().Session,
				FormatMenuBody: s.app.MenuCardBody,
				CommandLabel:   s.app.CommandLabel,
			})
		},
	})
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
