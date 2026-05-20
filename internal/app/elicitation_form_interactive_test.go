package app

import (
	"encoding/json"
	"strings"
	"testing"

	"feidex/internal/codexrpc"
	"feidex/internal/feishu"
)

func TestMcpElicitationInteractiveFormSubmitAndToggle(t *testing.T) {
	a, ff, fc := newTestApp(t)
	seedActiveSubmission(t, a, "sess-1", "thread-1", "turn-1")

	onMcpElicitationRequest(a, codexrpc.RequestEnvelope{
		ID: json.RawMessage(`"elicit-form-1"`),
		Params: json.RawMessage(`{
			"mode":"form",
			"threadId":"thread-1",
			"turnId":"turn-1",
			"serverName":"srv",
			"message":"configure deployment",
			"requestedSchema":{
				"type":"object",
				"required":["name","tags"],
				"properties":{
					"name":{"type":"string","title":"Project Name"},
					"enabled":{"type":"boolean"},
					"mode":{"type":"string","oneOf":[{"const":"fast","title":"Fast"},{"const":"safe","title":"Safe"}]},
					"tags":{"type":"array","minItems":1,"items":{"anyOf":[{"const":"alpha","title":"Alpha"},{"const":"beta","title":"Beta"},{"const":"gamma","title":"Gamma"}]}}
				}
			}
		}`),
	})
	if pending := a.store.PendingByID("elicit-form-1"); pending == nil || pending.Kind != "mcp_elicitation_form" {
		t.Fatalf("elicitation form pending = %+v, want interactive pending form", pending)
	}
	form := elicitationAppForm(t, ff.replyCards[0])
	selects := elicitationAppFormSelects(form)
	if selects["enabled"] == nil || selects["mode"] == nil {
		t.Fatalf("elicitation selects = %+v, want enabled/mode", selects)
	}
	if toggles := elicitationAppToggleButtons(form); len(toggles) != 3 {
		t.Fatalf("elicitation toggle buttons = %+v, want 3", toggles)
	}

	toggleResp, err := a.ServerRequestService().CompleteElicitationMultiToggle(&feishu.CardAction{
		UserID: "user-1",
		ActionValue: map[string]any{
			"request_id":   "elicit-form-1",
			"field_name":   "tags",
			"option_value": "alpha",
			"multi_drafts": map[string]any{},
		},
		FormValue: map[string]any{
			"name":    "Feidex",
			"enabled": map[string]any{"value": "true"},
			"mode":    map[string]any{"value": "safe"},
		},
	})
	if err != nil || toggleResp == nil || toggleResp.Card == nil {
		t.Fatalf("CompleteElicitationMultiToggle() = %#v, %v", toggleResp, err)
	}
	toggledCard, _ := toggleResp.Card.Data.(map[string]any)
	toggledForm := elicitationAppForm(t, toggledCard)
	foundSelected := false
	for _, button := range elicitationAppToggleButtons(toggledForm) {
		text, _ := button["text"].(map[string]any)
		content, _ := text["content"].(string)
		if strings.Contains(content, "[x] Alpha") {
			foundSelected = true
		}
	}
	if !foundSelected {
		t.Fatalf("toggle response did not mark Alpha selected: %#v", toggledCard)
	}

	resp, err := a.ServerRequestService().CompleteElicitationFormAnswer(&feishu.CardAction{
		UserID: "user-1",
		ActionValue: map[string]any{
			"request_id": "elicit-form-1",
			"multi_drafts": map[string]any{
				"tags": []any{"alpha", "beta"},
			},
		},
		FormValue: map[string]any{
			"name":    "Feidex",
			"enabled": map[string]any{"value": "true"},
			"mode":    map[string]any{"value": "safe"},
		},
	})
	if err != nil || resp == nil || resp.Toast == nil || resp.Toast.Type != "success" || resp.Card == nil {
		t.Fatalf("CompleteElicitationFormAnswer() = %#v, %v", resp, err)
	}
	if len(fc.replies) == 0 {
		t.Fatal("expected elicitation form submission to reply to codex")
	}
	reply, _ := fc.replies[len(fc.replies)-1].result.(map[string]any)
	if reply["action"] != "accept" {
		t.Fatalf("elicitation reply action = %+v, want accept", reply)
	}
	content, _ := reply["content"].(map[string]any)
	if content["name"] != "Feidex" || content["enabled"] != true || content["mode"] != "safe" {
		t.Fatalf("elicitation reply content = %+v", content)
	}
	tags, _ := content["tags"].([]string)
	if len(tags) != 2 || tags[0] != "alpha" || tags[1] != "beta" {
		t.Fatalf("elicitation reply tags = %+v, want [alpha beta]", tags)
	}
	cardData, _ := resp.Card.Data.(map[string]any)
	if got := cardMarkdownContent(t, cardData); !strings.Contains(got, "处理结果: 已提交") || !strings.Contains(got, "`tags`: alpha, beta") {
		t.Fatalf("resolved elicitation card = %q", got)
	}
}

