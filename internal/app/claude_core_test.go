package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

type fakeClaudeCore struct {
	ensureSessionID  string
	ensureSessionSet bool
	ensureSessionErr error
	startTurnErr     error
	interruptErr     error
	approvalErr      error
	userInputErr     error
	planErr          error
	cancelErr        error

	resetCalls int
	closed     bool

	ensureCalls []struct {
		sessionKey  string
		workspaceID string
		resumeID    string
		model       string
	}
	startTurnCalls []struct {
		sessionKey string
		threadID   string
		turnID     string
		prompt     string
	}
	interruptCalls []string
	approvalCalls  []struct {
		requestID  string
		resolution claudeApprovalResolution
	}
	userInputCalls []struct {
		requestID string
		answers   map[string]string
	}
	planCalls []struct {
		requestID string
		feedback  string
	}
	cancelCalls []struct {
		requestID string
		message   string
	}
}

func (f *fakeClaudeCore) EnsureSession(_ context.Context, sessionKey string, ws *config.Workspace, resumeID, model string) (string, error) {
	f.ensureCalls = append(f.ensureCalls, struct {
		sessionKey  string
		workspaceID string
		resumeID    string
		model       string
	}{
		sessionKey:  sessionKey,
		workspaceID: ws.ID,
		resumeID:    resumeID,
		model:       model,
	})
	if f.ensureSessionErr != nil {
		return "", f.ensureSessionErr
	}
	if f.ensureSessionSet {
		return strings.TrimSpace(f.ensureSessionID), nil
	}
	if strings.TrimSpace(f.ensureSessionID) == "" {
		return "claude-session-1", nil
	}
	return f.ensureSessionID, nil
}

func (f *fakeClaudeCore) ResetSession(string) error {
	f.resetCalls++
	return nil
}

func (f *fakeClaudeCore) StartTurn(_ context.Context, sessionKey, threadID, turnID, prompt string) error {
	f.startTurnCalls = append(f.startTurnCalls, struct {
		sessionKey string
		threadID   string
		turnID     string
		prompt     string
	}{
		sessionKey: sessionKey,
		threadID:   threadID,
		turnID:     turnID,
		prompt:     prompt,
	})
	return f.startTurnErr
}

func (f *fakeClaudeCore) Interrupt(_ context.Context, sessionKey string) error {
	f.interruptCalls = append(f.interruptCalls, sessionKey)
	return f.interruptErr
}

func (f *fakeClaudeCore) ResolveApproval(requestID string, resolution claudeApprovalResolution) error {
	f.approvalCalls = append(f.approvalCalls, struct {
		requestID  string
		resolution claudeApprovalResolution
	}{
		requestID:  requestID,
		resolution: resolution,
	})
	return f.approvalErr
}

func (f *fakeClaudeCore) ResolveUserInput(requestID string, answers map[string]string) error {
	cp := map[string]string{}
	for k, v := range answers {
		cp[k] = v
	}
	f.userInputCalls = append(f.userInputCalls, struct {
		requestID string
		answers   map[string]string
	}{
		requestID: requestID,
		answers:   cp,
	})
	return f.userInputErr
}

func (f *fakeClaudeCore) ResolvePlanFeedback(requestID, feedback string) error {
	f.planCalls = append(f.planCalls, struct {
		requestID string
		feedback  string
	}{
		requestID: requestID,
		feedback:  feedback,
	})
	return f.planErr
}

func (f *fakeClaudeCore) CancelPending(requestID, message string) error {
	f.cancelCalls = append(f.cancelCalls, struct {
		requestID string
		message   string
	}{
		requestID: requestID,
		message:   message,
	})
	return f.cancelErr
}

func (f *fakeClaudeCore) Close() error {
	f.closed = true
	return nil
}

