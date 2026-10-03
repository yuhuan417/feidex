package threadsettings

import (
	"context"
	"errors"
	"testing"

	"feidex/internal/domain/conversation"
)

type permissionFixture struct {
	sess              *conversation.Session
	job               func()
	allow, admit      bool
	saveErr, applyErr error
	applied           []string
	failures          int
}

func (f *permissionFixture) Session(string) *conversation.Session {
	return conversation.CloneSession(f.sess)
}
func (f *permissionFixture) UpdateSession(_ string, mutate func(*conversation.Session)) (*conversation.Session, error) {
	if f.saveErr != nil {
		return nil, f.saveErr
	}
	next := conversation.CloneSession(f.sess)
	mutate(next)
	f.sess = next
	return f.Session(""), nil
}
func (f *permissionFixture) PermissionRevision(string) PermissionRevision {
	return PermissionRevision{Session: f.Session(""), AllowBypass: f.allow}
}
func (f *permissionFixture) ApplyPermission(ctx context.Context, key, mode string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f.applied = append(f.applied, mode)
	return f.applyErr
}
func (f *permissionFixture) Run(_ string, job func()) bool {
	if f.admit {
		f.job = job
	}
	return f.admit
}
func (f *permissionFixture) PermissionFailed(string, string, error) { f.failures++ }
func permissionService(f *permissionFixture) PermissionService {
	return PermissionService{Settings: Service{Repository: f}, Source: f, Runtime: f, Tasks: f, Failure: f, Context: context.Background}
}

func TestPermissionApplyConvergesOnLatestSettingsAndIgnoresReplacementThread(t *testing.T) {
	f := &permissionFixture{sess: &conversation.Session{Key: "session", ActiveThreadID: "thread"}, admit: true}
	s := permissionService(f)
	if _, err := s.Set("session", "thread", "acceptEdits", "card", true); err != nil {
		t.Fatal(err)
	}
	first := f.job
	if _, err := s.Set("session", "thread", "default", "card", true); err != nil {
		t.Fatal(err)
	}
	first()
	f.job()
	if len(f.applied) != 2 || f.applied[0] != "default" || f.applied[1] != "default" {
		t.Fatalf("applied stale value: %v", f.applied)
	}
	if _, err := s.Set("session", "thread", "acceptEdits", "card", true); err != nil {
		t.Fatal(err)
	}
	f.sess.ActiveThreadID = "replacement"
	f.job()
	if len(f.applied) != 2 {
		t.Fatal("old request applied to replacement thread")
	}
}

func TestPermissionFailureCannotApplyUnsavedOrForbiddenMode(t *testing.T) {
	f := &permissionFixture{sess: &conversation.Session{Key: "session", ActiveThreadID: "thread"}, admit: true}
	s := permissionService(f)
	if _, err := s.Set("session", "thread", "bypassPermissions", "card", false); err == nil {
		t.Fatal("bypass granted without configuration")
	}
	f.saveErr = errors.New("disk failure")
	if _, err := s.Set("session", "thread", "acceptEdits", "card", false); err == nil {
		t.Fatal("save failure ignored")
	}
	if len(f.applied) != 0 || f.job != nil {
		t.Fatal("failed save applied runtime setting")
	}
	f.saveErr = nil
	f.admit = false
	if _, err := s.Set("session", "thread", "acceptEdits", "card", true); err != nil {
		t.Fatal(err)
	}
	if f.failures != 1 || f.sess.ActiveClaudePermissionMode != "acceptEdits" {
		t.Fatal("rejected task did not report saved-but-unapplied setting")
	}
}