func TestMcpElicitationDecisionCardQuickAnswer(t *testing.T) {
	a, ff, fc := newTestApp(t)
	seedActiveSubmission(t, a, "sess-1", "thread-1", "turn-1")

	onMcpElicitationRequest(a, codexrpc.RequestEnvelope{
		ID: json.RawMessage(`"elicit-quick-1"`),
		Params: json.RawMessage(`{
			"mode":"form",
			"threadId":"thread-1",
			"turnId":"turn-1",
			"serverName":"srv",
			"message":"enable feature?",
			"requestedSchema":{"type":"object","properties":{"enabled":{"type":"boolean"}}}
		}`),
	})
	if form := elicitationAppFormOptional(ff.replyCards[0]); form != nil {
		t.Fatalf("single boolean elicitation should render decision card, got form %#v", form)
	}
	resp, err := a.ServerRequestService().CompleteElicitationFormAnswer(&feishu.CardAction{
		UserID: "user-1",
		ActionValue: map[string]any{
			"request_id": "elicit-quick-1",
			"field_name": "enabled",
			"answer":     "true",
		},
	})
	if err != nil || resp == nil || resp.Toast == nil || resp.Toast.Type != "success" {
		t.Fatalf("CompleteElicitationFormAnswer(quick) = %#v, %v", resp, err)
	}
	reply, _ := fc.replies[len(fc.replies)-1].result.(map[string]any)
	content, _ := reply["content"].(map[string]any)
	if content["enabled"] != true {
		t.Fatalf("quick elicitation reply content = %+v, want enabled=true", content)
	}
}

func TestMcpElicitationConfirmCardDirectActions(t *testing.T) {
	a, ff, fc := newTestApp(t)
	seedActiveSubmission(t, a, "sess-1", "thread-1", "turn-1")

	onMcpElicitationRequest(a, codexrpc.RequestEnvelope{
		ID: json.RawMessage(`"elicit-confirm-1"`),
		Params: json.RawMessage(`{
			"mode":"form",
			"threadId":"thread-1",
			"turnId":"turn-1",
			"serverName":"feidex-send",
			"message":"Allow the feidex-send MCP server to run tool \"feishu_send_im_image\"?",
			"requestedSchema":{"type":"object","properties":{}}
		}`),
	})
	if form := elicitationAppFormOptional(ff.replyCards[0]); form != nil {
		t.Fatalf("zero-field elicitation should render confirm card, got form %#v", form)
	}
	if got := cardMarkdownContent(t, ff.replyCards[0]); !strings.Contains(got, "请直接点击下方按钮确认") {
		t.Fatalf("confirm card body = %q", got)
	}
	resp, err := a.ServerRequestService().CompleteElicitationFormAnswer(&feishu.CardAction{
		UserID: "user-1",
		ActionValue: map[string]any{
			"request_id":         "elicit-confirm-1",
			"elicitation_action": "decline",
		},
	})
	if err != nil || resp == nil || resp.Toast == nil || resp.Toast.Type != "success" {
		t.Fatalf("CompleteElicitationFormAnswer(confirm decline) = %#v, %v", resp, err)
	}
	reply, _ := fc.replies[len(fc.replies)-1].result.(map[string]any)
	if reply["action"] != "decline" {
		t.Fatalf("confirm decline reply = %+v, want action=decline", reply)
	}
}

func elicitationAppForm(t *testing.T, card map[string]any) map[string]any {
	t.Helper()
	form := elicitationAppFormOptional(card)
	if form == nil {
		t.Fatalf("missing elicitation form card: %#v", card)
	}
	return form
}

func elicitationAppFormOptional(card map[string]any) map[string]any {
	for _, elem := range cardElements(card) {
		if tag, _ := elem["tag"].(string); tag == "form" {
			name, _ := elem["name"].(string)
			if name == "elicitation_form" {
				return elem
			}
		}
	}
	return nil
}

func elicitationAppFormSelects(form map[string]any) map[string]map[string]any {
	elements, _ := form["elements"].([]map[string]any)
	selects := make(map[string]map[string]any)
	for _, elem := range elements {
		if tag, _ := elem["tag"].(string); tag != "select_static" {
			continue
		}
		name, _ := elem["name"].(string)
		selects[name] = elem
	}
	return selects
}

func elicitationAppToggleButtons(form map[string]any) []map[string]any {
	elements, _ := form["elements"].([]map[string]any)
	var out []map[string]any
	for _, elem := range elements {
		if tag, _ := elem["tag"].(string); tag != "column_set" {
			continue
		}
		columns, _ := elem["columns"].([]map[string]any)
		for _, column := range columns {
			columnElems, _ := column["elements"].([]map[string]any)
			for _, child := range columnElems {
				if tag, _ := child["tag"].(string); tag != "button" {
					continue
				}
				name, _ := child["name"].(string)
				if strings.HasPrefix(name, "elicitation_toggle_") {
					out = append(out, child)
				}
			}
		}
	}
	return out
}