func TestStartNextSubmissionClaudeStartsTurnAndBindsSession(t *testing.T) {
	a, _, _ := newTestApp(t)
	a.cfg.Workspaces[0].Backend = backendClaude
	a.codex = nil
	claude := &fakeClaudeCore{ensureSessionID: "claude-session-42"}
	a.claude = claude

	sessionKey := "feishu:p2p:chat:user"
	if err := a.store.UpsertSession(&state.Session{
		Key:                     sessionKey,
		WorkspaceID:             a.cfg.Workspaces[0].ID,
		ActiveThreadID:          "claude-prev",
		ActiveThreadWorkspaceID: a.cfg.Workspaces[0].ID,
		OwnerUserID:             "user",
		ChatID:                  "chat",
		ChatType:                "p2p",
		RootMessageID:           "root-1",
		Status:                  "idle",
	}); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}
	subID, err := a.store.CreateSubmission(&state.Submission{
		SessionKey:       sessionKey,
		WorkspaceID:      a.cfg.Workspaces[0].ID,
		UserID:           "user",
		ChatID:           "chat",
		TriggerMessageID: "msg-1",
		InputText:        "hello Claude",
		Status:           "queued",
	})
	if err != nil {
		t.Fatalf("CreateSubmission() error = %v", err)
	}
	if err := a.store.QueueSubmission(sessionKey, subID); err != nil {
		t.Fatalf("QueueSubmission() error = %v", err)
	}

	if err := a.startNextSubmission(sessionKey); err != nil {
		t.Fatalf("startNextSubmission() error = %v", err)
	}
	if len(claude.ensureCalls) != 1 {
		t.Fatalf("ensure calls = %d, want 1", len(claude.ensureCalls))
	}
	if claude.ensureCalls[0].resumeID != "claude-prev" {
		t.Fatalf("resumeID = %q, want claude-prev", claude.ensureCalls[0].resumeID)
	}
	if len(claude.startTurnCalls) != 1 {
		t.Fatalf("startTurn calls = %d, want 1", len(claude.startTurnCalls))
	}
	if !strings.Contains(claude.startTurnCalls[0].prompt, "hello Claude") {
		t.Fatalf("startTurn prompt = %q, want input text", claude.startTurnCalls[0].prompt)
	}

	sess := a.store.GetSession(sessionKey)
	if sess == nil {
		t.Fatal("session missing after start")
	}
	if sess.ActiveThreadID != "claude-session-42" || sess.ActiveTurnID == "" || sess.ActiveSubmissionID != subID || sess.Status != "turn_in_progress" {
		t.Fatalf("session after Claude start = %+v", sess)
	}

	sub := a.store.GetSubmission(subID)
	if sub == nil {
		t.Fatal("submission missing after start")
	}
	if sub.ThreadID != "claude-session-42" || sub.TurnID == "" || sub.Status != "running" {
		t.Fatalf("submission after Claude start = %+v", sub)
	}
}

func TestStartNextSubmissionClaudeBindsThreadAfterReady(t *testing.T) {
	a, _, _ := newTestApp(t)
	a.cfg.Workspaces[0].Backend = backendClaude
	a.codex = nil
	claude := &fakeClaudeCore{ensureSessionSet: true}
	a.claude = claude

	sessionKey := "feishu:group:chat-1:root:root-1"
	if err := a.store.UpsertSession(&state.Session{
		Key:           sessionKey,
		WorkspaceID:   a.cfg.Workspaces[0].ID,
		OwnerUserID:   "user",
		ChatID:        "chat-1",
		ChatType:      "group",
		RootMessageID: "root-1",
		Status:        "idle",
	}); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}
	subID, err := a.store.CreateSubmission(&state.Submission{
		SessionKey:           sessionKey,
		WorkspaceID:          a.cfg.Workspaces[0].ID,
		UserID:               "user",
		ChatID:               "chat-1",
		TriggerMessageID:     "msg-1",
		SourceMessageIDs:     []string{"msg-1"},
		SourceRootMessageIDs: []string{"root-1"},
		InputText:            "hello Claude",
		Status:               "queued",
	})
	if err != nil {
		t.Fatalf("CreateSubmission() error = %v", err)
	}
	if err := a.store.QueueSubmission(sessionKey, subID); err != nil {
		t.Fatalf("QueueSubmission() error = %v", err)
	}

	if err := a.startNextSubmission(sessionKey); err != nil {
		t.Fatalf("startNextSubmission() error = %v", err)
	}

	sess := a.store.GetSession(sessionKey)
	if sess == nil {
		t.Fatal("session missing after start")
	}
	if sess.ActiveThreadID != "" || sess.ActiveTurnID == "" || sess.ActiveSubmissionID != subID || sess.Status != "turn_in_progress" {
		t.Fatalf("session before Claude ready = %+v", sess)
	}

	sub := a.store.GetSubmission(subID)
	if sub == nil {
		t.Fatal("submission missing after start")
	}
	if sub.ThreadID != "" || sub.TurnID == "" || sub.Status != "running" {
		t.Fatalf("submission before Claude ready = %+v", sub)
	}

	a.bindClaudeSessionThread(sessionKey, sub.TurnID, "claude-session-ready")

	sess = a.store.GetSession(sessionKey)
	if sess == nil || sess.ActiveThreadID != "claude-session-ready" {
		t.Fatalf("session after Claude ready = %+v", sess)
	}

	sub = a.store.GetSubmission(subID)
	if sub == nil || sub.ThreadID != "claude-session-ready" {
		t.Fatalf("submission after Claude ready = %+v", sub)
	}

	binding := a.turnBindings[sub.TurnID]
	if binding.ThreadID != "claude-session-ready" {
		t.Fatalf("turn binding after Claude ready = %+v", binding)
	}

	rootLink := a.store.GetMessageLink("root-1")
	if rootLink == nil || rootLink.ThreadID != "claude-session-ready" || rootLink.TurnID != sub.TurnID {
		t.Fatalf("root link after Claude ready = %+v", rootLink)
	}
}

