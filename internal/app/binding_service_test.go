package app

import (
	"context"
	"strings"
	"testing"

	"feidex/internal/codexrpc"
	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

func TestCommandBindCreatesAndUpdatesLocalGroupBinding(t *testing.T) {
	a, ff, _ := newTestApp(t)
	a.frontendID = "bot-a"
	msg := &feishu.InboundMessage{ChatType: "group", ChatID: "chat-issue-9", MessageID: "msg-bind", UserID: "user-1"}

	if err := newBindingService(a).commandBind(msg, nil); err != nil {
		t.Fatalf("/bind status error = %v", err)
	}
	binding := agentBindingForChat(a, "group", "chat-issue-9")
	if binding == nil {
		t.Fatal("/bind did not create pending binding")
	}
	if binding.Status != state.AgentBindingStatusPending.String() || binding.WorkspaceID != "" || binding.Primary {
		t.Fatalf("initial binding = %+v, want pending without workspace/primary", binding)
	}
	if len(ff.replyCardsSnapshot()) != 1 {
		t.Fatalf("reply cards after status = %d, want 1", len(ff.replyCardsSnapshot()))
	}

	commands := [][]string{
		{"use", "default"},
		{"component", "client"},
		{"primary", "on"},
		{"model", "gpt-5-binding"},
		{"effort", "high"},
		{"fast", "fast"},
		{"sandbox", "read-only"},
		{"policy", "never"},
		{"multiagent", "proactive"},
		{"permissions", "acceptEdits"},
	}
	for _, args := range commands {
		if err := newBindingService(a).commandBind(msg, args); err != nil {
			t.Fatalf("/bind %s error = %v", strings.Join(args, " "), err)
		}
	}
	binding = agentBindingForChat(a, "group", "chat-issue-9")
	if binding == nil {
		t.Fatal("binding disappeared")
	}
	if binding.Status != state.AgentBindingStatusActive.String() || binding.WorkspaceID != "default" {
		t.Fatalf("activated binding = %+v", binding)
	}
	if !binding.Primary || binding.Component != "client" || binding.ModelOverride != "gpt-5-binding" || binding.ReasoningEffortOverride != "high" {
		t.Fatalf("binding user overrides = %+v", binding)
	}
	if binding.ServiceTierOverride != "fast" || binding.SandboxModeOverride != "read-only" || binding.ApprovalPolicyOverride != "never" || binding.MultiAgentModeOverride != "proactive" || binding.ClaudePermissionMode != "acceptEdits" {
		t.Fatalf("binding runtime overrides = %+v", binding)
	}
	if a.cfg.Codex.Model != "" || a.cfg.Codex.ReasoningEffort != "" {
		t.Fatalf("/bind should not mutate global codex config: %+v", a.cfg.Codex)
	}
}

func TestCommandBindNewCreatesWorkspaceAndActivatesBinding(t *testing.T) {
	a, _, _ := newTestApp(t)
	msg := &feishu.InboundMessage{ChatType: "group", ChatID: "chat-new", MessageID: "msg-new", UserID: "user-1"}
	cwd := t.TempDir() + "/client"

	if err := newBindingService(a).commandBind(msg, []string{"new", "client-x", cwd}); err != nil {
		t.Fatalf("/bind new error = %v", err)
	}
	if ws := findWorkspaceForTest(a, "client-x"); ws == nil || ws.Cwd != cwd {
		t.Fatalf("created workspace = %+v", ws)
	}
	if binding := agentBindingForChat(a, "group", "chat-new"); binding == nil || binding.WorkspaceID != "client-x" || binding.Status != state.AgentBindingStatusActive.String() {
		t.Fatalf("binding after new = %+v", binding)
	}
}

func TestBindingOverridesCodexThreadAndTurnStart(t *testing.T) {
	a, _, fc := newTestApp(t)
	a.frontendID = "bot-a"
	a.cfg.Codex.Model = "gpt-5-global"
	a.cfg.Codex.ReasoningEffort = "medium"
	a.cfg.Workspaces[0].Model = "gpt-5-workspace"
	a.cfg.Workspaces[0].ApprovalPolicy = "on-request"
	a.cfg.Workspaces[0].SandboxMode = "workspace-write"
	a.cfg.Workspaces[0].MultiAgentMode = "explicitRequestOnly"

	if err := a.State().SaveAgentBinding(&state.AgentBinding{
		ID:                      "binding-client",
		FrontendID:              "bot-a",
		ChatID:                  "chat-1",
		ChatType:                "group",
		WorkspaceID:             "default",
		ModelOverride:           "gpt-5-binding",
		ReasoningEffortOverride: "high",
		ServiceTierOverride:     serviceTierFast,
		SandboxModeOverride:     "read-only",
		ApprovalPolicyOverride:  "never",
		MultiAgentModeOverride:  "proactive",
		Status:                  state.AgentBindingStatusActive.String(),
	}); err != nil {
		t.Fatalf("SaveAgentBinding() error = %v", err)
	}
	sessionKey := "feishu:frontend:bot-a:group:chat-1:root:root-1"
	subID, err := a.store.CreateSubmission(&state.Submission{
		SessionKey:       sessionKey,
		BindingID:        "binding-client",
		WorkspaceID:      "default",
		UserID:           "user-1",
		ChatID:           "chat-1",
		TriggerMessageID: "trigger-1",
		InputText:        "hello binding",
		Status:           state.SubmissionStatusQueued.String(),
	})
	if err != nil {
		t.Fatalf("CreateSubmission() error = %v", err)
	}
	if err := a.store.UpsertSession(&state.Session{
		Key:           sessionKey,
		BindingID:     "binding-client",
		WorkspaceID:   "",
		ChatID:        "chat-1",
		ChatType:      "group",
		RootMessageID: "root-1",
		Queue:         []string{subID},
		Status:        state.SessionStatusIdle.String(),
	}); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}

	var threadParams map[string]any
	var turnParams map[string]any
	fc.callHook = func(_ context.Context, method string, params any, out any) error {
		switch method {
		case "thread/start":
			threadParams, _ = params.(map[string]any)
			result := out.(*codexrpc.ThreadStartResult)
			result.Thread.ID = "thread-binding"
			return nil
		case "turn/start":
			turnParams, _ = params.(map[string]any)
			result := out.(*codexrpc.TurnStartResult)
			result.Turn.ID = "turn-binding"
			return nil
		default:
			return nil
		}
	}

	if err := startNextSubmission(a, sessionKey); err != nil {
		t.Fatalf("startNextSubmission() error = %v", err)
	}
	if threadParams == nil || turnParams == nil {
		t.Fatalf("captured params = thread:%+v turn:%+v", threadParams, turnParams)
	}
	if got, _ := threadParams["model"].(string); got != "gpt-5-binding" {
		t.Fatalf("thread/start model = %q, want binding override", got)
	}
	if got, _ := threadParams["approvalPolicy"].(string); got != "never" {
		t.Fatalf("thread/start approvalPolicy = %q, want never", got)
	}
	if got, _ := threadParams["sandbox"].(string); got != "read-only" {
		t.Fatalf("thread/start sandbox = %q, want read-only", got)
	}
	if got, _ := threadParams["serviceTier"].(string); got != serviceTierFast {
		t.Fatalf("thread/start serviceTier = %q, want fast", got)
	}
	if got, _ := turnParams["model"].(string); got != "gpt-5-binding" {
		t.Fatalf("turn/start model = %q, want binding override", got)
	}
	if got, _ := turnParams["effort"].(string); got != "high" {
		t.Fatalf("turn/start effort = %q, want high", got)
	}
	if got, _ := turnParams["approvalPolicy"].(string); got != "never" {
		t.Fatalf("turn/start approvalPolicy = %q, want never", got)
	}
	if got, _ := turnParams["sandboxPolicy"].(map[string]any); got["type"] != "readOnly" {
		t.Fatalf("turn/start sandboxPolicy = %+v, want readOnly", got)
	}
	if got, _ := turnParams["serviceTier"].(string); got != serviceTierFast {
		t.Fatalf("turn/start serviceTier = %q, want fast", got)
	}
	if got, _ := turnParams["multiAgentMode"].(string); got != "proactive" {
		t.Fatalf("turn/start multiAgentMode = %q, want proactive", got)
	}
}

