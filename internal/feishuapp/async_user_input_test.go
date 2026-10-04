package feishuapp

import (
	"context"
	"encoding/json"
	"errors"
	"feidex/internal/adapter/feishu/pendingforms"
	appreviewcmd "feidex/internal/adapter/feishu/reviewcmd"
	"feidex/internal/codexrpc"
	"feidex/internal/config"
	"feidex/internal/domain/conversation"
	domainsubmission "feidex/internal/domain/submission"
	"feidex/internal/feishu"
	"feidex/internal/state"
	"strings"
	"testing"

	"feidex/internal/adapter/feishu/turnitem"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

// Matches the Codex 0.153.4 item emitted by request_user_input_async.
const asyncQuestionNotification = `{"threadId":"thread-1","turnId":"turn-1","item":{"id":"ask-1","type":"agentMessage","phase":"final_answer","delivery":"async","text":"Which behavior do you see?","questions":[{"title":"Which behavior do you see?","options":["Old bot responds","No bot responds","Several bots respond"]}]}}`

func seedAsyncUserInput(t *testing.T, a *App) (*domainsubmission.Submission, *state.PendingRequest) {
	t.Helper()
	msg := &feishu.InboundMessage{ChatID: "chat-1", ChatType: "p2p", UserID: "user-1"}
	sessionKey, sub := seedActiveSubmissionForInboundMessage(t, a, msg, "thread-1", "turn-1")
	_, err := a.State().UpdateSession(sessionKey, func(s *conversation.Session) { s.ActiveThreadWorkspaceID = sub.WorkspaceID })
	if err != nil {
		t.Fatal(err)
	}
	a.bindings.TurnPresentation.NoteTurnStarted(sessionKey, sub)
	handleNotification(a.BackendRuntimeDeps(), "item/completed", json.RawMessage(asyncQuestionNotification))
	pending := a.store.PendingByID("async_user_input:turn-1:ask-1")
	if pending == nil {
		t.Fatal("async question did not create a local pending form")
	}
	return sub, pending
}

func asyncAnswerAction(pending *state.PendingRequest) *feishu.CardAction {
	return &feishu.CardAction{
		UserID: "user-1", ChatID: "chat-1", MessageID: pending.FeishuMsgID,
		ActionValue: map[string]any{"action": "async_user_input.answer", "request_id": pending.ID},
		FormValue:   map[string]any{"q1": "No bot responds"},
	}
}

func TestAsyncUserInputStaysSeparateFromFinalInEveryQuietMode(t *testing.T) {
	for _, mode := range []config.QuietMode{config.QuietModeVerbose, config.QuietModeProgress, config.QuietModeNormal, config.QuietModeFinal} {
		t.Run(mode.String(), func(t *testing.T) {
			a, ff, fc := newTestApp(t)
			a.cfg.Feishu.Quiet = mode
			ff.replyCardIDs = []string{"question-card", "final-card"}
			sub, pending := seedAsyncUserInput(t, a)
			if a.bindings.TurnPresentation.StreamSawFinal("turn-1") {
				t.Fatal("async question was treated as final output")
			}
			if got := a.State().Submission(sub.ID).Status; got != "running" {
				t.Fatalf("submission status = %s, want running", got)
			}
			if pending.RequestIDRaw != "" || len(fc.replies) != 0 || len(fc.replyErrors) != 0 {
				t.Fatal("async question must not use server-request replies")
			}
			if len(ff.replyCards) != 1 || strings.Contains(cardHeaderTitle(t, ff.replyCards[0]), "最终答复") {
				t.Fatal("expected a standalone question form")
			}
			encoded := mustJSON(ff.replyCards[0])
			for _, want := range []string{"async_user_input.answer", "async_user_input.cancel", "No bot responds", "q1__other", "initial_option"} {
				if !strings.Contains(encoded, want) {
					t.Fatalf("question form missing %q", want)
				}
			}
			// Replayed completion must not send a second question card.
			handleNotification(a.BackendRuntimeDeps(), "item/completed", json.RawMessage(asyncQuestionNotification))
			// A later question in the same turn has its own form and survives too.
			handleNotification(a.BackendRuntimeDeps(), "item/completed", json.RawMessage(strings.ReplaceAll(asyncQuestionNotification, "ask-1", "ask-2")))
			handleNotification(a.BackendRuntimeDeps(), "item/completed", json.RawMessage(`{"threadId":"thread-1","turnId":"turn-1","item":{"id":"final-1","type":"agentMessage","phase":"final_answer","text":"Finished the work."}}`))
			finishTurn(a.bindings.Turns, "thread-1", "turn-1", "completed")
			a.waitAsync()
			if len(ff.replyCards) != 3 || len(ff.patchedCards) != 0 {
				t.Fatalf("cards = %d, patches = %d; question must survive final output", len(ff.replyCards), len(ff.patchedCards))
			}
			if a.store.PendingByID(pending.ID).Status != "pending" {
				t.Fatal("turn completion discarded an unanswered async question")
			}
		})
	}
}

func TestAsyncUserInputQuietWorkingCardBoundaries(t *testing.T) {
	for _, substantive := range []bool{false, true} {
		t.Run(map[bool]string{false: "reasoning", true: "command"}[substantive], func(t *testing.T) {
			a, ff, _ := newTestApp(t)
			a.cfg.Feishu.Quiet = config.QuietModeProgress
			sub := seedActiveSubmission(t, a, "sess-1", "thread-1", "turn-1")
			a.bindings.TurnPresentation.NoteTurnStarted("sess-1", sub)
			item := map[string]any{"id": "work-1", "type": "reasoning"}
			if substantive {
				item = map[string]any{"id": "work-1", "type": "commandExecution", "command": "ls", "status": "completed"}
			}
			a.bindings.TurnPresentation.CompleteTurnItem(context.Background(), "thread-1", "turn-1", "work-1", turnitem.NewProtocolItemWithID("work-1", item))
			handleNotification(a.BackendRuntimeDeps(), "item/completed", json.RawMessage(asyncQuestionNotification))
			questionPatches := len(ff.patchedCards)
			if substantive && questionPatches != 0 || !substantive && questionPatches != 1 {
				t.Fatalf("question reused working card incorrectly: substantive=%t, patches=%d", substantive, questionPatches)
			}
			handleNotification(a.BackendRuntimeDeps(), "item/completed", json.RawMessage(`{"threadId":"thread-1","turnId":"turn-1","item":{"id":"final-1","type":"agentMessage","phase":"final_answer","text":"Done"}}`))
			if len(ff.patchedCards) != questionPatches {
				t.Fatal("final answer patched the question or substantive working card")
			}
		})
	}
}

func TestAsyncUserInputIsNotPromotedAtTurnCompletion(t *testing.T) {
	a, ff, _ := newTestApp(t)
	a.cfg.Feishu.Quiet = config.QuietModeNormal
	_, pending := seedAsyncUserInput(t, a)
	finishTurn(a.bindings.Turns, "thread-1", "turn-1", "completed")
	a.waitAsync()
	if len(ff.patchedCards) != 0 || len(ff.replyCards) != 2 {
		t.Fatalf("completion replaced question: cards=%d patches=%d", len(ff.replyCards), len(ff.patchedCards))
	}
	if a.store.PendingByID(pending.ID).Status != "pending" {
		t.Fatal("completion resolved local question")
	}
}

func TestAsyncUserInputAnswerAcknowledgesBeforeSteerAndRejectsDuplicates(t *testing.T) {
	a, ff, fc := newTestApp(t)
	_, pending := seedAsyncUserInput(t, a)
	started, release := make(chan struct{}), make(chan struct{})
	defer a.waitAsync()
	defer close(release)
	fc.callHook = func(_ context.Context, method string, params any, _ any) error {
		if method != "turn/steer" {
			t.Errorf("unexpected RPC %s", method)
			return errors.New("unexpected RPC")
		}
		p := params.(map[string]any)
		if p["threadId"] != "thread-1" || p["expectedTurnId"] != "turn-1" || !strings.Contains(mustJSON(p["input"]), "No bot responds") || !strings.Contains(mustJSON(p["input"]), "Which behavior") {
			t.Errorf("wrong answer routing: %v", p)
		}
		close(started)
		<-release
		return nil
	}
	resp, err := callActionWithTimeout(t, func() (*callback.CardActionTriggerResponse, error) {
		return pendingCardActionHandlers(nil, appreviewcmd.ReviewFormService{})["async_user_input.answer"](cardActionService{app: a}, asyncAnswerAction(pending))
	})
	if err != nil || resp.Toast == nil || resp.Toast.Type != "info" || resp.Card != nil {
		t.Fatalf("callback = %+v, %v; want fast toast only", resp, err)
	}
	<-started
	resp, err = completeAsyncUserInput(a, asyncAnswerAction(pending), false)
	if err != nil || resp.Toast.Type != "warning" {
		t.Fatal("duplicate answer was accepted")
	}
	if a.store.PendingByID(pending.ID).Status != "replied" || len(ff.patchedCardsSnapshot()) != 0 {
		t.Fatal("input resolved before backend accepted the answer")
	}
}

func TestAsyncUserInputAnswerAfterCompletionStartsSameThread(t *testing.T) {
	a, _, fc := newTestApp(t)
	sub, pending := seedAsyncUserInput(t, a)
	finishTurn(a.bindings.Turns, "thread-1", "turn-1", "completed")
	a.waitAsync()
	var starts int
	fc.callHook = func(_ context.Context, method string, params any, out any) error {
		if method != "turn/start" {
			t.Errorf("unexpected RPC: %s", method)
			return errors.New("unexpected RPC")
		}
		starts++
		if p := params.(map[string]any); p["threadId"] != "thread-1" || !strings.Contains(mustJSON(p["input"]), "No bot responds") {
			t.Errorf("wrong follow-up: %v", p)
		}
		out.(*codexrpc.TurnStartResult).Turn.ID = "turn-2"
		return nil
	}
	resp, err := completeAsyncUserInput(a, asyncAnswerAction(pending), false)
	if err != nil || resp.Toast.Type != "info" {
		t.Fatalf("submit = %+v, %v", resp, err)
	}
	a.waitAsync()
	if starts != 1 || a.State().Session(sub.SessionKey).ActiveTurnID != "turn-2" || a.store.PendingByID(pending.ID).Status != "resolved" {
		t.Fatal("answer did not start a follow-up on the same thread")
	}
	if len(fc.replies) != 0 || len(fc.replyErrors) != 0 {
		t.Fatal("used server-request reply for async answer")
	}
}

func TestAsyncUserInputAnswerFailureCanRetry(t *testing.T) {
	a, ff, fc := newTestApp(t)
	_, pending := seedAsyncUserInput(t, a)
	fc.callErr = errors.New("temporarily unavailable")
	_, _ = completeAsyncUserInput(a, asyncAnswerAction(pending), false)
	a.waitAsync()
	if a.store.PendingByID(pending.ID).Status != "pending" || !strings.Contains(mustJSON(ff.patchedCardsSnapshot()), "async_user_input.answer") {
		t.Fatal("failed submission did not preserve retryable question form")
	}
	fc.callErr = nil
	_, _ = completeAsyncUserInput(a, asyncAnswerAction(pending), false)
	a.waitAsync()
	if a.store.PendingByID(pending.ID).Status != "resolved" {
		t.Fatal("retry did not resolve the local form")
	}
}

func TestAsyncUserInputRejectsWrongOwnerAndSwitchedThread(t *testing.T) {
	a, _, fc := newTestApp(t)
	sub, pending := seedAsyncUserInput(t, a)
	fc.callHook = func(context.Context, string, any, any) error { t.Error("unexpected backend call"); return nil }
	action := asyncAnswerAction(pending)
	action.UserID = "someone-else"
	resp, _ := completeAsyncUserInput(a, action, false)
	if resp.Toast.Type != "warning" {
		t.Fatal("another user could answer")
	}
	_, _ = a.State().UpdateSession(sub.SessionKey, func(s *conversation.Session) { s.ActiveThreadID = "thread-other" })
	resp, _ = completeAsyncUserInput(a, asyncAnswerAction(pending), false)
	if resp.Toast.Type != "warning" {
		t.Fatal("answer crossed thread boundary")
	}
	resp, _ = completeAsyncUserInput(a, asyncAnswerAction(pending), true)
	if resp.Toast.Type != "success" || a.store.PendingByID(pending.ID).Status != "resolved" || len(fc.replies) != 0 {
		t.Fatal("cancel should only resolve the local question")
	}
}

func TestAsyncUserInputFreeTextAndMultipleQuestions(t *testing.T) {
	payload := pendingforms.ToolUserInputPayload{Questions: []pendingforms.ToolUserInputQuestion{
		{ID: "q1", Question: "What next?", IsOther: true, Options: []pendingforms.ToolUserInputOption{{Label: "A, then B"}, {Label: "B"}}},
		{ID: "q2", Question: "Details?"},
	}}
	for _, other := range []string{"", "C, then D\nkeep this line"} {
		drafts := pendingforms.FormDrafts{Values: map[string]string{"q1": "A, then B", "q1__other": other, "q2": "details, with punctuation"}}
		text, err := pendingforms.AsyncUserInputAnswerText(payload, drafts)
		want := "A, then B"
		if other != "" {
			want = other
		}
		if err != nil || !strings.Contains(text, want) || !strings.Contains(text, "Details?\n回答：details, with punctuation") {
			t.Fatalf("answer = %q, %v", text, err)
		}
	}
}
