package feishuapp

import (
	appfeatures "feidex/internal/application/features"
	"feidex/internal/application/submission"
	domainbackend "feidex/internal/domain/backend"
	"feidex/internal/domain/conversation"
	"feidex/internal/textutil"
	"fmt"
	"strings"

	appdebugview "feidex/internal/adapter/feishu/debugview"
	"feidex/internal/feishu"
)

func HandleInboundCommand(a *App, msg *feishu.InboundMessage, raw string) error {
	raw = strings.TrimSpace(raw)
	fields := strings.Fields(raw)
	if len(fields) == 0 {
		return nil
	}
	spec := findLocalCommandSpec(fields[0])
	if spec == nil {
		return fmt.Errorf("unknown command: %s", fields[0])
	}
	if !a.configView().hasConfiguredBackend() && !commandAllowedWithoutBackend(msg, fields[0]) {
		return a.bindings.BackendSelection.ReplyBackendSelectionCard(msg, "")
	}
	backend := a.configView().configuredBackend()
	if reason := a.runtimeOwner.BackendTransition.BackendSwitchBlockedReasonForTraffic(); reason != "" {
		return conversation.NewWarning(reason)
	}
	if runtime := backendRuntime(backend); runtime != nil {
		if err := runtime.MaintenanceBlocksCommand(backendRuntimeContextForApp(a.BackendRuntimeDeps()), raw); err != nil {
			return err
		}
	}
	if !commandHandlesLocallyForBackend(spec, backend, fields) {
		return enqueuePassthroughCommand(a.bindings.Submissions, a.configView().makeSessionKey(msg), msg, raw)
	}
	if spec.HandleRaw != nil {
		return spec.HandleRaw(a, msg, raw, fields[1:])
	}
	return spec.Handle(a, msg, fields[1:])
}

func commandAllowedWithoutBackend(msg *feishu.InboundMessage, name string) bool {
	chatType := ""
	if msg != nil {
		chatType = msg.ChatType
	}
	return appfeatures.CommandAllowedWithoutBackend(chatType, name)
}

func enqueuePassthroughCommand(submissions *submission.SubmissionQueueService, sessionKey string, msg *feishu.InboundMessage, raw string) error {
	if submissions == nil || msg == nil {
		return nil
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	cloned := *msg
	cloned.Text = raw
	return enqueueSubmissionWithSessionKey(submissions, &cloned, sessionKey, false)
}

func isLocalCommandForBackend(backend, raw string) bool {
	return appfeatures.HandlesCommand(backend, raw)
}
func isLocalCommandForMessage(backend string, msg *feishu.InboundMessage, raw string) bool {
	chatType := ""
	if msg != nil {
		chatType = msg.ChatType
	}
	return appfeatures.HandlesMessageCommand(backend, chatType, raw)
}

func isLocalCommand(raw string) bool {
	return isLocalCommandForBackend(domainbackend.BackendCodex, raw)
}

func commandHelp(a *App, msg *feishu.InboundMessage, args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("usage: /help")
	}
	sessionKey := a.configView().makeSessionKey(msg)
	card := renderHelpCardData(a.configView().configuredBackend(), planModeTitleForSession(a, sessionKey, "帮助说明"), a.feishu, a.bindings.BindingCommands.scope, sessionKey)
	return replyCardEffect(a, msg, card)
}

func renderCommandMenuCardData(backend, title string, renderer bindingCardRenderer, sessionKey string) map[string]any {
	return renderer.SimpleStatusCard(title, "blue", menuCardBody("menu.root", "选择功能分组。"), renderRootMenuButtons(backend, sessionKey))
}

func renderToolsMenuCardData(backend, title string, renderer bindingCardRenderer, sessionKey string) map[string]any {
	spec, _ := menuGroupSpec("menu.tools")
	return renderer.SimpleStatusCard(title, "blue", menuCardBody(spec.Action, spec.Description), renderGroupMenuButtons(backend, spec.Action, sessionKey))
}

func renderSystemMenuCardData(backend, title string, renderer bindingCardRenderer, sessionKey string) map[string]any {
	spec, _ := menuGroupSpec("menu.group.system")
	configuredBackend := backend
	backend = textutil.FirstNonEmpty(backend, "unset")
	body := spec.Description + "\n\n当前 backend: `" + backend + "`\n当前 slog 日志级别: " + appdebugview.RenderRuntimeLogLevelValue() + "\n当前版本: `" + currentVersion() + "`"
	return renderer.SimpleStatusCard(title, "blue", menuCardBodyForBackend(configuredBackend, spec.Action, body), renderGroupMenuButtons(configuredBackend, spec.Action, sessionKey))
}

func renderBackendMenuCardData(backend, title string, renderer bindingCardRenderer, sessionKey string) map[string]any {
	spec, _ := menuGroupSpec("menu.group.backend")
	configuredBackend := backend
	backend = textutil.FirstNonEmpty(backend, "unset")
	body := spec.Description + "\n\n当前 backend: `" + backend + "`"
	return renderer.SimpleStatusCard(title, "blue", menuCardBodyForBackend(configuredBackend, spec.Action, body), renderGroupMenuButtons(configuredBackend, spec.Action, sessionKey))
}

func renderHelpCardData(backend, title string, renderer bindingCardRenderer, scope bindingSessionScope, sessionKey string) map[string]any {
	buttons := []feishu.Button{
		{Text: feishu.MenuBackButtonText, Type: "default", Value: map[string]any{"action": "menu.group.system", "session_key": sessionKey}},
	}
	return renderer.SimpleStatusCard(title, "blue", menuCardBody("menu.help", renderHelpBodyForSession(scope, backend, sessionKey)), buttons)
}
