package app

import (
	"encoding/json"
	"strings"
	"testing"

	"feidex/internal/codexrpc"
	"feidex/internal/state"
)

func TestHandleNotificationAdditionalBranches(t *testing.T) {
	a, ff, _ := newTestApp(t)
	sub := seedActiveSubmission(t, a, "sess-1", "thread-1", "turn-1")
	if err := a.store.UpdateSubmission(sub.ID, func(s *state.Submission) { s.StatusCardID = "status-1" }); err != nil {
		t.Fatalf("UpdateSubmission(status card) error = %v", err)
	}

	a.handleNotification("item/reasoning/summaryTextDelta", json.RawMessage(`{"threadId":"thread-1","turnId":"turn-1","itemId":"reason-1","delta":"thinking"}`))
	a.handleNotification("item/commandExecution/outputDelta", json.RawMessage(`{"threadId":"thread-1","turnId":"turn-1","itemId":"cmd-1","delta":"output"}`))
	a.handleNotification("item/fileChange/outputDelta", json.RawMessage(`{"threadId":"thread-1","turnId":"turn-1","itemId":"file-1","delta":"diff"}`))
	a.handleNotification("item/completed", json.RawMessage(`{"threadId":"thread-1","turnId":"turn-1","item":{"id":"agent-1","type":"agent_message","text":"done"}}`))
	a.handleNotification("turn/started", json.RawMessage(`{"threadId":"thread-1","turn":{"id":"turn-2"}}`))
	a.handleNotification("turn/completed", json.RawMessage(`{"threadId":"thread-1","turn":{"id":"turn-1","status":"failed"}}`))

	if len(ff.replyCards) == 0 && len(ff.replyTextWithIDs) == 0 {
		t.Fatal("expected completed/failed notifications to deliver output")
	}
}

func TestHandleServerRequestRoutesKnownMethods(t *testing.T) {
	a, ff, fc := newTestApp(t)
	seedActiveSubmission(t, a, "sess-1", "thread-1", "turn-1")

	a.handleServerRequest(codexrpc.RequestEnvelope{ID: json.RawMessage(`"cmd-1"`), Method: "item/commandExecution/requestApproval", Params: json.RawMessage(`{"threadId":"thread-1","turnId":"turn-1","itemId":"item-1","command":"pwd"}`)})
	a.handleServerRequest(codexrpc.RequestEnvelope{ID: json.RawMessage(`"file-1"`), Method: "item/fileChange/requestApproval", Params: json.RawMessage(`{"threadId":"thread-1","turnId":"turn-1","itemId":"item-2","files":["main.go"]}`)})
	a.handleServerRequest(codexrpc.RequestEnvelope{ID: json.RawMessage(`"perm-1"`), Method: "item/permissions/requestApproval", Params: json.RawMessage(`{"threadId":"thread-1","turnId":"turn-1","itemId":"item-3","permissions":{"mode":"read"}}`)})
	a.handleServerRequest(codexrpc.RequestEnvelope{ID: json.RawMessage(`"input-1"`), Method: "item/tool/requestUserInput", Params: json.RawMessage(`{"threadId":"thread-1","turnId":"turn-1","itemId":"item-4","questions":[{"id":"q1","question":"Pick","options":[{"label":"A"}]}]}`)})
	a.handleServerRequest(codexrpc.RequestEnvelope{ID: json.RawMessage(`"elicit-1"`), Method: "mcpServer/elicitation/request", Params: json.RawMessage(`{"mode":"url","threadId":"thread-1","turnId":"turn-1","serverName":"srv","message":"open","url":"https://example.test"}`)})

	if len(ff.sendCards) < 5 {
		t.Fatalf("expected routed server requests to send cards, got %d", len(ff.sendCards))
	}
	if len(fc.replyErrors) != 0 {
		t.Fatalf("unexpected replyErrors for known server requests: %+v", fc.replyErrors)
	}
}

func TestFinishTurnAndSubmissionCardStatuses(t *testing.T) {
	a, _, _ := newTestApp(t)
	sub := seedActiveSubmission(t, a, "sess-1", "thread-1", "turn-1")
	sub.OutputText = "partial"
	sub.Status = "running"
	if err := a.store.UpdateSubmission(sub.ID, func(s *state.Submission) {
		s.OutputText = "partial"
		s.Status = "running"
	}); err != nil {
		t.Fatalf("UpdateSubmission() error = %v", err)
	}

	a.finishTurn("thread-1", "turn-1", "interrupted")
	if sess := a.store.GetSession("sess-1"); sess == nil || sess.Status != "idle" {
		t.Fatalf("session after interrupted finish = %+v", sess)
	}

	a.finishTurn("missing", "missing", "failed")

	sub = &state.Submission{SessionKey: "sess-1"}
	for _, status := range []string{"queued", "waiting_approval", "waiting_user_input", "completed", "failed", "interrupted"} {
		card := a.renderSubmissionCard(sub, status)
		body := cardMarkdownContent(t, card)
		if !strings.Contains(body, "内容:") {
			t.Fatalf("renderSubmissionCard(%s) body = %q", status, body)
		}
	}
	if got := submissionStatusPlaceholder("waiting_approval"); got != "等待审批..." {
		t.Fatalf("submissionStatusPlaceholder(waiting_approval) = %q", got)
	}
	if got := submissionStatusPlaceholder("interrupted"); got != "任务已中断。" {
		t.Fatalf("submissionStatusPlaceholder(interrupted) = %q", got)
	}
	if got := submissionStatusPlaceholder("other"); got != "任务状态未知。" {
		t.Fatalf("submissionStatusPlaceholder(other) = %q", got)
	}
}

func TestNotificationHelperWrappers(t *testing.T) {
	a, ff, fc := newTestApp(t)
	sub := seedActiveSubmission(t, a, "sess-1", "thread-1", "turn-1")

	a.sendPermissionsCard(json.RawMessage(`"perm-wrap"`), "thread-1", "turn-1", "item-1", "body", map[string]any{"mode": "read"})
	if pending := a.store.PendingByID("perm-wrap"); pending == nil || pending.Kind != "permissions" {
		t.Fatalf("sendPermissionsCard() pending = %+v", pending)
	}
	if len(ff.sendCards) == 0 {
		t.Fatal("sendPermissionsCard() should send a card")
	}

	fc.replyErrors = nil
	a.onFileApproval(codexrpc.RequestEnvelope{ID: json.RawMessage(`"bad-file"`), Params: json.RawMessage(`{`)})
	a.onPermissionsApproval(codexrpc.RequestEnvelope{ID: json.RawMessage(`"bad-perm"`), Params: json.RawMessage(`{`)})
	a.onToolUserInput(codexrpc.RequestEnvelope{ID: json.RawMessage(`"bad-input"`), Params: json.RawMessage(`{`)})
	a.onMcpElicitationRequest(codexrpc.RequestEnvelope{ID: json.RawMessage(`"bad-json"`), Params: json.RawMessage(`{`)})
	if len(fc.replyErrors) < 4 {
		t.Fatalf("expected invalid request params to reply with errors, got %+v", fc.replyErrors)
	}

	_ = sub
}
