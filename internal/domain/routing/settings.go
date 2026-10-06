// Package routing owns the conversation-scoped configuration types: the
// Setting vocabulary and the per-tier field mapping shared by every read and
// write path. Adding a new tiered setting means adding it to the Setting
// list and to the BindingField/ProfileField/GlobalField tables (plus the
// session table in domain/conversation when the session tier persists it);
// the write services and card annotations consume the tables.
package routing

import (
	"fmt"
	"strings"

	"feidex/internal/domain/modelconfig"
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

// OverrideSettings lists every setting persisted as a string override on the
// binding/profile/session tiers. Normalization and bulk reads iterate this
// list instead of hand-maintaining per-field lines.
func OverrideSettings() []Setting {
	return []Setting{Model, Effort, PlanModel, PlanEffort, ReviewModel, SubagentModel, SubagentEffort, SmallModel, ServiceTier, Workspace, Sandbox, ApprovalPolicy, MultiAgent, Permissions}
}

// BindingField returns a pointer to the AgentBinding override field that
// stores the desired value of a setting, or nil when bindings do not persist
// the setting.
func BindingField(binding *AgentBinding, setting Setting) *string {
	if binding == nil {
		return nil
	}
	switch setting {
	case Model:
		return &binding.ModelOverride
	case Effort:
		return &binding.ReasoningEffortOverride
	case PlanModel:
		return &binding.PlanModelOverride
	case PlanEffort:
		return &binding.PlanReasoningEffortOverride
	case ReviewModel:
		return &binding.ReviewModelOverride
	case SubagentModel:
		return &binding.SubagentModelOverride
	case SubagentEffort:
		return &binding.SubagentReasoningEffortOverride
	case SmallModel:
		return &binding.SmallModelOverride
	case ServiceTier:
		return &binding.ServiceTierOverride
	case Workspace:
		return &binding.WorkspaceID
	case Sandbox:
		return &binding.SandboxModeOverride
	case ApprovalPolicy:
		return &binding.ApprovalPolicyOverride
	case MultiAgent:
		return &binding.MultiAgentModeOverride
	case Permissions:
		return &binding.ClaudePermissionMode
	}
	return nil
}

// BindingValue reads the stored override of a setting from a binding.
func BindingValue(binding *AgentBinding, setting Setting) string {
	if field := BindingField(binding, setting); field != nil {
		return *field
	}
	return ""
}

// ProfileField returns a pointer to the BotProfile field that stores the
// desired value of a setting, selecting backend-specific fields where the two
// backends differ. A nil result means the backend does not support the
// setting on the profile tier; writers must surface that as an error instead
// of silently dropping the value.
func ProfileField(profile *BotProfile, setting Setting, backend string) *string {
	if profile == nil {
		return nil
	}
	switch setting {
	case Model:
		if backend == modelconfig.BackendClaude {
			return &profile.ClaudeModel
		}
		return &profile.Model
	case Effort:
		return &profile.ReasoningEffort
	case PlanModel:
		if backend == modelconfig.BackendClaude {
			return nil
		}
		return &profile.PlanModel
	case PlanEffort:
		if backend == modelconfig.BackendClaude {
			return nil
		}
		return &profile.PlanReasoningEffort
	case ReviewModel:
		if backend == modelconfig.BackendClaude {
			return nil
		}
		return &profile.ReviewModel
	case SubagentModel:
		if backend == modelconfig.BackendClaude {
			return &profile.ClaudeSubagentModel
		}
		return &profile.SubagentModel
	case SubagentEffort:
		if backend == modelconfig.BackendClaude {
			return nil
		}
		return &profile.SubagentReasoningEffort
	case SmallModel:
		if backend == modelconfig.BackendClaude {
			return &profile.ClaudeSmallModel
		}
		return nil
	case ServiceTier:
		return &profile.ServiceTier
	case Workspace:
		return &profile.WorkspaceID
	case Sandbox:
		return &profile.SandboxMode
	case ApprovalPolicy:
		return &profile.ApprovalPolicy
	case MultiAgent:
		return &profile.MultiAgentMode
	case Permissions:
		return &profile.ClaudePermissionMode
	}
	return nil
}

// ProfileValue reads the stored value of a setting from a profile.
func ProfileValue(profile *BotProfile, backend string, setting Setting) string {
	if field := ProfileField(profile, setting, backend); field != nil {
		return *field
	}
	return ""
}

// GlobalField returns a pointer to the backend-global value of a setting, or
// nil when the backend does not support the setting globally.
func GlobalField(values *modelconfig.GlobalValues, setting Setting, backend string) *string {
	if values == nil {
		return nil
	}
	switch setting {
	case Model:
		if backend == modelconfig.BackendClaude {
			return &values.ClaudeModel
		}
		return &values.Model
	case Effort:
		if backend == modelconfig.BackendClaude {
			return &values.ClaudeEffort
		}
		return &values.Effort
	case PlanModel:
		if backend == modelconfig.BackendClaude {
			return nil
		}
		return &values.PlanModel
	case PlanEffort:
		if backend == modelconfig.BackendClaude {
			return nil
		}
		return &values.PlanEffort
	case ReviewModel:
		if backend == modelconfig.BackendClaude {
			return nil
		}
		return &values.ReviewModel
	case SubagentModel:
		if backend == modelconfig.BackendClaude {
			return &values.ClaudeSubagent
		}
		return &values.SubagentModel
	case SubagentEffort:
		if backend == modelconfig.BackendClaude {
			return nil
		}
		return &values.SubagentEffort
	case SmallModel:
		if backend == modelconfig.BackendClaude {
			return &values.ClaudeSmallModel
		}
		return nil
	}
	return nil
}

// SetBinding changes desired settings only; runtime thread state is separate.
func SetBinding(binding *AgentBinding, setting Setting, value string) error {
	field := BindingField(binding, setting)
	if field == nil {
		return fmt.Errorf("unknown binding setting %q", setting)
	}
	*field = value
	return nil
}

// SetProfile writes a desired profile setting. Unsupported backend/setting
// combinations are rejected so a write is never dropped silently.
func SetProfile(profile *BotProfile, backend string, setting Setting, value string) error {
	field := ProfileField(profile, setting, backend)
	if field == nil {
		return fmt.Errorf("unsupported %s profile setting %q", backend, setting)
	}
	*field = value
	return nil
}
