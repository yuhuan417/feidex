// Package convbackend provides the conversation backend facade and card
// rendering logic extracted from the app god package. It handles thread
// listing, resuming, forking, interrupting, and card rendering for both
// Codex and Claude backends.
package convbackend

import (
	"context"
	"errors"
	"fmt"
	"strings"

	appcards "feidex/internal/app/cards"
	appcore "feidex/internal/app/appcore"
	appmodelconfig "feidex/internal/app/modelconfig"
	appthreadview "feidex/internal/app/threadview"
	appworkspace "feidex/internal/app/workspace"
	backendcaps "feidex/internal/app/backendcaps"
	menutypes "feidex/internal/app/menutypes"
	"feidex/internal/codexrpc"
	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

// ---------------------------------------------------------------------------
// App interface — what the service needs from the host application
// ---------------------------------------------------------------------------

// App defines the interface the convbackend service requires from the host
// application. It embeds appcore.AppConfig so that appcore helpers like
// ConfiguredBackend, DefaultWorkspaceID, etc. can be called directly.
type App interface {
	appcore.AppConfig

	// Feishu returns the Feishu bot client.
	Feishu() appcore.FeishuClient

	// ConvBackendState returns the narrowed app state provider.
	ConvBackendState() AppStateProvider
	// ConvBackendConversation returns the narrowed conversation provider.
	ConvBackendConversation() ConversationProvider
	// ConvBackendWorkspaceConfig returns the narrowed workspace config provider.
	ConvBackendWorkspaceConfig() WorkspaceConfigProvider
}

// ---------------------------------------------------------------------------
// Narrow provider interfaces
// ---------------------------------------------------------------------------

// AppStateProvider narrows app state access to the methods used by the service.
type AppStateProvider interface {
	Session(key string) *state.Session
	SaveSession(sess *state.Session) error
}

// ConversationProvider narrows conversation backend access to the methods
// used by the service. It provides the core thread operations that the
// facade implementations delegate to.
type ConversationProvider interface {
	// ListCodexThreads lists codex workspace threads.
	ListCodexThreads(app App, sessionKey string, ws *config.Workspace, includeAll bool) ([]codexrpc.ThreadListEntry, error)
	// EnsureCodexBinding ensures a codex workspace thread binding.
	EnsureCodexBinding(app App, sessionKey string, sess *state.Session, ws *config.Workspace) (*ThreadBinding, error)
	// StartCodexThread starts a new codex workspace thread.
	StartCodexThread(app App, sessionKey string, sess *state.Session, ws *config.Workspace) (*ThreadBinding, error)
	// ResumeCodexThread resumes a selected codex thread.
	ResumeCodexThread(app App, sessionKey string, sess *state.Session, ws *config.Workspace, sel ThreadResumeSelection) (*ThreadBinding, error)
	// InterruptCodexTurn interrupts an active codex turn.
	InterruptCodexTurn(app App, ctx context.Context, sess *state.Session) error
	// ContinueCodexTurn continues an active codex turn with text.
	ContinueCodexTurn(app App, sessionKey, text string) error
	// TryCodexReplyContinuation tries to continue a codex reply.
	TryCodexReplyContinuation(app App, msg *feishu.InboundMessage, link *state.MessageLink, sessionKey string, sess *state.Session) (bool, error)
	// ForkCodexConversation forks the active codex conversation.
	ForkCodexConversation(app App, sessionKey string, sess *state.Session, ws *config.Workspace) (string, error)
	// RecoverCodexStartup recovers a codex startup conversation.
	RecoverCodexStartup(app App, sessionKey, workspaceID string, sess *state.Session, ws *config.Workspace, effectiveModel string)
	// ListClaudeThreads lists claude workspace threads.
	ListClaudeThreads(sessionKey string, ws *config.Workspace, includeAll bool) ([]codexrpc.ThreadListEntry, error)
	// EnsureClaudeBinding ensures a claude workspace thread binding.
	EnsureClaudeBinding(app App, sessionKey string, sess *state.Session, ws *config.Workspace) (*ThreadBinding, error)
	// StartClaudeThread starts a new claude workspace thread.
	StartClaudeThread(app App, sessionKey string, sess *state.Session, ws *config.Workspace) (*ThreadBinding, error)
	// ResumeClaudeThread resumes a selected claude thread.
	ResumeClaudeThread(app App, sessionKey string, sess *state.Session, ws *config.Workspace, sel ThreadResumeSelection) (*ThreadBinding, error)
	// InterruptClaudeTurn interrupts an active claude turn.
	InterruptClaudeTurn(app App, ctx context.Context, sessionKey string) error
	// ContinueClaudeTurn continues an active claude turn with text.
	ContinueClaudeTurn(app App, sessionKey, text string) error
	// TryClaudeReplyContinuation tries to continue a claude reply.
	TryClaudeReplyContinuation(app App, msg *feishu.InboundMessage, link *state.MessageLink, sessionKey string, sess *state.Session) (bool, error)
	// ForkClaudeConversation forks the active claude conversation.
	ForkClaudeConversation(app App, sessionKey string, sess *state.Session, ws *config.Workspace) (string, error)
	// RecoverClaudeStartup recovers a claude startup conversation.
	RecoverClaudeStartup(app App, sessionKey, workspaceID string, sess *state.Session)
	// StartNextSubmission starts the next queued submission.
	StartNextSubmission(app App, sessionKey string, sess *state.Session, sub *state.Submission, ws *config.Workspace, notifyFailure bool) error
	// MarkThreadLive marks a thread as live for a session.
	MarkThreadLive(app App, sessionKey, threadID string)
}

// WorkspaceConfigProvider narrows workspace config access.
type WorkspaceConfigProvider interface {
	// HistoryIndexForOrdinal returns the history index for a codex ordinal.
	HistoryIndexForOrdinal(app App, sessionKey string, ordinal int) (int, error)
	// RenderCodexHistoryCard renders the codex history card.
	RenderCodexHistoryCard(app App, sessionKey string, page int) (map[string]any, error)
	// RenderCodexHistoryDetailCard renders the codex history detail card.
	RenderCodexHistoryDetailCard(app App, sessionKey string, index int) (map[string]any, error)
	// RenderCodexUsageBody renders the codex usage body.
	RenderCodexUsageBody(app App, sess *state.Session) string
	// HistoryTurnIndexForOrdinal returns the history turn index for a claude ordinal.
	HistoryTurnIndexForOrdinal(app App, sessionKey string, ordinal int) (int, error)
	// RenderClaudeHistoryCard renders the claude history card.
	RenderClaudeHistoryCard(app App, sessionKey string, page int) (map[string]any, error)
	// RenderClaudeHistoryDetailCard renders the claude history detail card.
	RenderClaudeHistoryDetailCard(app App, sessionKey string, index int) (map[string]any, error)
	// RenderClaudeUsageBody renders the claude usage body.
	RenderClaudeUsageBody(app App, sess *state.Session) string
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
// ConversationBackendFacade interface
// ---------------------------------------------------------------------------

// ConversationBackendFacade defines the interface for conversation backend
// operations. Implementations are provided for both Codex and Claude backends.
type ConversationBackendFacade interface {
	ListWorkspaceThreads(sessionKey string, ws *config.Workspace, includeAll bool) ([]codexrpc.ThreadListEntry, error)
	EnsureWorkspaceThreadBinding(sessionKey string, sess *state.Session, ws *config.Workspace) (*ThreadBinding, error)
	StartWorkspaceThread(sessionKey string, sess *state.Session, ws *config.Workspace) (*ThreadBinding, error)
	ResumeSelectedThread(sessionKey string, sess *state.Session, ws *config.Workspace, selection ThreadResumeSelection) (*ThreadBinding, error)
	ForkActiveConversation(sessionKey string, sess *state.Session, ws *config.Workspace) (string, error)
	ForkReplyMessage(forkedID string) string
	RecoverStartupConversation(sessionKey, workspaceID string, sess *state.Session, ws *config.Workspace, effectiveModel string)
	RenderThreadsCard(sessionKey string, includeAll bool) (map[string]any, error)
	HistoryIndexForOrdinal(sessionKey string, ordinal int) (int, error)
	RenderHistoryCard(sessionKey string, page int) (map[string]any, error)
	RenderHistoryDetailCard(sessionKey string, index int) (map[string]any, error)
	RenderUsageBody(sess *state.Session) string
	InterruptActiveTurn(ctx context.Context, sessionKey string, sess *state.Session) error
	ContinueActiveTurn(sessionKey, text string) error
	TryReplyContinuation(msg *feishu.InboundMessage, link *state.MessageLink, sessionKey string, sess *state.Session) (bool, error)
	StartQueuedSubmission(sessionKey string, sess *state.Session, sub *state.Submission, ws *config.Workspace, notifyFailure bool) error
}

// ---------------------------------------------------------------------------
// UI warning error
// ---------------------------------------------------------------------------

// UIWarningError is a sentinel error type for UI warning messages.
type UIWarningError struct {
	message string
}

func (e UIWarningError) Error() string {
	return e.message
}

// NewUIWarningError creates a new UI warning error.
func NewUIWarningError(message string) error {
	return UIWarningError{message: strings.TrimSpace(message)}
}

// IsUIWarningError checks if an error is a UI warning error.
func IsUIWarningError(err error) bool {
	var target UIWarningError
	return errors.As(err, &target)
}

// ---------------------------------------------------------------------------
// Pure helpers
// ---------------------------------------------------------------------------

func firstNonEmpty(values ...string) string {
	return appcore.FirstNonEmpty(values...)
}

func commandLabel(label, slash string) string {
	label = strings.TrimSpace(label)
	slash = strings.TrimSpace(slash)
	if label == "" {
		return slash
	}
	if slash == "" {
		return label
	}
	return label + " " + slash
}

func submenuCommandLabel(label, slash string) string {
	label = strings.TrimSpace(label)
	slash = strings.TrimSpace(slash)
	if label == "" && slash == "" {
		return ">"
	}
	if slash == "" {
		return label + " >"
	}
	if label == "" {
		return slash + " >"
	}
	return label + " " + slash + " >"
}

func renderThreadListEntry(name, preview, id string) string {
	return appthreadview.RenderThreadListEntry(name, preview, id)
}

func renderThreadSettingValue(override, fallback string) string {
	return appthreadview.RenderThreadSettingValue(override, fallback)
}

func currentThreadLabel(sess *state.Session) string {
	if sess == nil {
		return "-"
	}
	return appthreadview.CurrentThreadLabel(sess.ActiveThreadName, sess.ActiveThreadPreview, sess.ActiveThreadID)
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

func normalizeClaudePermissionModeValue(value string) string {
	switch strings.TrimSpace(value) {
	case "", "default":
		return "default"
	case "acceptEdits":
		return "acceptEdits"
	case "bypassPermissions":
		return "bypassPermissions"
	case "plan":
		return "plan"
	default:
		return strings.TrimSpace(value)
	}
}

func claudePermissionModeLabel(value string) string {
	value = normalizeClaudePermissionModeValue(value)
	if value == "" {
		value = "default"
	}
	return "`" + value + "`"
}

func configuredGlobalModel(cfg *config.Config) string {
	return appmodelconfig.ConfiguredGlobalModel(cfg)
}

func sameWorkspaceCWD(a, b string) bool {
	return appthreadview.SameWorkspaceCWD(a, b)
}

func menuBreadcrumbLabelsForBackend(action, backend string) []string {
	action = strings.TrimSpace(action)
	if action == "" {
		action = "menu.root"
	}
	labels := []string{}
	for i := 0; action != "" && i < 16; i++ {
		node, ok := menutypes.MenuNodes[action]
		if !ok {
			break
		}
		labels = append(labels, menuNodeLabelForBackend(action, node.Label, backend))
		action = node.Parent
	}
	for i, j := 0, len(labels)-1; i < j; i, j = i+1, j-1 {
		labels[i], labels[j] = labels[j], labels[i]
	}
	return labels
}

func menuNodeLabelForBackend(action, label, backend string) string {
	return backendcaps.ForKind(backend).MenuNodeLabel(action, label)
}

func menuCardBodyForBackend(backend, action, body string) string {
	breadcrumbs := strings.Join(menuBreadcrumbLabelsForBackend(action, backend), " / ")
	body = strings.TrimSpace(body)
	if breadcrumbs == "" {
		return body
	}
	if body == "" {
		return "当前位置：" + breadcrumbs
	}
	return "当前位置：" + breadcrumbs + "\n\n" + body
}

// ---------------------------------------------------------------------------
// Card rendering: conversationThreadsCardView
// ---------------------------------------------------------------------------

// ConversationThreadsCardView holds the data needed to build a conversation
// threads card.
type ConversationThreadsCardView struct {
	Title          string
	Backend        string
	BodyLines      []string
	Buttons        []feishu.Button
	Items          []codexrpc.ThreadListEntry
	ActiveThreadID string
	IncludeAll     bool
}

// BuildConversationThreadsCard builds a conversation threads card from the
// given view data.
func BuildConversationThreadsCard(sessionKey string, view ConversationThreadsCardView) map[string]any {
	selectOptions := make([]appcards.SelectStaticOption, 0, len(view.Items))
	initialOption := ""
	for idx, item := range view.Items {
		entry := fmt.Sprintf("%d. %s", idx+1, renderThreadListEntry(item.Name, item.Preview, item.ID))
		if strings.TrimSpace(view.ActiveThreadID) != "" && item.ID == strings.TrimSpace(view.ActiveThreadID) {
			entry = fmt.Sprintf("%d. [current] %s", idx+1, renderThreadListEntry(item.Name, item.Preview, item.ID))
			initialOption = item.ID
		}
		selectOptions = append(selectOptions, appcards.SelectStaticOption{
			Text:  entry,
			Value: item.ID,
		})
	}
	card := appcards.NewMarkdownBodyCard(strings.TrimSpace(view.Title), "blue")
	appcards.AppendMarkdownBodyCardElement(card, map[string]any{
		"tag":     "markdown",
		"content": menuCardBodyForBackend(view.Backend, "menu.thread", strings.Join(view.BodyLines, "\n")),
	})
	if len(selectOptions) > 0 {
		appcards.AppendMarkdownBodyCardElement(card, appcards.BuildSelectStaticElement(
			"thread_resume_select",
			"list",
			map[string]any{"action": "thread.resume.select", "session_key": sessionKey, "include_all": view.IncludeAll},
			selectOptions,
			initialOption,
		))
	}
	for _, row := range appcards.BuildMarkdownBodyCardActionElements(view.Buttons) {
		appcards.AppendMarkdownBodyCardElement(card, row)
	}
	return card
}

// ---------------------------------------------------------------------------
// Card rendering: RenderCodexThreadsCard
// ---------------------------------------------------------------------------

// RenderCodexThreadsCard renders the codex threads card for a session.
func RenderCodexThreadsCard(app App, sessionKey string, includeAll bool) (map[string]any, error) {
	state := app.ConvBackendState()
	sess := state.Session(sessionKey)
	workspace := app.Config().Workspaces[0]
	if sess != nil {
		if ws := config.FindWorkspace(app.Config(), sess.WorkspaceID); ws != nil {
			workspace = *ws
		}
	}
	conversation := app.ConvBackendConversation()
	items, err := conversation.ListCodexThreads(app, sessionKey, &workspace, includeAll)
	if err != nil {
		return nil, err
	}
	appworkspace.SortThreadsByUpdated(items)
	currentLabel := "-"
	currentThreadID := "-"
	currentThreadSandbox := "-"
	currentThreadPolicy := "-"
	if sess != nil {
		currentLabel = currentThreadLabel(sess)
		if strings.TrimSpace(sess.ActiveThreadID) != "" {
			currentThreadID = strings.TrimSpace(sess.ActiveThreadID)
			currentThreadSandbox = renderThreadSettingValue(sess.ActiveThreadSandboxMode, workspace.SandboxMode)
			currentThreadPolicy = renderThreadSettingValue(sess.ActiveThreadApprovalPolicy, workspace.ApprovalPolicy)
		}
	}
	backend := appcore.ConfiguredBackend(app)
	scopeLabel := "current workspace"
	if includeAll {
		scopeLabel = "all sources (command entry only)"
	}
	lines := []string{
		primaryConversationCurrentLabel(backend) + ": " + currentLabel,
		"当前 " + primaryConversationIDLabel(backend) + ": `" + currentThreadID + "`",
		"workspace: `" + workspace.ID + "`",
		"current thread sandbox: " + currentThreadSandbox,
		"current thread policy: " + currentThreadPolicy,
		"list scope: " + scopeLabel,
		fmt.Sprintf("list count: `%d`", len(items)),
	}
	if len(items) == 0 {
		lines = append(lines, "", "no switchable threads available.")
	} else {
		lines = append(lines, "", "select a thread from the dropdown to switch.")
	}
	hasActiveThread := sess != nil && strings.TrimSpace(sess.ActiveThreadID) != ""
	if !hasActiveThread {
		lines = append(lines, "", "no active thread, so /thread fork, /thread sandbox, /thread policy are not shown.")
	}
	buttons := []feishu.Button{
		{
			Text: commandLabel("new thread", "/thread new"),
			Type: "default",
			Value: map[string]any{
				"action":        "menu.new",
				"session_key":   sessionKey,
				"parent_action": "menu.thread",
			},
		},
	}
	if hasActiveThread {
		buttons = append(buttons,
			feishu.Button{
				Text: commandLabel("fork thread", "/thread fork"),
				Type: "default",
				Value: map[string]any{
					"action":        "menu.fork",
					"session_key":   sessionKey,
					"parent_action": "menu.thread",
				},
			},
			feishu.Button{
				Text: submenuCommandLabel("configure sandbox", "/thread sandbox"),
				Type: "default",
				Value: map[string]any{
					"action":      "thread.sandbox.menu",
					"session_key": sessionKey,
				},
			},
			feishu.Button{
				Text: submenuCommandLabel("configure policy", "/thread policy"),
				Type: "default",
				Value: map[string]any{
					"action":      "thread.policy.menu",
					"session_key": sessionKey,
				},
			},
		)
	}
	buttons = append(buttons, feishu.Button{
		Text: "back",
		Type: "default",
		Value: map[string]any{
			"action":      "menu.root",
			"session_key": sessionKey,
		},
	})
	return BuildConversationThreadsCard(sessionKey, ConversationThreadsCardView{
		Title:          primaryConversationMenuLabel(backend),
		Backend:        backend,
		BodyLines:      lines,
		Buttons:        buttons,
		Items:          items,
		ActiveThreadID: currentThreadID,
		IncludeAll:     includeAll,
	}), nil
}

// ---------------------------------------------------------------------------
// Card rendering: RenderClaudeThreadsCard
// ---------------------------------------------------------------------------

// RenderClaudeThreadsCardForCurrentBackend renders the claude threads card
// for the current backend.
func RenderClaudeThreadsCardForCurrentBackend(app App, sessionKey string, includeAll bool) (map[string]any, error) {
	state := app.ConvBackendState()
	sess := state.Session(sessionKey)
	workspace := app.Config().Workspaces[0]
	if sess != nil {
		if ws := config.FindWorkspace(app.Config(), sess.WorkspaceID); ws != nil {
			workspace = *ws
		}
	}
	return RenderClaudeThreadsCard(app, sessionKey, sess, &workspace, includeAll)
}

// RenderClaudeThreadsCard renders the claude threads card.
func RenderClaudeThreadsCard(app App, sessionKey string, sess *state.Session, ws *config.Workspace, includeAll bool) (map[string]any, error) {
	conversation := app.ConvBackendConversation()
	items, err := conversation.ListClaudeThreads(sessionKey, ws, includeAll)
	if err != nil {
		return nil, err
	}
	appworkspace.SortThreadsByUpdated(items)
	workspaceID := "-"
	if ws != nil {
		workspaceID = firstNonEmpty(strings.TrimSpace(ws.ID), workspaceID)
	}
	currentLabel := "-"
	currentThreadID := "-"
	workspacePermission := "-"
	sessionPermission := "follow workspace"
	effectivePermission := "-"
	if sess != nil {
		currentLabel = currentThreadLabel(sess)
		if strings.TrimSpace(sess.ActiveThreadID) != "" {
			currentThreadID = strings.TrimSpace(sess.ActiveThreadID)
		}
	}
	if ws != nil {
		workspacePermission = claudePermissionModeLabel(effectiveClaudePermissionMode(nil, ws, app.Config().Claude))
		effectivePermission = claudePermissionModeLabel(effectiveClaudePermissionMode(sess, ws, app.Config().Claude))
	}
	if sess != nil && strings.TrimSpace(sess.ActiveClaudePermissionMode) != "" {
		sessionPermission = claudePermissionModeLabel(sess.ActiveClaudePermissionMode)
	}
	scopeLabel := "current workspace"
	if includeAll {
		scopeLabel = "all Claude sessions"
	}
	lines := []string{
		"current backend: `claude`",
		"current session: " + currentLabel,
		"current session id: `" + currentThreadID + "`",
		"workspace: `" + workspaceID + "`",
		"workspace default permission: " + workspacePermission,
		"session override: " + sessionPermission,
		"effective permission: " + effectivePermission,
		"list scope: " + scopeLabel,
		fmt.Sprintf("list count: `%d`", len(items)),
	}
	if len(items) == 0 {
		lines = append(lines, "", "no switchable Claude sessions available.")
	} else {
		lines = append(lines, "", "select a Claude session from the dropdown to switch.")
	}
	lines = append(lines, "", "tip: /session new and /session fork require starting a conversation first to generate a real Claude session and session id.")
	hasActiveSession := sess != nil && strings.TrimSpace(sess.ActiveThreadID) != ""
	if !hasActiveSession {
		lines = append(lines, "", "no active Claude session, so /session fork and /session permissions are not shown.")
	}
	buttons := []feishu.Button{
		{
			Text: commandLabel("new session", "/session new"),
			Type: "default",
			Value: map[string]any{
				"action":        "menu.new",
				"session_key":   sessionKey,
				"parent_action": "menu.thread",
			},
		},
	}
	if hasActiveSession {
		buttons = append(buttons,
			feishu.Button{
				Text: commandLabel("fork session", "/session fork"),
				Type: "default",
				Value: map[string]any{
					"action":        "menu.fork",
					"session_key":   sessionKey,
					"parent_action": "menu.thread",
				},
			},
			feishu.Button{
				Text: submenuCommandLabel("session permissions", "/session permissions"),
				Type: "default",
				Value: map[string]any{
					"action":      "thread.permission_mode.menu",
					"session_key": sessionKey,
				},
			},
		)
	}
	buttons = append(buttons, feishu.Button{
		Text: "back",
		Type: "default",
		Value: map[string]any{
			"action":      "menu.root",
			"session_key": sessionKey,
		},
	})
	return BuildConversationThreadsCard(sessionKey, ConversationThreadsCardView{
		Title:          "session management",
		Backend:        appcore.ConfiguredBackend(app),
		BodyLines:      lines,
		Buttons:        buttons,
		Items:          items,
		ActiveThreadID: currentThreadID,
		IncludeAll:     includeAll,
	}), nil
}

// ---------------------------------------------------------------------------
// Backend capability helpers
// ---------------------------------------------------------------------------

func primaryConversationMenuLabel(backend string) string {
	return backendcaps.ForKind(backend).Conversation.MenuLabel
}

func primaryConversationCurrentLabel(backend string) string {
	return backendcaps.ForKind(backend).CurrentConversationLabel()
}

func primaryConversationIDLabel(backend string) string {
	return backendcaps.ForKind(backend).Conversation.IDLabel
}

// ---------------------------------------------------------------------------
// CodexConversationBackend
// ---------------------------------------------------------------------------

// CodexConversationBackend implements ConversationBackendFacade for the Codex
// backend. It delegates to the ConversationProvider for operations that need
// app-level helpers.
type CodexConversationBackend struct {
	app App
}

// NewCodexConversationBackend creates a new CodexConversationBackend.
func NewCodexConversationBackend(app App) *CodexConversationBackend {
	return &CodexConversationBackend{app: app}
}

func (b *CodexConversationBackend) ListWorkspaceThreads(sessionKey string, ws *config.Workspace, includeAll bool) ([]codexrpc.ThreadListEntry, error) {
	return b.app.ConvBackendConversation().ListCodexThreads(b.app, sessionKey, ws, includeAll)
}

func (b *CodexConversationBackend) EnsureWorkspaceThreadBinding(sessionKey string, sess *state.Session, ws *config.Workspace) (*ThreadBinding, error) {
	return b.app.ConvBackendConversation().EnsureCodexBinding(b.app, sessionKey, sess, ws)
}

func (b *CodexConversationBackend) StartWorkspaceThread(sessionKey string, sess *state.Session, ws *config.Workspace) (*ThreadBinding, error) {
	return b.app.ConvBackendConversation().StartCodexThread(b.app, sessionKey, sess, ws)
}

func (b *CodexConversationBackend) ResumeSelectedThread(sessionKey string, sess *state.Session, ws *config.Workspace, selection ThreadResumeSelection) (*ThreadBinding, error) {
	return b.app.ConvBackendConversation().ResumeCodexThread(b.app, sessionKey, sess, ws, selection)
}

func (b *CodexConversationBackend) ForkActiveConversation(sessionKey string, sess *state.Session, ws *config.Workspace) (string, error) {
	return b.app.ConvBackendConversation().ForkCodexConversation(b.app, sessionKey, sess, ws)
}

func (b *CodexConversationBackend) ForkReplyMessage(string) string {
	return "forked current thread and switched to new branch thread."
}

func (b *CodexConversationBackend) RecoverStartupConversation(sessionKey, workspaceID string, sess *state.Session, ws *config.Workspace, effectiveModel string) {
	b.app.ConvBackendConversation().RecoverCodexStartup(b.app, sessionKey, workspaceID, sess, ws, effectiveModel)
}

func (b *CodexConversationBackend) RenderThreadsCard(sessionKey string, includeAll bool) (map[string]any, error) {
	return RenderCodexThreadsCard(b.app, sessionKey, includeAll)
}

func (b *CodexConversationBackend) HistoryIndexForOrdinal(sessionKey string, ordinal int) (int, error) {
	return b.app.ConvBackendWorkspaceConfig().HistoryIndexForOrdinal(b.app, sessionKey, ordinal)
}

func (b *CodexConversationBackend) RenderHistoryCard(sessionKey string, page int) (map[string]any, error) {
	return b.app.ConvBackendWorkspaceConfig().RenderCodexHistoryCard(b.app, sessionKey, page)
}

func (b *CodexConversationBackend) RenderHistoryDetailCard(sessionKey string, index int) (map[string]any, error) {
	return b.app.ConvBackendWorkspaceConfig().RenderCodexHistoryDetailCard(b.app, sessionKey, index)
}

func (b *CodexConversationBackend) RenderUsageBody(sess *state.Session) string {
	return b.app.ConvBackendWorkspaceConfig().RenderCodexUsageBody(b.app, sess)
}

func (b *CodexConversationBackend) InterruptActiveTurn(ctx context.Context, _ string, sess *state.Session) error {
	return b.app.ConvBackendConversation().InterruptCodexTurn(b.app, ctx, sess)
}

func (b *CodexConversationBackend) ContinueActiveTurn(sessionKey, text string) error {
	return b.app.ConvBackendConversation().ContinueCodexTurn(b.app, sessionKey, text)
}

func (b *CodexConversationBackend) TryReplyContinuation(msg *feishu.InboundMessage, link *state.MessageLink, sessionKey string, sess *state.Session) (bool, error) {
	return b.app.ConvBackendConversation().TryCodexReplyContinuation(b.app, msg, link, sessionKey, sess)
}

func (b *CodexConversationBackend) StartQueuedSubmission(sessionKey string, sess *state.Session, sub *state.Submission, ws *config.Workspace, notifyFailure bool) error {
	return b.app.ConvBackendConversation().StartNextSubmission(b.app, sessionKey, sess, sub, ws, notifyFailure)
}

// ---------------------------------------------------------------------------
// ClaudeConversationBackend
// ---------------------------------------------------------------------------

// ClaudeConversationBackend implements ConversationBackendFacade for the Claude
// backend. It delegates to the ConversationProvider for operations that need
// app-level helpers.
type ClaudeConversationBackend struct {
	app App
}

// NewClaudeConversationBackend creates a new ClaudeConversationBackend.
func NewClaudeConversationBackend(app App) *ClaudeConversationBackend {
	return &ClaudeConversationBackend{app: app}
}

func (b *ClaudeConversationBackend) ListWorkspaceThreads(sessionKey string, ws *config.Workspace, includeAll bool) ([]codexrpc.ThreadListEntry, error) {
	return b.app.ConvBackendConversation().ListClaudeThreads(sessionKey, ws, includeAll)
}

func (b *ClaudeConversationBackend) EnsureWorkspaceThreadBinding(sessionKey string, sess *state.Session, ws *config.Workspace) (*ThreadBinding, error) {
	return b.app.ConvBackendConversation().EnsureClaudeBinding(b.app, sessionKey, sess, ws)
}

func (b *ClaudeConversationBackend) StartWorkspaceThread(sessionKey string, sess *state.Session, ws *config.Workspace) (*ThreadBinding, error) {
	return b.app.ConvBackendConversation().StartClaudeThread(b.app, sessionKey, sess, ws)
}

func (b *ClaudeConversationBackend) ResumeSelectedThread(sessionKey string, sess *state.Session, ws *config.Workspace, selection ThreadResumeSelection) (*ThreadBinding, error) {
	return b.app.ConvBackendConversation().ResumeClaudeThread(b.app, sessionKey, sess, ws, selection)
}

func (b *ClaudeConversationBackend) ForkActiveConversation(sessionKey string, sess *state.Session, ws *config.Workspace) (string, error) {
	return b.app.ConvBackendConversation().ForkClaudeConversation(b.app, sessionKey, sess, ws)
}

func (b *ClaudeConversationBackend) ForkReplyMessage(forkedID string) string {
	if strings.TrimSpace(forkedID) == "" {
		return "prepared to fork current session. new Claude branch session will be created and switched on next message."
	}
	return "forked current session and switched to new branch session."
}

func (b *ClaudeConversationBackend) RecoverStartupConversation(sessionKey, workspaceID string, sess *state.Session, _ *config.Workspace, _ string) {
	b.app.ConvBackendConversation().RecoverClaudeStartup(b.app, sessionKey, workspaceID, sess)
}

func (b *ClaudeConversationBackend) RenderThreadsCard(sessionKey string, includeAll bool) (map[string]any, error) {
	return RenderClaudeThreadsCardForCurrentBackend(b.app, sessionKey, includeAll)
}

func (b *ClaudeConversationBackend) HistoryIndexForOrdinal(sessionKey string, ordinal int) (int, error) {
	return b.app.ConvBackendWorkspaceConfig().HistoryTurnIndexForOrdinal(b.app, sessionKey, ordinal)
}

func (b *ClaudeConversationBackend) RenderHistoryCard(sessionKey string, page int) (map[string]any, error) {
	return b.app.ConvBackendWorkspaceConfig().RenderClaudeHistoryCard(b.app, sessionKey, page)
}

func (b *ClaudeConversationBackend) RenderHistoryDetailCard(sessionKey string, index int) (map[string]any, error) {
	return b.app.ConvBackendWorkspaceConfig().RenderClaudeHistoryDetailCard(b.app, sessionKey, index)
}

func (b *ClaudeConversationBackend) RenderUsageBody(sess *state.Session) string {
	return b.app.ConvBackendWorkspaceConfig().RenderClaudeUsageBody(b.app, sess)
}

func (b *ClaudeConversationBackend) InterruptActiveTurn(ctx context.Context, sessionKey string, _ *state.Session) error {
	return b.app.ConvBackendConversation().InterruptClaudeTurn(b.app, ctx, sessionKey)
}

func (b *ClaudeConversationBackend) ContinueActiveTurn(sessionKey, text string) error {
	return b.app.ConvBackendConversation().ContinueClaudeTurn(b.app, sessionKey, text)
}

func (b *ClaudeConversationBackend) TryReplyContinuation(msg *feishu.InboundMessage, link *state.MessageLink, sessionKey string, sess *state.Session) (bool, error) {
	return b.app.ConvBackendConversation().TryClaudeReplyContinuation(b.app, msg, link, sessionKey, sess)
}

func (b *ClaudeConversationBackend) StartQueuedSubmission(sessionKey string, sess *state.Session, sub *state.Submission, ws *config.Workspace, notifyFailure bool) error {
	return b.app.ConvBackendConversation().StartNextSubmission(b.app, sessionKey, sess, sub, ws, notifyFailure)
}

// ---------------------------------------------------------------------------
// Service — manages conversation backend operations
// ---------------------------------------------------------------------------

// Service manages conversation backend operations for a single app instance.
type Service struct {
	app App
}

// NewService creates a new conversation backend service bound to the given app.
func NewService(app App) *Service {
	return &Service{app: app}
}

// App returns the app interface.
func (s *Service) App() App {
	return s.app
}

// NewCodexBackend creates a new codex conversation backend for the service.
func (s *Service) NewCodexBackend() *CodexConversationBackend {
	return NewCodexConversationBackend(s.app)
}

// NewClaudeBackend creates a new claude conversation backend for the service.
func (s *Service) NewClaudeBackend() *ClaudeConversationBackend {
	return NewClaudeConversationBackend(s.app)
}
