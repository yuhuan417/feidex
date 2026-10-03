package routing

import (
	"fmt"
	"strings"
)

type Setting string

const (
	Model          Setting = "model"
	Effort         Setting = "effort"
	PlanModel      Setting = "plan"
	PlanEffort     Setting = "plan_effort"
	ReviewModel    Setting = "review"
	SubagentModel  Setting = "subagent"
	SubagentEffort Setting = "subagent_effort"
	SmallModel     Setting = "small"
	ServiceTier    Setting = "fast"
	Workspace      Setting = "workspace"
	Sandbox        Setting = "sandbox"
	ApprovalPolicy Setting = "policy"
	MultiAgent     Setting = "multiagent"
	Permissions    Setting = "permissions"
)

func ClearableValue(value string) string {
	value = strings.TrimSpace(value)
	switch strings.ToLower(value) {
	case "", "default", "inherit", "follow", "clear", "unset":
		return ""
	default:
		return value
	}
}

func (s Setting) Auxiliary() bool {
	switch s {
	case PlanModel, PlanEffort, ReviewModel, SubagentModel, SubagentEffort, SmallModel:
		return true
	}
	return false
}

// SetBinding changes desired settings only; runtime thread state is separate.
func SetBinding(binding *AgentBinding, setting Setting, value string) error {
	switch setting {
	case Model:
		binding.ModelOverride = value
	case Effort:
		binding.ReasoningEffortOverride = value
	case PlanModel:
		binding.PlanModelOverride = value
	case PlanEffort:
		binding.PlanReasoningEffortOverride = value
	case ReviewModel:
		binding.ReviewModelOverride = value
	case SubagentModel:
		binding.SubagentModelOverride = value
	case SubagentEffort:
		binding.SubagentReasoningEffortOverride = value
	case SmallModel:
		binding.SmallModelOverride = value
	case ServiceTier:
		binding.ServiceTierOverride = value
	case Workspace:
		binding.WorkspaceID = value
	case Sandbox:
		binding.SandboxModeOverride = value
	case ApprovalPolicy:
		binding.ApprovalPolicyOverride = value
	case MultiAgent:
		binding.MultiAgentModeOverride = value
	case Permissions:
		binding.ClaudePermissionMode = value
	default:
		return fmt.Errorf("unknown binding setting %q", setting)
	}
	return nil
}

func SetProfile(profile *BotProfile, backend string, setting Setting, value string) error {
	switch setting {
	case Model:
		if backend == "claude" {
			profile.ClaudeModel = value
		} else {
			profile.Model = value
		}
	case Effort:
		profile.ReasoningEffort = value
	case PlanModel:
		if backend != "claude" {
			profile.PlanModel = value
		}
	case PlanEffort:
		if backend != "claude" {
			profile.PlanReasoningEffort = value
		}
	case ReviewModel:
		if backend != "claude" {
			profile.ReviewModel = value
		}
	case SubagentModel:
		if backend == "claude" {
			profile.ClaudeSubagentModel = value
		} else {
			profile.SubagentModel = value
		}
	case SubagentEffort:
		if backend != "claude" {
			profile.SubagentReasoningEffort = value
		}
	case SmallModel:
		if backend == "claude" {
			profile.ClaudeSmallModel = value
		}
	case ServiceTier:
		profile.ServiceTier = value
	case Workspace:
		profile.WorkspaceID = value
	case Sandbox:
		profile.SandboxMode = value
	case ApprovalPolicy:
		profile.ApprovalPolicy = value
	case MultiAgent:
		profile.MultiAgentMode = value
	case Permissions:
		profile.ClaudePermissionMode = value
	default:
		return fmt.Errorf("unknown profile setting %q", setting)
	}
	return nil
}
