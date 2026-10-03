package codex

import (
	"context"
	"encoding/json"
	codexhistory "feidex/internal/adapter/backend/codex/history"
	"feidex/internal/application/backendops"
	"feidex/internal/codexrpc"
	"feidex/internal/domain/conversation"
	"feidex/internal/domain/modelconfig"
	"feidex/internal/domain/skill"
	"feidex/internal/domain/submission"
	"fmt"
	"strings"
)

// RPCClient is the minimal transport port exposed by Codex app-server.
type RPCClient interface {
	Call(context.Context, string, any, any) error
}

// Gateway owns Codex method names and wire request/response shapes. Callers
// consume semantic operations and do not construct raw protocol envelopes.
type Gateway struct{ Client RPCClient }

func (g Gateway) call(ctx context.Context, method string, params, out any) error {
	if g.Client == nil {
		return fmt.Errorf("codex client unavailable")
	}
	return g.Client.Call(ctx, method, params, out)
}

func (g Gateway) StartTurn(ctx context.Context, request backendops.StartTurnRequest) (backendops.TurnResult, error) {
	if request.Submission == nil {
		return backendops.TurnResult{}, fmt.Errorf("nil submission")
	}
	if len(BuildTurnInputs(request.Submission)) == 0 {
		return backendops.TurnResult{}, fmt.Errorf("submission %q has no input", request.Submission.ID)
	}
	var out codexrpc.TurnStartResult
	err := g.call(ctx, "turn/start", TurnParams(request), &out)
	return backendops.TurnResult{ID: out.Turn.ID, Status: out.Turn.Status}, err
}
func (g Gateway) SteerTurn(ctx context.Context, threadID, expectedTurnID string, input *submission.Submission) error {
	return g.call(ctx, "turn/steer", map[string]any{"threadId": threadID, "expectedTurnId": expectedTurnID, "input": BuildTurnInputs(input)}, nil)
}
func (g Gateway) ReadThreadTurns(ctx context.Context, threadID string) (backendops.ThreadTurns, error) {
	var out codexrpc.ThreadReadResult
	err := g.call(ctx, "thread/read", map[string]any{"threadId": strings.TrimSpace(threadID), "includeTurns": true}, &out)
	result := backendops.ThreadTurns{ThreadID: out.Thread.ID}
	for _, turn := range out.Thread.Turns {
		result.Turns = append(result.Turns, backendops.TurnResult{ID: turn.ID, Status: turn.Status})
	}
	return result, err
}

func (g Gateway) ReadThreadHistory(ctx context.Context, threadID string) (backendops.ThreadHistory, error) {
	var out codexrpc.ThreadReadResult
	err := g.call(ctx, "thread/read", map[string]any{"threadId": strings.TrimSpace(threadID), "includeTurns": true}, &out)
	name := ""
	if out.Thread.Name != nil {
		name = strings.TrimSpace(*out.Thread.Name)
	}
	result := backendops.ThreadHistory{ID: out.Thread.ID, Name: name, Preview: out.Thread.Preview, Cwd: out.Thread.Cwd,
		Turns: codexhistory.SummarizeThreadHistory(out.Thread.Turns, "")}
	return result, err
}

func (g Gateway) ReadConversationHistory(ctx context.Context, threadID string) (backendops.ThreadHistory, error) {
	return g.ReadThreadHistory(ctx, threadID)
}

func (g Gateway) StartCompaction(ctx context.Context, threadID string) error {
	return g.call(ctx, "thread/compact/start", map[string]any{"threadId": strings.TrimSpace(threadID)}, nil)
}
func (g Gateway) ListModels(ctx context.Context, limit int) (modelconfig.ModelListResult, error) {
	var out modelconfig.ModelListResult
	if limit <= 0 {
		limit = 100
	}
	err := g.call(ctx, "model/list", map[string]any{"limit": limit, "includeHidden": false}, &out)
	return out, err
}
func (g Gateway) ListCollaborationModes(ctx context.Context) (modelconfig.CollaborationModeListResponse, error) {
	var out modelconfig.CollaborationModeListResponse
	err := g.call(ctx, "collaborationMode/list", map[string]any{}, &out)
	return out, err
}
func (g Gateway) ListSkills(ctx context.Context, cwd string, forceReload bool) (skill.SkillsListEntry, error) {
	var out skill.SkillsListResult
	params := map[string]any{"forceReload": forceReload}
	if strings.TrimSpace(cwd) != "" {
		params["cwds"] = []string{strings.TrimSpace(cwd)}
	}
	if err := g.call(ctx, "skills/list", params, &out); err != nil {
		return skill.SkillsListEntry{}, err
	}
	for _, entry := range out.Data {
		if strings.TrimSpace(entry.Cwd) == strings.TrimSpace(cwd) {
			return entry, nil
		}
	}
	if len(out.Data) > 0 {
		return out.Data[0], nil
	}
	return skill.SkillsListEntry{Cwd: strings.TrimSpace(cwd)}, nil
}
func (g Gateway) GetGoal(ctx context.Context, threadID string) (backendops.GoalLookup, error) {
	var out codexrpc.ThreadGoalGetResponse
	err := g.call(ctx, "thread/goal/get", map[string]any{"threadId": strings.TrimSpace(threadID)}, &out)
	return backendops.GoalLookup{Goal: out.Goal}, err
}
func (g Gateway) SetGoal(ctx context.Context, request backendops.GoalUpdate) (backendops.GoalResult, error) {
	var out codexrpc.ThreadGoalSetResponse
	err := g.call(ctx, "thread/goal/set", goalParams(request), &out)
	return backendops.GoalResult{Goal: out.Goal}, err
}
func (g Gateway) ClearGoal(ctx context.Context, threadID string) (backendops.GoalCleared, error) {
	var out codexrpc.ThreadGoalClearResponse
	err := g.call(ctx, "thread/goal/clear", map[string]any{"threadId": strings.TrimSpace(threadID)}, &out)
	return backendops.GoalCleared{Cleared: out.Cleared}, err
}
func (g Gateway) StartReview(ctx context.Context, request backendops.ReviewRequest) (backendops.ReviewResult, error) {
	var out codexrpc.ReviewStartResult
	err := g.call(ctx, "review/start", map[string]any{"threadId": request.ThreadID, "delivery": "inline", "target": ReviewTargetParams(request.Target)}, &out)
	return backendops.ReviewResult{ReviewThreadID: out.ReviewThreadID, Turn: backendops.TurnResult{ID: out.Turn.ID, Status: out.Turn.Status}}, err
}

