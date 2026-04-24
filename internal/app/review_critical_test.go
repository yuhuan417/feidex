package app

import (
	"context"
	appreview "feidex/internal/app/review"
	"testing"

	"feidex/internal/codexrpc"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

func TestReviewTargetResolutionAndSubmissionPayloads(t *testing.T) {
	a, _, _ := newTestApp(t)
	repo, commits := initReviewGitRepoWithCommits(t, a.cfg.Workspaces[0].Cwd)

	resolvedBase, err := newReviewGitService(a).resolveReviewTarget(repo, appreview.TargetSpec{
		Type:   appreview.TargetBaseBranch,
		Branch: "main",
	})
	if err != nil {
		t.Fatalf("resolveReviewTarget(base) error = %v", err)
	}
	if resolvedBase.Type != appreview.TargetBaseBranch || resolvedBase.Branch != "main" {
		t.Fatalf("resolved base target = %+v, want base branch main", resolvedBase)
	}
	if got := appreview.SubmissionInputText(resolvedBase); got != "Review: base branch main" {
		t.Fatalf("appreview.SubmissionInputText(base) = %q", got)
	}

	resolvedCommit, err := newReviewGitService(a).resolveReviewTarget(repo, appreview.TargetSpec{
		Type:      appreview.TargetCommit,
		CommitSHA: commits[1][:8],
	})
	if err != nil {
		t.Fatalf("resolveReviewTarget(commit) error = %v", err)
	}
	if resolvedCommit.Type != appreview.TargetCommit || resolvedCommit.CommitSHA != commits[1] || resolvedCommit.CommitTitle != "feature change" {
		t.Fatalf("resolved commit target = %+v, want full sha and title", resolvedCommit)
	}
	if got := appreview.TargetSummary(resolvedCommit); got != "commit `"+appreview.ShortCommitSHA(commits[1])+"` feature change" {
		t.Fatalf("appreview.TargetSummary(commit) = %q", got)
	}

	writeFile(t, repo+"/main.go", "package main\n\nfunc main() { println(\"dirty\") }\n")
	resolvedUncommitted, err := newReviewGitService(a).resolveReviewTarget(repo, appreview.TargetSpec{Type: appreview.TargetUncommitted})
	if err != nil {
		t.Fatalf("resolveReviewTarget(uncommitted) error = %v", err)
	}
	if resolvedUncommitted.Type != appreview.TargetUncommitted {
		t.Fatalf("resolved uncommitted target = %+v, want uncommitted", resolvedUncommitted)
	}

	resolvedCustom, err := newReviewGitService(a).resolveReviewTarget(repo, appreview.TargetSpec{
		Type:         appreview.TargetCustom,
		Instructions: "focus on regressions",
	})
	if err != nil {
		t.Fatalf("resolveReviewTarget(custom) error = %v", err)
	}
	if resolvedCustom.Type != appreview.TargetCustom || resolvedCustom.Instructions != "focus on regressions" {
		t.Fatalf("resolved custom target = %+v, want custom instructions", resolvedCustom)
	}
	if got := appreview.SubmissionInputText(resolvedCustom); got != "Review: focus on regressions" {
		t.Fatalf("appreview.SubmissionInputText(custom) = %q", got)
	}
}

func TestStartSubmissionReviewUsesStoredTargetPayload(t *testing.T) {
	a, _, fc := newTestApp(t)

	var gotMethod string
	var gotParams map[string]any
	fc.callHook = func(_ context.Context, method string, params any, out any) error {
		gotMethod = method
		gotParams, _ = params.(map[string]any)
		if result, ok := out.(*codexrpc.ReviewStartResult); ok {
			result.ReviewThreadID = "thread-1"
			result.Turn.ID = "review-turn-1"
		}
		return nil
	}

	turnID, err := startSubmissionReview(a,context.Background(), "thread-1", &state.Submission{
		Kind:              submissionKindReview,
		ReviewTargetType:  appreview.TargetCommit,
		ReviewCommitSHA:   "abcdef1234567890",
		ReviewCommitTitle: "feature change",
	})
	if err != nil {
		t.Fatalf("startSubmissionReview() error = %v", err)
	}
	if turnID != "review-turn-1" {
		t.Fatalf("startSubmissionReview() turnID = %q, want review-turn-1", turnID)
	}
	if gotMethod != "review/start" {
		t.Fatalf("review start method = %q, want review/start", gotMethod)
	}
	if got, _ := gotParams["threadId"].(string); got != "thread-1" {
		t.Fatalf("review/start threadId = %q, want thread-1", got)
	}
	target, _ := gotParams["target"].(map[string]any)
	if got, _ := target["type"].(string); got != appreview.TargetCommit {
		t.Fatalf("review/start target.type = %q, want commit", got)
	}
	if got, _ := target["sha"].(string); got != "abcdef1234567890" {
		t.Fatalf("review/start target.sha = %q", got)
	}
	if got, _ := target["title"].(string); got != "feature change" {
		t.Fatalf("review/start target.title = %q", got)
	}
}

func TestReviewFormSelectorsUpdatePendingPayload(t *testing.T) {
	a, _, _ := newTestApp(t)
	_, commits := initReviewGitRepoWithCommits(t, a.cfg.Workspaces[0].Cwd)

	msg := &feishu.InboundMessage{MessageID: "msg-review-select", ChatID: "chat-1", ChatType: "p2p", UserID: "user-1"}
	sessionKey := makeSessionKey(a, msg)
	mustUpsertReviewSession(t, a, sessionKey, msg.ChatID, msg.ChatType, msg.UserID, "thread-1")
	markSessionThreadLive(a, sessionKey, "thread-1")

	if err := newReviewFormService(a).beginReviewForm(msg, reviewFormModeBase); err != nil {
		t.Fatalf("beginReviewForm(base) error = %v", err)
	}
	basePending := singleReviewPendingRequest(t, a)
	resp, err := newReviewFormService(a).completeReviewBaseSelect(&feishu.CardAction{
		UserID:      msg.UserID,
		ActionValue: map[string]any{"request_id": basePending.ID},
		Option:      "main",
	})
	if err != nil {
		t.Fatalf("completeReviewBaseSelect() error = %v", err)
	}
	if resp == nil || resp.Card == nil {
		t.Fatalf("base select response = %#v, want updated card", resp)
	}
	basePayload := reviewPendingPayloadFromPending(a.store.PendingByID(basePending.ID))
	if basePayload.Branch != "main" {
		t.Fatalf("base payload after select = %+v, want branch main", basePayload)
	}

	a.store.DeletePending(basePending.ID)
	if err := newReviewFormService(a).beginReviewForm(msg, reviewFormModeCommit); err != nil {
		t.Fatalf("beginReviewForm(commit) error = %v", err)
	}
	commitPending := singleReviewPendingRequest(t, a)
	resp, err = newReviewFormService(a).completeReviewCommitSelect(&feishu.CardAction{
		UserID:      msg.UserID,
		ActionValue: map[string]any{"request_id": commitPending.ID},
		Option:      commits[0],
	})
	if err != nil {
		t.Fatalf("completeReviewCommitSelect() error = %v", err)
	}
	if resp == nil || resp.Card == nil {
		t.Fatalf("commit select response = %#v, want updated card", resp)
	}
	commitPayload := reviewPendingPayloadFromPending(a.store.PendingByID(commitPending.ID))
	if commitPayload.CommitSHA != commits[0] || commitPayload.CommitTitle != "initial" {
		t.Fatalf("commit payload after select = %+v, want initial commit payload", commitPayload)
	}
}
