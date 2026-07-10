package app

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	appreview "feidex/internal/app/review"
	"feidex/internal/codexrpc"
	"feidex/internal/daemon"
	"feidex/internal/feishu"
	"feidex/internal/release"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

func waitForPatchedCard(t *testing.T, ff *fakeFeishuClient) map[string]any {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for len(ff.patchedCardsSnapshot()) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	patchedCards := ff.patchedCardsSnapshot()
	if len(patchedCards) == 0 {
		t.Fatal("expected asynchronous card patch")
	}
	return patchedCards[len(patchedCards)-1]
}

func callActionWithTimeout(t *testing.T, fn func() (*callback.CardActionTriggerResponse, error)) (*callback.CardActionTriggerResponse, error) {
	t.Helper()
	type result struct {
		resp *callback.CardActionTriggerResponse
		err  error
	}
	done := make(chan result, 1)
	go func() {
		resp, err := fn()
		done <- result{resp: resp, err: err}
	}()
	select {
	case res := <-done:
		return res.resp, res.err
	case <-time.After(500 * time.Millisecond):
		t.Fatal("card action did not return before background work completed")
		return nil, nil
	}
}

func TestCompleteMenuPlanReturnsToastOnlyAndPatchesAsync(t *testing.T) {
	a, ff, fc := newTestApp(t)
	a.cfg.Codex.ExperimentalAPI = true
	a.cfg.Codex.Model = "gpt-test"
	msg := &feishu.InboundMessage{MessageID: "msg-plan", ChatID: "chat-1", ChatType: "p2p", UserID: "user-1"}
	sessionKey := seedGoalTestSession(t, a, msg, "thread-1")

	started := make(chan struct{})
	release := make(chan struct{})
	var closeStarted sync.Once
	modePlan := "plan"
	fc.callHook = func(_ context.Context, method string, _ any, out any) error {
		if method != "collaborationMode/list" {
			t.Fatalf("unexpected method: %s", method)
		}
		closeStarted.Do(func() { close(started) })
		<-release
		out.(*codexrpc.CollaborationModeListResponse).Data = []codexrpc.CollaborationModeMask{{
			Name: "Plan",
			Mode: &modePlan,
		}}
		return nil
	}

	resp, err := callActionWithTimeout(t, func() (*callback.CardActionTriggerResponse, error) {
		return completeMenuPlanAsync(a, &feishu.CardAction{
			UserID:      msg.UserID,
			ChatID:      msg.ChatID,
			MessageID:   msg.MessageID,
			ActionValue: map[string]any{"session_key": sessionKey},
		}, sessionKey)
	})
	if err != nil || resp == nil || resp.Toast == nil || resp.Toast.Type != "info" || resp.Card != nil {
		t.Fatalf("completeMenuPlanAsync() = %#v, %v; want info toast without card", resp, err)
	}
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("background plan RPC did not start")
	}
	if patched := ff.patchedCardsSnapshot(); len(patched) != 0 {
		t.Fatalf("patched cards before plan RPC release = %+v, want none", patched)
	}
	close(release)

	_ = waitForPatchedCard(t, ff)
	sess := a.State().Session(sessionKey)
	if mode := normalizeThreadCollaborationMode(sess.ActiveThreadCollaborationMode); mode == nil || mode.Mode != "plan" {
		t.Fatalf("session collaboration mode = %+v, want plan", sess.ActiveThreadCollaborationMode)
	}
}