func TestBindClaudeSessionThreadReadyDoesNotClearRootTurnBinding(t *testing.T) {
	a, _, _ := newTestApp(t)
	a.cfg.Workspaces[0].Backend = backendClaude

	sessionKey := "feishu:group:chat-1:root:root-1"
	subID, err := a.store.CreateSubmission(&state.Submission{
		SessionKey:           sessionKey,
		WorkspaceID:          a.cfg.Workspaces[0].ID,
		UserID:               "user",
		ChatID:               "chat-1",
		TriggerMessageID:     "msg-1",
		SourceMessageIDs:     []string{"msg-1"},
		SourceRootMessageIDs: []string{"root-1"},
		InputText:            "hello Claude",
		TurnID:               "claude-turn-1",
		Status:               "running",
	})
	if err != nil {
		t.Fatalf("CreateSubmission() error = %v", err)
	}
	if err := a.store.UpsertSession(&state.Session{
		Key:           sessionKey,
		WorkspaceID:   a.cfg.Workspaces[0].ID,
		OwnerUserID:   "user",
		ChatID:        "chat-1",
		ChatType:      "group",
		RootMessageID: "root-1",
		Status:        "turn_in_progress",
		ActiveOperations: []state.SessionActiveOperation{{
			Kind:         sessionOpKindSubmission,
			SubmissionID: subID,
			TurnID:       "claude-turn-1",
		}},
	}); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}
	if err := a.store.UpsertMessageLink(&state.MessageLink{
		MessageID:  "root-1",
		SessionKey: sessionKey,
		TurnID:     "claude-turn-1",
	}); err != nil {
		t.Fatalf("UpsertMessageLink() error = %v", err)
	}

	a.bindClaudeSessionThread(sessionKey, "", "claude-session-ready")

	rootLink := a.store.GetMessageLink("root-1")
	if rootLink == nil || rootLink.ThreadID != "claude-session-ready" || rootLink.TurnID != "claude-turn-1" {
		t.Fatalf("root link after ready bind = %+v", rootLink)
	}
}

