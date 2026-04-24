package app

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"feidex/internal/codexrpc"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

func testSkillsListEntry(cwd string, skills ...codexrpc.SkillMetadata) codexrpc.SkillsListEntry {
	return codexrpc.SkillsListEntry{
		Cwd:    cwd,
		Skills: append([]codexrpc.SkillMetadata(nil), skills...),
	}
}

func TestCommandSkillsRendersCardFromAppServer(t *testing.T) {
	a, ff, fc := newTestApp(t)
	msg := &feishu.InboundMessage{MessageID: "m-skills", ChatID: "chat-1", ChatType: "p2p", UserID: "user-1"}

	callCount := 0
	fc.callHook = func(_ context.Context, method string, params any, out any) error {
		if method != "skills/list" {
			t.Fatalf("unexpected codex method: %s", method)
		}
		callCount++
		got, _ := params.(map[string]any)
		if got["forceReload"] != false {
			t.Fatalf("skills/list forceReload = %+v, want false", got)
		}
		cwds, _ := got["cwds"].([]string)
		if len(cwds) != 1 || cwds[0] != a.cfg.Workspaces[0].Cwd {
			t.Fatalf("skills/list cwds = %+v, want current workspace cwd", got["cwds"])
		}
		result := out.(*codexrpc.SkillsListResult)
		result.Data = []codexrpc.SkillsListEntry{
			testSkillsListEntry(a.cfg.Workspaces[0].Cwd,
				codexrpc.SkillMetadata{Name: "openai-docs", Path: "/skills/openai-docs", Scope: "system", Enabled: true, Description: "Docs"},
				codexrpc.SkillMetadata{Name: "disabled-skill", Path: "/skills/disabled", Scope: "user", Enabled: false, Description: "Disabled"},
			),
		}
		return nil
	}

	if err := newSkillsService(a).commandSkills(msg, nil); err != nil {
		t.Fatalf("commandSkills() error = %v", err)
	}
	if callCount != 1 {
		t.Fatalf("skills/list call count = %d, want 1", callCount)
	}
	if len(ff.replyCards) != 1 {
		t.Fatalf("replyCards = %d, want 1", len(ff.replyCards))
	}
	body := cardMarkdownContent(t, ff.replyCards[0])
	for _, want := range []string{
		"当前 cwd:",
		"skills: `2` (enabled `1`, disabled `1`)",
		"通过下拉选择 skill；下一条非命令消息会自动携带它。",
		"也可以直接发送 `$skill-name 你的需求`。",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("skills card body missing %q: %q", want, body)
		}
	}
	selects := cardSelectStaticForTest(ff.replyCards[0])
	if len(selects) != 1 {
		t.Fatalf("skills card selects = %+v, want 1 select_static", selects)
	}
	options, _ := selects[0]["options"].([]map[string]any)
	if len(options) != 2 {
		t.Fatalf("skills card options = %+v, want 2", options)
	}
	if text, _ := options[0]["text"].(map[string]any); text["content"] != "openai-docs [system]" {
		t.Fatalf("first option = %+v, want enabled skill first", options[0])
	}
	if text, _ := options[1]["text"].(map[string]any); !strings.Contains(text["content"].(string), "[disabled] disabled-skill [user]") {
		t.Fatalf("second option = %+v, want disabled label", options[1])
	}
}

func TestCommandSkillsReloadForcesReload(t *testing.T) {
	a, ff, fc := newTestApp(t)
	msg := &feishu.InboundMessage{MessageID: "m-skills-reload", ChatID: "chat-1", ChatType: "p2p", UserID: "user-1"}

	fc.callHook = func(_ context.Context, method string, params any, out any) error {
		if method != "skills/list" {
			t.Fatalf("unexpected codex method: %s", method)
		}
		got, _ := params.(map[string]any)
		if got["forceReload"] != true {
			t.Fatalf("skills/list forceReload = %+v, want true", got)
		}
		result := out.(*codexrpc.SkillsListResult)
		result.Data = []codexrpc.SkillsListEntry{testSkillsListEntry(a.cfg.Workspaces[0].Cwd)}
		return nil
	}

	if err := newSkillsService(a).commandSkills(msg, []string{"reload"}); err != nil {
		t.Fatalf("commandSkills(reload) error = %v", err)
	}
	if len(ff.replyCards) != 1 {
		t.Fatalf("replyCards = %d, want 1", len(ff.replyCards))
	}
}