func TestCompleteGoalActionReturnsToastOnlyAndPatchesAsync(t *testing.T) {
	a, ff, fc := newTestApp(t)
	msg := &feishu.InboundMessage{MessageID: "msg-goal", ChatID: "chat-1", ChatType: "p2p", UserID: "user-1"}
	sessionKey := seedGoalTestSession(t, a, msg, "thread-1")

	started := make(chan struct{})
	release := make(chan struct{})
	var closeStarted sync.Once
	fc.callHook = func(_ context.Context, method string, params any, out any) error {
		if method != "thread/goal/set" {
			t.Fatalf("unexpected method: %s", method)
		}
		closeStarted.Do(func() { close(started) })
		<-release
		p := params.(codexrpc.ThreadGoalSetParams)
		status := codexrpc.ThreadGoalStatusPaused
		if p.Status != nil {
			status = *p.Status
		}
		out.(*codexrpc.ThreadGoalSetResponse).Goal = testThreadGoal(p.ThreadID, "keep going", status)
		return nil
	}

	resp, err := callActionWithTimeout(t, func() (*callback.CardActionTriggerResponse, error) {
		return completeGoalRenderedActionAsync(a, &feishu.CardAction{
			UserID:      msg.UserID,
			ChatID:      msg.ChatID,
			MessageID:   msg.MessageID,
			ActionValue: map[string]any{"session_key": sessionKey, "thread_id": "thread-1"},
		}, sessionKey, "正在更新 goal", func(goalSvc goalService) (*callback.CardActionTriggerResponse, error) {
			return goalSvc.CompleteGoalStatusAction(&feishu.CardAction{
				UserID:      msg.UserID,
				ChatID:      msg.ChatID,
				MessageID:   msg.MessageID,
				ActionValue: map[string]any{"session_key": sessionKey, "thread_id": "thread-1"},
			}, codexrpc.ThreadGoalStatusPaused)
		})
	})
	if err != nil || resp == nil || resp.Toast == nil || resp.Toast.Type != "info" || resp.Card != nil {
		t.Fatalf("completeGoalRenderedActionAsync() = %#v, %v; want info toast without card", resp, err)
	}
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("background goal RPC did not start")
	}
	if patched := ff.patchedCardsSnapshot(); len(patched) != 0 {
		t.Fatalf("patched cards before goal RPC release = %+v, want none", patched)
	}
	close(release)

	patched := waitForPatchedCard(t, ff)
	body := cardMarkdownContent(t, patched)
	if !strings.Contains(body, "paused") || !strings.Contains(body, "keep going") {
		t.Fatalf("patched goal body = %q, want paused goal card", body)
	}
}

func TestCompleteUpgradeDevReturnsPreparingCardAndPatchesAsync(t *testing.T) {
	origRelease := newReleaseClient
	origManager := newDaemonManager
	origVersion := currentVersion
	origGOARCH := currentGOARCH
	defer func() {
		newReleaseClient = origRelease
		newDaemonManager = origManager
		currentVersion = origVersion
		currentGOARCH = origGOARCH
	}()

	a, ff, _ := newTestApp(t)
	blocking := &blockingReleaseClient{
		started: make(chan struct{}),
		release: make(chan struct{}),
		info: &release.ReleaseInfo{
			Version:        "dev-20260423T120000-a1b2c3d4",
			ReleaseTag:     release.DevReleaseTag,
			HTMLURL:        "https://example.test/releases/dev-latest",
			BinaryName:     "feidex-linux-amd64",
			BinaryURL:      "https://github.com/example/feidex-linux-amd64",
			ExpectedSHA256: "abc123",
			SourceCommit:   "a1b2c3d4",
		},
	}
	newReleaseClient = func() releaseClient { return blocking }
	newDaemonManager = func(string) (daemon.Manager, error) {
		return &fakeDaemonManagerForApp{status: &daemon.Status{Installed: true, Running: true, PID: os.Getpid()}}, nil
	}
	currentVersion = func() string { return "0.1.0" }
	currentGOARCH = func() string { return "amd64" }

	resp, err := newMenuActionService(a).completeUpgradeDev(&feishu.CardAction{
		UserID:      "user-1",
		MessageID:   "msg-upgrade-dev",
		ActionValue: map[string]any{"session_key": "sess-1"},
	})
	if err != nil || resp == nil || resp.Toast == nil || resp.Toast.Type != "info" || resp.Card == nil {
		t.Fatalf("completeUpgradeDev(async) = %#v, %v", resp, err)
	}
	card, _ := resp.Card.Data.(map[string]any)
	if body := cardMarkdownContent(t, card); !strings.Contains(body, "正在检查可升级版本") {
		t.Fatalf("upgrade dev preparing body = %q", body)
	}
	if patchedCards := ff.patchedCardsSnapshot(); len(patchedCards) != 0 {
		t.Fatalf("patched cards before dev release completes = %+v, want none", patchedCards)
	}

	<-blocking.started
	close(blocking.release)

	patched := waitForPatchedCard(t, ff)
	body := cardMarkdownContent(t, patched)
	if !strings.Contains(body, "开发版本: `dev-20260423T120000-a1b2c3d4`") {
		t.Fatalf("patched upgrade dev body = %q", body)
	}
}