func TestStartNextSubmissionClaudeAllowsAdditionalInflightAndPreservesForeground(t *testing.T) {
	a, _, _ := newTestApp(t)
	a.cfg.Workspaces[0].Backend = backendClaude
	a.codex = nil
	claude := &fakeClaudeCore{ensureSessionID: "claude-thread-1"}
	a.claude = claude

	sessionKey := "feishu:group:chat-1:root:root-1"
	if err := a.store.UpsertSession(&state.Session{
		Key:                     sessionKey,
		WorkspaceID:             a.cfg.Workspaces[0].ID,
		ActiveThreadID:          "claude-thread-1",
		ActiveThreadWorkspaceID: a.cfg.Workspaces[0].ID,
		ActiveTurnID:            "claude-turn-1",
		ActiveSubmissionID:      "sub-running",
		OwnerUserID:             "user",
		ChatID:                  "chat-1",
		ChatType:                "group",
		RootMessageID:           "root-1",
		Status:                  "turn_in_progress",
	}); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}
	if _, err := a.store.CreateSubmission(&state.Submission{
		ID:                   "sub-running",
		SessionKey:           sessionKey,
		WorkspaceID:          a.cfg.Workspaces[0].ID,
		UserID:               "user",
		ChatID:               "chat-1",
		TriggerMessageID:     "msg-running",
		SourceMessageIDs:     []string{"msg-running"},
		SourceRootMessageIDs: []string{"root-1"},
		InputText:            "running",
		ThreadID:             "claude-thread-1",
		TurnID:               "claude-turn-1",
		Status:               "running",
	}); err != nil {
		t.Fatalf("CreateSubmission(sub-running) error = %v", err)
	}
	subID, err := a.store.CreateSubmission(&state.Submission{
		SessionKey:           sessionKey,
		WorkspaceID:          a.cfg.Workspaces[0].ID,
		UserID:               "user",
		ChatID:               "chat-1",
		TriggerMessageID:     "msg-2",
		SourceMessageIDs:     []string{"msg-2"},
		SourceRootMessageIDs: []string{"root-1"},
		InputText:            "follow up",
		Status:               "queued",
	})
	if err != nil {
		t.Fatalf("CreateSubmission() error = %v", err)
	}
	if err := a.store.QueueSubmission(sessionKey, subID); err != nil {
		t.Fatalf("QueueSubmission() error = %v", err)
	}

	if err := a.startNextSubmission(sessionKey); err != nil {
		t.Fatalf("startNextSubmission() error = %v", err)
	}
	if len(claude.ensureCalls) != 1 || claude.ensureCalls[0].resumeID != "claude-thread-1" {
		t.Fatalf("ensure calls = %#v", claude.ensureCalls)
	}
	if len(claude.startTurnCalls) != 1 || !strings.Contains(claude.startTurnCalls[0].prompt, "follow up") {
		t.Fatalf("startTurn calls = %#v", claude.startTurnCalls)
	}

	sess := a.store.GetSession(sessionKey)
	if sess == nil {
		t.Fatal("session missing after additional start")
	}
	if len(sess.Queue) != 0 || len(sess.ActiveOperations) != 2 || sess.ActiveTurnID != "claude-turn-1" || sess.ActiveSubmissionID != "sub-running" || sess.Status != "turn_in_progress" {
		t.Fatalf("session after additional Claude start = %+v", sess)
	}
	if got := sess.ActiveOperations[0].SubmissionID; got != subID {
		t.Fatalf("first active operation submission = %q, want %q", got, subID)
	}
	if got := sess.ActiveOperations[1].SubmissionID; got != "sub-running" {
		t.Fatalf("foreground active operation submission = %q, want sub-running", got)
	}

	sub := a.store.GetSubmission(subID)
	if sub == nil || sub.ThreadID != "claude-thread-1" || sub.TurnID == "" || sub.Status != "running" {
		t.Fatalf("submission after additional Claude start = %+v", sub)
	}
}

func TestCompleteApprovalActionUsesClaudeResolver(t *testing.T) {
	a, ff, _ := newTestApp(t)
	a.cfg.Workspaces[0].Backend = backendClaude
	a.codex = nil
	claude := &fakeClaudeCore{}
	a.claude = claude
	a.feishu = ff

	if err := a.store.UpsertPending(&state.PendingRequest{
		ID:          "approve-1",
		Backend:     backendClaude,
		Kind:        "command",
		OwnerUserID: "user-1",
		PayloadJSON: mustJSON(map[string]any{"body": "命令审批"}),
		Status:      "pending",
	}); err != nil {
		t.Fatalf("UpsertPending() error = %v", err)
	}

	resp, err := a.completeApprovalAction(&feishu.CardAction{
		UserID:      "user-1",
		ActionValue: map[string]any{"request_id": "approve-1"},
	}, "approval.command.accept_session")
	if err != nil {
		t.Fatalf("completeApprovalAction() error = %v", err)
	}
	if resp == nil || resp.Toast == nil || resp.Toast.Type != "success" {
		t.Fatalf("approval response = %#v, want success toast", resp)
	}
	if len(claude.approvalCalls) != 1 {
		t.Fatalf("approval calls = %d, want 1", len(claude.approvalCalls))
	}
	if claude.approvalCalls[0].resolution.Behavior != "allow" || claude.approvalCalls[0].resolution.Scope != "session" {
		t.Fatalf("approval resolution = %+v", claude.approvalCalls[0].resolution)
	}
	pending := a.store.PendingByID("approve-1")
	if pending == nil || pending.Status != "resolved" {
		t.Fatalf("pending after approval = %+v, want resolved", pending)
	}
}