func TestCompleteSkillsSelectStoresPendingSkill(t *testing.T) {
	a, _, fc := newTestApp(t)
	sessionKey := "sess-skills-select"
	wantSkill := codexrpc.SkillMetadata{Name: "openai-docs", Path: "/skills/openai-docs", Scope: "system", Enabled: true}

	fc.callHook = func(_ context.Context, method string, _ any, out any) error {
		if method != "skills/list" {
			t.Fatalf("unexpected codex method: %s", method)
		}
		result := out.(*codexrpc.SkillsListResult)
		result.Data = []codexrpc.SkillsListEntry{testSkillsListEntry(a.cfg.Workspaces[0].Cwd, wantSkill)}
		return nil
	}

	resp, err := newSkillsService(a).completeSkillsSelect(&feishu.CardAction{Option: wantSkill.Path}, sessionKey, wantSkill.Path)
	if err != nil {
		t.Fatalf("completeSkillsSelect() error = %v", err)
	}
	if resp == nil || resp.Toast == nil || resp.Toast.Type != "success" {
		t.Fatalf("completeSkillsSelect() = %#v, want success toast", resp)
	}
	if got, ok := newSkillsService(a).sessionPendingSkill(sessionKey); !ok || got.Name != wantSkill.Name || got.Path != wantSkill.Path {
		t.Fatalf("pending skill = %+v, %v, want selected skill", got, ok)
	}
	if resp.Card == nil {
		t.Fatal("expected refreshed skills card")
	}
	cardData, _ := resp.Card.Data.(map[string]any)
	selects := cardSelectStaticForTest(cardData)
	if len(selects) != 1 || selects[0]["initial_option"] != wantSkill.Path {
		t.Fatalf("refreshed skills card = %+v, want initial_option %q", selects, wantSkill.Path)
	}
}

func TestCompleteSkillsSelectRejectsDisabledSkill(t *testing.T) {
	a, _, fc := newTestApp(t)
	sessionKey := "sess-skills-disabled"
	disabled := codexrpc.SkillMetadata{Name: "disabled-skill", Path: "/skills/disabled", Scope: "user", Enabled: false}

	fc.callHook = func(_ context.Context, method string, _ any, out any) error {
		if method != "skills/list" {
			t.Fatalf("unexpected codex method: %s", method)
		}
		result := out.(*codexrpc.SkillsListResult)
		result.Data = []codexrpc.SkillsListEntry{testSkillsListEntry(a.cfg.Workspaces[0].Cwd, disabled)}
		return nil
	}

	resp, err := newSkillsService(a).completeSkillsSelect(&feishu.CardAction{Option: disabled.Path}, sessionKey, disabled.Path)
	if err != nil {
		t.Fatalf("completeSkillsSelect(disabled) error = %v", err)
	}
	if resp == nil || resp.Toast == nil || resp.Toast.Type != "warning" {
		t.Fatalf("completeSkillsSelect(disabled) = %#v, want warning toast", resp)
	}
	if !strings.Contains(resp.Toast.Content, "disabled") {
		t.Fatalf("disabled select toast = %#v, want disabled hint", resp.Toast)
	}
	if _, ok := newSkillsService(a).sessionPendingSkill(sessionKey); ok {
		t.Fatal("disabled skill should not become pending")
	}
}

