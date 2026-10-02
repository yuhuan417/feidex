package routing

type AgentBinding struct {
	ID                              string                        `json:"id"`
	FrontendID                      string                        `json:"frontend_id"`
	ChatID                          string                        `json:"chat_id"`
	ChatType                        string                        `json:"chat_type"`
	WorkspaceID                     string                        `json:"workspace_id"`
	ModelOverride                   string                        `json:"model_override,omitempty"`
	ReasoningEffortOverride         string                        `json:"reasoning_effort_override,omitempty"`
	PlanModelOverride               string                        `json:"plan_model_override,omitempty"`
	PlanReasoningEffortOverride     string                        `json:"plan_reasoning_effort_override,omitempty"`
	ReviewModelOverride             string                        `json:"review_model_override,omitempty"`
	SubagentModelOverride           string                        `json:"subagent_model_override,omitempty"`
	SubagentReasoningEffortOverride string                        `json:"subagent_reasoning_effort_override,omitempty"`
	SmallModelOverride              string                        `json:"small_model_override,omitempty"`
	ServiceTierOverride             string                        `json:"service_tier_override,omitempty"`
	SandboxModeOverride             string                        `json:"sandbox_mode_override,omitempty"`
	ApprovalPolicyOverride          string                        `json:"approval_policy_override,omitempty"`
	MultiAgentModeOverride          string                        `json:"multi_agent_mode_override,omitempty"`
	ClaudePermissionMode            string                        `json:"claude_permission_mode,omitempty"`
	PendingMessage                  *AgentBindingPendingMessage   `json:"pending_message,omitempty"`
	PendingMessages                 []*AgentBindingPendingMessage `json:"pending_messages,omitempty"`
	Status                          string                        `json:"status"`
	CreatedAt                       int64                         `json:"created_at"`
	UpdatedAt                       int64                         `json:"updated_at"`
}

type BotProfile struct {
	ID                      string `json:"id"`
	FrontendID              string `json:"frontend_id"`
	WorkspaceID             string `json:"workspace_id,omitempty"`
	Model                   string `json:"model,omitempty"`
	ReasoningEffort         string `json:"reasoning_effort,omitempty"`
	PlanModel               string `json:"plan_model,omitempty"`
	PlanReasoningEffort     string `json:"plan_reasoning_effort,omitempty"`
	ReviewModel             string `json:"review_model,omitempty"`
	SubagentModel           string `json:"subagent_model,omitempty"`
	SubagentReasoningEffort string `json:"subagent_reasoning_effort,omitempty"`
	ServiceTier             string `json:"service_tier,omitempty"`
	SandboxMode             string `json:"sandbox_mode,omitempty"`
	ApprovalPolicy          string `json:"approval_policy,omitempty"`
	MultiAgentMode          string `json:"multi_agent_mode,omitempty"`
	ClaudeModel             string `json:"claude_model,omitempty"`
	ClaudeSmallModel        string `json:"claude_small_model,omitempty"`
	ClaudeSubagentModel     string `json:"claude_subagent_model,omitempty"`
	ClaudePermissionMode    string `json:"claude_permission_mode,omitempty"`
	CreatedAt               int64  `json:"created_at"`
	UpdatedAt               int64  `json:"updated_at"`
}

type AgentBindingPendingMessage struct {
	SessionKey             string                          `json:"session_key,omitempty"`
	MessageID              string                          `json:"message_id"`
	ChatID                 string                          `json:"chat_id"`
	ChatType               string                          `json:"chat_type"`
	UserID                 string                          `json:"user_id"`
	UserName               string                          `json:"user_name,omitempty"`
	ChatName               string                          `json:"chat_name,omitempty"`
	Text                   string                          `json:"text,omitempty"`
	RootMessageID          string                          `json:"root_message_id,omitempty"`
	ParentMessageID        string                          `json:"parent_message_id,omitempty"`
	ThreadID               string                          `json:"thread_id,omitempty"`
	Attachments            []AgentBindingPendingAttachment `json:"attachments,omitempty"`
	MergeForwardMessageIDs []string                        `json:"merge_forward_message_ids,omitempty"`
	ExpandedMergeForward   bool                            `json:"expanded_merge_forward,omitempty"`
	MentionedOpenIDs       []string                        `json:"mentioned_open_ids,omitempty"`
	MentionedAny           bool                            `json:"mentioned_any,omitempty"`
	MentionedSelf          bool                            `json:"mentioned_self,omitempty"`
	CreatedAt              int64                           `json:"created_at,omitempty"`
	StoredAt               int64                           `json:"stored_at,omitempty"`
}

type AgentBindingPendingAttachment struct {
	Kind            string `json:"kind,omitempty"`
	ResourceKey     string `json:"resource_key,omitempty"`
	SourceMessageID string `json:"source_message_id,omitempty"`
}