func TestCompleteUserInputAnswerUsesClaudeResolver(t *testing.T) {
	a, ff, _ := newTestApp(t)
	a.cfg.Workspaces[0].Backend = backendClaude
	a.codex = nil
	claude := &fakeClaudeCore{}
	a.claude = claude
	a.feishu = ff

	payload := toolUserInputPayload{
		ThreadID: "claude-thread-1",
		TurnID:   "claude-turn-1",
		ItemID:   "item-1",
		Questions: []toolUserInputQuestion{
			{ID: "q1", Question: "Choose a mode", Options: []toolUserInputOption{{Label: "Fast"}, {Label: "Safe"}}},
		},
	}
	if err := a.store.UpsertPending(&state.PendingRequest{
		ID:          "question-1",
		Backend:     backendClaude,
		Kind:        "tool_request_user_input",
		OwnerUserID: "user-1",
		PayloadJSON: mustJSON(payload),
		Status:      "pending",
	}); err != nil {
		t.Fatalf("UpsertPending() error = %v", err)
	}

	resp, err := a.completeUserInputAnswer(&feishu.CardAction{
		UserID:      "user-1",
		ActionValue: map[string]any{"request_id": "question-1", "question_id": "q1", "answer": "Fast"},
	})
	if err != nil {
		t.Fatalf("completeUserInputAnswer() error = %v", err)
	}
	if resp == nil || resp.Toast == nil || resp.Toast.Type != "success" {
		t.Fatalf("user input response = %#v, want success toast", resp)
	}
	if len(claude.userInputCalls) != 1 {
		t.Fatalf("user input calls = %d, want 1", len(claude.userInputCalls))
	}
	if got := claude.userInputCalls[0].answers["Choose a mode"]; got != "Fast" {
		t.Fatalf("resolved Claude answer = %q, want Fast", got)
	}
	pending := a.store.PendingByID("question-1")
	if pending == nil || pending.Status != "resolved" {
		t.Fatalf("pending after answer = %+v, want resolved", pending)
	}
}

func TestCommandInterruptUsesClaudeBackend(t *testing.T) {
	a, ff, _ := newTestApp(t)
	a.cfg.Workspaces[0].Backend = backendClaude
	a.codex = nil
	claude := &fakeClaudeCore{}
	a.claude = claude
	a.feishu = ff

	sessionKey := "feishu:p2p:chat:user"
	if err := a.store.UpsertSession(&state.Session{
		Key:            sessionKey,
		WorkspaceID:    a.cfg.Workspaces[0].ID,
		ActiveThreadID: "claude-thread-1",
		ActiveTurnID:   "claude-turn-1",
		Status:         "turn_in_progress",
	}); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}

	msg := &feishu.InboundMessage{MessageID: "msg-1", ChatID: "chat", ChatType: "p2p", UserID: "user"}
	if err := a.commandInterrupt(msg); err != nil {
		t.Fatalf("commandInterrupt() error = %v", err)
	}
	if len(claude.interruptCalls) != 1 || claude.interruptCalls[0] != sessionKey {
		t.Fatalf("interrupt calls = %#v, want session key %q", claude.interruptCalls, sessionKey)
	}
	if len(ff.replyTexts) == 0 || !strings.Contains(ff.replyTexts[0], "已请求中断当前任务") {
		t.Fatalf("interrupt reply = %#v, want success text", ff.replyTexts)
	}
}

func TestClaudePlanFilePathFromTool(t *testing.T) {
	got := claudePlanFilePathFromTool("Write", map[string]interface{}{
		"file_path": "/home/yuhuan/.claude/plans/example-plan.md",
	})
	if got != "/home/yuhuan/.claude/plans/example-plan.md" {
		t.Fatalf("claudePlanFilePathFromTool() = %q", got)
	}
	if got := claudePlanFilePathFromTool("Edit", map[string]interface{}{"file_path": "/home/yuhuan/.claude/plans/example-plan.md"}); got != "" {
		t.Fatalf("claudePlanFilePathFromTool(non-write) = %q, want empty", got)
	}
}

