package app

import (
	"context"
	"fmt"
	"strings"

	appdebugview "feidex/internal/app/debugview"
	"feidex/internal/feishu"
)

func handleCommand(a *App, msg *feishu.InboundMessage, raw string) error {
	raw = strings.TrimSpace(raw)
	fields := strings.Fields(raw)
	if len(fields) == 0 {
		return nil
	}
	spec := findLocalCommandSpec(fields[0])
	if spec == nil {
		return fmt.Errorf("unknown command: %s", fields[0])
	}
	if !hasConfiguredBackend(a) && fields[0] != "/backend" && fields[0] != "/bind" {
		return newBackendSelectionService(a).replyBackendSelectionCard(msg, "")
	}
	backend := configuredBackend(a)
	if reason := newRuntimeStateService(a).backendSwitchBlockedReasonForTraffic(); reason != "" {
		return newUIWarningError(reason)
	}
	if runtime := backendRuntime(a); runtime != nil {
		if err := runtime.maintenanceBlocksCommand(a, raw); err != nil {
			return err
		}
	}
	if !commandHandlesLocallyForBackend(spec, backend, fields) {
		return enqueuePassthroughCommand(a, msg, raw)
	}
	if spec.HandleRaw != nil {
		return spec.HandleRaw(a, msg, raw, fields[1:])
	}
	return spec.Handle(a, msg, fields[1:])
}

func enqueuePassthroughCommand(a *App, msg *feishu.InboundMessage, raw string) error {
	if a == nil || msg == nil {
		return nil
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	cloned := *msg
	cloned.Text = raw
	return enqueueSubmission(a, &cloned)
}

func isLocalCommandForBackend(backend, raw string) bool {
	raw = strings.TrimSpace(raw)
	fields := strings.Fields(raw)
	if len(fields) == 0 {
		return false
	}
	spec := findLocalCommandSpec(fields[0])
	if spec == nil {
		return false
	}
	return commandHandlesLocallyForBackend(spec, backend, fields)
}

func isLocalCommand(raw string) bool {
	return isLocalCommandForBackend(backendCodex, raw)
}

func commandHelp(a *App, msg *feishu.InboundMessage, args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("usage: /help")
	}
	card := renderHelpCard(a, makeSessionKey(a, msg))
	_, err := a.feishu.ReplyCard(context.Background(), msg.MessageID, card, replyInThreadEnabled(a, msg.ChatType))
	return err
}

func renderToolsMenuCard(a *App, sessionKey string) map[string]any {
	spec, _ := menuGroupSpec("menu.tools")
	return a.feishu.SimpleStatusCard(planModeTitleForSession(a, sessionKey, spec.Label), "blue", menuCardBodyForSession(a, sessionKey, spec.Action, spec.Description), renderGroupMenuButtons(configuredBackend(a), spec.Action, sessionKey))
}

func renderCurrentBotMenu(a *App, sessionKey string) map[string]any {
	return renderCurrentBotMenuCard(a, sessionKey)
}

func renderSessionMenuCard(a *App, sessionKey string) map[string]any {
	return renderToolsMenuCard(a, sessionKey)
}

func renderContextMenuCard(a *App, sessionKey string) map[string]any {
	return renderCommandMenuCard(a, sessionKey)
}

func renderSystemMenuCard(a *App, sessionKey string) map[string]any {
	spec, _ := menuGroupSpec("menu.group.system")
	backend := firstNonEmpty(configuredBackend(a), "unset")
	body := spec.Description + "\n\n当前 backend: `" + backend + "`\n当前 slog 日志级别: " + appdebugview.RenderRuntimeLogLevelValue() + "\n当前版本: `" + currentVersion() + "`"
	return a.feishu.SimpleStatusCard(planModeTitleForSession(a, sessionKey, spec.Label), "blue", menuCardBodyForBackendForSession(a, sessionKey, configuredBackend(a), spec.Action, body), renderGroupMenuButtons(configuredBackend(a), spec.Action, sessionKey))
}

func renderBackendMenuCard(a *App, sessionKey string) map[string]any {
	spec, _ := menuGroupSpec("menu.group.backend")
	backend := firstNonEmpty(configuredBackend(a), "unset")
	body := spec.Description + "\n\n当前 backend: `" + backend + "`"
	return a.feishu.SimpleStatusCard(planModeTitleForSession(a, sessionKey, spec.Label), "blue", menuCardBodyForBackendForSession(a, sessionKey, configuredBackend(a), spec.Action, body), renderGroupMenuButtons(configuredBackend(a), spec.Action, sessionKey))
}

func renderHelpCard(a *App, sessionKey string) map[string]any {
	buttons := []feishu.Button{
		{Text: "返回上一级", Type: "default", Value: map[string]any{"action": "menu.group.system", "session_key": sessionKey}},
	}
	return a.feishu.SimpleStatusCard(planModeTitleForSession(a, sessionKey, "帮助说明"), "blue", menuCardBody("menu.help", renderHelpBodyFromRegistry(configuredBackend(a))), buttons)
}
