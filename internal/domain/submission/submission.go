package submission

import domainmodelconfig "feidex/internal/domain/modelconfig"

type SubmissionAttachment struct {
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	LocalPath string `json:"local_path"`
}

type SubmissionSkill struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

type Submission struct {
	ModelConfig          domainmodelconfig.Snapshot `json:"model_config,omitempty"`
	ID                   string                     `json:"id"`
	SessionKey           string                     `json:"session_key"`
	BindingID            string                     `json:"binding_id,omitempty"`
	WorkspaceID          string                     `json:"workspace_id"`
	ThreadID             string                     `json:"thread_id"`
	TurnID               string                     `json:"turn_id"`
	UserID               string                     `json:"user_id"`
	ChatID               string                     `json:"chat_id"`
	TriggerMessageID     string                     `json:"trigger_message_id"`
	SourceMessageIDs     []string                   `json:"source_message_ids,omitempty"`
	SourceRootMessageIDs []string                   `json:"source_root_message_ids,omitempty"`
	InputText            string                     `json:"input_text"`
	Skills               []SubmissionSkill          `json:"skills,omitempty"`
	Attachments          []SubmissionAttachment     `json:"attachments,omitempty"`
	Kind                 string                     `json:"kind,omitempty"`
	ReviewTargetType     string                     `json:"review_target_type,omitempty"`
	ReviewBranch         string                     `json:"review_branch,omitempty"`
	ReviewCommitSHA      string                     `json:"review_commit_sha,omitempty"`
	ReviewCommitTitle    string                     `json:"review_commit_title,omitempty"`
	ReviewInstructions   string                     `json:"review_instructions,omitempty"`
	Status               string                     `json:"status"`
	WaitedInQueue        bool                       `json:"waited_in_queue,omitempty"`
	StartNoticeSent      bool                       `json:"start_notice_sent,omitempty"`
	Finalized            bool                       `json:"finalized"`
	CreatedAt            int64                      `json:"created_at"`
	UpdatedAt            int64                      `json:"updated_at"`
}
