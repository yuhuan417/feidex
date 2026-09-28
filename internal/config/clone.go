package config

// Clone returns a detached configuration for validation before publication.
func Clone(c *Config) *Config {
	if c == nil {
		return nil
	}
	next := *c
	next.Workspaces = append([]Workspace(nil), c.Workspaces...)
	next.Frontends = append([]FrontendConfig(nil), c.Frontends...)
	next.Feishu.AllowFrom = append([]string(nil), c.Feishu.AllowFrom...)
	next.Feishu.DebugAllowFrom = append([]string(nil), c.Feishu.DebugAllowFrom...)
	next.Claude.ModelOptions = append([]string(nil), c.Claude.ModelOptions...)
	for i := range next.Frontends {
		next.Frontends[i].AllowFrom = append([]string(nil), c.Frontends[i].AllowFrom...)
		next.Frontends[i].DebugAllowFrom = append([]string(nil), c.Frontends[i].DebugAllowFrom...)
	}
	return &next
}