func TestEnqueueSubmissionUsesPendingSkillWithoutListingSkills(t *testing.T) {
	a, _, fc := newTestApp(t)
	msg := &feishu.InboundMessage{MessageID: "m-pending", ChatID: "chat-1", ChatType: "p2p", UserID: "user-1", Text: "summarize this"}
	sessionKey := a.makeSessionKey(msg)
	newSkillsService(a).setSessionPendingSkill(sessionKey, state.SubmissionSkill{Name: "openai-docs", Path: "/skills/openai-docs"})

	var seenInputs []map[string]any
	fc.callHook = func(_ context.Context, method string, params any, out any) error {
		switch method {
		case "skills/list":
			t.Fatal("pending skill path should not call skills/list")
		case "thread/start":
			result := out.(*codexrpc.ThreadStartResult)
			result.Thread.ID = "thread-1"
			return nil
		case "turn/start":
			got, _ := params.(map[string]any)
			seenInputs, _ = got["input"].([]map[string]any)
			result := out.(*codexrpc.TurnStartResult)
			result.Turn.ID = "turn-1"
			return nil
		default:
			return nil
		}
		return nil
	}

	if err := a.enqueueSubmission(msg); err != nil {
		t.Fatalf("enqueueSubmission() error = %v", err)
	}
	if len(seenInputs) != 2 || seenInputs[0]["type"] != "skill" || seenInputs[1]["type"] != "text" {
		t.Fatalf("turn/start inputs = %+v, want skill + text", seenInputs)
	}
	if seenInputs[0]["name"] != "openai-docs" || seenInputs[1]["text"] != "summarize this" {
		t.Fatalf("turn/start inputs = %+v, want pending skill then original text", seenInputs)
	}
	if _, ok := newSkillsService(a).sessionPendingSkill(sessionKey); ok {
		t.Fatal("pending skill should be consumed after submission is created")
	}
}

func TestEnqueueSubmissionExplicitSkillPrefixOverridesPending(t *testing.T) {
	a, _, fc := newTestApp(t)
	msg := &feishu.InboundMessage{MessageID: "m-explicit", ChatID: "chat-1", ChatType: "p2p", UserID: "user-1", Text: "$openai-docs summarize this"}
	sessionKey := a.makeSessionKey(msg)
	newSkillsService(a).setSessionPendingSkill(sessionKey, state.SubmissionSkill{Name: "old-skill", Path: "/skills/old"})

	var skillsListCalls int
	var seenInputs []map[string]any
	fc.callHook = func(_ context.Context, method string, params any, out any) error {
		switch method {
		case "skills/list":
			skillsListCalls++
			got, _ := params.(map[string]any)
			cwds, _ := got["cwds"].([]string)
			if len(cwds) != 1 || cwds[0] != a.cfg.Workspaces[0].Cwd {
				t.Fatalf("skills/list cwds = %+v, want current workspace cwd", got["cwds"])
			}
			result := out.(*codexrpc.SkillsListResult)
			result.Data = []codexrpc.SkillsListEntry{
				testSkillsListEntry(a.cfg.Workspaces[0].Cwd,
					codexrpc.SkillMetadata{Name: "openai-docs", Path: "/skills/openai-docs", Enabled: true},
				),
			}
			return nil
		case "thread/start":
			result := out.(*codexrpc.ThreadStartResult)
			result.Thread.ID = "thread-1"
			return nil
		case "turn/start":
			got, _ := params.(map[string]any)
			seenInputs, _ = got["input"].([]map[string]any)
			result := out.(*codexrpc.TurnStartResult)
			result.Turn.ID = "turn-1"
			return nil
		default:
			return nil
		}
	}

	if err := a.enqueueSubmission(msg); err != nil {
		t.Fatalf("enqueueSubmission(explicit skill) error = %v", err)
	}
	if skillsListCalls != 1 {
		t.Fatalf("skills/list call count = %d, want 1", skillsListCalls)
	}
	if len(seenInputs) != 2 || seenInputs[0]["type"] != "skill" || seenInputs[1]["type"] != "text" {
		t.Fatalf("turn/start inputs = %+v, want skill + text", seenInputs)
	}
	if seenInputs[0]["name"] != "openai-docs" || seenInputs[0]["path"] != "/skills/openai-docs" {
		t.Fatalf("turn/start skill input = %+v, want explicit skill", seenInputs[0])
	}
	if seenInputs[1]["text"] != "summarize this" {
		t.Fatalf("turn/start text input = %+v, want prefix stripped body", seenInputs[1])
	}
	if _, ok := newSkillsService(a).sessionPendingSkill(sessionKey); ok {
		t.Fatal("explicit skill should consume previous pending skill")
	}
}

