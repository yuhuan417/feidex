// Package workspace owns repository location and execution policy values.
package workspace

type Workspace struct {
	ID                   string `toml:"id"`
	Name                 string `toml:"name"`
	Cwd                  string `toml:"cwd"`
	ApprovalPolicy       string `toml:"approval_policy"`
	SandboxMode          string `toml:"sandbox_mode"`
	MultiAgentMode       string `toml:"multi_agent_mode"`
	ClaudePermissionMode string `toml:"claude_permission_mode"`
}
