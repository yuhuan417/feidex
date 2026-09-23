package app

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"feidex/internal/codexrpc"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

func TestCommonMenuFamiliesRenderEquallyForP2PAndGroup(t *testing.T) {
	a, _, fc := newTestApp(t)
	a.frontendID = "bot-menu-parity"
	p2pMsg := &feishu.InboundMessage{ChatID: "p2p-menu-parity", ChatType: "p2p", UserID: "user-1"}
	groupMsg := &feishu.InboundMessage{ChatID: "group-menu-parity", ChatType: "group", UserID: "user-1", RootMessageID: "root-1"}
	p2pKey := makeSessionKey(a, p2pMsg)
	groupKey := makeSessionKey(a, groupMsg)
	for _, sess := range []*state.Session{
		{Key: p2pKey, ChatID: p2pMsg.ChatID, ChatType: p2pMsg.ChatType, WorkspaceID: "default", ActiveThreadID: "thread-p2p", ActiveThreadWorkspaceID: "default"},
		{Key: groupKey, ChatID: groupMsg.ChatID, ChatType: groupMsg.ChatType, WorkspaceID: "default", ActiveThreadID: "thread-group", ActiveThreadWorkspaceID: "default"},
	} {
		if err := a.State().SaveSession(sess); err != nil {
			t.Fatalf("SaveSession(%q): %v", sess.Key, err)
		}
	}
	binding := &state.AgentBinding{
		ID:          defaultBindingID(a.frontendID, "group", groupMsg.ChatID),
		FrontendID:  a.frontendID,
		ChatID:      groupMsg.ChatID,
		ChatType:    "group",
		WorkspaceID: "default",
		Status:      state.AgentBindingStatusActive.String(),
	}
	if err := a.State().SaveAgentBinding(binding); err != nil {
		t.Fatalf("SaveAgentBinding(): %v", err)
	}
	fc.callHook = func(_ context.Context, method string, _ any, out any) error {
		if method == "thread/list" {
			*out.(*codexrpc.ThreadListResult) = codexrpc.ThreadListResult{}
			return nil
		}
		return fmt.Errorf("unexpected method %q", method)
	}

	p2pWorkspace := newWorkspaceRenderServiceInner(a).RenderWorkspaceMenuCard(p2pKey)
	groupWorkspace := newWorkspaceRenderServiceInner(a).RenderWorkspaceMenuCard(groupKey)
	p2pThread, err := conversationBackend(a).RenderThreadsCard(p2pKey, false)
	if err != nil {
		t.Fatalf("render p2p thread menu: %v", err)
	}
	groupThread, err := conversationBackend(a).RenderThreadsCard(groupKey, false)
	if err != nil {
		t.Fatalf("render group thread menu: %v", err)
	}
	p2pModelConfig := newModelConfigService(a).renderModelConfigCard(codexrpc.ModelListResult{Data: []codexrpc.ModelListEntry{{ID: "gpt-5", DisplayName: "GPT-5", DefaultReasoningEffort: "medium"}}}, nil, p2pKey, "menu.model")
	groupModelConfig := newBindingService(a).renderBindingCodexModelConfigCard(groupKey, binding, codexrpc.ModelListResult{Data: []codexrpc.ModelListEntry{{ID: "gpt-5", DisplayName: "GPT-5", DefaultReasoningEffort: "medium"}}})

	families := []struct {
		name  string
		p2p   map[string]any
		group map[string]any
	}{
		{name: "root", p2p: renderCommandMenuCard(a, p2pKey), group: renderCommandMenuCard(a, groupKey)},
		{name: "tools", p2p: renderToolsMenuCard(a, p2pKey), group: renderToolsMenuCard(a, groupKey)},
		{name: "system", p2p: renderSystemMenuCard(a, p2pKey), group: renderSystemMenuCard(a, groupKey)},
		{name: "backend", p2p: renderBackendMenuCard(a, p2pKey), group: renderBackendMenuCard(a, groupKey)},
		{name: "workspace", p2p: p2pWorkspace, group: groupWorkspace},
		{name: "model overview", p2p: newBackendConfigurationService(a).renderModelMenuCard(p2pKey), group: newBindingService(a).renderBindingModelMenuCard(groupKey, binding)},
		{name: "service tier", p2p: renderServiceTierMenuCard(a, p2pKey), group: newBindingService(a).renderBindingFastCard(groupKey, binding)},
		{name: "thread/session", p2p: p2pThread, group: groupThread},
		{name: "model config", p2p: p2pModelConfig, group: groupModelConfig},
	}
	for _, family := range families {
		t.Run(family.name, func(t *testing.T) {
			p2p := menuCardSignature(t, family.p2p)
			group := menuCardSignature(t, family.group)
			if !reflect.DeepEqual(p2p, group) {
				t.Fatalf("p2p/group menu rendering differs:\np2p:   %#v\ngroup: %#v", p2p, group)
			}
			assertBackActionIsLast(t, family.p2p)
			assertBackActionIsLast(t, family.group)
		})
	}

	a.backend = backendClaude
	a.cfg.Feishu.Backend = backendClaude
	p2pClaudeModelConfig := newModelConfigService(a).renderClaudeModelConfigCard(p2pKey, "menu.model")
	groupClaudeModelConfig := newBindingService(a).renderBindingClaudeModelConfigCard(groupKey, binding)
	if p2p := menuCardSignature(t, p2pClaudeModelConfig); !reflect.DeepEqual(p2p, menuCardSignature(t, groupClaudeModelConfig)) {
		t.Fatalf("p2p/group Claude model configuration differs:\np2p:   %#v\ngroup: %#v", p2p, menuCardSignature(t, groupClaudeModelConfig))
	}
	assertBackActionIsLast(t, p2pClaudeModelConfig)
	assertBackActionIsLast(t, groupClaudeModelConfig)
}

