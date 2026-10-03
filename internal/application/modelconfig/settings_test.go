package modelconfig

import (
	"errors"
	"reflect"
	"testing"

	applicationrouting "feidex/internal/application/routing"
	"feidex/internal/domain/conversation"
	"feidex/internal/domain/identity"
	domain "feidex/internal/domain/modelconfig"
	"feidex/internal/domain/routing"
)

type settingsRepository struct {
	session *conversation.Session
	profile *routing.BotProfile
	failure error
	writes  int
}

func (r *settingsRepository) Session(key string) *conversation.Session {
	if r.session != nil && r.session.Key == key {
		return conversation.CloneSession(r.session)
	}
	return nil
}
func (r *settingsRepository) UpdateSession(key string, mutate func(*conversation.Session)) (*conversation.Session, error) {
	r.writes++
	if r.failure != nil {
		return nil, r.failure
	}
	next := r.Session(key)
	mutate(next)
	r.session = next
	return r.Session(key), nil
}
func (r *settingsRepository) BotProfile() *routing.BotProfile { return r.profile }
func (r *settingsRepository) UpdateBotProfile(fallback *routing.BotProfile, mutate func(*routing.BotProfile)) (*routing.BotProfile, error) {
	r.writes++
	if r.failure != nil {
		return nil, r.failure
	}
	next := *fallback
	if r.profile != nil {
		next = *r.profile
	}
	if mutate != nil {
		mutate(&next)
	}
	r.profile = &next
	return r.profile, nil
}
func (*settingsRepository) AgentBinding(string) *routing.AgentBinding                   { return nil }
func (*settingsRepository) AgentBindingsForChat(string, string) []*routing.AgentBinding { return nil }
func (*settingsRepository) UpdateAgentBinding(*routing.AgentBinding, func(*routing.AgentBinding)) (*routing.AgentBinding, error) {
	panic("auxiliary profile/session save must not update a binding")
}

type deniedSettings string

func (d deniedSettings) ModelConfigBlockedReason() string { return string(d) }

func TestAuxiliarySaveKeepsActiveTurnAndAppliedSnapshot(t *testing.T) {
	original := &conversation.Session{Key: "session", ActiveThreadID: "thread", ActiveTurnID: "turn", ActiveSubmissionID: "submission",
		Queue: []string{"next"}, ModelOverride: "main", Status: "running", ModelConfigError: "previous apply failure",
		AppliedModelConfig:            domain.Snapshot{Valid: true, Backend: "codex", Model: "old", PlanModel: "old-plan"},
		ActiveThreadCollaborationMode: &conversation.SessionCollaborationMode{Mode: "plan", Model: "old-plan"}}
	repo := &settingsRepository{session: conversation.CloneSession(original), profile: &routing.BotProfile{PlanModel: "profile-plan"}}
	service := SettingsService{Repository: repo, Frontend: identity.FrontendID("bot")}
	result, err := service.SaveAuxiliary("session", "codex", routing.PlanModel, " new-plan ")
	if err != nil || result.Scope != SessionScope {
		t.Fatalf("save = %+v, %v", result, err)
	}
	expected := conversation.CloneSession(original)
	expected.PlanModelOverride = "new-plan"
	if !reflect.DeepEqual(repo.session, expected) || repo.profile.PlanModel != "profile-plan" {
		t.Fatalf("save changed active state or profile: %+v, %+v", repo.session, repo.profile)
	}
	if _, err := service.SaveAuxiliary("session", "codex", routing.PlanModel, "default"); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(repo.session, original) {
		t.Fatalf("clear changed active state: %+v", repo.session)
	}
}

func TestAuxiliarySaveUsesBackendScopedProfileWhenSessionAbsent(t *testing.T) {
	repo := &settingsRepository{profile: &routing.BotProfile{Model: "codex-main", ClaudeModel: "claude-main", SubagentModel: "codex-child"}}
	service := SettingsService{Repository: repo, Frontend: identity.FrontendID("bot")}
	result, err := service.SaveAuxiliary("missing", "claude", routing.SubagentModel, "claude-child")
	if err != nil || result.Scope != ProfileScope || repo.profile.ClaudeSubagentModel != "claude-child" || repo.profile.SubagentModel != "codex-child" {
		t.Fatalf("save = %+v, %v, %+v", result, err, repo.profile)
	}
	if _, err := service.SaveAuxiliary("missing", "codex", routing.PlanEffort, "high"); err != nil || repo.profile.PlanReasoningEffort != "high" {
		t.Fatalf("plan effort: %+v, %v", repo.profile, err)
	}
}

func TestAuxiliarySaveDenialAndPersistenceFailureDoNotPublish(t *testing.T) {
	original := &conversation.Session{Key: "session", PlanModelOverride: "old"}
	repo := &settingsRepository{session: conversation.CloneSession(original)}
	service := SettingsService{Repository: repo, Admission: deniedSettings("maintenance")}
	if _, err := service.SaveAuxiliary("session", "codex", routing.PlanModel, "new"); err == nil || repo.writes != 0 {
		t.Fatalf("denied save: %v, writes=%d", err, repo.writes)
	}
	service.Admission = nil
	repo.failure = errors.New("disk full")
	if _, err := service.SaveAuxiliary("session", "codex", routing.PlanModel, "new"); !errors.Is(err, repo.failure) || !reflect.DeepEqual(repo.session, original) {
		t.Fatalf("failed save published: %+v, %v", repo.session, err)
	}
	if _, err := service.SaveAuxiliary("session", "codex", routing.Model, "new"); err == nil || repo.writes != 1 {
		t.Fatalf("invalid setting wrote: %v, writes=%d", err, repo.writes)
	}
}

func TestProfileWorkspaceDefaultIsAnID(t *testing.T) {
	repo := &settingsRepository{}
	profile, err := (applicationrouting.ConfigurationService{Repository: repo, Frontend: identity.FrontendID("bot")}).SetProfile("codex", routing.Workspace, " default ")
	if err != nil || profile.WorkspaceID != "default" {
		t.Fatalf("workspace profile = %+v, %v", profile, err)
	}
}