func TestReadClaudePlanTextFallsBackToLatestHomePlan(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	plansDir := filepath.Join(home, ".claude", "plans")
	if err := os.MkdirAll(plansDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	oldPlan := filepath.Join(plansDir, "old-plan.md")
	if err := os.WriteFile(oldPlan, []byte("old plan"), 0o644); err != nil {
		t.Fatalf("WriteFile(oldPlan) error = %v", err)
	}
	oldTime := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(oldPlan, oldTime, oldTime); err != nil {
		t.Fatalf("Chtimes(oldPlan) error = %v", err)
	}

	latestPlan := filepath.Join(plansDir, "latest-plan.md")
	if err := os.WriteFile(latestPlan, []byte("1. inspect\n2. implement\n3. test"), 0o644); err != nil {
		t.Fatalf("WriteFile(latestPlan) error = %v", err)
	}

	got := readClaudePlanText("", "", time.Now().Add(-30*time.Minute))
	if got != "1. inspect\n2. implement\n3. test" {
		t.Fatalf("readClaudePlanText() = %q", got)
	}
}

func TestCompleteClaudePlanModeTextPreservesOriginalPlanBody(t *testing.T) {
	a, ff, _ := newTestApp(t)
	a.cfg.Workspaces[0].Backend = backendClaude
	a.codex = nil
	claude := &fakeClaudeCore{}
	a.claude = claude

	pending := &state.PendingRequest{
		ID:          "plan-1",
		Backend:     backendClaude,
		Kind:        claudePlanModePendingKind,
		OwnerUserID: "user-1",
		FeishuMsgID: "msg-1",
		PayloadJSON: mustJSON(map[string]any{
			"body": "Claude 已完成计划阶段，请直接回复下一条消息作为反馈。\n\n计划：\n1. inspect /tmp\n2. remove stale files",
		}),
		Status: "pending",
	}
	if err := a.store.UpsertPending(pending); err != nil {
		t.Fatalf("UpsertPending() error = %v", err)
	}

	if err := a.completeClaudePlanModeText(&feishu.InboundMessage{Text: "退出plan"}, pending); err != nil {
		t.Fatalf("completeClaudePlanModeText() error = %v", err)
	}
	if len(claude.planCalls) != 1 || claude.planCalls[0].requestID != "plan-1" || claude.planCalls[0].feedback != "退出plan" {
		t.Fatalf("plan calls = %#v", claude.planCalls)
	}
	if got := a.store.PendingByID("plan-1"); got == nil || got.Status != "resolved" {
		t.Fatalf("pending after submit = %+v", got)
	}
	if len(ff.patchedCards) != 1 {
		t.Fatalf("patched cards = %d, want 1", len(ff.patchedCards))
	}
	if got := cardHeaderTitle(t, ff.patchedCards[0]); got != "计划反馈已提交" {
		t.Fatalf("patched card title = %q", got)
	}
	body := cardMarkdownContent(t, ff.patchedCards[0])
	for _, want := range []string{"你的反馈：", "退出plan", "原计划：", "1. inspect /tmp", "2. remove stale files"} {
		if !strings.Contains(body, want) {
			t.Fatalf("patched card body missing %q: %q", want, body)
		}
	}
}

func TestCompletePendingFormCancelClaudePlanPreservesOriginalPlanBody(t *testing.T) {
	a, _, _ := newTestApp(t)
	a.cfg.Workspaces[0].Backend = backendClaude
	a.codex = nil
	claude := &fakeClaudeCore{}
	a.claude = claude

	pending := &state.PendingRequest{
		ID:          "plan-cancel-1",
		Backend:     backendClaude,
		Kind:        claudePlanModePendingKind,
		SessionKey:  "sess-1",
		OwnerUserID: "user-1",
		PayloadJSON: mustJSON(map[string]any{
			"body": "Claude 已完成计划阶段，请直接回复下一条消息作为反馈。\n\n计划：\n1. inspect /tmp\n2. remove stale files",
		}),
		Status: "pending",
	}
	if err := a.store.UpsertPending(pending); err != nil {
		t.Fatalf("UpsertPending() error = %v", err)
	}

	resp, err := a.completePendingFormCancel(&feishu.CardAction{
		UserID:      "user-1",
		ActionValue: map[string]any{"request_id": "plan-cancel-1"},
	})
	if err != nil {
		t.Fatalf("completePendingFormCancel() error = %v", err)
	}
	if resp == nil || resp.Toast == nil || resp.Card == nil {
		t.Fatalf("completePendingFormCancel() = %#v", resp)
	}
	if len(claude.cancelCalls) != 1 || claude.cancelCalls[0].requestID != "plan-cancel-1" {
		t.Fatalf("cancel calls = %#v", claude.cancelCalls)
	}
	card, _ := resp.Card.Data.(map[string]any)
	if got := cardHeaderTitle(t, card); got != "计划确认已取消" {
		t.Fatalf("cancelled card title = %q", got)
	}
	body := cardMarkdownContent(t, card)
	for _, want := range []string{"已取消本次计划确认。", "原计划：", "1. inspect /tmp", "2. remove stale files"} {
		if !strings.Contains(body, want) {
			t.Fatalf("cancelled card body missing %q: %q", want, body)
		}
	}
}

func TestHandleFeishuMessageReplyStartsAdditionalClaudeTurn(t *testing.T) {
	a, _, _ := newTestApp(t)
	a.cfg.Workspaces[0].Backend = backendClaude
	a.codex = nil
	claude := &fakeClaudeCore{ensureSessionID: "claude-thread-1"}
	a.claude = claude

	targetSessionKey := "feishu:group:chat-1:root:root-msg"
	if err := a.store.UpsertSession(&state.Session{
		Key:                targetSessionKey,
		WorkspaceID:        a.cfg.Workspaces[0].ID,
		ChatID:             "chat-1",
		ChatType:           "group",
		OwnerUserID:        "user-1",
		RootMessageID:      "root-msg",
		ActiveThreadID:     "claude-thread-1",
		ActiveTurnID:       "claude-turn-1",
		ActiveSubmissionID: "sub-running",
		Status:             "turn_in_progress",
	}); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}
	if _, err := a.store.CreateSubmission(&state.Submission{
		ID:                   "sub-running",
		SessionKey:           targetSessionKey,
		WorkspaceID:          a.cfg.Workspaces[0].ID,
		UserID:               "user-1",
		ChatID:               "chat-1",
		TriggerMessageID:     "msg-running",
		SourceMessageIDs:     []string{"msg-running"},
		SourceRootMessageIDs: []string{"root-msg"},
		InputText:            "running",
		ThreadID:             "claude-thread-1",
		TurnID:               "claude-turn-1",
		Status:               "running",
	}); err != nil {
		t.Fatalf("CreateSubmission(sub-running) error = %v", err)
	}
	if err := a.store.UpsertMessageLink(&state.MessageLink{
		MessageID:  "root-msg",
		SessionKey: targetSessionKey,
		ThreadID:   "claude-thread-1",
		TurnID:     "claude-turn-1",
	}); err != nil {
		t.Fatalf("UpsertMessageLink() error = %v", err)
	}

	a.handleFeishuMessage(&feishu.InboundMessage{
		MessageID:       "reply-1",
		ChatID:          "chat-1",
		ChatType:        "group",
		UserID:          "user-1",
		Text:            "300字就行了",
		RootMessageID:   "root-msg",
		ParentMessageID: "some-parent",
	})

	if len(claude.interruptCalls) != 0 {
		t.Fatalf("interrupt calls = %#v, want none", claude.interruptCalls)
	}
	if len(claude.startTurnCalls) != 1 || !strings.Contains(claude.startTurnCalls[0].prompt, "300字就行了") {
		t.Fatalf("startTurn calls = %#v", claude.startTurnCalls)
	}
	sess := a.store.GetSession(targetSessionKey)
	if sess == nil || len(sess.Queue) != 0 || len(sess.ActiveOperations) != 2 || sess.ActiveTurnID != "claude-turn-1" || sess.ActiveSubmissionID != "sub-running" || sess.Status != "turn_in_progress" {
		t.Fatalf("session after reply continuation = %+v", sess)
	}
	nextSubmissionID := strings.TrimSpace(sess.ActiveOperations[0].SubmissionID)
	sub := a.store.GetSubmission(nextSubmissionID)
	if sub == nil || sub.InputText != "300字就行了" || sub.SessionKey != targetSessionKey || sub.TurnID == "" || sub.Status != "running" {
		t.Fatalf("follow-up submission = %+v", sub)
	}
}

