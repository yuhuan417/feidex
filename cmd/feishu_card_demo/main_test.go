package main

import (
	"context"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"feidex/internal/config"
)

type stubSender struct {
	chatID string
	card   map[string]any
	msgID  string
	err    error
}

func (s *stubSender) SendCard(_ context.Context, chatID string, card map[string]any) (string, error) {
	s.chatID = chatID
	s.card = card
	return s.msgID, s.err
}

func withCapturedOutput(t *testing.T, fn func()) (stdout string, stderr string) {
	t.Helper()

	origStdout := os.Stdout
	origStderr := os.Stderr
	defer func() {
		os.Stdout = origStdout
		os.Stderr = origStderr
	}()

	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe(stdout): %v", err)
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe(stderr): %v", err)
	}
	os.Stdout = outW
	os.Stderr = errW

	var outBuf, errBuf strings.Builder
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, _ = io.Copy(&outBuf, outR)
	}()
	go func() {
		defer wg.Done()
		_, _ = io.Copy(&errBuf, errR)
	}()

	fn()

	_ = outW.Close()
	_ = errW.Close()
	wg.Wait()
	return outBuf.String(), errBuf.String()
}

func TestBuildCardDemoCardIncludesButtons(t *testing.T) {
	opts := options{
		Title:     "卡片 Demo",
		Body:      "卡片效果测试",
		Color:     "green",
		Kind:      "plain",
		RequestID: "req-1",
	}
	card, resolvedKind, err := buildCardDemoCard(config.Default(), opts)
	if err != nil {
		t.Fatalf("buildCardDemoCard() error = %v", err)
	}
	if resolvedKind != "plain" {
		t.Fatalf("resolved kind = %q, want plain", resolvedKind)
	}
	elements, _ := card["elements"].([]map[string]any)
	if len(elements) != 2 {
		t.Fatalf("element count = %d, want 2", len(elements))
	}
	if got, _ := elements[0]["content"].(string); !strings.Contains(got, "卡片效果测试") {
		t.Fatalf("body element = %#v", elements[0])
	}
	if got, _ := elements[1]["actions"].([]map[string]any); len(got) != 2 {
		t.Fatalf("button count = %d, want 2", len(got))
	}
}

func TestBuildCardDemoCardUsesOutboundRendererForAgentAndFinal(t *testing.T) {
	cfg := config.Default()
	card, resolvedKind, err := buildCardDemoCard(cfg, options{Kind: "agent", Body: "hello"})
	if err != nil {
		t.Fatalf("buildCardDemoCard(agent) error = %v", err)
	}
	if resolvedKind != "turn_output" {
		t.Fatalf("resolved kind = %q, want turn_output", resolvedKind)
	}
	if _, ok := card["body"].(map[string]any); !ok {
		t.Fatalf("agent card body = %#v, want schema v2 body", card)
	}

	card, resolvedKind, err = buildCardDemoCard(cfg, options{Kind: "final", Body: "hello"})
	if err != nil {
		t.Fatalf("buildCardDemoCard(final) error = %v", err)
	}
	if resolvedKind != "final_message" {
		t.Fatalf("resolved kind = %q, want final_message", resolvedKind)
	}
	header, _ := card["header"].(map[string]any)
	if template, _ := header["template"].(string); template != "green" {
		t.Fatalf("final card header template = %q, want green", template)
	}
	title, _ := header["title"].(map[string]any)["content"].(string)
	if title != "最终答复" {
		t.Fatalf("final card title = %q, want 最终答复", title)
	}
}

func TestDefaultCardDemoBodyUsesPlainPreset(t *testing.T) {
	got := defaultCardDemoBody("plain", "", "")
	if !strings.Contains(got, "卡片效果测试") {
		t.Fatalf("defaultCardDemoBody(plain) = %q", got)
	}
}

func TestParseOptionsRejectsMissingChatID(t *testing.T) {
	if _, err := parseOptions(nil); err == nil || !strings.Contains(err.Error(), "missing --chat-id") {
		t.Fatalf("parseOptions() error = %v, want missing chat-id", err)
	}
}

func TestRunDryRunPrintsCardJSON(t *testing.T) {
	origLoadConfig := loadConfig
	origNewSender := newSender
	origTimeNow := timeNow
	defer func() {
		loadConfig = origLoadConfig
		newSender = origNewSender
		timeNow = origTimeNow
	}()

	loadConfig = func(string) (*config.Config, error) {
		cfg := config.Default()
		cfg.Feishu.AppID = "cli_a1b2c3"
		cfg.Feishu.AppSecret = "secret"
		return cfg, nil
	}
	newSender = func(cfg config.FeishuConfig) cardSender {
		return &stubSender{}
	}
	timeNow = func() time.Time {
		return time.Unix(1700000000, 0)
	}

	stdout, stderr := withCapturedOutput(t, func() {
		if got := run([]string{"--chat-id", "oc_123", "--kind", "plain", "--dry-run"}); got != 0 {
			t.Fatalf("run(dry-run) = %d, want 0", got)
		}
	})
	if !strings.Contains(stdout, `"content": "卡片效果测试\n这是 demo 消息。"`) {
		t.Fatalf("stdout = %q, want card body", stdout)
	}
	if !strings.Contains(stderr, "card demo dry run complete") {
		t.Fatalf("stderr = %q, want dry run log", stderr)
	}
}

func TestRunSendSuccess(t *testing.T) {
	origLoadConfig := loadConfig
	origNewSender := newSender
	defer func() {
		loadConfig = origLoadConfig
		newSender = origNewSender
	}()

	sender := &stubSender{msgID: "msg-123"}
	loadConfig = func(string) (*config.Config, error) {
		cfg := config.Default()
		cfg.Feishu.AppID = "cli_a1b2c3"
		cfg.Feishu.AppSecret = "secret"
		return cfg, nil
	}
	newSender = func(cfg config.FeishuConfig) cardSender {
		return sender
	}

	stdout, _ := withCapturedOutput(t, func() {
		if got := run([]string{"--chat-id", "oc_123"}); got != 0 {
			t.Fatalf("run(send) = %d, want 0", got)
		}
	})
	if sender.chatID != "oc_123" {
		t.Fatalf("sender chatID = %q, want oc_123", sender.chatID)
	}
	if !strings.Contains(stdout, "message_id=msg-123") {
		t.Fatalf("stdout = %q, want message id", stdout)
	}
}