func goalParams(request backendops.GoalUpdate) codexrpc.ThreadGoalSetParams {
	params := codexrpc.ThreadGoalSetParams{ThreadID: request.ThreadID, Objective: request.Objective, Status: request.Status}
	if request.TokenBudget != nil {
		params.TokenBudget = codexrpc.NewNullableInt64(request.TokenBudget.Value)
	}
	return params
}
func CollaborationModeFromState(mode *conversation.SessionCollaborationMode) *codexrpc.CollaborationMode {
	if mode == nil || strings.TrimSpace(mode.Mode) == "" || strings.TrimSpace(mode.Model) == "" {
		return nil
	}
	modeName := strings.TrimSpace(mode.Mode)
	modelName := strings.TrimSpace(mode.Model)
	var effort *string
	if value := strings.TrimSpace(mode.ReasoningEffort); value != "" {
		effort = &value
	}
	return &codexrpc.CollaborationMode{Mode: modeName, Settings: codexrpc.CollaborationModeSettings{Model: modelName, ReasoningEffort: effort}}
}
func SandboxPolicy(mode string) map[string]any {
	switch strings.TrimSpace(mode) {
	case "read-only":
		return map[string]any{"type": "readOnly"}
	case "workspace-write":
		return map[string]any{"type": "workspaceWrite"}
	case "danger-full-access":
		return map[string]any{"type": "dangerFullAccess"}
	default:
		return nil
	}
}
func TurnParams(r backendops.StartTurnRequest) map[string]any {
	params := map[string]any{"threadId": r.ThreadID, "input": BuildTurnInputs(r.Submission), "cwd": r.Cwd, "approvalPolicy": r.ApprovalPolicy}
	for key, value := range map[string]string{"model": r.Model, "effort": r.Effort, "serviceTier": strings.TrimSpace(r.ServiceTier), "multiAgentMode": strings.TrimSpace(r.MultiAgentMode)} {
		if strings.TrimSpace(value) != "" {
			params[key] = value
		}
	}
	if policy := SandboxPolicy(r.SandboxMode); policy != nil {
		params["sandboxPolicy"] = policy
	}
	if mode := CollaborationModeFromState(r.Collaboration); mode != nil {
		params["collaborationMode"] = mode
	}
	return params
}

type ReplyClient interface {
	Reply(json.RawMessage, any) error
	ReplyError(json.RawMessage, int, string) error
}

func Respond(ctx context.Context, client ReplyClient, response backendops.Response) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if client == nil {
		return fmt.Errorf("codex client unavailable")
	}
	if response.Error != nil {
		return client.ReplyError(json.RawMessage(response.Token), response.Error.Code, response.Error.Message)
	}
	var payload any
	if len(response.Payload) == 0 {
		payload = nil
	} else if err := json.Unmarshal(response.Payload, &payload); err != nil {
		return fmt.Errorf("decode backend response payload: %w", err)
	} else {
		payload = normalizeResponseValue(payload)
	}
	return client.Reply(json.RawMessage(response.Token), payload)
}

// normalizeResponseValue retains the concrete string-list shape used by the
// backend reply builders while keeping the application port JSON-only.
func normalizeResponseValue(value any) any {
	switch typed := value.(type) {
	case []any:
		allStrings := len(typed) > 0
		stringsValue := make([]string, len(typed))
		for i, item := range typed {
			normalized := normalizeResponseValue(item)
			typed[i] = normalized
			stringItem, ok := normalized.(string)
			if !ok {
				allStrings = false
				continue
			}
			stringsValue[i] = stringItem
		}
		if allStrings {
			return stringsValue
		}
		return typed
	case map[string]any:
		for key, item := range typed {
			typed[key] = normalizeResponseValue(item)
		}
	}
	return value
}
