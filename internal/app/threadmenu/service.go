// Package threadmenu provides the thread/session menu service extracted from
// the app god package. It handles /thread and /session command routing, menu
// rendering, interrupt/append commands, and card-action completers.
package threadmenu

import (
	"context"
	"errors"
	feishutransport "feidex/internal/adapter/feishu/transport"
	"feidex/internal/domain/conversation"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	appthreadview "feidex/internal/adapter/feishu/threadview"
	appcore "feidex/internal/app/appcore"
	appbackend "feidex/internal/app/backend"

	appworkspace "feidex/internal/app/workspace"
	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

const (
	ThreadCommandUsage        = "/thread | /thread list [all] | /thread new | /thread fork | /thread resume THREAD_ID | /thread sandbox [MODE] | /thread policy [POLICY] | /thread multiagent [MODE]"
	ClaudeSessionCommandUsage = "/session | /session list [all] | /session new | /session fork | /session resume SESSION_ID | /session permissions [MODE|inherit]"
)

// Dependencies is the explicit thread-menu capability set assembled by the
// composition root. The menu service never receives the application root.
type Dependencies struct {
	ConfigProvider                            appcore.AppConfig
	FeishuClient                              feishutransport.Client
	AppStateFn                                func() StateProvider
	EffectiveSessionKeyFn                     func(string) string
	ConversationBackendFn                     func() ConversationBackendProvider
	BackendRuntimeFn                          func() BackendRuntimeProvider
	PendingQueueFn                            func() PendingQueueProvider
	WorkspaceThreadFn                         func() WorkspaceThreadProvider
	WorkspaceConfigFn                         func() WorkspaceConfigProvider
	BackendActionsFn                          func() BackendActionProvider
	BackendDriver                             appbackend.Driver
	SessionHasActiveWorkFn                    func(*conversation.Session) bool
	CancelAutoRetryFn                         func(string, bool, string) bool
	LockAutoRetryDispatchFn                   func(string) func()
	ReplyCommandActionResponseFn              func(*feishu.InboundMessage, *callback.CardActionTriggerResponse) error
	CommandForkFn                             func(*feishu.InboundMessage, []string) error
	CompleteMenuCommandFn                     func(*feishu.CardAction, string, string, string) (*callback.CardActionTriggerResponse, error)
	ActionStringValueFn                       func(*feishu.CardAction, string) string
	MenuCardBodyFn                            func(string, string) string
	MenuCardBodyForBackendFn                  func(string, string, string) string
	NormalizeRequestedClaudePermissionModeFn  func(context.Context, string) (string, string, error)
	ApplyClaudePermissionModeToRuntimeFn      func(string, string) error
	ApplyClaudePermissionModeToRuntimeAsyncFn func(string, string, string)
	RenderClaudeSessionPermissionMenuCardFn   func(string) (map[string]any, error)
	ShowClaudeSessionPermissionMenuFromAppFn  func(*feishu.InboundMessage) error
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
func (d Dependencies) Feishu() feishutransport.Client { return d.FeishuClient }
func (d Dependencies) ThreadMenuAppState() StateProvider {
	if d.AppStateFn == nil {
		return nil
	}
	return d.AppStateFn()
}
func (d Dependencies) ThreadMenuEffectiveSessionKey(s string) string {
	if d.EffectiveSessionKeyFn == nil {
		return s
	}
	return d.EffectiveSessionKeyFn(s)
}
func (d Dependencies) ThreadMenuConversationBackend() ConversationBackendProvider {
	if d.ConversationBackendFn == nil {
		return nil
	}
	return d.ConversationBackendFn()
}
func (d Dependencies) ThreadMenuBackendRuntime() BackendRuntimeProvider {
	if d.BackendRuntimeFn == nil {
		return nil
	}
	return d.BackendRuntimeFn()
}
func (d Dependencies) ThreadMenuPendingQueue() PendingQueueProvider {
	if d.PendingQueueFn == nil {
		return nil
	}
	return d.PendingQueueFn()
}
func (d Dependencies) ThreadMenuWorkspaceThread() WorkspaceThreadProvider {
	if d.WorkspaceThreadFn == nil {
		return nil
	}
	return d.WorkspaceThreadFn()
}
func (d Dependencies) ThreadMenuWorkspaceConfig() WorkspaceConfigProvider {
	if d.WorkspaceConfigFn == nil {
		return nil
	}
	return d.WorkspaceConfigFn()
}
func (d Dependencies) ThreadMenuBackendActions() BackendActionProvider {
	if d.BackendActionsFn == nil {
		return nil
	}
	return d.BackendActionsFn()
}
func (d Dependencies) PermissionDriver() appbackend.PermissionDriver {
	if d.BackendDriver == nil {
		return nil
	}
	return d.BackendDriver.Permission()
}
func (d Dependencies) SessionHasActiveWork(s *conversation.Session) bool {
	return d.SessionHasActiveWorkFn != nil && d.SessionHasActiveWorkFn(s)
}
func (d Dependencies) CancelAutoRetry(s string, k bool, n string) bool {
	return d.CancelAutoRetryFn != nil && d.CancelAutoRetryFn(s, k, n)
}
func (d Dependencies) LockAutoRetryDispatch(s string) func() {
	if d.LockAutoRetryDispatchFn == nil {
		return func() {}
	}
	return d.LockAutoRetryDispatchFn(s)
}
func (d Dependencies) ReplyCommandActionResponse(m *feishu.InboundMessage, r *callback.CardActionTriggerResponse) error {
	if d.ReplyCommandActionResponseFn == nil {
		return nil
	}
	return d.ReplyCommandActionResponseFn(m, r)
}
func (d Dependencies) CommandFork(m *feishu.InboundMessage, a []string) error {
	if d.CommandForkFn == nil {
		return fmt.Errorf("fork unavailable")
	}
	return d.CommandForkFn(m, a)
}
func (d Dependencies) CompleteMenuCommand(a *feishu.CardAction, s, r, p string) (*callback.CardActionTriggerResponse, error) {
	if d.CompleteMenuCommandFn == nil {
		return nil, fmt.Errorf("menu command unavailable")
	}
	return d.CompleteMenuCommandFn(a, s, r, p)
}
func (d Dependencies) ActionStringValue(a *feishu.CardAction, k string) string {
	if d.ActionStringValueFn == nil {
		return ""
	}
	return d.ActionStringValueFn(a, k)
}
func (d Dependencies) MenuCardBody(a, b string) string {
	if d.MenuCardBodyFn == nil {
		return b
	}
	return d.MenuCardBodyFn(a, b)
}
func (d Dependencies) MenuCardBodyForBackend(x, a, b string) string {
	if d.MenuCardBodyForBackendFn == nil {
		return b
	}
	return d.MenuCardBodyForBackendFn(x, a, b)
}
func (d Dependencies) NormalizeRequestedClaudePermissionMode(c context.Context, r string) (string, string, error) {
	if d.NormalizeRequestedClaudePermissionModeFn == nil {
		return "", "", fmt.Errorf("permission mode unavailable")
	}
	return d.NormalizeRequestedClaudePermissionModeFn(c, r)
}
func (d Dependencies) ApplyClaudePermissionModeToRuntime(s, m string) error {
	if d.ApplyClaudePermissionModeToRuntimeFn == nil {
		return fmt.Errorf("permission mode unavailable")
	}
	return d.ApplyClaudePermissionModeToRuntimeFn(s, m)
}
func (d Dependencies) ApplyClaudePermissionModeToRuntimeAsync(m, s, x string) {
	if d.ApplyClaudePermissionModeToRuntimeAsyncFn != nil {
		d.ApplyClaudePermissionModeToRuntimeAsyncFn(m, s, x)
	}
}
func (d Dependencies) RenderClaudeSessionPermissionMenuCard(s string) (map[string]any, error) {
	if d.RenderClaudeSessionPermissionMenuCardFn == nil {
		return nil, fmt.Errorf("permission menu unavailable")
	}
	return d.RenderClaudeSessionPermissionMenuCardFn(s)
}
func (d Dependencies) ShowClaudeSessionPermissionMenuFromApp(m *feishu.InboundMessage) error {
	if d.ShowClaudeSessionPermissionMenuFromAppFn == nil {
		return fmt.Errorf("permission menu unavailable")
	}
	return d.ShowClaudeSessionPermissionMenuFromAppFn(m)
}

// StateProvider narrows app state access to the methods used by the service.
type StateProvider interface {
	Session(key string) *conversation.Session
	Sessions() []*conversation.Session
	SaveSession(sess *conversation.Session) error
}

// ConversationBackendProvider narrows conversation backend access to the
// methods used by the service.
type ConversationBackendProvider interface {
	RenderThreadsCard(sessionKey string, includeAll bool) (map[string]any, error)
	InterruptActiveTurn(ctx context.Context, sessionKey string, sess *conversation.Session) error
	ContinueActiveTurn(sessionKey string, text string) error
	ResumeSelectedThread(sessionKey string, sess *conversation.Session, ws *config.Workspace, selection ThreadResumeSelection) (*ThreadBinding, error)
	ForkReplyMessage(forkedID string) string
}

// BackendRuntimeProvider narrows backend runtime access to the methods used by
// the service.
type BackendRuntimeProvider interface {
	ReconcileCompletedTurnFromFinalOutput(sessionKey string, sess *conversation.Session) *conversation.Session
	// ClearActiveOperationsAfterInterrupt clears stale active operations after
	// an interrupt request. For backends where the interrupt response is
	// asynchronous (e.g. Claude), this prevents the session from getting stuck
	// in "queuing" state if the interrupt doesn't trigger a turn completion.
	ClearActiveOperationsAfterInterrupt(sessionKey string, sess *conversation.Session) *conversation.Session
}

// PendingQueueProvider narrows pending queue access to the methods used by the
// service.
type PendingQueueProvider interface {
	DiscardSessionPendingInputs(sessionKey string) int
}

// WorkspaceThreadProvider narrows workspace thread access to the methods used
// by the service.
type WorkspaceThreadProvider interface {
	StartWorkspaceThread(sessionKey string, sess *conversation.Session, ws *config.Workspace) (*ThreadBinding, error)
}

// WorkspaceConfigProvider narrows workspace config access to the methods used
// by the service.
type WorkspaceConfigProvider interface {
	CurrentThreadForMessage(msg *feishu.InboundMessage) (sessionKey string, sess *conversation.Session, ws *config.Workspace, threadID string, err error)
}

// BackendActionProvider narrows backend action access to the methods used by
// the service.
type BackendActionProvider interface {
	CompleteMenuInterrupt(action *feishu.CardAction, sessionKey, targetTurnID string) (*callback.CardActionTriggerResponse, error)
}

// ThreadResumeSelection describes a thread resume selection from the UI.
type ThreadResumeSelection = conversation.ThreadSelection

// ThreadBinding is an alias for the workspace thread binding type.
type ThreadBinding = appworkspace.ThreadBinding

// uiWarningError is a sentinel error type for UI warning messages.
type uiWarningError struct{ message string }

func (e uiWarningError) Error() string { return e.message }

func isUIWarningError(err error) bool {
	var target uiWarningError
	if errors.As(err, &target) {
		return true
	}
	return conversation.IsWarning(err)
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

func primaryConversationSlash(backend string) string {
	return appbackend.DriverForKind(backend).Conversation().PrimarySlash()
}

func primaryConversationNoun(backend string) string {
	return appbackend.DriverForKind(backend).Conversation().Noun()
}

func primaryConversationSummaryLabel(backend string) string {
	return appbackend.DriverForKind(backend).Conversation().SummaryLabel()
}

func sessionHasInFlightSubmission(sess *conversation.Session) bool {
	return conversation.HasInFlightSubmission(sess)
}

// Service manages thread/session menu actions for a single app instance.
type Service struct {
	app Dependencies
}

// NewService creates a new thread-menu service bound to the given app.
func NewService(app Dependencies) *Service {
	return &Service{app: app}
}

func (s *Service) effectiveSessionKey(sessionKey string) string {
	sessionKey = strings.TrimSpace(sessionKey)
	if s == nil || s.app.ConfigProvider == nil {
		return sessionKey
	}
	resolved := strings.TrimSpace(s.app.ThreadMenuEffectiveSessionKey(sessionKey))
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

func (s *Service) interruptSurfaceSessionKeys(sessionKey string) []string {
	sessionKey = strings.TrimSpace(sessionKey)
	keys := appendUniqueSessionKey(nil, sessionKey)
	if s == nil || s.app.ConfigProvider == nil {
		return keys
	}
	st := s.app.ThreadMenuAppState()
	if st == nil {
		return keys
	}
	chatType, chatID := sessionGroupChat(sessionKey, st.Session(sessionKey))
	if chatType != "group" || strings.TrimSpace(chatID) == "" {
		return keys
	}
	for _, sess := range st.Sessions() {
		if sess == nil {
			continue
		}
		candidateKey := strings.TrimSpace(sess.Key)
		if candidateKey == "" || !appcore.SessionBelongsToFrontend(s.app, candidateKey) {
			continue
		}
		candidateChatType, candidateChatID := sessionGroupChat(candidateKey, sess)
		if candidateChatType == "group" && candidateChatID == chatID {
			keys = appendUniqueSessionKey(keys, candidateKey)
		}
	}
	sort.Strings(keys)
	return keys
}

func appendUniqueSessionKey(keys []string, key string) []string {
	key = strings.TrimSpace(key)
	if key == "" {
		return keys
	}
	for _, existing := range keys {
		if strings.TrimSpace(existing) == key {
			return keys
		}
	}
	return append(keys, key)
}

func sessionGroupChat(sessionKey string, sess *conversation.Session) (chatType, chatID string) {
	_, chatType, chatID, _, _ = appcore.ParseSessionKey(sessionKey)
	if chatType == "" && sess != nil {
		chatType = strings.TrimSpace(sess.ChatType)
		chatID = strings.TrimSpace(sess.ChatID)
	}
	return chatType, chatID
}

func (s *Service) discardInterruptSurfacePendingInputs(sessionKeys []string) int {
	if s == nil || s.app.ConfigProvider == nil {
		return 0
	}
	pendingQueue := s.app.ThreadMenuPendingQueue()
	if pendingQueue == nil {
		return 0
	}
	discarded := 0
	for _, key := range sessionKeys {
		discarded += pendingQueue.DiscardSessionPendingInputs(key)
	}
	return discarded
}

func (s *Service) interruptTargetSession(sessionKeys []string) (string, *conversation.Session) {
	if s == nil || s.app.ConfigProvider == nil {
		return "", nil
	}
	st := s.app.ThreadMenuAppState()
	if st == nil {
		return "", nil
	}
	var bestKey string
	var best *conversation.Session
	for _, key := range sessionKeys {
		sess := st.Session(key)
		if !interruptSessionActive(sess) {
			continue
		}
		if best == nil || sess.UpdatedAt > best.UpdatedAt || (sess.UpdatedAt == best.UpdatedAt && strings.TrimSpace(key) > strings.TrimSpace(bestKey)) {
			bestKey = strings.TrimSpace(key)
			best = sess
		}
	}
	return bestKey, best
}

func interruptSessionActive(sess *conversation.Session) bool {
	return sess != nil && strings.TrimSpace(sess.ActiveTurnID) != "" && strings.TrimSpace(sess.ActiveThreadID) != ""
}

func (s *Service) cancelInterruptSurfaceAutoRetry(sessionKeys []string, activeSessionKey string, activeSess *conversation.Session) bool {
	if s == nil || s.app.ConfigProvider == nil {
		return false
	}
	st := s.app.ThreadMenuAppState()
	canceled := false
	for _, key := range sessionKeys {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		sess := activeSess
		if key != strings.TrimSpace(activeSessionKey) && st != nil {
			sess = st.Session(key)
		}
		keepUntilTerminal := key == strings.TrimSpace(activeSessionKey) && interruptSessionActive(sess)
		if s.app.CancelAutoRetry(key, keepUntilTerminal, "已停止当前 session 的自动重试。") {
			canceled = true
		}
	}
	return canceled
}

// StartFreshThread creates a new workspace thread for the session.
func (s *Service) StartFreshThread(sessionKey, userID, chatID, chatType string) (int, *ThreadBinding, error) {
	if s.app.ConfigProvider == nil || s.app.Store() == nil {
		return 0, nil, fmt.Errorf("store not initialized")
	}
	appState := s.app.ThreadMenuAppState()
	defaultWorkspaceID := appcore.DefaultWorkspaceID(s.app)
	sess := appState.Session(sessionKey)
	if sess != nil && s.app.SessionHasActiveWork(sess) {
		return 0, nil, fmt.Errorf("当前任务仍在运行，请先等待结束或中断")
	}
	if sess == nil {
		sess = &conversation.Session{
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
		sess = &conversation.Session{
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
		return s.app.PermissionDriver().HandleConversationCommand(appbackend.ConversationPermissionCommandRequest{
			Message:    msg,
			Args:       args,
			SessionKey: sessionKey,
			CurrentThread: func(msg *feishu.InboundMessage) (string, *conversation.Session, *config.Workspace, string, error) {
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
		return s.app.PermissionDriver().HandleConversationCommand(appbackend.ConversationPermissionCommandRequest{
			Message:    msg,
			Args:       args,
			SessionKey: sessionKey,
			CurrentThread: func(msg *feishu.InboundMessage) (string, *conversation.Session, *config.Workspace, string, error) {
				return s.app.ThreadMenuWorkspaceConfig().CurrentThreadForMessage(msg)
			},
			ShowConversationPermissionModeMenu: func(msg *feishu.InboundMessage) error {
				card, err := s.app.PermissionDriver().RenderConversationPermissionModeMenu(sessionKey, appbackend.ConversationPermissionRenderDeps{
					App:            s.app,
					Session:        s.app.ThreadMenuAppState().Session,
					FormatMenuBody: s.app.MenuCardBody,
				})
				if err != nil {
					return err
				}
				_, err = s.app.Feishu().ReplyCard(context.Background(), msg.MessageID, card, appcore.ReplyInThreadEnabled(s.app, msg.ChatType))
				return err
			},
			CompleteConversationPermissionModeSet: func(action *feishu.CardAction, sessionKey, threadID, rawMode string) (*callback.CardActionTriggerResponse, error) {
				// Slash command path: no card ack deadline, apply synchronously
				// so the reply reflects the actual runtime result.
				return s.completeClaudeSessionPermissionModeSet(action, sessionKey, threadID, rawMode, false)
			},
			ReplyCommandActionResponse: s.app.ReplyCommandActionResponse,
			CommandActionFromMessage:   CommandActionFromMessage,
		})
	default:
		return fmt.Errorf("usage: %s", ClaudeSessionCommandUsage)
	}
}

// CommandInterrupt handles /stop — interrupts the active turn.
func (s *Service) CommandInterrupt(msg *feishu.InboundMessage) error {
	sessionKey := appcore.MakeSessionKey(s.app, msg)
	sessionKeys := s.interruptSurfaceSessionKeys(sessionKey)
	sort.Strings(sessionKeys)
	for _, key := range sessionKeys {
		unlock := s.app.LockAutoRetryDispatch(key)
		defer unlock()
	}
	discarded := s.discardInterruptSurfacePendingInputs(sessionKeys)
	targetSessionKey, sess := s.interruptTargetSession(sessionKeys)
	canceledRetry := s.cancelInterruptSurfaceAutoRetry(sessionKeys, targetSessionKey, sess)
	if runtime := s.app.ThreadMenuBackendRuntime(); runtime != nil && sess != nil {
		runtime.ReconcileCompletedTurnFromFinalOutput(targetSessionKey, sess)
	}
	// Notifications may have completed the turn while cancellation cards were sent.
	targetSessionKey, sess = s.interruptTargetSession(sessionKeys)
	if s.cancelInterruptSurfaceAutoRetry(sessionKeys, targetSessionKey, sess) {
		canceledRetry = true
	}
	if !interruptSessionActive(sess) {
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
	if err := s.app.ThreadMenuConversationBackend().InterruptActiveTurn(ctx, targetSessionKey, sess); err != nil {
		return err
	}
	// For backends with asynchronous interrupt responses (e.g. Claude), clear
	// stale active operations so the session doesn't get stuck in "queuing".
	if runtime := s.app.ThreadMenuBackendRuntime(); runtime != nil {
		// Only the side effect matters here: the store is updated in place and
		// this function builds its reply from the local flags above.
		runtime.ClearActiveOperationsAfterInterrupt(targetSessionKey, sess)
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
	return s.app.PermissionDriver().RenderConversationSandboxMenu(sessionKey, appbackend.ConversationPermissionRenderDeps{
		App:            s.app,
		Session:        s.app.ThreadMenuAppState().Session,
		FormatMenuBody: s.app.MenuCardBody,
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
	return s.app.PermissionDriver().RenderConversationPolicyMenu(sessionKey, appbackend.ConversationPermissionRenderDeps{
		App:            s.app,
		Session:        s.app.ThreadMenuAppState().Session,
		FormatMenuBody: s.app.MenuCardBody,
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
	return s.app.PermissionDriver().RenderConversationMultiAgentMenu(sessionKey, appbackend.ConversationPermissionRenderDeps{
		App:            s.app,
		Session:        s.app.ThreadMenuAppState().Session,
		FormatMenuBody: s.app.MenuCardBody,
	})
}

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
	return s.app.PermissionDriver().CompleteConversationSandboxSet(sessionKey, threadID, sandboxMode, appbackend.ConversationPermissionUpdateDeps{
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
	return s.app.PermissionDriver().CompleteConversationPolicySet(sessionKey, threadID, approvalPolicy, appbackend.ConversationPermissionUpdateDeps{
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
	return s.app.PermissionDriver().CompleteConversationMultiAgentSet(sessionKey, threadID, mode, appbackend.ConversationPermissionUpdateDeps{
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
		sess = &conversation.Session{Key: sessionKey, OwnerUserID: action.UserID, ChatID: action.ChatID}
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

// CompleteClaudeSessionPermissionModeSet handles the session permission mode
// card action. The runtime apply is enqueued so the Feishu callback can answer
// immediately; a failure patches the menu card with a warning.
func (s *Service) CompleteClaudeSessionPermissionModeSet(action *feishu.CardAction, sessionKey, threadID, rawMode string) (*callback.CardActionTriggerResponse, error) {
	return s.completeClaudeSessionPermissionModeSet(action, sessionKey, threadID, rawMode, true)
}

// completeClaudeSessionPermissionModeSet applies the mode synchronously when it
// is not on the card callback ack path (slash commands have no ack deadline).
func (s *Service) completeClaudeSessionPermissionModeSet(action *feishu.CardAction, sessionKey, threadID, rawMode string, asyncRuntimeApply bool) (*callback.CardActionTriggerResponse, error) {
	sessionKey = s.effectiveSessionKey(sessionKey)
	applyRuntime := s.app.ApplyClaudePermissionModeToRuntime
	if asyncRuntimeApply {
		messageID := ""
		if action != nil {
			messageID = strings.TrimSpace(action.MessageID)
		}
		applyRuntime = func(key, mode string) error {
			s.app.ApplyClaudePermissionModeToRuntimeAsync(messageID, key, mode)
			return nil
		}
	}
	return s.app.PermissionDriver().CompleteConversationPermissionModeSet(sessionKey, threadID, rawMode, appbackend.ConversationPermissionModeUpdateDeps{
		App:         s.app,
		Session:     s.app.ThreadMenuAppState().Session,
		SaveSession: s.app.ThreadMenuAppState().SaveSession,
		NormalizeRequested: func(raw string) (string, string, error) {
			return s.app.NormalizeRequestedClaudePermissionMode(context.Background(), raw)
		},
		ApplyRuntime: applyRuntime,
		RenderPermissionMenu: func(sessionKey string) (map[string]any, error) {
			return s.app.PermissionDriver().RenderConversationPermissionModeMenu(sessionKey, appbackend.ConversationPermissionRenderDeps{
				App:            s.app,
				Session:        s.app.ThreadMenuAppState().Session,
				FormatMenuBody: s.app.MenuCardBody,
			})
		},
	})
}

// SessionCurrentThreadLabel returns the current thread label for a session.
func SessionCurrentThreadLabel(sess *conversation.Session) string {
	if sess == nil {
		return "-"
	}
	return appthreadview.CurrentThreadLabel(sess.ActiveThreadName, sess.ActiveThreadPreview, sess.ActiveThreadID)
}
