package upgrade

type Payload struct {
	CurrentVersion  string `json:"current_version"`
	TargetVersion   string `json:"target_version"`
	ReleaseTag      string `json:"release_tag"`
	BinaryPath      string `json:"binary_path"`
	DownloadURL     string `json:"download_url"`
	SourcePath      string `json:"source_path"`
	SourceKind      string `json:"source_kind"`
	SourceName      string `json:"source_name"`
	SourceSize      int64  `json:"source_size"`
	SourceCommit    string `json:"source_commit"`
	ExpectedSHA256  string `json:"expected_sha256"`
	ReleaseURL      string `json:"release_url"`
	UnitName        string `json:"unit_name,omitempty"`
	LaunchStartedAt int64  `json:"launch_started_at,omitempty"`
	ChatID          string `json:"chat_id,omitempty"`
	FeishuMsgID     string `json:"feishu_msg_id,omitempty"`
}