func TestMenuIncludesCurrentBotBindingWithoutBotSelector(t *testing.T) {
	a, _, _ := newTestApp(t)
	sessionKey := "feishu:frontend:default:group:chat-1:root:root-1"
	root := renderCommandMenuCard(a, sessionKey)
	labels := cardButtonLabelsByAction(root)
	if got := labels["menu.current_bot"]; !strings.Contains(got, "当前 Bot") {
		t.Fatalf("root menu labels = %+v, want current bot", labels)
	}
	currentBot := renderCurrentBotMenu(a, sessionKey)
	currentLabels := cardButtonLabelsByAction(currentBot)
	if got := currentLabels["menu.binding"]; !strings.Contains(got, "/bind") {
		t.Fatalf("current bot labels = %+v, want /bind", currentLabels)
	}
	body := cardMarkdownContent(t, currentBot)
	if strings.Contains(body, "target_bot") || strings.Contains(body, "选择 Bot") {
		t.Fatalf("current bot menu should not expose bot selector: %q", body)
	}
}

func TestGroupBindingScopedCommandsUpdateBindingNotGlobalState(t *testing.T) {
	a, _, _ := newTestApp(t)
	a.frontendID = "bot-a"
	a.cfg.Codex.Model = "gpt-global"
	a.cfg.Codex.ReasoningEffort = "medium"
	a.cfg.Workspaces[0].SandboxMode = "workspace-write"
	a.cfg.Workspaces[0].ApprovalPolicy = "on-request"
	a.cfg.Workspaces[0].MultiAgentMode = "explicitRequestOnly"
	a.cfg.Workspaces = append(a.cfg.Workspaces, config.Workspace{ID: "server", Cwd: t.TempDir(), SandboxMode: "workspace-write", ApprovalPolicy: "on-request", MultiAgentMode: "explicitRequestOnly"})

	msg := &feishu.InboundMessage{ChatType: "group", ChatID: "chat-bind-cmd", MessageID: "msg-bind-cmd", UserID: "user-1"}
	if err := newBindingService(a).commandBind(msg, []string{"use", "default"}); err != nil {
		t.Fatalf("/bind use default error = %v", err)
	}
	commands := []string{
		"/workspace use server",
		"/model set gpt-binding",
		"/model effort high",
		"/effort low",
		"/fast fast",
		"/workspace sandbox read-only",
		"/workspace policy never",
		"/workspace multiagent proactive",
		"/workspace permissions acceptEdits",
	}
	for _, raw := range commands {
		msg.Text = raw
		msg.MessageID = strings.ReplaceAll(strings.TrimPrefix(raw, "/"), " ", "-")
		if err := handleCommand(a, msg, raw); err != nil {
			t.Fatalf("handleCommand(%q) error = %v", raw, err)
		}
	}
	binding := agentBindingForChat(a, "group", "chat-bind-cmd")
	if binding == nil {
		t.Fatal("binding disappeared")
	}
	if binding.WorkspaceID != "server" || binding.ModelOverride != "gpt-binding" || binding.ReasoningEffortOverride != "low" {
		t.Fatalf("binding command overrides = %+v", binding)
	}
	if binding.ServiceTierOverride != serviceTierFast || binding.SandboxModeOverride != "read-only" || binding.ApprovalPolicyOverride != "never" || binding.MultiAgentModeOverride != "proactive" || binding.ClaudePermissionMode != "acceptEdits" {
		t.Fatalf("binding runtime overrides = %+v", binding)
	}
	if a.cfg.Codex.Model != "gpt-global" || a.cfg.Codex.ReasoningEffort != "medium" {
		t.Fatalf("global codex config changed = %+v", a.cfg.Codex)
	}
	if ws := findWorkspaceForTest(a, "default"); ws == nil || ws.SandboxMode != "workspace-write" || ws.ApprovalPolicy != "on-request" || ws.MultiAgentMode != "explicitRequestOnly" {
		t.Fatalf("workspace defaults changed = %+v", ws)
	}
}