func TestEnqueueSubmissionInvalidSkillPrefixFallsBackToTextAndConsumesPending(t *testing.T) {
	a, _, fc := newTestApp(t)
	msg := &feishu.InboundMessage{MessageID: "m-invalid", ChatID: "chat-1", ChatType: "p2p", UserID: "user-1", Text: "$bad/name keep raw"}
	sessionKey := a.makeSessionKey(msg)
	newSkillsService(a).setSessionPendingSkill(sessionKey, state.SubmissionSkill{Name: "openai-docs", Path: "/skills/openai-docs"})

	var seenInputs []map[string]any
	fc.callHook = func(_ context.Context, method string, params any, out any) error {
		switch method {
		case "skills/list":
			t.Fatal("invalid skill prefix should not call skills/list")
		case "thread/start":
			result := out.(*codexrpc.ThreadStartResult)
			result.Thread.ID = "thread-1"
			return nil
		case "turn/start":
			got, _ := params.(map[string]any)
			seenInputs, _ = got["input"].([]map[string]any)
			result := out.(*codexrpc.TurnStartResult)
			result.Turn.ID = "turn-1"
			return nil
		default:
			return nil
		}
		return nil
	}

	if err := a.enqueueSubmission(msg); err != nil {
		t.Fatalf("enqueueSubmission(invalid skill) error = %v", err)
	}
	if len(seenInputs) != 1 || seenInputs[0]["type"] != "text" || seenInputs[0]["text"] != "$bad/name keep raw" {
		t.Fatalf("turn/start inputs = %+v, want raw text only", seenInputs)
	}
	if _, ok := newSkillsService(a).sessionPendingSkill(sessionKey); ok {
		t.Fatal("invalid explicit prefix should still consume pending skill")
	}
}

func TestEnqueueSubmissionSkillOnlySetsPendingSkill(t *testing.T) {
	a, ff, fc := newTestApp(t)
	msg := &feishu.InboundMessage{MessageID: "m-skill-only", ChatID: "chat-1", ChatType: "p2p", UserID: "user-1", Text: "$openai-docs"}
	sessionKey := a.makeSessionKey(msg)
	turnStarted := false

	fc.callHook = func(_ context.Context, method string, _ any, out any) error {
		switch method {
		case "skills/list":
			result := out.(*codexrpc.SkillsListResult)
			result.Data = []codexrpc.SkillsListEntry{
				testSkillsListEntry(a.cfg.Workspaces[0].Cwd,
					codexrpc.SkillMetadata{Name: "openai-docs", Path: "/skills/openai-docs", Enabled: true},
				),
			}
			return nil
		case "thread/start", "turn/start":
			turnStarted = true
			t.Fatalf("unexpected method for skill-only pending selection: %s", method)
		default:
			return nil
		}
		return nil
	}

	if err := a.enqueueSubmission(msg); err != nil {
		t.Fatalf("enqueueSubmission(skill-only) error = %v", err)
	}
	if turnStarted {
		t.Fatal("skill-only message should not start a turn")
	}
	if len(ff.replyTexts) != 1 || !strings.Contains(ff.replyTexts[0], "已选择 `$openai-docs`") {
		t.Fatalf("replyTexts = %+v, want pending skill confirmation", ff.replyTexts)
	}
	if got, ok := newSkillsService(a).sessionPendingSkill(sessionKey); !ok || got.Name != "openai-docs" || got.Path != "/skills/openai-docs" {
		t.Fatalf("pending skill = %+v, %v, want stored openai-docs", got, ok)
	}
	if sess := a.store.GetSession(sessionKey); sess != nil {
		t.Fatalf("session should not be persisted for skill-only selection, got %+v", sess)
	}
}

