package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"feidex/internal/app"
	"feidex/internal/config"
	"feidex/internal/feishu"
)

type cardSender interface {
	SendCard(context.Context, string, map[string]any) (string, error)
}

var (
	loadConfig = config.Load
	newSender  = func(cfg config.FeishuConfig) cardSender { return feishu.New(cfg) }
	timeNow    = time.Now
)

type options struct {
	ConfigPath string
	ChatID     string
	Kind       string
	Workspace  string
	UserID     string
	Title      string
	Body       string
	Color      string
	Command    string
	Reason     string
	RequestID  string
	LogLevel   string
	Timeout    time.Duration
	DryRun     bool
	NoButtons  bool
}

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	opts, err := parseOptions(args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return 1
	}

	logger, err := newLogger(opts.LogLevel)
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid log level: %v\n", err)
		return 1
	}
	slog.SetDefault(logger)

	cfg, err := loadConfig(opts.ConfigPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load config: %v\n", err)
		return 1
	}
	if strings.TrimSpace(cfg.Feishu.AppID) == "" || strings.TrimSpace(cfg.Feishu.AppSecret) == "" {
		fmt.Fprintln(os.Stderr, "missing feishu.app_id or feishu.app_secret in config")
		return 1
	}

	card, resolvedKind, err := buildCardDemoCard(cfg, opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "build card: %v\n", err)
		return 1
	}
	cardJSON, err := json.MarshalIndent(card, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "marshal card: %v\n", err)
		return 1
	}

	slog.Info("card demo prepared",
		"config_path", opts.ConfigPath,
		"chat_id", opts.ChatID,
		"kind", resolvedKind,
		"workspace", opts.Workspace,
		"color", opts.Color,
		"app_id", maskAppID(cfg.Feishu.AppID),
		"request_id", opts.RequestID,
		"button_enabled", !opts.NoButtons,
	)
	slog.Debug("card demo payload", "card_json", string(cardJSON))

	if opts.DryRun {
		slog.Info("card demo dry run complete", "chat_id", opts.ChatID)
		fmt.Printf("%s\n", cardJSON)
		return 0
	}

	slog.Info("card demo sending", "chat_id", opts.ChatID, "timeout", opts.Timeout.String())
	ctx, cancel := context.WithTimeout(context.Background(), opts.Timeout)
	defer cancel()

	sender := newSender(cfg.Feishu)
	messageID, err := sender.SendCard(ctx, opts.ChatID, card)
	if err != nil {
		slog.Error("card demo send failed", "chat_id", opts.ChatID, "error", err)
		fmt.Fprintf(os.Stderr, "send card demo: %v\n", err)
		return 1
	}
	slog.Info("card demo sent", "chat_id", opts.ChatID, "message_id", messageID)
	fmt.Printf("message_id=%s\n", messageID)
	return 0
}

