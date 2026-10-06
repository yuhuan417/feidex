package feishuapp

import (
	"context"
	"errors"
	"strings"
	"testing"

	domainbackend "feidex/internal/domain/backend"
	"feidex/internal/domain/conversation"
	catalog "feidex/internal/domain/modelconfig"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

func clickModelMenuAction(t *testing.T, a *Frontend, msg *feishu.InboundMessage, card map[string]any, name string) map[string]any {
	t.Helper()
	for _, button := range cardButtonsForTest(card) {
		value, _ := button["value"].(map[string]any)
		if len(value) == 0 {
			behaviors, _ := button["behaviors"].([]map[string]any)
			if len(behaviors) > 0 {
				value, _ = behaviors[0]["value"].(map[string]any)
			}
		}
		if value["action"] != name {
			continue
		}
		if value["session_key"] != a.configView().makeSessionKey(msg) {
			t.Fatalf("%s has wrong session key: %#v", name, value)
		}
		resp, err := newCardActionService(a).dispatch(&feishu.CardAction{
			ActionValue: value, UserID: msg.UserID, ChatID: msg.ChatID, MessageID: "card-model-nav",
		})
		if err != nil || resp == nil || resp.Card == nil {
			t.Fatalf("click(%s) = %#v, %v", name, resp, err)
		}
		result, ok := resp.Card.Data.(map[string]any)
		if !ok {
			t.Fatalf("click(%s) card = %T", name, resp.Card.Data)
		}
		return result
	}
	t.Fatalf("card has no %s action: %s", name, mustJSON(card))
	return nil
}

func TestModelAndFastMenusAreReachableFromMenu(t *testing.T) {
	for _, chatType := range []string{"p2p", "group"} {
		t.Run(chatType, func(t *testing.T) {
			a, _, fc := newTestApp(t)
			a.frontendID = "bot-model-nav"
			recomposeTestApp(a)
			msg := &feishu.InboundMessage{ChatID: "chat-model-nav", ChatType: chatType, UserID: "user-1"}
			sessionKey := a.configView().makeSessionKey(msg)
			if err := a.State().SaveSession(&conversation.Session{
				Key: sessionKey, ChatID: msg.ChatID, ChatType: chatType,
				WorkspaceID: "default", ActiveThreadID: "thread-1", ActiveThreadWorkspaceID: "default",
			}); err != nil {
				t.Fatal(err)
			}
			if chatType == "group" {
				if err := a.State().SaveAgentBinding(&state.AgentBinding{
					ID:         defaultBindingID(a.frontendID, chatType, msg.ChatID),
					FrontendID: a.frontendID, ChatID: msg.ChatID, ChatType: chatType,
					WorkspaceID: "default", Status: state.AgentBindingStatusActive.String(),
				}); err != nil {
					t.Fatal(err)
				}
			}
			fc.callHook = func(_ context.Context, method string, _ any, out any) error {
				if method != "model/list" {
					return errors.New("unexpected method: " + method)
				}
				*out.(*catalog.ModelListResult) = catalog.ModelListResult{Data: []catalog.ModelListEntry{{
					ID: "gpt-5", DisplayName: "GPT-5", DefaultReasoningEffort: "medium", IsDefault: true,
				}}}
				return nil
			}
			root := renderCommandMenuCard(a, sessionKey)
			if !containsMenuAction(root, "menu.group.model") {
				t.Fatal("/menu has no model configuration entry")
			}
			model := clickModelMenuAction(t, a, msg, root, "menu.group.model")
			if len(cardSelectStaticForTest(model)) != 2 || !containsMenuAction(model, "menu.fast") {
				t.Fatalf("root model action did not open model configuration: %s", mustJSON(model))
			}
			if containsMenuAction(model, "menu.model") {
				t.Fatal("model configuration unexpectedly links to an overview card")
			}
			assertBackActionIsLast(t, model)
			if !containsMenuAction(model, "menu.model_auxiliary") {
				t.Fatal("model configuration has no auxiliary-model entry")
			}
			auxiliary := clickModelMenuAction(t, a, msg, model, "menu.model_auxiliary")
			if !containsMenuAction(auxiliary, "menu.model") {
				t.Fatalf("auxiliary model page cannot return to model configuration: %s", mustJSON(auxiliary))
			}
			if body := cardMarkdownContent(t, auxiliary); !strings.Contains(body, "主菜单 / 模型配置 / 辅助模型配置") {
				t.Fatalf("auxiliary model page has no menu breadcrumb: %q", body)
			}
			assertBackActionIsLast(t, auxiliary)
			backFromAuxiliary := clickModelMenuAction(t, a, msg, auxiliary, "menu.model")
			if !containsMenuAction(backFromAuxiliary, "menu.fast") {
				t.Fatal("auxiliary model back action did not restore model configuration")
			}

			fast := clickModelMenuAction(t, a, msg, backFromAuxiliary, "menu.fast")
			fastBody := cardMarkdownContent(t, fast)
			if !containsMenuAction(fast, "menu.group.model") || (!strings.Contains(fastBody, "响应速度") && !strings.Contains(fastBody, "service tier")) {
				t.Fatalf("model configuration did not open response-speed settings: %s", mustJSON(fast))
			}
			assertBackActionIsLast(t, fast)
			backToModel := clickModelMenuAction(t, a, msg, fast, "menu.group.model")
			if len(cardSelectStaticForTest(backToModel)) != 2 || !containsMenuAction(backToModel, "menu.fast") {
				t.Fatalf("response-speed back action did not restore model configuration: %s", mustJSON(backToModel))
			}
			if !containsMenuAction(backToModel, "menu.root") {
				t.Fatal("model configuration cannot return to /menu")
			}
			if got := clickModelMenuAction(t, a, msg, backToModel, "menu.root"); !containsMenuAction(got, "menu.group.model") {
				t.Fatal("model configuration back action did not restore /menu")
			}
		})
	}
}

func TestClaudeModelMenuAndAuxiliaryAreReachableFromMenu(t *testing.T) {
	for _, chatType := range []string{"p2p", "group"} {
		t.Run(chatType, func(t *testing.T) {
			a, _, _ := newTestApp(t)
			a.frontendID = "bot-claude-model-nav"
			recomposeTestApp(a)
			selectBackendForTest(a, domainbackend.BackendClaude)
			a.cfg.Feishu.Backend = domainbackend.BackendClaude
			msg := &feishu.InboundMessage{ChatID: "chat-claude-model-nav", ChatType: chatType, UserID: "user-1"}
			sessionKey := a.configView().makeSessionKey(msg)
			if chatType == "group" {
				if err := a.State().SaveAgentBinding(&state.AgentBinding{
					ID:         defaultBindingID(a.frontendID, chatType, msg.ChatID),
					FrontendID: a.frontendID, ChatID: msg.ChatID, ChatType: chatType,
					WorkspaceID: "default", Status: state.AgentBindingStatusActive.String(),
				}); err != nil {
					t.Fatal(err)
				}
			}
			root := renderCommandMenuCard(a, sessionKey)
			if !containsMenuAction(root, "menu.group.model") {
				t.Fatal("Claude /menu has no model configuration entry")
			}
			model := clickModelMenuAction(t, a, msg, root, "menu.group.model")
			if len(cardSelectStaticForTest(model)) != 2 || containsMenuAction(model, "menu.fast") || !containsMenuAction(model, "menu.model_auxiliary") {
				t.Fatalf("Claude model entry did not open the expected configuration page: %s", mustJSON(model))
			}
			auxiliary := clickModelMenuAction(t, a, msg, model, "menu.model_auxiliary")
			if !containsMenuAction(auxiliary, "menu.model") {
				t.Fatalf("Claude auxiliary model page cannot return to model configuration: %s", mustJSON(auxiliary))
			}
			if body := cardMarkdownContent(t, auxiliary); !strings.Contains(body, "主菜单 / 模型配置 / 辅助模型配置") {
				t.Fatalf("Claude auxiliary model page has no menu breadcrumb: %q", body)
			}
			assertBackActionIsLast(t, auxiliary)
			backToModel := clickModelMenuAction(t, a, msg, auxiliary, "menu.model")
			if !containsMenuAction(backToModel, "menu.model_auxiliary") {
				t.Fatal("Claude auxiliary model back action did not restore model configuration")
			}
			if backToRoot := clickModelMenuAction(t, a, msg, backToModel, "menu.root"); !containsMenuAction(backToRoot, "menu.group.model") {
				t.Fatal("Claude model back action did not restore /menu")
			}
		})
	}
}

func TestModelMenuCatalogFailureReturnsToMenuWithoutOverview(t *testing.T) {
	for _, chatType := range []string{"p2p", "group"} {
		t.Run(chatType, func(t *testing.T) {
			a, _, fc := newTestApp(t)
			a.frontendID = "bot-model-failure"
			recomposeTestApp(a)
			fc.callHook = func(_ context.Context, method string, _ any, _ any) error {
				if method == "model/list" {
					return errors.New("catalog unavailable")
				}
				return errors.New("unexpected method: " + method)
			}
			msg := &feishu.InboundMessage{ChatID: "chat-model-failure", ChatType: chatType, UserID: "user-1"}
			sessionKey := a.configView().makeSessionKey(msg)
			if chatType == "group" {
				binding := &state.AgentBinding{
					ID:         defaultBindingID(a.frontendID, chatType, msg.ChatID),
					FrontendID: a.frontendID, ChatID: msg.ChatID, ChatType: chatType,
					WorkspaceID: "default", Status: state.AgentBindingStatusActive.String(),
				}
				if err := a.State().SaveAgentBinding(binding); err != nil {
					t.Fatal(err)
				}
				errorCard := a.bindings.BindingCommands.renderBindingModelConfigOrErrorCard(sessionKey, binding)
				if body := cardMarkdownContent(t, errorCard); !strings.Contains(body, "catalog unavailable") {
					t.Fatalf("group model refresh failure is hidden: %q", body)
				}
				if !containsMenuAction(errorCard, "menu.group.model") || !containsMenuAction(errorCard, "menu.root") || containsMenuAction(errorCard, "menu.model") {
					t.Fatalf("group model refresh failure restored an overview card: %s", mustJSON(errorCard))
				}
				assertBackActionIsLast(t, errorCard)
			}
			resp, err := newCardActionService(a).dispatch(&feishu.CardAction{
				ActionValue: map[string]any{"action": "menu.group.model", "session_key": sessionKey},
				UserID:      msg.UserID, ChatID: msg.ChatID, MessageID: "card-model-failure",
			})
			if err != nil || resp == nil || resp.Toast == nil || resp.Toast.Type != "warning" || !strings.Contains(resp.Toast.Content, "catalog unavailable") || resp.Card == nil {
				t.Fatalf("model catalog failure = %#v, %v", resp, err)
			}
			card := resp.Card.Data.(map[string]any)
			if !containsMenuAction(card, "menu.group.model") || containsMenuAction(card, "menu.model") || containsMenuAction(card, "menu.fast") {
				t.Fatalf("catalog failure should restore /menu, got %s", mustJSON(card))
			}
		})
	}
}