func TestEnqueueSubmissionSkillOnlyWithAttachmentStartsTurn(t *testing.T) {
	a, ff, fc := newTestApp(t)
	downloadPath := filepath.Join(t.TempDir(), "image.png")
	ff.downloadPath = downloadPath
	ff.downloadName = "image.png"
	msg := &feishu.InboundMessage{
		MessageID: "m-skill-attachment",
		ChatID:    "chat-1",
		ChatType:  "p2p",
		UserID:    "user-1",
		Text:      "$openai-docs",
		Attachments: []feishu.Attachment{{
			Kind:        "image",
			ResourceKey: "img-1",
		}},
	}

	var seenInputs []map[string]any
	fc.callHook = func(_ context.Context, method string, params any, out any) error {
		switch method {
		case "skills/list":
			result := out.(*codexrpc.SkillsListResult)
			result.Data = []codexrpc.SkillsListEntry{
				testSkillsListEntry(a.cfg.Workspaces[0].Cwd,
					codexrpc.SkillMetadata{Name: "openai-docs", Path: "/skills/openai-docs", Enabled: true},
				),
			}
			return nil
		case "thread/start":
			result := out.(*codexrpc.ThreadStartResult)
			result.Thread.ID = "thread-1"
			return nil
		case "turn/start":
			got, _ := params.(map[string]any)
			seenInputs, _ = got["input"].([]map[string]any)
			result := out.(*codexrpc.TurnStartResult)
			result.Turn.ID = "turn-1"
			return nil
		default:
			return nil
		}
	}

	if err := a.enqueueSubmission(msg); err != nil {
		t.Fatalf("enqueueSubmission(skill + attachment) error = %v", err)
	}
	if len(seenInputs) != 2 || seenInputs[0]["type"] != "skill" || seenInputs[1]["type"] != "localImage" {
		t.Fatalf("turn/start inputs = %+v, want skill + localImage", seenInputs)
	}
	if seenInputs[1]["path"] != downloadPath {
		t.Fatalf("localImage input = %+v, want downloaded image path", seenInputs[1])
	}
}

func TestTrySteerInboundReplyIgnoresSkillSemantics(t *testing.T) {
	a, _, fc := newTestApp(t)
	sessionKey := "sess-steer-skill"
	newSkillsService(a).setSessionPendingSkill(sessionKey, state.SubmissionSkill{Name: "openai-docs", Path: "/skills/openai-docs"})

	skillsListCalls := 0
	var seenInputs []map[string]any
	fc.callHook = func(_ context.Context, method string, params any, _ any) error {
		switch method {
		case "skills/list":
			skillsListCalls++
			return nil
		case "turn/steer":
			got, _ := params.(map[string]any)
			seenInputs, _ = got["input"].([]map[string]any)
			return nil
		default:
			return nil
		}
	}

	got, err := newReplyContinuationService(a).trySteerInboundReply(&feishu.InboundMessage{
		MessageID: "m-steer",
		ChatID:    "chat-1",
		ChatType:  "p2p",
		UserID:    "user-1",
		Text:      "$openai-docs help",
	}, &state.MessageLink{
		SessionKey: sessionKey,
		ThreadID:   "thread-1",
		TurnID:     "turn-1",
	})
	if err != nil || !got {
		t.Fatalf("trySteerInboundReply() = %v, %v", got, err)
	}
	if skillsListCalls != 0 {
		t.Fatalf("steer path should not call skills/list, got %d", skillsListCalls)
	}
	if len(seenInputs) != 1 || seenInputs[0]["type"] != "text" || seenInputs[0]["text"] != "$openai-docs help" {
		t.Fatalf("turn/steer inputs = %+v, want raw text only", seenInputs)
	}
	if pending, ok := newSkillsService(a).sessionPendingSkill(sessionKey); !ok || pending.Name != "openai-docs" {
		t.Fatalf("pending skill after steer = %+v, %v, want untouched", pending, ok)
	}
}
