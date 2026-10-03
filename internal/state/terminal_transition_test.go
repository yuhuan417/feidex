package state

import (
	"feidex/internal/domain/conversation"
	"feidex/internal/domain/interaction"
	"feidex/internal/domain/submission"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func terminalFixture(t *testing.T) (*Store, *conversation.Session, *submission.Submission, *interaction.PendingRequest) {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	key := "feishu:frontend:a:chat:chat"
	if err := s.UpsertSession(&conversation.Session{Key: key, WorkspaceID: "ws", Status: "turn_in_progress", ActiveThreadID: "thread", ActiveThreadWorkspaceID: "ws", ActiveTurnID: "turn"}); err != nil {
		t.Fatal(err)
	}
	id, err := s.CreateSubmission(&submission.Submission{ID: "sub", SessionKey: key, Status: "running", ThreadID: "thread", TurnID: "turn"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertPending(&interaction.PendingRequest{ID: "request", FrontendID: "a", SessionKey: key, Status: "pending", Kind: "command", ThreadID: "thread", TurnID: "turn"}); err != nil {
		t.Fatal(err)
	}
	return s, s.GetSession(key), s.GetSubmission(id), s.PendingByScopedID("a", "request")
}

func TestCommitTerminalSaveFailureRollsBackEveryOwner(t *testing.T) {
	s, sess, sub, req := terminalFixture(t)
	nextSession := conversation.CloneSession(sess)
	conversation.ResetActiveOperations(nextSession)
	nextSession.Status = "idle"
	nextSub, nextRequest := *sub, *req
	nextSub.Finalize("failed")
	nextRequest.Status = "resolved"
	blocked := filepath.Join(t.TempDir(), "blocked")
	if err := os.Mkdir(blocked, 0o755); err != nil {
		t.Fatal(err)
	}
	s.path = blocked
	if err := s.CommitTerminal(sess, nextSession, sub, &nextSub, []*interaction.PendingRequest{req}, []*interaction.PendingRequest{&nextRequest}); err == nil {
		t.Fatal("expected disk error")
	}
	if !reflect.DeepEqual(sess, s.GetSession(sess.Key)) || !reflect.DeepEqual(sub, s.GetSubmission(sub.ID)) || !reflect.DeepEqual(req, s.PendingByScopedID("a", req.ID)) {
		t.Fatal("failed commit published partial state")
	}
}

func TestCommitTerminalRejectsInteractionAddedAfterRead(t *testing.T) {
	s, sess, sub, req := terminalFixture(t)
	if err := s.UpsertPending(&interaction.PendingRequest{ID: "late", FrontendID: "a", SessionKey: sess.Key, Kind: "command", Status: "pending"}); err != nil {
		t.Fatal(err)
	}
	next := conversation.CloneSession(sess)
	next.Status = "idle"
	if err := s.CommitTerminal(sess, next, sub, sub, []*interaction.PendingRequest{req}, nil); err == nil {
		t.Fatal("accepted stale interaction read set")
	}
	if s.GetSession(sess.Key).Status != sess.Status {
		t.Fatal("conflict changed session")
	}
}

func TestCommitTerminalPreservesOtherFrontend(t *testing.T) {
	s, sess, sub, req := terminalFixture(t)
	foreign := &interaction.PendingRequest{ID: req.ID, FrontendID: "b", SessionKey: "feishu:frontend:b:chat:chat", Kind: "command", Status: "pending"}
	if err := s.UpsertPending(foreign); err != nil {
		t.Fatal(err)
	}
	before := s.PendingByScopedID("b", req.ID)
	nextSession, nextSub, nextRequest := conversation.CloneSession(sess), *sub, *req
	nextSession.Status, nextRequest.Status = "idle", "resolved"
	nextSub.Finalize("failed")
	if err := s.CommitTerminal(sess, nextSession, sub, &nextSub, []*interaction.PendingRequest{req}, []*interaction.PendingRequest{&nextRequest}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, s.PendingByScopedID("b", req.ID)) {
		t.Fatal("changed another frontend")
	}
}
