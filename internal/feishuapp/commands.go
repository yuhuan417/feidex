package feishuapp

import (
	appdebugview "feidex/internal/adapter/feishu/debugview"
	appmenuutil "feidex/internal/adapter/feishu/menuutil"
	"feidex/internal/adapter/feishu/planmode"
	appfeatures "feidex/internal/application/features"
	"feidex/internal/application/submission"
	domainbackend "feidex/internal/domain/backend"
	"feidex/internal/feishu"
	frontendruntime "feidex/internal/runtime"
	"feidex/internal/textutil"
	"fmt"
	"strings"
)

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

func CommandPassthroughQueue(submissions *submission.SubmissionQueueService) func(string, *feishu.InboundMessage, string) error {
	return func(sessionKey string, msg *feishu.InboundMessage, raw string) error {
		return enqueuePassthroughCommand(submissions, sessionKey, msg, raw)
	}
}

func CommandMaintenanceBlocker(deps BackendRuntimeDeps) func(string, string) error {
	return func(backend, raw string) error {
		runtime := backendRuntime(backend)
		if runtime == nil {
			return nil
		}
		return runtime.MaintenanceBlocksCommand(backendRuntimeContextForApp(deps.currentBackend()), raw)
	}
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

func handleHelpCommand(scope bindingSessionScope, backend func() string, makeSessionKey func(*feishu.InboundMessage) string, state planmode.StateProvider, runner frontendruntime.EffectRunner, frontendID string, replyInThread bool, msg *feishu.InboundMessage, args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("usage: /help")
	}
	sessionKey := makeSessionKey(msg)
	backendKind := backend()
	card := renderHelpCardData(backendKind, planModeTitleForSession(state, state != nil, sessionKey, "帮助说明"), scope, sessionKey)
	return replyCardEffect(runner, frontendID, replyInThread, msg, card)
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

func renderHelpCardData(backend, title string, scope bindingSessionScope, sessionKey string) map[string]any {
	return appmenuutil.PageCard{
		Node: "menu.help", SessionKey: sessionKey, Title: title, Color: "blue",
		Body: renderHelpBodyForSession(scope, backend, sessionKey),
	}.Render()
}