type menuSignature struct {
	Title       string
	Breadcrumb  string
	ElementTags []string
	Selects     []menuSelectSignature
	ButtonTexts []string
}

type menuSelectSignature struct {
	Name        string
	Placeholder string
}

func menuCardSignature(t *testing.T, card map[string]any) menuSignature {
	t.Helper()
	signature := menuSignature{Title: cardHeaderTitle(t, card)}
	for _, elem := range cardElementsForTest(card) {
		tag, _ := elem["tag"].(string)
		signature.ElementTags = append(signature.ElementTags, tag)
		if tag == "markdown" && signature.Breadcrumb == "" {
			content, _ := elem["content"].(string)
			if strings.HasPrefix(content, "当前位置：") {
				signature.Breadcrumb = strings.SplitN(content, "\n", 2)[0]
			}
		}
		if tag == "select_static" {
			placeholder, _ := elem["placeholder"].(map[string]any)
			signature.Selects = append(signature.Selects, menuSelectSignature{
				Name:        fmt.Sprint(elem["name"]),
				Placeholder: fmt.Sprint(placeholder["content"]),
			})
		}
	}
	for _, button := range cardButtonsForTest(card) {
		textValue, _ := button["text"].(map[string]any)
		label, _ := textValue["content"].(string)
		signature.ButtonTexts = append(signature.ButtonTexts, label)
	}
	return signature
}

func assertBackActionIsLast(t *testing.T, card map[string]any) {
	t.Helper()
	elements := cardElementsForTest(card)
	buttons := cardButtonsForTest(card)
	if len(buttons) == 0 {
		return
	}
	backElementIndex := -1
	backButtonIndex := -1
	buttonIndex := 0
	for i, element := range elements {
		for _, button := range cardButtonsForElement(element) {
			textValue, _ := button["text"].(map[string]any)
			if label, _ := textValue["content"].(string); label == "返回上一级" {
				backElementIndex = i
				backButtonIndex = buttonIndex
			}
			buttonIndex++
		}
	}
	if backElementIndex < 0 {
		return
	}
	if backButtonIndex != len(buttons)-1 {
		t.Fatalf("返回上一级 button index = %d, want last button index %d", backButtonIndex, len(buttons)-1)
	}
	for i := backElementIndex + 1; i < len(elements); i++ {
		if isMenuInteractiveElement(elements[i]) {
			t.Fatalf("返回上一级 is not the final menu action: element %d follows it", i)
		}
	}
}

func cardButtonsForElement(element map[string]any) []map[string]any {
	var buttons []map[string]any
	if actions, ok := element["actions"].([]map[string]any); ok {
		buttons = append(buttons, actions...)
	}
	if columns, ok := element["columns"].([]map[string]any); ok {
		for _, column := range columns {
			children, _ := column["elements"].([]map[string]any)
			for _, child := range children {
				if child["tag"] == "button" {
					buttons = append(buttons, child)
				}
			}
		}
	}
	return buttons
}

func isMenuInteractiveElement(element map[string]any) bool {
	switch element["tag"] {
	case "button", "action", "action_set", "column_set", "input", "select_static", "select_person", "date_picker", "picker_datetime", "checkbox", "radio_group", "form":
		return true
	default:
		return len(cardButtonsForElement(element)) > 0
	}
}
