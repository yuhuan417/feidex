package feishuapp

import (
	"fmt"
	"strings"

	appmenuutil "feidex/internal/adapter/feishu/menuutil"
	"feidex/internal/adapter/feishu/planmode"
	"feidex/internal/adapter/feishu/quietmode"
	"feidex/internal/adapter/feishu/turnitem"
	"feidex/internal/application/runtimeconfig"
	"feidex/internal/config"
	"feidex/internal/feishu"
	frontendruntime "feidex/internal/runtime"
)

func shouldDeliverTurnItemInQuiet(mode config.QuietMode, itemType string, isFinalAnswer bool) bool {
	return quietmode.ShouldDeliverTurnItem(mode, itemType, isFinalAnswer)
}

func shouldDeliverTurnItemPayloadInQuiet(mode config.QuietMode, payload turnitem.CardPayload) bool {
	return quietmode.ShouldDeliverTurnItemPayload(mode, payload.ItemType, payload.ProtocolItemType, payload.ToolName, payload.IsFinalAnswer)
}

func renderQuietModeMenuCard(mode config.QuietMode, sessionKey, title string) map[string]any {
	lines := []string{
		"当前模式: `" + quietmode.StatusText(mode) + "`",
		"",
	}
	for _, option := range quietmode.Options {
		lines = append(lines, "- `"+option.Title+"`: "+option.Description)
	}
	buttons := make([]feishu.Button, 0, len(quietmode.Options))
	for _, option := range quietmode.Options {
		buttons = append(buttons, feishu.Button{
			Text: func() string {
				if option.Mode == mode {
					return "当前 · " + option.Title
				}
				return option.Title
			}(),
			Type: func() string {
				if option.Mode == mode {
					return "primary"
				}
				return "default"
			}(),
			Value: map[string]any{
				"action":      "quiet.set",
				"mode":        option.Mode.String(),
				"session_key": sessionKey,
			},
		})
	}
	return appmenuutil.PageCard{
		Node:       "menu.quiet",
		SessionKey: sessionKey,
		Title:      title,
		Color:      "blue",
		Body:       strings.Join(lines, "\n"),
		Buttons:    buttons,
	}.Render()
}

func updateQuietMode(settings runtimeconfig.Service, mode config.QuietMode) error {
	if settings.Repository == nil {
		return fmt.Errorf("nil config")
	}
	normalized, err := config.ParseQuietMode(mode)
	if err != nil {
		return err
	}
	return settings.SetQuietMode(normalized.String())
}

type QuietCommandInputs struct {
	Settings       runtimeconfig.Service
	Mode           func() config.QuietMode
	MakeSessionKey func(*feishu.InboundMessage) string
	State          planmode.StateProvider
	Renderer       bindingCardRenderer
	Effects        frontendruntime.EffectRunner
	FrontendID     string
	ReplyInThread  bool
}

func handleQuietCommand(inputs QuietCommandInputs, msg *feishu.InboundMessage, args []string) error {
	if len(args) > 1 {
		return fmt.Errorf("usage: /quiet | /quiet <verbose|progress|normal|final> | /quiet config")
	}
	if len(args) == 0 {
		if msg == nil {
			return nil
		}
		sessionKey := inputs.MakeSessionKey(msg)
		card := renderQuietModeMenuCard(inputs.Mode(), sessionKey, planModeTitleForSession(inputs.State, inputs.State != nil, sessionKey, "Quiet Mode"))
		return replyCardEffect(inputs.Effects, inputs.FrontendID, inputs.ReplyInThread, msg, card)
	}
	arg := strings.TrimSpace(args[0])
	if len(args) == 1 {
		switch arg {
		case "config":
			if msg == nil {
				return nil
			}
			sessionKey := inputs.MakeSessionKey(msg)
			card := renderQuietModeMenuCard(inputs.Mode(), sessionKey, planModeTitleForSession(inputs.State, inputs.State != nil, sessionKey, "Quiet Mode"))
			return replyCardEffect(inputs.Effects, inputs.FrontendID, inputs.ReplyInThread, msg, card)
		default:
			mode, err := config.ParseQuietMode(config.QuietMode(arg))
			if err != nil {
				return fmt.Errorf("usage: /quiet | /quiet <verbose|progress|normal|final> | /quiet config")
			}
			if msg == nil {
				return nil
			}
			if err := updateQuietMode(inputs.Settings, mode); err != nil {
				return err
			}
			return replyTextEffect(inputs.Effects, inputs.FrontendID, inputs.ReplyInThread, msg, "Quiet Mode 已切换为 `"+quietmode.StatusText(mode)+"`。")
		}
	}
	return nil
}
