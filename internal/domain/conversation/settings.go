package conversation

import (
	"feidex/internal/domain/routing"
	"feidex/internal/domain/workspace"
	"feidex/internal/textutil"
	"strings"
)

type Settings struct {
	ApprovalPolicy, SandboxMode, ServiceTier, MultiAgentMode, ClaudePermissionMode string
}

func ResolveSettings(sess *Session, binding *routing.AgentBinding, profile *routing.BotProfile, ws *workspace.Workspace, claudePermission string) Settings {
	var active, bound, defaults, work Settings
	if sess != nil {
		active = Settings{ApprovalPolicy: sess.ActiveThreadApprovalPolicy, SandboxMode: sess.ActiveThreadSandboxMode, ServiceTier: EffectiveServiceTier(sess), MultiAgentMode: sess.ActiveThreadMultiAgentMode, ClaudePermissionMode: sess.ActiveClaudePermissionMode}
	}
	if binding != nil {
		bound = Settings{ApprovalPolicy: binding.ApprovalPolicyOverride, SandboxMode: binding.SandboxModeOverride, ServiceTier: binding.ServiceTierOverride, MultiAgentMode: binding.MultiAgentModeOverride, ClaudePermissionMode: binding.ClaudePermissionMode}
	}
	if profile != nil {
		defaults = Settings{ApprovalPolicy: profile.ApprovalPolicy, SandboxMode: profile.SandboxMode, ServiceTier: profile.ServiceTier, MultiAgentMode: profile.MultiAgentMode, ClaudePermissionMode: profile.ClaudePermissionMode}
	}
	if ws != nil {
		work = Settings{ApprovalPolicy: ws.ApprovalPolicy, SandboxMode: ws.SandboxMode, MultiAgentMode: ws.MultiAgentMode, ClaudePermissionMode: ws.ClaudePermissionMode}
	}
	return Settings{
		ApprovalPolicy:       firstSetting(active.ApprovalPolicy, bound.ApprovalPolicy, defaults.ApprovalPolicy, EffectiveApprovalPolicy(sess, work.ApprovalPolicy)),
		SandboxMode:          firstSetting(active.SandboxMode, bound.SandboxMode, defaults.SandboxMode, EffectiveSandboxMode(sess, work.SandboxMode)),
		ServiceTier:          firstSetting(active.ServiceTier, bound.ServiceTier, defaults.ServiceTier),
		MultiAgentMode:       firstSetting(active.MultiAgentMode, bound.MultiAgentMode, defaults.MultiAgentMode, EffectiveMultiAgentMode(sess, work.MultiAgentMode)),
		ClaudePermissionMode: NormalizeClaudePermissionMode(firstSetting(active.ClaudePermissionMode, bound.ClaudePermissionMode, defaults.ClaudePermissionMode, work.ClaudePermissionMode, claudePermission)),
	}
}

// SessionField returns a pointer to the session override field that stores
// the desired value of a setting, or nil when the session tier does not
// persist the setting.
func SessionField(sess *Session, setting routing.Setting) *string {
	if sess == nil {
		return nil
	}
	switch setting {
	case routing.Model:
		return &sess.ModelOverride
	case routing.PlanModel:
		return &sess.PlanModelOverride
	case routing.PlanEffort:
		return &sess.PlanReasoningEffortOverride
	case routing.ReviewModel:
		return &sess.ReviewModelOverride
	case routing.SubagentModel:
		return &sess.SubagentModelOverride
	case routing.SubagentEffort:
		return &sess.SubagentReasoningEffortOverride
	case routing.SmallModel:
		return &sess.SmallModelOverride
	}
	return nil
}

func NormalizeClaudePermissionMode(value string) string {
	if value = strings.TrimSpace(value); value == "" {
		return "default"
	}
	return value
}

func firstSetting(values ...string) string {
	return textutil.FirstNonEmpty(values...)
}