func TestGroupBindingScopedCardActionsUpdateBindingNotSession(t *testing.T) {
	a, _, _ := newTestApp(t)
	a.frontendID = "bot-a"
	a.cfg.Workspaces = append(a.cfg.Workspaces, config.Workspace{ID: "server", Cwd: t.TempDir()})
	sessionKey := "feishu:frontend:bot-a:group:chat-card:root:root-1"
	if err := a.State().SaveAgentBinding(&state.AgentBinding{
		ID:          "binding-card",
		FrontendID:  "bot-a",
		ChatID:      "chat-card",
		ChatType:    "group",
		WorkspaceID: "default",
		Status:      state.AgentBindingStatusActive.String(),
	}); err != nil {
		t.Fatalf("SaveAgentBinding() error = %v", err)
	}
	if err := a.store.UpsertSession(&state.Session{Key: sessionKey, ChatID: "chat-card", ChatType: "group", WorkspaceID: "default"}); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}

	actions := []feishu.CardAction{
		{ActionValue: map[string]any{"action": "workspace.use.select", "session_key": sessionKey}, Option: "server", UserID: "user-1", ChatID: "chat-card", MessageID: "card-1"},
		{ActionValue: map[string]any{"action": "model.config.set_model", "session_key": sessionKey, "model_id": "gpt-card"}, UserID: "user-1", ChatID: "chat-card", MessageID: "card-2"},
		{ActionValue: map[string]any{"action": "model.config.set_effort", "session_key": sessionKey, "reasoning_effort": "high"}, UserID: "user-1", ChatID: "chat-card", MessageID: "card-3"},
		{ActionValue: map[string]any{"action": "service_tier.set", "session_key": sessionKey, "service_tier": serviceTierFast}, UserID: "user-1", ChatID: "chat-card", MessageID: "card-4"},
		{ActionValue: map[string]any{"action": "workspace.sandbox.set", "session_key": sessionKey, "sandbox_mode": "read-only"}, UserID: "user-1", ChatID: "chat-card", MessageID: "card-5"},
		{ActionValue: map[string]any{"action": "workspace.policy.set", "session_key": sessionKey, "approval_policy": "never"}, UserID: "user-1", ChatID: "chat-card", MessageID: "card-6"},
	}
	for _, action := range actions {
		resp, err := newCardActionService(a).dispatch(&action)
		if err != nil || resp == nil || resp.Toast == nil {
			t.Fatalf("dispatch(%v) = %#v, %v", action.ActionValue["action"], resp, err)
		}
	}
	binding := agentBindingForChat(a, "group", "chat-card")
	if binding == nil {
		t.Fatal("binding disappeared")
	}
	if binding.WorkspaceID != "server" || binding.ModelOverride != "gpt-card" || binding.ReasoningEffortOverride != "high" || binding.ServiceTierOverride != serviceTierFast || binding.SandboxModeOverride != "read-only" || binding.ApprovalPolicyOverride != "never" {
		t.Fatalf("binding after card actions = %+v", binding)
	}
	if sess := a.State().Session(sessionKey); sess == nil || sess.WorkspaceID != "default" {
		t.Fatalf("session workspace changed = %+v", sess)
	}
}

func TestWorkspaceDeletionBlockedWhenReferencedByLocalBinding(t *testing.T) {
	a, _, _ := newTestApp(t)
	a.frontendID = "bot-a"
	a.cfg.Workspaces = append(a.cfg.Workspaces, config.Workspace{ID: "bound", Cwd: t.TempDir()})
	if err := a.State().SaveAgentBinding(&state.AgentBinding{
		ID:          "binding-bound",
		FrontendID:  "bot-a",
		ChatID:      "chat-bound",
		ChatType:    "group",
		WorkspaceID: "bound",
		Status:      state.AgentBindingStatusActive.String(),
	}); err != nil {
		t.Fatalf("SaveAgentBinding() error = %v", err)
	}
	err := newWorkspaceConfigServiceInner(a).ValidateWorkspaceDeletion("sess-1", "bound")
	if err == nil || !strings.Contains(err.Error(), "binding-bound") {
		t.Fatalf("ValidateWorkspaceDeletion(bound) error = %v, want binding guard", err)
	}
}

func findWorkspaceForTest(a *App, id string) *config.Workspace {
	return config.FindWorkspace(a.cfg, id)
}