func TestCompleteMenuReviewUncommittedReturnsPreparingCardAndPatchesAsync(t *testing.T) {
	a, ff, fc := newTestApp(t)
	repo := initReviewGitRepo(t, a.cfg.Workspaces[0].Cwd)
	writeFile(t, repo+"/main.go", "package main\n\nfunc main() { println(\"dirty\") }\n")

	msg := &feishu.InboundMessage{MessageID: "msg-review-uncommitted", ChatID: "chat-1", ChatType: "p2p", UserID: "user-1"}
	sessionKey := makeSessionKey(a, msg)
	mustUpsertReviewSession(t, a, sessionKey, msg.ChatID, msg.ChatType, msg.UserID, "thread-1")
	markSessionThreadLive(a, sessionKey, "thread-1")

	var gotParams map[string]any
	fc.callHook = func(_ context.Context, method string, params any, out any) error {
		if method != "review/start" {
			t.Fatalf("unexpected method: %s", method)
		}
		gotParams = params.(map[string]any)
		if result, ok := out.(*codexrpc.ReviewStartResult); ok {
			result.ReviewThreadID = "thread-1"
			result.Turn.ID = "review-turn-uncommitted"
		}
		return nil
	}

	resp, err := completeMenuReviewUncommitted(a, &feishu.CardAction{
		UserID:      msg.UserID,
		ChatID:      msg.ChatID,
		MessageID:   msg.MessageID,
		ActionValue: map[string]any{"session_key": sessionKey},
	}, sessionKey)
	if err != nil || resp == nil || resp.Toast == nil || resp.Toast.Type != "info" || resp.Card == nil {
		t.Fatalf("completeMenuReviewUncommitted(async) = %#v, %v", resp, err)
	}
	card, _ := resp.Card.Data.(map[string]any)
	if body := cardMarkdownContent(t, card); !strings.Contains(body, "正在准备 review") {
		t.Fatalf("review preparing body = %q", body)
	}

	patched := waitForPatchedCard(t, ff)
	body := cardMarkdownContent(t, patched)
	if !strings.Contains(body, "未提交改动") {
		t.Fatalf("review patched body = %q, want uncommitted review confirmation", body)
	}
	target, _ := gotParams["target"].(map[string]any)
	if got, _ := target["type"].(string); got != appreview.TargetUncommitted {
		t.Fatalf("target.type = %q, want %q", got, appreview.TargetUncommitted)
	}
}

func TestCompleteMenuReviewBaseReturnsPreparingCardAndPatchesAsync(t *testing.T) {
	a, ff, _ := newTestApp(t)
	_ = initReviewGitRepo(t, a.cfg.Workspaces[0].Cwd)

	msg := &feishu.InboundMessage{MessageID: "msg-review-base", ChatID: "chat-1", ChatType: "p2p", UserID: "user-1"}
	sessionKey := makeSessionKey(a, msg)
	mustUpsertReviewSession(t, a, sessionKey, msg.ChatID, msg.ChatType, msg.UserID, "thread-1")
	markSessionThreadLive(a, sessionKey, "thread-1")

	resp, err := completeMenuReviewBase(a, &feishu.CardAction{
		UserID:      msg.UserID,
		ChatID:      msg.ChatID,
		MessageID:   msg.MessageID,
		ActionValue: map[string]any{"session_key": sessionKey},
	}, sessionKey)
	if err != nil || resp == nil || resp.Toast == nil || resp.Toast.Type != "info" || resp.Card == nil {
		t.Fatalf("completeMenuReviewBase(async) = %#v, %v", resp, err)
	}
	card, _ := resp.Card.Data.(map[string]any)
	if body := cardMarkdownContent(t, card); !strings.Contains(body, "正在加载 base branch 选择") {
		t.Fatalf("review base preparing body = %q", body)
	}

	patched := waitForPatchedCard(t, ff)
	if got := cardSelectStaticForTest(patched); len(got) != 1 {
		t.Fatalf("patched review base card selects = %+v, want 1", got)
	}
	pending := singleReviewPendingRequest(t, a)
	if pending.FeishuMsgID != msg.MessageID {
		t.Fatalf("pending.FeishuMsgID = %q, want %q", pending.FeishuMsgID, msg.MessageID)
	}
}

