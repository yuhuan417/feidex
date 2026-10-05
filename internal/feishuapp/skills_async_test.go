package feishuapp

import (
	"context"
	skillcatalog "feidex/internal/domain/skill"
	"feidex/internal/feishu"
	"testing"
)

func TestSkillsCallbacksAckBeforeReadingCatalog(t *testing.T) {
	for _, name := range []string{"menu.skills", "skills.reload", "skills.select"} {
		t.Run(name, func(t *testing.T) {
			a, ff, fc := newTestApp(t)
			var admitted func()
			a.asyncRunner = func(work func()) { admitted = work }
			reads := 0
			fc.callHook = func(_ context.Context, method string, _ any, out any) error {
				if method != "skills/list" {
					t.Fatalf("unexpected method %q", method)
				}
				reads++
				out.(*skillcatalog.SkillsListResult).Data = []skillcatalog.SkillsListEntry{testSkillsListEntry(a.cfg.Workspaces[0].Cwd,
					skillcatalog.SkillMetadata{Name: "selected", Path: "/selected", Enabled: true})}
				return nil
			}
			action := &feishu.CardAction{MessageID: "card", Option: "/selected", ActionValue: map[string]any{"action": name, "session_key": "session"}}
			response, err := dispatchCardAction(a.BackendRuntimeDeps(), a.FrontendID(), action)
			if err != nil || response == nil || response.Toast == nil || response.Toast.Type != "info" || admitted == nil || reads != 0 {
				t.Fatalf("callback did not ack before catalog: response=%v err=%v reads=%d", response, err, reads)
			}
			admitted()
			if reads == 0 || len(ff.patchedCards) != 1 {
				t.Fatalf("async completion: reads=%d patches=%d", reads, len(ff.patchedCards))
			}
			if name == "skills.select" {
				if selected, ok := a.bindings.Skills.SessionPendingSkill("session"); !ok || selected.Name != "selected" {
					t.Fatalf("async selection was not stored: %+v %v", selected, ok)
				}
			}
		})
	}
}

func TestSkillsCallbackRejectedAfterShutdown(t *testing.T) {
	a, ff, fc := newTestApp(t)
	runtimeViewOf(a.runtimeOwner).ensureRuntimeOwner().Lifecycle.Cancel()
	fc.callHook = func(context.Context, string, any, any) error {
		t.Fatal("stopping frontend must not read catalog")
		return nil
	}
	response, err := dispatchCardAction(a.BackendRuntimeDeps(), a.FrontendID(), &feishu.CardAction{MessageID: "card", ActionValue: map[string]any{"action": "skills.reload", "session_key": "session"}})
	if err != nil || response == nil || response.Toast == nil || response.Toast.Type != "warning" || len(ff.patchedCards) != 0 {
		t.Fatalf("shutdown callback: response=%v err=%v", response, err)
	}
}