func TestTryClaudeReplyContinuationUsesActiveSessionDespiteStaleLink(t *testing.T) {
	a, _, _ := newTestApp(t)
	a.cfg.Workspaces[0].Backend = backendClaude
	a.codex = nil
	claude := &fakeClaudeCore{ensureSessionID: "claude-thread-1"}
	a.claude = claude

	sessionKey := "feishu:group:chat-1:root:root-msg"
	if err := a.store.UpsertSession(&state.Session{
		Key:                     sessionKey,
		WorkspaceID:             a.cfg.Workspaces[0].ID,
		ChatID:                  "chat-1",
		ChatType:                "group",
		OwnerUserID:             "user-1",
		RootMessageID:           "root-msg",
		ActiveThreadID:          "claude-thread-1",
		ActiveThreadWorkspaceID: a.cfg.Workspaces[0].ID,
		ActiveTurnID:            "claude-turn-current",
		ActiveSubmissionID:      "sub-running",
		Status:                  "turn_in_progress",
	}); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}
	if _, err := a.store.CreateSubmission(&state.Submission{
		ID:                   "sub-running",
		SessionKey:           sessionKey,
		WorkspaceID:          a.cfg.Workspaces[0].ID,
		UserID:               "user-1",
		ChatID:               "chat-1",
		TriggerMessageID:     "msg-running",
		SourceMessageIDs:     []string{"msg-running"},
		SourceRootMessageIDs: []string{"root-msg"},
		InputText:            "running",
		ThreadID:             "claude-thread-1",
		TurnID:               "claude-turn-current",
		Status:               "running",
	}); err != nil {
		t.Fatalf("CreateSubmission(sub-running) error = %v", err)
	}

	sess := a.store.GetSession(sessionKey)
	if sess == nil {
		t.Fatal("session missing")
	}
	steered, err := a.tryClaudeReplyContinuation(&feishu.InboundMessage{
		MessageID:       "reply-1",
		ChatID:          "chat-1",
		ChatType:        "group",
		UserID:          "user-1",
		Text:            "follow up from stale link",
		RootMessageID:   "root-msg",
		ParentMessageID: "target-msg",
	}, &state.MessageLink{
		SessionKey: sessionKey,
		ThreadID:   "stale-thread",
		TurnID:     "stale-turn",
	}, sessionKey, sess)
	if err != nil || !steered {
		t.Fatalf("tryClaudeReplyContinuation() = %v, %v", steered, err)
	}
	if len(claude.startTurnCalls) != 1 || !strings.Contains(claude.startTurnCalls[0].prompt, "follow up from stale link") {
		t.Fatalf("startTurn calls = %#v", claude.startTurnCalls)
	}
	updated := a.store.GetSession(sessionKey)
	if updated == nil || len(updated.ActiveOperations) != 2 || updated.ActiveTurnID != "claude-turn-current" || updated.ActiveSubmissionID != "sub-running" {
		t.Fatalf("session after stale-link continuation = %+v", updated)
	}
}

