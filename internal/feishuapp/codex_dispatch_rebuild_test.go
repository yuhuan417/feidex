package feishuapp

import (
	"encoding/json"
	"strings"
	"testing"

	"feidex/internal/codexrpc"
	"feidex/internal/feishu"
)

func TestCodexRebuiltClientDispatchesAfterComposition(t *testing.T) {
	for _, frontendID := range []string{"default", "bot-a"} {
		t.Run(frontendID, func(t *testing.T) {
			a, ff, fc := newTestApp(t)
			a.frontendID = frontendID
			recomposeTestApp(a)
			key := a.configView().makeSessionKey(&feishu.InboundMessage{ChatID: "chat-1", ChatType: "group"})
			sub := seedActiveSubmission(t, a, key, "thread-1", "turn-1")

			// Upgrade/recovery ports are composed before the input dispatcher.
			// Their saved runtime dependencies are later used for a new client.
			dispatcher := a.runtimeOwner.Dispatcher
			a.runtimeOwner.Dispatcher = nil
			ports := CodexUpgradePorts(a.cfg, a.ConfigMu(), frontendID, a.frontendConfigIndex,
				a.runtimeOwner, a.BackendRuntimeDeps(), a.bindings.CodexRecovery, nil, nil)
			a.runtimeOwner.Dispatcher = dispatcher
			ports.ConfigureClient(fc)
			notify, request, _ := fc.handlersSnapshot()

			request(codexrpc.RequestEnvelope{
				ID: json.RawMessage(`"cmd-1"`), Method: "item/commandExecution/requestApproval",
				Params: json.RawMessage(`{"threadId":"thread-1","turnId":"turn-1","itemId":"item-1","command":"pwd"}`),
			})
			if pending := a.State().Pending("cmd-1"); pending == nil || pending.Status != "pending" {
				t.Fatalf("approval after runtime rebuild = %+v, want pending", pending)
			}
			if _, err := a.bindings.ServerRequests.CompleteApprovalAction(&feishu.CardAction{
				UserID: "user-1", ActionValue: map[string]any{"request_id": "cmd-1"},
			}, "approval.command.accept"); err != nil {
				t.Fatal(err)
			}
			if pending := a.State().Pending("cmd-1"); pending == nil || pending.Status != "replied" {
				t.Fatalf("approval after reply = %+v, want replied", pending)
			}
			notify("serverRequest/resolved", json.RawMessage(`{"threadId":"thread-1","requestId":"cmd-1"}`))
			if pending := a.State().Pending("cmd-1"); pending == nil || pending.Status != "resolved" {
				t.Fatalf("approval after resolved notification = %+v, want resolved", pending)
			}

			notify("item/completed", json.RawMessage(`{"threadId":"thread-1","turnId":"turn-1","item":{"id":"reply-1","type":"agentMessage","text":"Recovered Codex reply","phase":"final_answer"}}`))
			if got := a.State().Session(key); got == nil || got.ActiveTurnID != "turn-1" {
				t.Fatalf("session before turn/completed = %+v, want active turn", got)
			}
			notify("turn/completed", json.RawMessage(`{"threadId":"thread-1","turn":{"id":"turn-1","status":"completed"}}`))
			a.waitAsync()
			if got := a.State().Session(key); got == nil || got.ActiveTurnID != "" || got.ActiveSubmissionID != "" || got.Status != "idle" {
				t.Fatalf("session after turn/completed = %+v, want idle", got)
			}
			if got := a.State().Submission(sub.ID); got != nil {
				t.Fatalf("submission after completion = %+v, want cleanup", got)
			}
			if body := lastDeliveredCardMarkdown(t, ff); !strings.Contains(body, "Recovered Codex reply") {
				t.Fatalf("final card = %q, want backend reply", body)
			}
		})
	}
}
