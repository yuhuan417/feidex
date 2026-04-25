package app

import (
	"fmt"
	"path/filepath"
	"strings"

	appcards "feidex/internal/app/cards"
	"feidex/internal/app/appcore"
	apppathpick "feidex/internal/app/pathpick"
	appworkspace "feidex/internal/app/workspace"
	appworkspacecmd "feidex/internal/app/workspacecmd"
	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

// ---------------------------------------------------------------------------
// Indirect function references to break initialization cycles.
// Set in init() so adapter constructors don't statically reference
// functions that participate in the menu rendering cycle.
// ---------------------------------------------------------------------------

var indirectCompleteMenuCommand func(a *App, action *feishu.CardAction, sessionKey, rawCommand, parentAction string) (*callback.CardActionTriggerResponse, error)
var indirectReplyCommandActionResponse func(a *App, msg *feishu.InboundMessage, resp *callback.CardActionTriggerResponse) error

func init() {
	indirectCompleteMenuCommand = func(a *App, action *feishu.CardAction, sessionKey, rawCommand, parentAction string) (*callback.CardActionTriggerResponse, error) {
		return completeMenuCommand(a, action, sessionKey, rawCommand, parentAction)
	}
	indirectReplyCommandActionResponse = func(a *App, msg *feishu.InboundMessage, resp *callback.CardActionTriggerResponse) error {
		return replyCommandActionResponse(a, msg, resp)
	}
}

// ---------------------------------------------------------------------------
// Adapter constructors — wire workspacecmd service structs to app/ callbacks.
// These return the raw workspacecmd service types; the original files wrap
// them in local structs that provide lowercase method names.
// ---------------------------------------------------------------------------

func newWorkspaceConfigServiceInner(a *App) *appworkspacecmd.ConfigService {
	st := appState(a)
	bcfg := newBackendConfigurationService(a)
	return &appworkspacecmd.ConfigService{
		App: a,

		// State callbacks
		GetSession:    func(key string) *state.Session { return st.session(key) },
		Sessions:      func() []*state.Session { return st.sessions() },
		SaveSession:   func(sess *state.Session) error { return st.saveSession(sess) },
		NextLocalID:   func(prefix string) (string, error) { return st.nextLocalID(prefix) },
		Pending:       func(id string) *state.PendingRequest { return st.pending(id) },
		SavePending:   func(req *state.PendingRequest) error { return st.savePending(req) },
		UpdatePending: func(id string, mutate func(*state.PendingRequest)) error { return st.updatePending(id, mutate) },

		// Session context callbacks
		SessionHasInFlight:     sessionHasInFlightSubmission,
		SwitchSessionWorkspace: switchSessionWorkspace,
		ClearSessionThreadCtx:  clearSessionThreadContext,
		ClearSessionLiveThread: func(sessionKey string) { clearSessionLiveThread(a, sessionKey) },

		// Thread callbacks
		EnsureWorkspaceThreadBinding: func(sessionKey string, sess *state.Session, ws *config.Workspace) (*appworkspacecmd.ThreadBinding, error) {
			return newWorkspaceThreadServiceInner(a).EnsureWorkspaceThreadBinding(sessionKey, sess, ws)
		},

		// Backend config callbacks
		BackendWorkspaceSummaryLines:               bcfg.appendBackendWorkspaceSummaryLines,
		BackendWorkspaceConfigButtons:              bcfg.backendWorkspaceConfigButtons,
		BackendWorkspaceSwitchBindingNotice:        bcfg.backendWorkspaceSwitchBindingNotice,
		BackendWorkspaceSwitchBindingFailureNotice: bcfg.backendWorkspaceSwitchBindingFailureNotice,
		BackendWorkspaceSwitchInFlightNotice:       bcfg.backendWorkspaceSwitchInFlightNotice,
		BackendWorkspaceCommandUsage:               bcfg.backendWorkspaceCommandUsage,
		BackendWorkspacePermissionCommand:          bcfg.handleBackendWorkspacePermissionCommand,

		// Action callbacks
		CompleteMenuCommand: func(action *feishu.CardAction, sessionKey, rawCommand, parentAction string) (*callback.CardActionTriggerResponse, error) {
			return indirectCompleteMenuCommand(a, action, sessionKey, rawCommand, parentAction)
		},
		ReplyCommandActionResponse: func(msg *feishu.InboundMessage, resp *callback.CardActionTriggerResponse) error {
			return indirectReplyCommandActionResponse(a, msg, resp)
		},
		CommandActionFromMessage: commandActionFromMessage,

		// Formatting
		FormatMenuBody: menuCardBody,

		// Render callbacks
		RenderMenuCard:                func(sessionKey string) map[string]any { return newWorkspaceRenderServiceInner(a).RenderWorkspaceMenuCard(sessionKey) },
		RenderSandboxMenuCard:         func(sessionKey string) (map[string]any, error) { return newWorkspaceRenderServiceInner(a).RenderWorkspaceSandboxMenuCard(sessionKey) },
		RenderPolicyMenuCard:          func(sessionKey string) (map[string]any, error) { return newWorkspaceRenderServiceInner(a).RenderWorkspacePolicyMenuCard(sessionKey) },
		RenderDeleteMenuCard:          func(sessionKey string) (map[string]any, error) { return newWorkspaceRenderServiceInner(a).RenderWorkspaceDeleteMenuCard(sessionKey) },
		RenderDeleteConfirmCard:       func(sessionKey, workspaceID string) (map[string]any, error) { return newWorkspaceRenderServiceInner(a).RenderWorkspaceDeleteConfirmCard(sessionKey, workspaceID) },
		RenderCloneSwitchExistingCard: func(sessionKey, workspaceID, targetDir string) map[string]any { return newWorkspaceRenderServiceInner(a).RenderWorkspaceCloneSwitchExistingCard(sessionKey, workspaceID, targetDir) },
	}
}

func newWorkspaceManagementServiceInner(a *App) *appworkspacecmd.ManagementService {
	st := appState(a)
	bcfg := newBackendConfigurationService(a)
	return &appworkspacecmd.ManagementService{
		App: a,

		// State callbacks
		GetSession:    func(key string) *state.Session { return st.session(key) },
		Sessions:      func() []*state.Session { return st.sessions() },
		SaveSession:   func(sess *state.Session) error { return st.saveSession(sess) },
		NextLocalID:   func(prefix string) (string, error) { return st.nextLocalID(prefix) },
		Pending:       func(id string) *state.PendingRequest { return st.pending(id) },
		SavePending:   func(req *state.PendingRequest) error { return st.savePending(req) },
		UpdatePending: func(id string, mutate func(*state.PendingRequest)) error { return st.updatePending(id, mutate) },

		// Session context callbacks
		SessionHasInFlight:     sessionHasInFlightSubmission,
		SwitchSessionWorkspace: switchSessionWorkspace,
		ClearSessionThreadCtx:  clearSessionThreadContext,
		SetSessionThreadCtx:    setSessionThreadContext,
		SessionResetActiveOps:  sessionResetActiveOperations,

		// Thread callbacks
		EnsureWorkspaceThreadBinding: func(sessionKey string, sess *state.Session, ws *config.Workspace) (*appworkspacecmd.ThreadBinding, error) {
			return newWorkspaceThreadServiceInner(a).EnsureWorkspaceThreadBinding(sessionKey, sess, ws)
		},
		MarkSessionThreadLive:  func(sessionKey, threadID string) { markSessionThreadLive(a, sessionKey, threadID) },
		ClearSessionLiveThread: func(sessionKey string) { clearSessionLiveThread(a, sessionKey) },
		StartWorkspaceThread: func(sessionKey string, sess *state.Session, ws *config.Workspace) (*appworkspacecmd.ThreadBinding, error) {
			return newWorkspaceThreadServiceInner(a).StartWorkspaceThread(sessionKey, sess, ws)
		},

		// Clone operation callbacks
		SetCloneOp: func(requestID string, op *appworkspacecmd.CloneOperation) {
			if a == nil {
				return
			}
			requestID = strings.TrimSpace(requestID)
			if requestID == "" || op == nil {
				return
			}
			tracker := a.trackers.workspaceCloneOps
			if tracker == nil {
				tracker = newWorkspaceCloneTracker()
				a.trackers.workspaceCloneOps = tracker
			}
			tracker.Mu.Lock()
			defer tracker.Mu.Unlock()
			if tracker.Ops == nil {
				tracker.Ops = map[string]*appworkspacecmd.CloneOperation{}
			}
			if previous := tracker.Ops[requestID]; previous != nil && previous.Cancel != nil && previous != op {
				previous.Cancel()
			}
			tracker.Ops[requestID] = op
		},
		GetCloneOp: func(requestID string) *appworkspacecmd.CloneOperation {
			if a == nil {
				return nil
			}
			tracker := a.trackers.workspaceCloneOps
			if tracker == nil {
				return nil
			}
			tracker.Mu.Lock()
			defer tracker.Mu.Unlock()
			return tracker.Ops[strings.TrimSpace(requestID)]
		},
		ClearCloneOp: func(requestID string) {
			if a == nil {
				return
			}
			tracker := a.trackers.workspaceCloneOps
			if tracker == nil {
				return
			}
			tracker.Mu.Lock()
			defer tracker.Mu.Unlock()
			delete(tracker.Ops, strings.TrimSpace(requestID))
		},
		GitClone: workspaceGitClone,

		// Codex client callbacks
		RequireCodexClient: func() (appworkspacecmd.CodexClient, error) { return requireCodexClient(a) },
		BuildThreadStartParams: func(ws *config.Workspace, sess *state.Session, effectiveModel string) map[string]any {
			return buildThreadStartParams(a, ws, sess, effectiveModel)
		},

		// Backend config callbacks
		BackendWorkspaceSwitchBindingNotice:        bcfg.backendWorkspaceSwitchBindingNotice,
		BackendWorkspaceSwitchBindingFailureNotice: bcfg.backendWorkspaceSwitchBindingFailureNotice,
		BackendWorkspaceSwitchInFlightNotice:       bcfg.backendWorkspaceSwitchInFlightNotice,
		BackendWorkspaceCommandUsage:               bcfg.backendWorkspaceCommandUsage,
		BackendWorkspacePermissionCommand:          bcfg.handleBackendWorkspacePermissionCommand,

		// Action callbacks
		CompleteMenuCommand: func(action *feishu.CardAction, sessionKey, rawCommand, parentAction string) (*callback.CardActionTriggerResponse, error) {
			return indirectCompleteMenuCommand(a, action, sessionKey, rawCommand, parentAction)
		},
		ReplyCommandActionResponse: func(msg *feishu.InboundMessage, resp *callback.CardActionTriggerResponse) error {
			return indirectReplyCommandActionResponse(a, msg, resp)
		},
		CommandActionFromMessage: commandActionFromMessage,
		CommandMessageFromAction: func(action *feishu.CardAction, sessionKey, rawCommand string) *feishu.InboundMessage {
			return commandMessageFromAction(a, action, sessionKey, rawCommand)
		},

		// Formatting
		FormatMenuBody: menuCardBody,

		// Render callbacks
		RenderNewCard: func(sessionKey, requestID string, payload appworkspacecmd.NewPayload) map[string]any {
			return newWorkspaceRenderServiceInner(a).RenderWorkspaceNewCard(sessionKey, requestID, payload)
		},
		RenderCloneCard: func(sessionKey, requestID string, payload appworkspacecmd.ClonePayload) map[string]any {
			return newWorkspaceRenderServiceInner(a).RenderWorkspaceCloneCard(sessionKey, requestID, payload)
		},
		RenderClonePreparingCard: func(requestID string, payload appworkspacecmd.ClonePayload, parentDir string, snapshot appworkspacecmd.CloneProgressSnapshot) map[string]any {
			return newWorkspaceRenderServiceInner(a).RenderWorkspaceClonePreparingCard(requestID, payload, parentDir, snapshot)
		},
		RenderCloneSuccessCard: func(sessionKey, workspaceID, targetDir string) map[string]any {
			return newWorkspaceRenderServiceInner(a).RenderWorkspaceCloneSuccessCard(sessionKey, workspaceID, targetDir)
		},
		RenderSwitchExistingCard: func(sessionKey, workspaceID, targetDir, notice string) map[string]any {
			return newWorkspaceRenderServiceInner(a).RenderWorkspaceSwitchExistingCard(sessionKey, workspaceID, targetDir, notice)
		},
		RenderCloneSwitchExistingCard: func(sessionKey, workspaceID, targetDir string) map[string]any {
			return newWorkspaceRenderServiceInner(a).RenderWorkspaceCloneSwitchExistingCard(sessionKey, workspaceID, targetDir)
		},
		RenderCloneManualHintCard: func(sessionKey, workspaceID, targetDir, errText string) map[string]any {
			return newWorkspaceRenderServiceInner(a).RenderWorkspaceCloneManualHintCard(sessionKey, workspaceID, targetDir, errText)
		},
		RenderCloneCanceledCard: func(sessionKey string, payload appworkspacecmd.ClonePayload, parentDir string, snapshot appworkspacecmd.CloneProgressSnapshot) map[string]any {
			return newWorkspaceRenderServiceInner(a).RenderWorkspaceCloneCanceledCard(sessionKey, payload, parentDir, snapshot)
		},
		RenderMenuCard: func(sessionKey string) map[string]any { return newWorkspaceRenderServiceInner(a).RenderWorkspaceMenuCard(sessionKey) },
	}
}

// renderPathPickerCardDirect implements the full path picker card rendering
// without going through the RenderService callback (which would be recursive).
func renderPathPickerCardDirect(requestID string, payload appworkspace.PathPickerPayload) (map[string]any, error) {
	mode := apppathpick.NormalizePathPickerMode(payload.Mode)
	_ = apppathpick.NormalizePathPickerStyle(payload.Style) // style is always dropdown
	payload.Mode = mode

	currentPath, err := apppathpick.ResolvePathPickerPath(payload.RootPath, payload.CurrentPath)
	if err != nil {
		return nil, err
	}
	payload.CurrentPath = currentPath
	if strings.TrimSpace(payload.SelectedPath) != "" {
		selectedPath, err := apppathpick.ResolvePathPickerPath(payload.RootPath, payload.SelectedPath)
		if err == nil {
			payload.SelectedPath = selectedPath
		} else {
			payload.SelectedPath = ""
		}
	}

	entries, total, hiddenFiles, _, err := apppathpick.ListPathPickerEntries(payload)
	if err != nil {
		return nil, err
	}

	title := "路径选择器"
	if mode == appworkspace.PathPickerModeDirectory {
		title += " · 目录"
	} else {
		title += " · 文件"
	}
	card := appcards.NewMarkdownBodyCard(title, "blue")
	lines := []string{
		"浏览根目录: `" + payload.RootPath + "`",
		"当前目录: `" + payload.CurrentPath + "`",
	}
	if strings.TrimSpace(payload.SelectedPath) != "" {
		lines = append(lines, "已选择: `"+payload.SelectedPath+"`")
	}
	lines = append(lines, fmt.Sprintf("当前目录条目: `%d`", total))
	if mode == appworkspace.PathPickerModeDirectory && hiddenFiles > 0 {
		lines = append(lines, fmt.Sprintf("已隐藏文件: `%d`", hiddenFiles))
	}
	appcards.AppendMarkdownBodyCardElement(card, map[string]any{
		"tag":     "markdown",
		"content": strings.Join(lines, "\n"),
	})

	// Build dropdown element
	placeholder := "选择条目"
	if mode == appworkspace.PathPickerModeDirectory {
		placeholder = "选择子目录并进入"
	}
	options := make([]map[string]any, 0, len(entries))
	initialOption := ""
	if filepath.Clean(payload.CurrentPath) != filepath.Clean(payload.RootPath) {
		parentPath := filepath.Dir(payload.CurrentPath)
		options = append(options, map[string]any{
			"text":  map[string]any{"tag": "plain_text", "content": "../"},
			"value": apppathpick.EncodeOption(appworkspace.PathPickerEntry{Name: "..", Path: parentPath, IsDir: true}),
		})
	}
	for _, entry := range entries {
		value := apppathpick.EncodeOption(entry)
		options = append(options, map[string]any{
			"text":  map[string]any{"tag": "plain_text", "content": apppathpick.RenderEntryLabel(entry)},
			"value": value,
		})
		if filepath.Clean(payload.SelectedPath) == filepath.Clean(entry.Path) {
			initialOption = value
		}
	}
	dropdown := map[string]any{
		"tag":         "select_static",
		"placeholder": map[string]any{"tag": "plain_text", "content": placeholder},
		"options":     options,
		"name":        "path_picker_select",
		"behaviors": []map[string]any{{
			"type": "callback",
			"value": map[string]any{
				"action":     "path_picker.dropdown",
				"request_id": requestID,
			},
		}},
	}
	if initialOption != "" {
		dropdown["initial_option"] = initialOption
	}
	appcards.AppendMarkdownBodyCardElement(card, dropdown)

	if len(entries) == 0 {
		appcards.AppendMarkdownBodyCardElement(card, map[string]any{
			"tag":     "markdown",
			"content": "当前目录下没有可显示的条目。",
		})
	}

	// Footer buttons
	buttons := []feishu.Button{
		{
			Text: "上一级",
			Type: "default",
			Value: map[string]any{
				"action":     "path_picker.up",
				"request_id": requestID,
			},
		},
	}
	confirmType := "default"
	if mode == appworkspace.PathPickerModeDirectory || strings.TrimSpace(payload.SelectedPath) != "" {
		confirmType = "primary"
	}
	buttons = append(buttons,
		feishu.Button{
			Text: "确认",
			Type: confirmType,
			Value: map[string]any{
				"action":     "path_picker.confirm",
				"request_id": requestID,
			},
		},
		feishu.Button{
			Text: "取消",
			Type: "default",
			Value: map[string]any{
				"action":     "path_picker.cancel",
				"request_id": requestID,
			},
		},
	)
	appcards.AppendMarkdownBodyCardElement(card, appcards.BuildMarkdownBodyCardActionElement(buttons))

	return card, nil
}

func newWorkspaceRenderServiceInner(a *App) *appworkspacecmd.RenderService {
	bcfg := newBackendConfigurationService(a)
	return &appworkspacecmd.RenderService{
		App: a,

		// Session state
		GetSession: func(key string) *state.Session { return appState(a).session(key) },

		// Backend config
		BackendWorkspaceSummaryLines:  bcfg.appendBackendWorkspaceSummaryLines,
		BackendWorkspaceConfigButtons: bcfg.backendWorkspaceConfigButtons,

		// Formatting
		FormatMenuBody: menuCardBody,

		// Path picker render callback
		RenderPathPickerCard: func(requestID string, payload appworkspacecmd.PathPickerPayload) (map[string]any, error) {
			return renderPathPickerCardDirect(requestID, payload)
		},

		// Management helpers
		DefaultWorkspaceCloneRoot:   func(ws *config.Workspace) string { return "/" },
		DefaultWorkspaceCloneParent: func(ws *config.Workspace) string {
			if ws != nil && strings.TrimSpace(ws.Cwd) != "" {
				return filepath.Dir(strings.TrimSpace(ws.Cwd))
			}
			if cp := strings.TrimSpace(a.ConfigPath()); cp != "" {
				return filepath.Dir(cp)
			}
			return "."
		},
	}
}

func newWorkspaceThreadServiceInner(a *App) *appworkspacecmd.ThreadService {
	st := appState(a)
	return &appworkspacecmd.ThreadService{
		App: a,

		// State callbacks
		GetSession:  func(key string) *state.Session { return st.session(key) },
		SaveSession: func(sess *state.Session) error { return st.saveSession(sess) },

		// Thread callbacks
		MarkSessionThreadLive: func(sessionKey, threadID string) { markSessionThreadLive(a, sessionKey, threadID) },

		// Session context callbacks
		SessionHasInFlight:     sessionHasInFlightSubmission,
		SwitchSessionWorkspace: switchSessionWorkspace,
		ClearSessionThreadCtx:  clearSessionThreadContext,
		SetSessionThreadCtx:    setSessionThreadContext,
		SessionResetActiveOps:  sessionResetActiveOperations,

		// Codex client callbacks
		RequireCodexClient: func() (appworkspacecmd.CodexClient, error) { return requireCodexClient(a) },
		BuildThreadStartParams: func(ws *config.Workspace, sess *state.Session, effectiveModel string) map[string]any {
			return buildThreadStartParams(a, ws, sess, effectiveModel)
		},

		// Claude client callbacks
		RequireClaudeCore: func() (appcore.ClaudeCore, error) { return a.Claude(), nil },
	}
}


