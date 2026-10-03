package conversation

import (
	"feidex/internal/domain/routing"
	"feidex/internal/domain/workspace"
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

func NormalizeClaudePermissionMode(value string) string {
	if value = strings.TrimSpace(value); value == "" {
		return "default"
	}
	return value
}

func firstSetting(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}