func TestCompleteReviewBaseSelectReturnsPreparingCardAndPatchesAsync(t *testing.T) {
	a, ff, _ := newTestApp(t)
	_ = initReviewGitRepo(t, a.cfg.Workspaces[0].Cwd)

	msg := &feishu.InboundMessage{MessageID: "msg-review-select-base", ChatID: "chat-1", ChatType: "p2p", UserID: "user-1"}
	sessionKey := makeSessionKey(a, msg)
	mustUpsertReviewSession(t, a, sessionKey, msg.ChatID, msg.ChatType, msg.UserID, "thread-1")
	markSessionThreadLive(a, sessionKey, "thread-1")

	if err := newReviewFormService(a).beginReviewForm(msg, reviewFormModeBase); err != nil {
		t.Fatalf("beginReviewForm(base) error = %v", err)
	}
	pending := singleReviewPendingRequest(t, a)

	resp, err := newReviewFormService(a).completeReviewBaseSelect(&feishu.CardAction{
		UserID:      msg.UserID,
		MessageID:   pending.FeishuMsgID,
		ActionValue: map[string]any{"request_id": pending.ID},
		Option:      "main",
	})
	if err != nil || resp == nil || resp.Toast == nil || resp.Toast.Type != "info" || resp.Card == nil {
		t.Fatalf("completeReviewBaseSelect(async) = %#v, %v", resp, err)
	}
	card, _ := resp.Card.Data.(map[string]any)
	if body := cardMarkdownContent(t, card); !strings.Contains(body, "正在刷新 base branch 选择") {
		t.Fatalf("review base select preparing body = %q", body)
	}

	patched := waitForPatchedCard(t, ff)
	body := cardMarkdownContent(t, patched)
	if !strings.Contains(body, "当前选择: `main`") {
		t.Fatalf("patched review base select body = %q", body)
	}
	basePayload := reviewPendingPayloadFromPending(a.store.PendingByID(pending.ID))
	if basePayload.Branch != "main" {
		t.Fatalf("base payload after async select = %+v, want branch main", basePayload)
	}
}