func parseOptions(args []string) (options, error) {
	fs := flag.NewFlagSet("feishu_card_demo", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	var opts options
	timeoutSeconds := fs.Int("timeout", 15, "send timeout in seconds")
	fs.StringVar(&opts.ConfigPath, "config", "config.toml", "path to config file")
	fs.StringVar(&opts.ChatID, "chat-id", "", "target Feishu open chat id")
	fs.StringVar(&opts.Kind, "kind", "agent", "card demo kind: agent/turn_output, final/final_message, turn_reasoning, turn_command_execution, turn_file_change, turn_plan, turn_queued, turn_terminal, plain, command, file, permissions")
	fs.StringVar(&opts.Workspace, "workspace", "", "workspace id used when rendering current outbound message cards")
	fs.StringVar(&opts.UserID, "user-id", "", "optional user id used by local file link rewrite")
	fs.StringVar(&opts.Title, "title", "", "card title override")
	fs.StringVar(&opts.Body, "body", "", "card markdown body override")
	fs.StringVar(&opts.Color, "color", "orange", "card color template")
	fs.StringVar(&opts.Command, "command", "pwd", "demo command shown when kind=command")
	fs.StringVar(&opts.Reason, "reason", "这是 demo 消息。", "demo reason text")
	fs.StringVar(&opts.RequestID, "request-id", "", "demo request id used in button payloads")
	fs.StringVar(&opts.LogLevel, "log-level", "debug", "log level: debug, info, warn, error")
	fs.BoolVar(&opts.DryRun, "dry-run", false, "print the card payload without sending")
	fs.BoolVar(&opts.NoButtons, "no-buttons", false, "omit preset buttons")
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), `Usage:
  go run ./cmd/feishu_card_demo --config config.toml --chat-id oc_xxx
  go run ./cmd/feishu_card_demo --config config.toml --chat-id oc_xxx --kind agent
  go run ./cmd/feishu_card_demo --config config.toml --chat-id oc_xxx --kind final
  go run ./cmd/feishu_card_demo --config config.toml --chat-id oc_xxx --title "卡片标题" --body "自定义正文"

Options:`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return options{}, err
	}

	opts.ConfigPath = strings.TrimSpace(opts.ConfigPath)
	opts.ChatID = strings.TrimSpace(opts.ChatID)
	opts.Kind = strings.ToLower(strings.TrimSpace(opts.Kind))
	opts.Workspace = strings.TrimSpace(opts.Workspace)
	opts.UserID = strings.TrimSpace(opts.UserID)
	opts.Title = strings.TrimSpace(opts.Title)
	opts.Body = strings.TrimSpace(opts.Body)
	opts.Color = strings.TrimSpace(opts.Color)
	opts.Command = strings.TrimSpace(opts.Command)
	opts.Reason = strings.TrimSpace(opts.Reason)
	opts.RequestID = strings.TrimSpace(opts.RequestID)
	opts.Timeout = time.Duration(*timeoutSeconds) * time.Second

	if opts.ChatID == "" {
		return options{}, errors.New("missing --chat-id")
	}
	if opts.Timeout <= 0 {
		return options{}, errors.New("--timeout must be greater than 0")
	}
	switch opts.Kind {
	case "agent", "agent_message", "turn_output", "final", "final_agent", "final_message",
		"turn_reasoning", "turn_command_execution", "turn_file_change", "turn_plan", "turn_queued", "turn_terminal",
		"plain", "command", "file", "permissions":
	default:
		return options{}, fmt.Errorf("unsupported --kind %q", opts.Kind)
	}
	if usesLegacyCardDemo(opts.Kind) && opts.Title == "" {
		opts.Title = defaultCardDemoTitle(opts.Kind)
	}
	if usesLegacyCardDemo(opts.Kind) && opts.Body == "" {
		opts.Body = defaultCardDemoBody(opts.Kind, opts.Command, opts.Reason)
	}
	if opts.Color == "" {
		opts.Color = "orange"
	}
	if opts.RequestID == "" {
		opts.RequestID = fmt.Sprintf("demo-%s-%d", opts.Kind, timeNow().Unix())
	}
	return opts, nil
}

func newLogger(level string) (*slog.Logger, error) {
	logLevel, err := config.ParseLogLevel(level)
	if err != nil {
		return nil, err
	}
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: logLevel})), nil
}

func defaultCardDemoTitle(kind string) string {
	switch kind {
	case "permissions":
		return "权限请求"
	case "command":
		return "等待审批"
	case "file":
		return "文件审批"
	default:
		return "卡片 Demo"
	}
}

func defaultCardDemoBody(kind, command, reason string) string {
	reason = strings.TrimSpace(reason)
	switch kind {
	case "plain":
		if reason == "" {
			reason = "这是 demo 消息。"
		}
		return "卡片效果测试\n" + reason
	case "file":
		if reason == "" {
			reason = "文件改动需要确认。"
		}
		return "文件变更审批\n" + reason
	case "permissions":
		if reason == "" {
			reason = "需要授予 demo 权限。"
		}
		return "权限审批\n" + reason
	default:
		command = strings.TrimSpace(command)
		if command == "" {
			command = "pwd"
		}
		if reason == "" {
			reason = "命令执行需要确认。"
		}
		return fmt.Sprintf("命令审批\n`%s`\n%s", command, reason)
	}
}

