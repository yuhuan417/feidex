package codex

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"feidex/internal/application/backendops"
	"feidex/internal/codexrpc"
	"feidex/internal/domain/conversation"
	"feidex/internal/domain/submission"
)

type gatewayRPCFunc func(context.Context, string, any, any) error

func (f gatewayRPCFunc) Call(ctx context.Context, method string, in, out any) error {
	return f(ctx, method, in, out)
}
func TestGoalBudgetEncodingPreservesOmitNullAndNumber(t *testing.T) {
	budget := int64(1200)
	for _, tc := range []struct {
		name   string
		update *backendops.BudgetUpdate
		want   string
	}{{"omit", nil, `{"threadId":"thread"}`}, {"clear", &backendops.BudgetUpdate{}, `{"threadId":"thread","tokenBudget":null}`}, {"set", &backendops.BudgetUpdate{Value: &budget}, `{"threadId":"thread","tokenBudget":1200}`}} {
		t.Run(tc.name, func(t *testing.T) {
			g := Gateway{Client: gatewayRPCFunc(func(_ context.Context, method string, in, out any) error {
				if method != "thread/goal/set" {
					t.Fatalf("method = %q", method)
				}
				encoded, err := json.Marshal(in)
				if err != nil {
					t.Fatal(err)
				}
				if string(encoded) != tc.want {
					t.Fatalf("payload = %s", encoded)
				}
				out.(*codexrpc.ThreadGoalSetResponse).Goal = conversation.ThreadGoal{ThreadID: "thread", Objective: "goal"}
				return nil
			})}
			result, err := g.SetGoal(context.Background(), backendops.GoalUpdate{ThreadID: "thread", TokenBudget: tc.update})
			if err != nil || result.Goal.Objective != "goal" {
				t.Fatalf("result = %+v, %v", result, err)
			}
		})
	}
}
func TestTurnGatewayRejectsEmptyInputBeforeTransportAndPreservesResult(t *testing.T) {
	calls := 0
	g := Gateway{Client: gatewayRPCFunc(func(_ context.Context, method string, in, out any) error {
		calls++
		if method != "turn/start" {
			t.Fatalf("method = %s", method)
		}
		params := in.(map[string]any)
		if !reflect.DeepEqual(params["sandboxPolicy"], map[string]any{"type": "workspaceWrite"}) {
			t.Fatalf("sandbox = %v", params["sandboxPolicy"])
		}
		mode := params["collaborationMode"].(*codexrpc.CollaborationMode)
		if mode.Settings.Model != "plan-model" || *mode.Settings.ReasoningEffort != "high" || mode.Settings.DeveloperInstructions != nil {
			t.Fatalf("mode = %+v", mode)
		}
		out.(*codexrpc.TurnStartResult).Turn.ID = "returned-turn"
		return nil
	})}
	if _, err := g.StartTurn(context.Background(), backendops.StartTurnRequest{Submission: &submission.Submission{ID: "empty"}}); err == nil || calls != 0 {
		t.Fatalf("empty input = %v, calls %d", err, calls)
	}
	result, err := g.StartTurn(context.Background(), backendops.StartTurnRequest{ThreadID: "thread", Submission: &submission.Submission{InputText: "hello"}, SandboxMode: "workspace-write", Collaboration: &conversation.SessionCollaborationMode{Mode: "plan", Model: "plan-model", ReasoningEffort: "high"}})
	if err != nil || result.ID != "returned-turn" || calls != 1 {
		t.Fatalf("result = %+v, %v", result, err)
	}
}

func TestHistoryGatewayConvertsThreadReadIntoSemanticTurns(t *testing.T) {
	g := Gateway{Client: gatewayRPCFunc(func(_ context.Context, method string, _ any, out any) error {
		if method != "thread/read" {
			t.Fatalf("method = %q", method)
		}
		name := "Thread"
		result := out.(*codexrpc.ThreadReadResult)
		result.Thread = codexrpc.ThreadReadThread{
			ID: "thread", Name: &name, Preview: "preview", Cwd: "/repo",
			Turns: []codexrpc.ThreadReadTurn{{
				ID: "turn-1", Status: "completed",
				Items: []codexrpc.ThreadReadItem{
					{Type: "userMessage", Content: json.RawMessage(`[{"type":"text","text":"hello"}]`)},
					{Type: "agentMessage", Text: "world"},
				},
			}},
		}
		return nil
	})}
	history, err := g.ReadThreadHistory(context.Background(), "thread")
	if err != nil {
		t.Fatal(err)
	}
	if history.ID != "thread" || history.Name != "Thread" || history.Cwd != "/repo" || len(history.Turns) != 1 {
		t.Fatalf("history = %+v", history)
	}
	turn := history.Turns[0]
	if turn.Ordinal != 1 || turn.InputPreview != "hello" || len(turn.Inputs) != 1 || turn.Inputs[0] != "hello" || len(turn.Outputs) != 1 || turn.Outputs[0] != "world" {
		t.Fatalf("turn = %+v", turn)
	}
}

type replyRecorder struct {
	calls   int
	token   json.RawMessage
	payload any
}

func (r *replyRecorder) Reply(token json.RawMessage, payload any) error {
	r.calls++
	r.token = token
	r.payload = payload
	return nil
}
func (r *replyRecorder) ReplyError(token json.RawMessage, code int, message string) error {
	r.calls++
	r.token = token
	r.payload = backendops.ResponseError{Code: code, Message: message}
	return nil
}
func TestResponseGatewayPreservesOpaqueTokenAndCancellation(t *testing.T) {
	for _, token := range []string{`42`, `"42"`} {
		r := &replyRecorder{}
		if err := Respond(context.Background(), r, backendops.Response{Token: []byte(token), Payload: json.RawMessage(`{"decision":"accept"}`)}); err != nil || string(r.token) != token {
			t.Fatalf("token %q: %v, %s", token, err, r.token)
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := Respond(ctx, r, backendops.Response{Token: []byte(token)}); !errors.Is(err, context.Canceled) || r.calls != 1 {
			t.Fatalf("cancellation = %v, calls %d", err, r.calls)
		}
	}
}
