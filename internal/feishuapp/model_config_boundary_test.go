package feishuapp

import (
	"context"
	"errors"
	domainbackend "feidex/internal/domain/backend"
	"feidex/internal/domain/conversation"
	catalog "feidex/internal/domain/modelconfig"
	"feidex/internal/domain/routing"
	domainsubmission "feidex/internal/domain/submission"
	appclauderuntime "feidex/internal/runtime/claude"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"feidex/internal/claudecli"
	"feidex/internal/codexrpc"
	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

func modelBoundaryQueuedSubmission(t *testing.T, a *Frontend, key, thread, text string) *domainsubmission.Submission {
	t.Helper()
	if a.store.GetSession(key) == nil {
		if err := a.store.UpsertSession(&conversation.Session{Key: key, WorkspaceID: a.cfg.Workspaces[0].ID,
			ActiveThreadID: thread, ActiveThreadWorkspaceID: a.cfg.Workspaces[0].ID, ChatID: "chat", Status: "idle"}); err != nil {
			t.Fatal(err)
		}
	}
	id, err := a.store.CreateSubmission(&domainsubmission.Submission{SessionKey: key, WorkspaceID: a.cfg.Workspaces[0].ID,
		ChatID: "chat", TriggerMessageID: "msg-" + text, InputText: text, Status: "queued"})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.store.QueueSubmission(key, id); err != nil {
		t.Fatal(err)
	}
	if thread != "" {
		markSessionThreadLive(a, key, thread)
	}
	return a.store.GetSubmission(id)
}

func TestModelConfigQueuedCodexUsesStartSnapshotIncludingPlan(t *testing.T) {
	a, _, fc := newTestApp(t)
	a.cfg.Codex.Model, a.cfg.Codex.ReasoningEffort = "old", "low"
	sub := modelBoundaryQueuedSubmission(t, a, "sess-config", "thread-config", "queued")
	_, err := a.store.UpdateSession(sub.SessionKey, func(sess *conversation.Session) {
		sess.ActiveThreadCollaborationMode = &conversation.SessionCollaborationMode{Mode: "plan", Model: "old-plan", ReasoningEffort: "low"}
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.bindings.ModelCommands.UpdateGlobalAuxiliaryConfig(map[routing.Setting]string{routing.Model: "new", routing.Effort: "high", routing.PlanModel: "new-plan", routing.PlanEffort: "high"}); err != nil {
		t.Fatal(err)
	}
	fc.callHook = func(_ context.Context, method string, params any, out any) error {
		if method != "turn/start" {
			t.Fatalf("unexpected method %s", method)
		}
		p := params.(map[string]any)
		if p["model"] != "new" || p["effort"] != "high" {
			t.Fatalf("mixed/stale config: %#v", p)
		}
		mode := p["collaborationMode"].(*codexrpc.CollaborationMode)
		if mode.Settings.Model != "new-plan" || mode.Settings.ReasoningEffort == nil || *mode.Settings.ReasoningEffort != "high" {
			t.Fatalf("stale plan: %+v", mode)
		}
		// A subsequent write during the RPC cannot change the captured submission.
		if err := a.bindings.ModelCommands.UpdateGlobalAuxiliaryConfig(map[routing.Setting]string{routing.Model: "later"}); err != nil {
			t.Fatal(err)
		}
		out.(*codexrpc.TurnStartResult).Turn.ID = "turn-config"
		return nil
	}
	if err := startNextSubmission(a.bindings.Submissions, sub.SessionKey); err != nil {
		t.Fatal(err)
	}
	if got := a.store.GetSubmission(sub.ID).ModelConfig; got.Model != "new" || got.PlanModel != "new-plan" {
		t.Fatalf("snapshot changed: %+v", got)
	}
	if got := a.store.GetSession(sub.SessionKey).AppliedModelConfig; got.Model != "new" {
		t.Fatalf("applied snapshot lost: %+v", got)
	}
	if got := modelConfigStatus(a.bindings.ModelSnapshots, a.State(), a.configView(), sub.SessionKey); !strings.Contains(got, "最近已应用模型：`new-plan`；推理强度：`high`") ||
		!strings.Contains(got, "下一轮本地启动模型：`new-plan`；推理强度：`high`") {
		t.Fatalf("plan status did not reflect collaboration mode: %s", got)
	}
}

func TestModelConfigClaudeFailureRetainsQueueAndLineage(t *testing.T) {
	a, _, _ := newTestApp(t)
	selectBackendForTest(a, domainbackend.BackendClaude)
	a.cfg.Feishu.Backend = domainbackend.BackendClaude
	fake := &fakeClaudeCore{ensureSessionErr: fmt.Errorf("%w: rejected", claudecli.ErrModelConfigApply)}
	runtimeViewOf(a.runtimeOwner).setClaudeCore(fake)
	first := modelBoundaryQueuedSubmission(t, a, "sess-config", "original-thread", "first")
	second := modelBoundaryQueuedSubmission(t, a, "sess-config", "original-thread", "second")
	if err := startNextSubmission(a.bindings.Submissions, first.SessionKey); !errors.Is(err, claudecli.ErrModelConfigApply) {
		t.Fatalf("error = %v", err)
	}
	sess := a.store.GetSession(first.SessionKey)
	if sess.ActiveThreadID != "original-thread" || len(sess.Queue) != 2 || sess.Queue[0] != first.ID || sess.Queue[1] != second.ID || sess.ModelConfigError == "" {
		t.Fatalf("lost queue/context: %+v", sess)
	}
	if len(fake.ensureCalls) != 1 || len(fake.startTurnCalls) != 0 || a.store.GetSubmission(first.ID).Finalized {
		t.Fatal("configuration error used fresh fallback or consumed input")
	}
	fake.ensureSessionErr, fake.ensureSessionID = nil, "original-thread"
	if err := startNextSubmission(a.bindings.Submissions, first.SessionKey); err != nil {
		t.Fatal(err)
	}
	if len(fake.startTurnCalls) != 1 || fake.startTurnCalls[0].prompt != "first" {
		t.Fatalf("retry reordered prompts: %+v", fake.startTurnCalls)
	}
}

type modelConfigProtectedClaude struct{ *fakeClaudeCore }

func (*modelConfigProtectedClaude) CanRetryFreshSession(string) bool { return false }

func TestModelConfigClaudeRestartedTurnFailureRetainsQueueAndLineage(t *testing.T) {
	a, _, _ := newTestApp(t)
	selectBackendForTest(a, domainbackend.BackendClaude)
	a.cfg.Feishu.Backend = domainbackend.BackendClaude
	fake := &fakeClaudeCore{ensureSessionID: "original-thread", startTurnErr: errors.New("restarted process rejected turn")}
	a.runtimeOwner.SetClaudeCore(&modelConfigProtectedClaude{fake})
	first := modelBoundaryQueuedSubmission(t, a, "sess-config", "original-thread", "first")
	second := modelBoundaryQueuedSubmission(t, a, "sess-config", "original-thread", "second")
	if err := startNextSubmission(a.bindings.Submissions, first.SessionKey); !errors.Is(err, claudecli.ErrModelConfigApply) {
		t.Fatalf("error = %v", err)
	}
	sess := a.store.GetSession(first.SessionKey)
	if sess.ActiveThreadID != "original-thread" || len(sess.Queue) != 2 || sess.Queue[0] != first.ID || sess.Queue[1] != second.ID || sess.ModelConfigError == "" {
		t.Fatalf("lost queue/context: %+v", sess)
	}
	if got := a.store.GetSubmission(first.ID); got.Finalized || got.Status != domainsubmission.SubmissionStatusQueued.String() {
		t.Fatalf("failed submission was consumed: %+v", got)
	}
	if len(fake.ensureCalls) != 1 || len(fake.startTurnCalls) != 1 {
		t.Fatal("configuration failure started a fresh conversation")
	}
}

func TestModelConfigCodexResumeAcknowledgesAuxiliarySettings(t *testing.T) {
	a, _, fc := newTestApp(t)
	a.cfg.Codex.Model = "new-model"
	a.cfg.Codex.ReviewModel = "new-review"
	a.cfg.Codex.SubagentModel = "new-subagent"
	a.cfg.Codex.SubagentReasoningEffort = "high"
	sub := modelBoundaryQueuedSubmission(t, a, "sess-resume", "old-thread", "queued")
	fc.callHook = func(_ context.Context, method string, params any, out any) error {
		if method != "thread/resume" {
			t.Fatalf("unexpected method %s", method)
		}
		p := params.(map[string]any)
		cfg := p["config"].(map[string]any)
		if cfg["review_model"] != "new-review" || cfg["agents.default_subagent_model"] != "new-subagent" {
			t.Fatalf("resume config = %#v", cfg)
		}
		out.(*codexrpc.ThreadStartResult).Thread.ID = "old-thread"
		return nil
	}
	sess := a.store.GetSession(sub.SessionKey)
	_, err := a.bindings.Conversations.ResumeSelectedThread(sub.SessionKey, sess, &a.cfg.Workspaces[0], conversation.ThreadSelection{ThreadID: "old-thread", Cwd: a.cfg.Workspaces[0].Cwd})
	if err != nil {
		t.Fatal(err)
	}
	applied := a.store.GetSession(sub.SessionKey).AppliedModelConfig
	if !applied.Valid || applied.ReviewModel != "new-review" || applied.SubagentModel != "new-subagent" || applied.SubagentEffort != "high" || applied.Effort != "" {
		t.Fatalf("resume acknowledgment = %+v", applied)
	}
}

func TestModelConfigStartupRecoveryUsesSessionScope(t *testing.T) {
	for _, resumeFails := range []bool{false, true} {
		t.Run(fmt.Sprintf("resumeFails=%t", resumeFails), func(t *testing.T) {
			a, _, fc := newTestApp(t)
			a.frontendID = "bot-a"
			recomposeTestApp(a)
			a.cfg.Codex.Model = "config-default"
			if err := a.State().SaveBotProfile(&state.BotProfile{Model: "gpt-5.6-sol"}); err != nil {
				t.Fatal(err)
			}
			models := map[string]string{
				"p2p": "gpt-5.6-sol", "group-a": "gpt-6-astra",
				"group-b": "gpt-6.1-sol", "session": "session-model",
			}
			for chat, model := range models {
				sess := &conversation.Session{
					Key: "feishu:frontend:bot-a:chat:" + chat, ChatID: chat, ChatType: "group",
					WorkspaceID: a.cfg.Workspaces[0].ID, ActiveThreadWorkspaceID: a.cfg.Workspaces[0].ID,
					ActiveThreadID: chat, Status: "idle",
				}
				if chat == "p2p" {
					sess.ChatType = "p2p"
				} else {
					sess.BindingID = "binding-" + chat
					if err := a.State().SaveAgentBinding(&state.AgentBinding{
						ID: sess.BindingID, ChatID: chat, ChatType: "group", ModelOverride: model,
					}); err != nil {
						t.Fatal(err)
					}
				}
				if chat == "session" {
					sess.ModelOverride = model
					if err := a.State().SaveAgentBinding(&state.AgentBinding{
						ID: sess.BindingID, ChatID: chat, ChatType: "group", ModelOverride: "binding-model",
					}); err != nil {
						t.Fatal(err)
					}
				}
				if err := a.State().SaveSession(sess); err != nil {
					t.Fatal(err)
				}
			}
			foreign := &conversation.Session{
				Key: "feishu:frontend:bot-b:chat:foreign", WorkspaceID: a.cfg.Workspaces[0].ID,
				ActiveThreadWorkspaceID: a.cfg.Workspaces[0].ID, ActiveThreadID: "foreign", Status: "idle",
			}
			if err := a.store.UpsertSession(foreign); err != nil {
				t.Fatal(err)
			}
			seen := map[string]int{}
			fc.callHook = func(_ context.Context, method string, params any, out any) error {
				p := params.(map[string]any)
				model, _ := p["model"].(string)
				switch method {
				case "thread/resume":
					chat := p["threadId"].(string)
					if model != models[chat] || model == "" {
						t.Fatalf("resume %s model = %q, want %q", chat, model, models[chat])
					}
					seen[chat]++
					if resumeFails {
						return errors.New("thread not found")
					}
					out.(*codexrpc.ThreadStartResult).Thread.ID = chat
				case "thread/start":
					chat := ""
					for candidate, expected := range models {
						if model == expected {
							chat = candidate
						}
					}
					if !resumeFails || chat == "" {
						t.Fatalf("unexpected fresh thread model = %q", model)
					}
					seen[chat]++
					out.(*codexrpc.ThreadStartResult).Thread.ID = chat
				default:
					t.Fatalf("unexpected method %s", method)
				}
				return nil
			}
			recoverFrontendRuntimeState(a.bindings.StartupRecovery)
			for chat, model := range models {
				wantCalls := 1
				if resumeFails {
					wantCalls = 2
				}
				if seen[chat] != wantCalls {
					t.Fatalf("%s calls = %d, want %d", chat, seen[chat], wantCalls)
				}
				if !resumeFails {
					key := "feishu:frontend:bot-a:chat:" + chat
					if got := a.State().Session(key).AppliedModelConfig.Model; got != model {
						t.Fatalf("%s applied model = %q, want %q", chat, got, model)
					}
					if got := modelConfigStatus(a.bindings.ModelSnapshots, a.State(), a.configView(), key); !strings.Contains(got, "最近已应用模型：`"+model+"`") {
						t.Fatalf("%s status = %s", chat, got)
					}
				}
			}
		})
	}
}

func TestModelConfigGroupMenuTracksTurnBoundary(t *testing.T) {
	a, _, fc := newTestApp(t)
	a.frontendID = "bot-a"
	recomposeTestApp(a)
	a.cfg.Codex.Model = "gpt-5.6-sol"
	a.cfg.Codex.ReviewModel, a.cfg.Codex.SubagentModel = "review-model", "subagent-model"
	key := "feishu:frontend:bot-a:chat:group-model"
	binding := &state.AgentBinding{ID: "binding-model", ChatID: "group-model", ChatType: "group", ModelOverride: "gpt-6-astra"}
	if err := a.State().SaveAgentBinding(binding); err != nil {
		t.Fatal(err)
	}
	if err := a.State().SaveSession(&conversation.Session{
		Key: key, ChatID: binding.ChatID, ChatType: "group", BindingID: binding.ID,
		WorkspaceID: a.cfg.Workspaces[0].ID, ActiveThreadWorkspaceID: a.cfg.Workspaces[0].ID,
		ActiveThreadID: "group-thread", Status: "idle",
	}); err != nil {
		t.Fatal(err)
	}
	fc.callHook = func(_ context.Context, method string, _ any, out any) error {
		if method != "thread/resume" {
			t.Fatalf("unexpected method %s", method)
		}
		out.(*codexrpc.ThreadStartResult).Thread.ID = "group-thread"
		return nil
	}
	recoverFrontendRuntimeState(a.bindings.StartupRecovery)
	binding.ModelOverride = "gpt-6.1-sol"
	if err := a.State().SaveAgentBinding(binding); err != nil {
		t.Fatal(err)
	}
	assertStatus := func(applied string, pending bool) {
		t.Helper()
		card := a.bindings.BindingCommands.renderBindingCodexModelConfigCard(key, binding, catalog.ModelListResult{
			Data: []catalog.ModelListEntry{{ID: "gpt-6.1-sol", Model: "gpt-6.1-sol"}},
		})
		got := mustJSON(card)
		if strings.Contains(got, "gpt-5.6-sol") || !strings.Contains(got, "下一轮本地启动模型：`gpt-6.1-sol`") ||
			!strings.Contains(got, "最近已应用模型：`"+applied+"`") || strings.Contains(got, "已保存配置与当前应用值不同") != pending {
			t.Fatalf("incorrect model menu: %s", got)
		}
	}
	assertStatus("gpt-6-astra", true)
	fc.callHook = func(_ context.Context, method string, params any, out any) error {
		if method != "turn/start" || params.(map[string]any)["model"] != "gpt-6.1-sol" {
			t.Fatalf("unexpected turn: %s %+v", method, params)
		}
		out.(*codexrpc.TurnStartResult).Turn.ID = "new-turn"
		return nil
	}
	modelBoundaryQueuedSubmission(t, a, key, "group-thread", "next")
	if err := startNextSubmission(a.bindings.Submissions, key); err != nil {
		t.Fatal(err)
	}
	assertStatus("gpt-6.1-sol", false)
}

func TestModelConfigClaudeSteerDoesNotEnsureOrApply(t *testing.T) {
	a, _, _ := newTestApp(t)
	selectBackendForTest(a, domainbackend.BackendClaude)
	a.cfg.Feishu.Backend = domainbackend.BackendClaude
	fake := &fakeClaudeCore{ensureSessionErr: errors.New("must not initialize while steering")}
	runtimeViewOf(a.runtimeOwner).setClaudeCore(fake)
	sub := seedActiveSubmission(t, a, "sess-steer", "original-thread", "turn-original")
	if _, err := a.store.UpdateSession(sub.SessionKey, func(sess *conversation.Session) { sess.ActiveThreadWorkspaceID = a.cfg.Workspaces[0].ID }); err != nil {
		t.Fatal(err)
	}
	follow := modelBoundaryQueuedSubmission(t, a, sub.SessionKey, "original-thread", "answer")
	err := a.bindings.Submissions.StartNextClaudeSubmissionWithFailureNoticeEx(sub.SessionKey, a.store.GetSession(sub.SessionKey), follow, &a.cfg.Workspaces[0], false, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(fake.ensureCalls) != 0 || len(fake.startSteerTurnCalls) != 1 {
		t.Fatalf("steer applied config: %+v", fake)
	}
}

func TestModelConfigFailedSaveDoesNotPublish(t *testing.T) {
	for _, backend := range []string{domainbackend.BackendCodex, domainbackend.BackendClaude} {
		t.Run(backend, func(t *testing.T) {
			a, _, _ := newTestApp(t)
			selectBackendForTest(a, backend)
			a.cfg.Feishu.Backend = backend
			runtimeViewOf(a.runtimeOwner).setClaudeCore(&fakeClaudeCore{})
			before := *config.Clone(a.cfg)
			a.cfgPath = t.TempDir()
			recomposeTestApp(a) // A directory cannot be replaced by config.toml.
			var err error
			if backend == domainbackend.BackendClaude {
				err = a.bindings.ModelCommands.UpdateClaudeModelConfig(map[routing.Setting]string{routing.Model: "changed"})
			} else {
				err = a.bindings.ModelCommands.UpdateGlobalAuxiliaryConfig(map[routing.Setting]string{routing.Model: "changed"})
			}
			if err == nil || a.cfg.Codex.Model != before.Codex.Model || a.cfg.Claude.Model != before.Claude.Model {
				t.Fatalf("failed save published settings: %v", err)
			}
			if len(runtimeViewOf(a.runtimeOwner).currentClaudeCore().(*fakeClaudeCore).updatedConfigs) != 0 {
				t.Fatal("failed save updated runtime")
			}
		})
	}
}

func TestModelConfigSnapshotConcurrentWritesRemainCoherent(t *testing.T) {
	a, _, _ := newTestApp(t)
	a.cfg.Codex.Model, a.cfg.Codex.ReasoningEffort = "a", "a"
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			value := fmt.Sprint(i)
			if err := a.bindings.ModelCommands.UpdateGlobalAuxiliaryConfig(map[routing.Setting]string{routing.Model: value, routing.Effort: value}); err != nil {
				t.Error(err)
				return
			}
		}
	}()
	for i := 0; i < 100; i++ {
		// Configuration cards and starts can read concurrently with writes.
		if i%10 == 0 {
			_ = a.bindings.ModelCommands.RenderModelConfigCard(catalog.ModelListResult{}, nil, "", "menu.model")
		}
		got := modelConfigSnapshot(a.bindings.ModelSnapshots, nil, domainbackend.BackendCodex)
		if got.Model != got.Effort {
			t.Errorf("mixed settings: %+v", got)
		}
	}
	wg.Wait()
}

func TestModelConfigGroupWritesDuringWorkPreservePending(t *testing.T) {
	a, _, _ := newTestApp(t)
	selectBackendForTest(a, domainbackend.BackendClaude)
	a.cfg.Feishu.Backend = domainbackend.BackendClaude
	fake := &fakeClaudeCore{}
	runtimeViewOf(a.runtimeOwner).setClaudeCore(fake)
	msg := &feishu.InboundMessage{ChatID: "group-model", ChatType: "group", UserID: "user", MessageID: "config"}
	key := a.configView().makeSessionKey(msg)
	seedActiveSubmission(t, a, key, "group-thread", "group-turn")
	if err := a.State().SaveAgentBinding(&state.AgentBinding{ID: "binding", ChatID: msg.ChatID, ChatType: "group", WorkspaceID: a.cfg.Workspaces[0].ID, Status: "active"}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.State().UpdateSession(key, func(s *conversation.Session) {
		s.BindingID = "binding"
		s.ChatID = msg.ChatID
		s.ChatType = "group"
		s.StagedImages = []conversation.SessionStagedImage{{}}
	}); err != nil {
		t.Fatal(err)
	}
	if err := a.State().SavePending(&state.PendingRequest{ID: "pending", SessionKey: key, ThreadID: "group-thread", Kind: "async_user_input", Status: "pending"}); err != nil {
		t.Fatal(err)
	}
	resp, err := a.bindings.BindingCommands.completeBindingModelSet(&feishu.CardAction{ActionValue: map[string]any{"session_key": key}}, key, "opus")
	if err != nil || resp.Toast.Type != "success" {
		t.Fatalf("save rejected: %v %+v", err, resp)
	}
	if a.store.GetSession(key).ActiveTurnID != "group-turn" || len(fake.setModelCalls) != 0 || fake.resetCalls != 0 || a.store.PendingByID("pending") == nil {
		t.Fatal("saving disturbed active work")
	}
}

// A fake CLI exercises real control-request acknowledgments without credentials
// or model calls. All I/O stays in this test's temporary directory.
func writeModelConfigCLI(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	logPath, cliPath := filepath.Join(dir, "requests.jsonl"), filepath.Join(dir, "claude.py")
	script := fmt.Sprintf(`#!/usr/bin/env python3
import json, sys, os
log = %q
with open(log, 'a') as f:
    f.write(json.dumps({'argv': sys.argv, 'pid': os.getpid()})+'\n')
for line in sys.stdin:
    message = json.loads(line)
    with open(log, 'a') as f: f.write(json.dumps(message)+'\n')
    if message.get('type') == 'control_request':
        request = message.get('request', {})
        fail = request.get('model') == 'rejected' or (request.get('subtype') == 'initialize' and '--model' in sys.argv and sys.argv[sys.argv.index('--model')+1] == 'bad-init')
        response = {'subtype': 'error' if fail else 'success', 'request_id': message['request_id'], 'response': {'pid': os.getpid()}}
        if fail: response['error'] = 'model rejected'
        print(json.dumps({'type': 'control_response', 'response': response}), flush=True)
`, logPath)
	if err := os.WriteFile(cliPath, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	return cliPath, logPath
}

func TestModelConfigClaudeAcknowledgesAndRestartsOnlyTargetSession(t *testing.T) {
	a, _, _ := newTestApp(t)
	selectBackendForTest(a, domainbackend.BackendClaude)
	a.cfg.Feishu.Backend = domainbackend.BackendClaude
	cli, logPath := writeModelConfigCLI(t)
	a.cfg.Claude.Command, a.cfg.Claude.Model, a.cfg.Claude.Effort, a.cfg.Claude.SubagentModel = cli, "sonnet", "low", "fixed-subagent"
	r := appclauderuntime.NewService(testClaudeRuntimePorts(a, a.cfg.Claude))
	runtimeViewOf(a.runtimeOwner).setClaudeCore(r)
	t.Cleanup(func() { _ = r.Close() })
	for _, key := range []string{"one", "two"} {
		if err := a.store.UpsertSession(&conversation.Session{Key: key, ActiveThreadID: "thread-" + key, WorkspaceID: a.cfg.Workspaces[0].ID, Status: "idle"}); err != nil {
			t.Fatal(err)
		}
		if _, err := r.EnsureSession(context.Background(), key, &a.cfg.Workspaces[0], "thread-"+key, ""); err != nil {
			t.Fatal(err)
		}
	}
	one, _ := r.SessionState("one")
	two, _ := r.SessionState("two")
	if err := a.bindings.ModelCommands.UpdateClaudeModelConfig(map[routing.Setting]string{routing.Model: "opus", routing.Effort: "high"}); err != nil {
		t.Fatal(err)
	}
	if id, err := r.EnsureSession(context.Background(), "one", &a.cfg.Workspaces[0], "thread-one", ""); err != nil || id != "thread-one" {
		t.Fatalf("apply = %s %v", id, err)
	}
	got, _ := r.SessionState("one")
	if got != one || got.AppliedModelConfig.Model != "opus" || got.AppliedModelConfig.Effort != "high" {
		t.Fatalf("hot apply failed: %+v", got.AppliedModelConfig)
	}
	if two.AppliedModelConfig.Model != "sonnet" {
		t.Fatal("changed another session")
	}
	data, err := os.ReadFile(logPath)
	if err != nil || !strings.Contains(string(data), "set_model") || !strings.Contains(string(data), "apply_flag_settings") {
		t.Fatalf("missing controls: %s %v", data, err)
	}
	if err := a.bindings.ModelCommands.UpdateClaudeModelConfig(map[routing.Setting]string{routing.Effort: ""}); err != nil {
		t.Fatal(err)
	}
	if id, err := r.EnsureSession(context.Background(), "one", &a.cfg.Workspaces[0], "thread-one", ""); err != nil || id != "thread-one" {
		t.Fatalf("resume = %s %v", id, err)
	}
	got, _ = r.SessionState("one")
	other, _ := r.SessionState("two")
	if got == one || other != two || got.AppliedModelConfig.Effort != "" {
		t.Fatal("default effort did not recreate only the target")
	}
	if err := a.bindings.ModelCommands.UpdateClaudeModelConfig(map[routing.Setting]string{routing.Model: "bad-init"}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.EnsureSession(context.Background(), "one", &a.cfg.Workspaces[0], "thread-one", ""); !errors.Is(err, claudecli.ErrModelConfigApply) {
		t.Fatalf("migration error = %v", err)
	}
	if a.store.GetSession("one").ActiveThreadID != "thread-one" {
		t.Fatal("migration discarded lineage")
	}
}

func TestModelConfigPlanDefaultUsesPresetAfterClearingOverride(t *testing.T) {
	a, _, _ := newTestApp(t)
	sess := &conversation.Session{ActiveThreadCollaborationMode: &conversation.SessionCollaborationMode{
		Mode: "plan", Model: "plan", ReasoningEffort: "high", PresetReasoningEffort: "medium",
	}}
	a.cfg.Codex.PlanReasoningEffort = "high"
	if got := modelConfigSnapshot(a.bindings.ModelSnapshots, sess, domainbackend.BackendCodex).PlanEffort; got != "high" {
		t.Fatal(got)
	}
	a.cfg.Codex.PlanReasoningEffort = ""
	if got := modelConfigSnapshot(a.bindings.ModelSnapshots, sess, domainbackend.BackendCodex).PlanEffort; got != "medium" {
		t.Fatalf("cleared override used stale effort: %s", got)
	}
}
