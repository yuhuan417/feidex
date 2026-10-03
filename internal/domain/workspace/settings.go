package workspace

type SettingOption struct{ Value, Label string }

func SandboxOptions() []SettingOption {
	return []SettingOption{{"read-only", "read-only"}, {"workspace-write", "workspace-write"}, {"danger-full-access", "danger-full-access"}}
}
func ApprovalPolicyOptions() []SettingOption {
	return []SettingOption{{"untrusted", "untrusted"}, {"on-request", "on-request"}, {"never", "never"}}
}
func MultiAgentModeOptions() []SettingOption {
	return []SettingOption{{"explicitRequestOnly", "explicit request only"}, {"proactive", "proactive"}, {"none", "none"}}
}