func TestCompleteReviewFormSubmitBaseReturnsPreparingCardAndPatchesAsync(t *testing.T) {
	a, ff, fc := newTestApp(t)
	_ = initReviewGitRepo(t, a.cfg.Workspaces[0].Cwd)

	msg := &feishu.InboundMessage{MessageID: "msg-review-submit-base", ChatID: "chat-1", ChatType: "p2p", UserID: "user-1"}
	sessionKey := makeSessionKey(a, msg)
	mustUpsertReviewSession(t, a, sessionKey, msg.ChatID, msg.ChatType, msg.UserID, "thread-1")
	markSessionThreadLive(a, sessionKey, "thread-1")

	if err := newReviewFormService(a).beginReviewForm(msg, reviewFormModeBase); err != nil {
		t.Fatalf("beginReviewForm(base) error = %v", err)
	}
	pending := singleReviewPendingRequest(t, a)
	_ = a.store.UpdatePending(pending.ID, func(req *state.PendingRequest) {
		payload := reviewPendingPayloadFromPending(req)
		payload.Branch = "main"
		req.PayloadJSON = mustJSON(payload)
	})

	var gotParams map[string]any
	fc.callHook = func(_ context.Context, method string, params any, out any) error {
		if method != "review/start" {
			t.Fatalf("unexpected method: %s", method)
		}
		gotParams = params.(map[string]any)
		if result, ok := out.(*codexrpc.ReviewStartResult); ok {
			result.ReviewThreadID = "thread-1"
			result.Turn.ID = "review-turn-base"
		}
		return nil
	}

	resp, err := newReviewFormService(a).completeReviewFormSubmit(&feishu.CardAction{
		UserID:      msg.UserID,
		ChatID:      msg.ChatID,
		MessageID:   pending.FeishuMsgID,
		ActionValue: map[string]any{"request_id": pending.ID},
	})
	if err != nil || resp == nil || resp.Toast == nil || resp.Toast.Type != "info" || resp.Card == nil {
		t.Fatalf("completeReviewFormSubmit(async) = %#v, %v", resp, err)
	}
	card, _ := resp.Card.Data.(map[string]any)
	if body := cardMarkdownContent(t, card); !strings.Contains(body, "正在启动 review") {
		t.Fatalf("review submit preparing body = %q", body)
	}

	patched := waitForPatchedCard(t, ff)
	body := cardMarkdownContent(t, patched)
	if !strings.Contains(body, "已启动 review") {
		t.Fatalf("patched review submit body = %q", body)
	}
	target, _ := gotParams["target"].(map[string]any)
	if got, _ := target["type"].(string); got != appreview.TargetBaseBranch {
		t.Fatalf("target.type = %q, want %q", got, appreview.TargetBaseBranch)
	}
	if got, _ := target["branch"].(string); got != "main" {
		t.Fatalf("target.branch = %q, want main", got)
	}
	refreshed := a.store.PendingByID(pending.ID)
	if refreshed == nil || refreshed.Status != "resolved" {
		t.Fatalf("pending after async submit = %+v, want resolved", refreshed)
	}
}

func TestCompleteMenuInterruptClaudeReturnsPreparingCardAndPatchesAsync(t *testing.T) {
	a, ff, _ := newTestApp(t)
	a.cfg.Feishu.Backend = backendClaude
	a.codex = nil
	claude := &fakeClaudeCore{}
	a.claude = claude

	sessionKey := makeSessionKey(a, &feishu.InboundMessage{ChatType: "p2p", ChatID: "chat-1", UserID: "user-1"})
	if err := a.store.UpsertSession(&state.Session{
		Key:            sessionKey,
		WorkspaceID:    a.cfg.Workspaces[0].ID,
		OwnerUserID:    "user-1",
		ChatID:         "chat-1",
		ChatType:       "p2p",
		ActiveThreadID: "claude-session-1",
		ActiveTurnID:   "turn-1",
		Status:         "running",
	}); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}

	resp, err := newThreadService(a).CompleteMenuInterrupt(&feishu.CardAction{
		UserID:      "user-1",
		ChatID:      "chat-1",
		MessageID:   "msg-stop",
		ActionValue: map[string]any{"session_key": sessionKey, "parent_action": "menu.tools"},
	}, sessionKey, "turn-1")
	if err != nil || resp == nil || resp.Toast == nil || resp.Toast.Type != "info" || resp.Card == nil {
		t.Fatalf("completeMenuInterrupt(async Claude) = %#v, %v", resp, err)
	}
	card, _ := resp.Card.Data.(map[string]any)
	if body := cardMarkdownContent(t, card); !strings.Contains(body, "正在向 Claude 请求中断当前任务") {
		t.Fatalf("interrupt preparing body = %q", body)
	}

	patched := waitForPatchedCard(t, ff)
	body := cardMarkdownContent(t, patched)
	if !strings.Contains(body, "已请求中断当前任务") {
		t.Fatalf("interrupt patched body = %q", body)
	}
	if len(claude.interruptCalls) != 1 || claude.interruptCalls[0] != sessionKey {
		t.Fatalf("interrupt calls = %#v, want %q", claude.interruptCalls, sessionKey)
	}
}
