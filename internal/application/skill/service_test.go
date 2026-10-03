package skill

import (
	"context"
	"errors"
	"testing"

	"feidex/internal/domain/identity"
	catalog "feidex/internal/domain/skill"
	"feidex/internal/domain/submission"
	"feidex/internal/domain/workspace"
)

type testWorkspaceSource struct{ source WorkspaceSource }

func (r testWorkspaceSource) SkillWorkspaceSource(string) WorkspaceSource { return r.source }

type testPending map[string]submission.SubmissionSkill

func (r testPending) Get(key string) (submission.SubmissionSkill, bool) {
	v, ok := r[key]
	return v, ok
}
func (r testPending) Set(key string, v submission.SubmissionSkill) { r[key] = v }
func (r testPending) Clear(key string)                             { delete(r, key) }

type testCatalog struct {
	calls  int
	cwd    string
	reload bool
	err    error
	onList func()
}

func (c *testCatalog) ListSkills(ctx context.Context, cwd string, reload bool) (catalog.SkillsListEntry, error) {
	c.calls++
	c.cwd, c.reload = cwd, reload
	if c.onList != nil {
		c.onList()
	}
	return catalog.SkillsListEntry{Cwd: cwd, Skills: []catalog.SkillMetadata{
		{Name: "enabled", Path: "/skills/enabled", Enabled: true},
		{Name: "disabled", Path: "/skills/disabled"},
	}}, c.err
}

func testService() (*Service, *testCatalog, testPending) {
	c, pending := &testCatalog{}, testPending{}
	s := &Service{Deps: Dependencies{
		Frontend: identity.FrontendID("bot-a"), Catalog: c, Pending: pending,
		Workspaces: testWorkspaceSource{WorkspaceSource{
			WorkspaceID: "session", Workspaces: []workspace.Workspace{
				{ID: "default", Cwd: "/default"}, {ID: "session", Cwd: "/session"},
			},
		}},
	}}
	return s, c, pending
}

func TestCatalogScopeReloadAndDisabledSelection(t *testing.T) {
	s, c, pending := testService()
	key := "feishu:frontend:bot-a:chat:chat"
	if _, err := s.Snapshot(context.Background(), key, true); err != nil || c.cwd != "/session" || !c.reload {
		t.Fatalf("snapshot: cwd=%q reload=%v err=%v", c.cwd, c.reload, err)
	}
	if _, err := s.Select(context.Background(), key, "/skills/disabled"); !errors.Is(err, ErrDisabled) || len(pending) != 0 {
		t.Fatalf("disabled selection: pending=%v err=%v", pending, err)
	}
	if selected, err := s.Select(context.Background(), key, "enabled"); err != nil || selected.Path != "/skills/enabled" {
		t.Fatalf("selection=%v err=%v", selected, err)
	}
	before := c.calls
	foreign := "feishu:frontend:bot-b:chat:chat"
	if _, err := s.Snapshot(context.Background(), foreign, false); err == nil || c.calls != before {
		t.Fatal("foreign frontend reached the catalog")
	}
	s.SetSessionPendingSkill(foreign, submission.SubmissionSkill{Name: "bad", Path: "/bad"})
	if _, ok := pending[foreign]; ok {
		t.Fatal("foreign frontend modified pending state")
	}
}

func TestSubmissionResolutionPreservesPendingUntilPersistence(t *testing.T) {
	for _, tc := range []struct {
		name, text, wantText, wantSkill string
		attachments                     []submission.SubmissionAttachment
		wantCalls                       int
		wantReplacement                 bool
	}{
		{name: "pending", text: "hello", wantText: "hello", wantSkill: "old"},
		{name: "explicit", text: "$enabled hello", wantText: "hello", wantSkill: "enabled", wantCalls: 1},
		{name: "invalid", text: "$bad/name hello", wantText: "$bad/name hello"},
		{name: "unknown", text: "$unknown hello", wantText: "$unknown hello", wantCalls: 1},
		{name: "disabled", text: "$disabled hello", wantText: "$disabled hello", wantCalls: 1},
		{name: "selection only", text: "$enabled", wantReplacement: true, wantCalls: 1},
		{name: "selection with attachment", text: "$enabled", attachments: []submission.SubmissionAttachment{{Kind: "image"}}, wantSkill: "enabled", wantCalls: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, c, pending := testService()
			pending["key"] = submission.SubmissionSkill{Name: "old", Path: "/old"}
			result := s.ResolveSubmissionSkill("key", "default", tc.text, tc.attachments)
			gotSkill := ""
			if len(result.Skills) > 0 {
				gotSkill = result.Skills[0].Name
			}
			if result.InputText != tc.wantText || gotSkill != tc.wantSkill || (result.PendingReplacement != nil) != tc.wantReplacement || !result.ConsumePending {
				t.Fatalf("resolution=%+v", result)
			}
			if c.calls != tc.wantCalls || (c.calls > 0 && c.cwd != "/default") {
				t.Fatalf("catalog calls=%d cwd=%q", c.calls, c.cwd)
			}
			if pending["key"].Name != "old" {
				t.Fatal("resolution consumed pending before submission persistence")
			}
		})
	}
}

func TestCancelledCatalogCannotPublishSelection(t *testing.T) {
	s, c, pending := testService()
	ctx, cancel := context.WithCancel(context.Background())
	c.onList = cancel
	if _, err := s.Select(ctx, "key", "enabled"); !errors.Is(err, context.Canceled) || len(pending) != 0 {
		t.Fatalf("selection after cancellation: pending=%v err=%v", pending, err)
	}
	before := c.calls
	if _, err := s.Snapshot(ctx, "key", false); !errors.Is(err, context.Canceled) || c.calls != before {
		t.Fatal("cancelled query reached external catalog")
	}
}
