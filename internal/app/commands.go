package app

import (
	"context"
	"fmt"
	"strings"

	"feidex/internal/feishu"
)

func (s commandService) handleCommand(msg *feishu.InboundMessage, raw string) error {
	raw = strings.TrimSpace(raw)
	fields := strings.Fields(raw)
	if len(fields) == 0 {
		return nil
	}
	spec := findLocalCommandSpec(fields[0])
	if spec == nil {
		return fmt.Errorf("unknown command: %s", fields[0])
	}
	if !hasConfiguredBackend(s.app) && fields[0] != "/backend" {
		return newBackendSelectionService(s.app).replyBackendSelectionCard(msg, "")
	}
	backend := configuredBackend(s.app)
	if err := s.app.handleBackendMaintenanceBlock(raw); err != nil {
		return err
	}
	if !commandHandlesLocallyForBackend(spec, backend, fields) {
		return newCommandService(s.app).enqueuePassthroughCommand(msg, raw)
	}
	return spec.Handle(s.app, msg, fields[1:])
}

func (s commandService) enqueuePassthroughCommand(msg *feishu.InboundMessage, raw string) error {
	if s.app == nil || msg == nil {
		return nil
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	cloned := *msg
	cloned.Text = raw
	return enqueueSubmission(s.app, &cloned)
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

func (s commandService) commandHelp(msg *feishu.InboundMessage, args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("usage: /help")
	}
	card := newCommandService(s.app).renderHelpCard(makeSessionKey(s.app, msg))
	_, err := s.app.feishu.ReplyCard(context.Background(), msg.MessageID, card, replyInThreadEnabled(s.app, msg.ChatType))
	return err
}

func (s commandService) renderToolsMenuCard(sessionKey string) map[string]any {
	spec, _ := menuGroupSpec("menu.tools")
	return s.app.feishu.SimpleStatusCard(spec.Label, "blue", menuCardBody(spec.Action, spec.Description), renderGroupMenuButtons(configuredBackend(s.app), spec.Action, sessionKey))
}

func (s commandService) renderSessionMenuCard(sessionKey string) map[string]any {
	return newCommandService(s.app).renderToolsMenuCard(sessionKey)
}

func (s commandService) renderContextMenuCard(sessionKey string) map[string]any {
	return renderCommandMenuCard(s.app, sessionKey)
}

func (s commandService) renderSystemMenuCard(sessionKey string) map[string]any {
	spec, _ := menuGroupSpec("menu.group.system")
	backend := firstNonEmpty(configuredBackend(s.app), "unset")
	body := spec.Description + "\n\n当前 backend: `" + backend + "`\n当前 slog 日志级别: " + renderRuntimeLogLevelValue() + "\n当前版本: `" + currentVersion() + "`"
	return s.app.feishu.SimpleStatusCard(spec.Label, "blue", menuCardBody(spec.Action, body), renderGroupMenuButtons(configuredBackend(s.app), spec.Action, sessionKey))
}

func (s commandService) renderBackendMenuCard(sessionKey string) map[string]any {
	spec, _ := menuGroupSpec("menu.group.backend")
	backend := firstNonEmpty(configuredBackend(s.app), "unset")
	body := spec.Description + "\n\n当前 backend: `" + backend + "`"
	return s.app.feishu.SimpleStatusCard(spec.Label, "blue", menuCardBody(spec.Action, body), renderGroupMenuButtons(configuredBackend(s.app), spec.Action, sessionKey))
}

func (s commandService) renderHelpCard(sessionKey string) map[string]any {
	buttons := []feishu.Button{
		{Text: "返回上一级", Type: "default", Value: map[string]any{"action": "menu.group.system", "session_key": sessionKey}},
	}
	return s.app.feishu.SimpleStatusCard("帮助说明", "blue", menuCardBody("menu.help", renderHelpBodyFromRegistry(configuredBackend(s.app))), buttons)
}
