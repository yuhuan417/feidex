// Package conversation owns session context, backend lineage and active operations.
package conversation

import domainmodelconfig "feidex/internal/domain/modelconfig"

type SessionBackendThread struct {
	ThreadID             string                    `json:"thread_id,omitempty"`
	WorkspaceID          string                    `json:"workspace_id,omitempty"`
	ApprovalPolicy       string                    `json:"approval_policy,omitempty"`
	SandboxMode          string                    `json:"sandbox_mode,omitempty"`
	MultiAgentMode       string                    `json:"multi_agent_mode,omitempty"`
	ClaudePermissionMode string                    `json:"claude_permission_mode,omitempty"`
	ServiceTier          string                    `json:"service_tier,omitempty"`
	CollaborationMode    *SessionCollaborationMode `json:"collaboration_mode,omitempty"`
	Name                 string                    `json:"name,omitempty"`
	Preview              string                    `json:"preview,omitempty"`
}

type SessionCollaborationMode struct {
	PresetReasoningEffort string  `json:"preset_reasoning_effort,omitempty"`
	Mode                  string  `json:"mode"`
	Model                 string  `json:"model"`
	ReasoningEffort       string  `json:"reasoning_effort,omitempty"`
	DeveloperInstructions *string `json:"developer_instructions"`
}

type Session struct {
	AppliedModelConfig              domainmodelconfig.Snapshot      `json:"applied_model_config,omitempty"`
	ModelConfigError                string                          `json:"model_config_error,omitempty"`
	Key                             string                          `json:"key"`
	BindingID                       string                          `json:"binding_id,omitempty"`
	WorkspaceID                     string                          `json:"workspace_id"`
	ActiveThreadID                  string                          `json:"active_thread_id"`
	ActiveThreadWorkspaceID         string                          `json:"active_thread_workspace_id"`
	ActiveThreadApprovalPolicy      string                          `json:"active_thread_approval_policy"`
	ActiveThreadSandboxMode         string                          `json:"active_thread_sandbox_mode"`
	ActiveThreadMultiAgentMode      string                          `json:"active_thread_multi_agent_mode,omitempty"`
	ActiveClaudePermissionMode      string                          `json:"active_claude_permission_mode,omitempty"`
	ActiveThreadServiceTier         string                          `json:"active_thread_service_tier,omitempty"`
	ActiveThreadCollaborationMode   *SessionCollaborationMode       `json:"active_thread_collaboration_mode,omitempty"`
	ActiveThreadName                string                          `json:"active_thread_name"`
	ActiveThreadPreview             string                          `json:"active_thread_preview"`
	BackendThreads                  map[string]SessionBackendThread `json:"backend_threads,omitempty"`
	ActiveTurnID                    string                          `json:"active_turn_id"`
	ActiveSubmissionID              string                          `json:"active_submission_id"`
	OwnerUserID                     string                          `json:"owner_user_id"`
	ChatID                          string                          `json:"chat_id"`
	ChatType                        string                          `json:"chat_type"`
	RootMessageID                   string                          `json:"root_message_id"`
	ModelOverride                   string                          `json:"model_override"`
	PlanModelOverride               string                          `json:"plan_model_override,omitempty"`
	PlanReasoningEffortOverride     string                          `json:"plan_reasoning_effort_override,omitempty"`
	ReviewModelOverride             string                          `json:"review_model_override,omitempty"`
	SubagentModelOverride           string                          `json:"subagent_model_override,omitempty"`
	SubagentReasoningEffortOverride string                          `json:"subagent_reasoning_effort_override,omitempty"`
	SmallModelOverride              string                          `json:"small_model_override,omitempty"`
	Status                          string                          `json:"status"`
	Queue                           []string                        `json:"queue"`
	ActiveOperations                []SessionActiveOperation        `json:"active_operations,omitempty"`
	StagedImages                    []SessionStagedImage            `json:"staged_images,omitempty"`
	RecentWorkspaceIDs              []string                        `json:"recent_workspace_ids,omitempty"`
	UpdatedAt                       int64                           `json:"updated_at"`
}

type SessionActiveOperation struct {
	Kind         string `json:"kind,omitempty"`
	SubmissionID string `json:"submission_id,omitempty"`
	ThreadID     string `json:"thread_id,omitempty"`
	TurnID       string `json:"turn_id,omitempty"`
	StartedAt    int64  `json:"started_at,omitempty"`
}

type SessionStagedImage struct {
	SourceMessageID string `json:"source_message_id"`
	RootMessageID   string `json:"root_message_id,omitempty"`
	Name            string `json:"name"`
	LocalPath       string `json:"local_path"`
	CreatedAt       int64  `json:"created_at"`
}
