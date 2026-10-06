package modelconfig

import (
	"feidex/internal/domain/conversation"
	domain "feidex/internal/domain/modelconfig"
	"feidex/internal/domain/routing"
	"feidex/internal/domain/submission"
)

// SourceRevision is read atomically by the configuration repository adapter.
type SourceRevision struct {
	Session *conversation.Session
	Binding *routing.AgentBinding
	Profile *routing.BotProfile
	Global  domain.GlobalValues
}

type SourceRepository interface {
	ModelSourceRevision(*conversation.Session) SourceRevision
}

type SnapshotService struct{ Repository SourceRepository }

type StatusView struct {
	Backend    string
	HasSession bool
	Status     Status
}

func (s SnapshotService) SessionView(backend string, sess *conversation.Session) StatusView {
	view := StatusView{Backend: backend, HasSession: sess != nil}
	if sess != nil {
		view.Status = SessionStatus(backend, sess.ActiveThreadID, s.TurnSnapshot(backend, sess), sess.AppliedModelConfig, sess.ModelConfigError)
	}
	return view
}

func (s SnapshotService) Desired(backend string, sess *conversation.Session) domain.Snapshot {
	return domain.Resolve(backend, sources(s.Repository.ModelSourceRevision(sess), false))
}

// DesiredTraced resolves desired settings and additionally reports the tier
// each value came from, for cards that annotate effective values.
func (s SnapshotService) DesiredTraced(backend string, sess *conversation.Session) (domain.Snapshot, domain.Origins) {
	return domain.ResolveTraced(backend, sources(s.Repository.ModelSourceRevision(sess), false))
}

func (s SnapshotService) Auxiliary(sess *conversation.Session) domain.GlobalValues {
	return domain.ResolveAuxiliary(sources(s.Repository.ModelSourceRevision(sess), false))
}

func (s SnapshotService) TurnSnapshot(backend string, sess *conversation.Session) domain.Snapshot {
	return domain.Resolve(backend, sources(s.Repository.ModelSourceRevision(sess), true))
}

func (s SnapshotService) SubmissionSnapshot(backend string, sess *conversation.Session, sub *submission.Submission) domain.Snapshot {
	if sub != nil && sub.BindingID != "" {
		sess = conversation.CloneSession(sess)
		if sess == nil {
			sess = &conversation.Session{}
		}
		sess.BindingID = sub.BindingID
	}
	return s.TurnSnapshot(backend, sess)
}

func sources(revision SourceRevision, includeActive bool) domain.Sources {
	result := domain.Sources{Global: revision.Global}
	if sess := revision.Session; sess != nil {
		result.Session = domain.SessionValues{Model: sess.ModelOverride, PlanModel: sess.PlanModelOverride,
			PlanEffort: sess.PlanReasoningEffortOverride, ReviewModel: sess.ReviewModelOverride,
			SubagentModel: sess.SubagentModelOverride, SubagentEffort: sess.SubagentReasoningEffortOverride,
			SmallModel: sess.SmallModelOverride}
		if active := sess.ActiveThreadCollaborationMode; includeActive && active != nil {
			result.Active = &domain.ActiveCollaboration{Mode: active.Mode, Model: active.Model, PresetEffort: active.PresetReasoningEffort}
		}
	}
	if binding := revision.Binding; binding != nil {
		result.Binding = domain.ScopeValues{Model: binding.ModelOverride, Effort: binding.ReasoningEffortOverride,
			PlanModel: binding.PlanModelOverride, PlanEffort: binding.PlanReasoningEffortOverride,
			ReviewModel: binding.ReviewModelOverride, SubagentModel: binding.SubagentModelOverride,
			SubagentEffort: binding.SubagentReasoningEffortOverride, SmallModel: binding.SmallModelOverride}
	}
	if profile := revision.Profile; profile != nil {
		result.Profile = domain.ProfileValues{Model: profile.Model, ClaudeModel: profile.ClaudeModel, Effort: profile.ReasoningEffort,
			PlanModel: profile.PlanModel, PlanEffort: profile.PlanReasoningEffort,
			ReviewModel: profile.ReviewModel, SubagentModel: profile.SubagentModel,
			SubagentEffort: profile.SubagentReasoningEffort, ClaudeSubagent: profile.ClaudeSubagentModel,
			ClaudeSmallModel: profile.ClaudeSmallModel}
	}
	return result
}