func buildCardDemoCard(cfg *config.Config, opts options) (map[string]any, string, error) {
	if !usesLegacyCardDemo(opts.Kind) {
		card, resolvedKind, err := app.BuildOutboundCardDemo(cfg, app.OutboundCardDemoOptions{
			Kind:        opts.Kind,
			WorkspaceID: opts.Workspace,
			ChatID:      opts.ChatID,
			UserID:      opts.UserID,
			Body:        opts.Body,
		})
		if err != nil {
			return nil, "", err
		}
		return card, resolvedKind, nil
	}
	card := newLegacyMarkdownCard(opts.Title, opts.Color, strings.TrimSpace(opts.Body))
	if !opts.NoButtons {
		buttons := cardDemoButtons(opts.Kind, opts.RequestID)
		if len(buttons) > 0 {
			appendLegacyCardElement(card, buildLegacyCardActionElement(buttons))
		}
	}
	return card, opts.Kind, nil
}

func usesLegacyCardDemo(kind string) bool {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "plain", "command", "file", "permissions":
		return true
	default:
		return false
	}
}

func cardDemoButtons(kind, requestID string) []feishu.Button {
	switch kind {
	case "plain":
		return []feishu.Button{
			{Text: "按钮 A", Type: "primary", Value: map[string]any{"action": "demo.primary", "request_id": requestID}},
			{Text: "按钮 B", Type: "default", Value: map[string]any{"action": "demo.secondary", "request_id": requestID}},
		}
	case "permissions":
		return []feishu.Button{
			{Text: "本次允许", Type: "primary", Value: map[string]any{"action": "approval.permissions.accept_turn", "request_id": requestID, "demo": true}},
			{Text: "本会话允许", Type: "default", Value: map[string]any{"action": "approval.permissions.accept_session", "request_id": requestID, "demo": true}},
		}
	default:
		return []feishu.Button{
			{Text: "允许一次", Type: "primary", Value: map[string]any{"action": "approval." + kind + ".accept", "request_id": requestID, "demo": true}},
			{Text: "本会话允许", Type: "default", Value: map[string]any{"action": "approval." + kind + ".accept_session", "request_id": requestID, "demo": true}},
			{Text: "拒绝", Type: "danger", Value: map[string]any{"action": "approval." + kind + ".decline", "request_id": requestID, "demo": true}},
		}
	}
}

func newLegacyMarkdownCard(title, color, body string) map[string]any {
	if strings.TrimSpace(color) == "" {
		color = "blue"
	}
	card := map[string]any{
		"config": map[string]any{
			"wide_screen_mode": true,
			"update_multi":     true,
		},
		"elements": []map[string]any{},
	}
	if strings.TrimSpace(title) != "" {
		card["header"] = map[string]any{
			"title": map[string]any{
				"tag":     "plain_text",
				"content": strings.TrimSpace(title),
			},
			"template": color,
		}
	}
	if strings.TrimSpace(body) != "" {
		appendLegacyCardElement(card, map[string]any{
			"tag":     "markdown",
			"content": strings.TrimSpace(body),
		})
	}
	return card
}

func appendLegacyCardElement(card map[string]any, elem map[string]any) {
	elements, _ := card["elements"].([]map[string]any)
	card["elements"] = append(elements, elem)
}

func buildLegacyCardActionElement(buttons []feishu.Button) map[string]any {
	actions := make([]map[string]any, 0, len(buttons))
	for _, btn := range buttons {
		action := map[string]any{
			"tag":   "button",
			"type":  btn.Type,
			"text":  map[string]any{"tag": "plain_text", "content": btn.Text},
			"value": btn.Value,
		}
		if strings.TrimSpace(btn.Name) != "" {
			action["name"] = btn.Name
		}
		actions = append(actions, action)
	}
	return map[string]any{
		"tag":     "action",
		"actions": actions,
	}
}

func maskAppID(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= 6 {
		return value
	}
	return value[:3] + "..." + value[len(value)-3:]
}