func TestCommandAppendUsesClaudeContinuation(t *testing.T) {
	a, _, _ := newTestApp(t)
	a.cfg.Workspaces[0].Backend = backendClaude
	a.codex = nil
	claude := &fakeClaudeCore{ensureSessionID: "claude-thread-1"}
	a.claude = claude

	msg := &feishu.InboundMessage{
		MessageID:     "cmd-msg-1",
		ChatID:        "chat-1",
		ChatType:      "group",
		UserID:        "user-1",
		RootMessageID: "root-msg",
	}
	sessionKey := a.makeSessionKey(msg)
	if err := a.store.UpsertSession(&state.Session{
		Key:                     sessionKey,
		WorkspaceID:             a.cfg.Workspaces[0].ID,
		ChatID:                  "chat-1",
		ChatType:                "group",
		OwnerUserID:             "user-1",
		RootMessageID:           "root-msg",
		ActiveThreadID:          "claude-thread-1",
		ActiveThreadWorkspaceID: a.cfg.Workspaces[0].ID,
		ActiveTurnID:            "claude-turn-current",
		ActiveSubmissionID:      "sub-running",
		Status:                  "turn_in_progress",
	}); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}
	if _, err := a.store.CreateSubmission(&state.Submission{
		ID:                   "sub-running",
		SessionKey:           sessionKey,
		WorkspaceID:          a.cfg.Workspaces[0].ID,
		UserID:               "user-1",
		ChatID:               "chat-1",
		TriggerMessageID:     "msg-running",
		SourceMessageIDs:     []string{"msg-running"},
		SourceRootMessageIDs: []string{"root-msg"},
		InputText:            "running",
		ThreadID:             "claude-thread-1",
		TurnID:               "claude-turn-current",
		Status:               "running",
	}); err != nil {
		t.Fatalf("CreateSubmission(sub-running) error = %v", err)
	}

	if err := a.commandAppend(msg, "  append from command  "); err != nil {
		t.Fatalf("commandAppend() error = %v", err)
	}
	if len(claude.startTurnCalls) != 1 || !strings.Contains(claude.startTurnCalls[0].prompt, "append from command") {
		t.Fatalf("startTurn calls = %#v", claude.startTurnCalls)
	}
	updated := a.store.GetSession(sessionKey)
	if updated == nil || len(updated.ActiveOperations) != 2 || updated.ActiveTurnID != "claude-turn-current" || updated.ActiveSubmissionID != "sub-running" {
		t.Fatalf("session after Claude command append = %+v", updated)
	}
	nextSubmissionID := strings.TrimSpace(updated.ActiveOperations[0].SubmissionID)
	sub := a.store.GetSubmission(nextSubmissionID)
	if sub == nil || sub.InputText != "append from command" || sub.TriggerMessageID != "" || len(sub.SourceRootMessageIDs) != 1 || sub.SourceRootMessageIDs[0] != "root-msg" {
		t.Fatalf("Claude append submission = %+v", sub)
	}
}
