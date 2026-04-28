package codexrpc

import "strings"

type ThreadStartParams struct {
	Cwd                    string
	ApprovalPolicy         string
	Sandbox                string
	ServiceName            string
	ExperimentalRawEvents  bool
	PersistExtendedHistory bool
	ServiceTier            string
	Model                  string
}

func (p ThreadStartParams) Map() map[string]any {
	params := map[string]any{
		"cwd":                    strings.TrimSpace(p.Cwd),
		"approvalPolicy":         strings.TrimSpace(p.ApprovalPolicy),
		"sandbox":                strings.TrimSpace(p.Sandbox),
		"serviceName":            strings.TrimSpace(p.ServiceName),
		"experimentalRawEvents":  p.ExperimentalRawEvents,
		"persistExtendedHistory": p.PersistExtendedHistory,
	}
	if value := strings.TrimSpace(p.ServiceTier); value != "" {
		params["serviceTier"] = value
	}
	if value := strings.TrimSpace(p.Model); value != "" {
		params["model"] = value
	}
	return params
}

type ThreadResumeParams struct {
	ThreadID               string
	PersistExtendedHistory bool
	Model                  string
}

func (p ThreadResumeParams) Map() map[string]any {
	params := map[string]any{
		"threadId":               strings.TrimSpace(p.ThreadID),
		"persistExtendedHistory": p.PersistExtendedHistory,
	}
	if value := strings.TrimSpace(p.Model); value != "" {
		params["model"] = value
	}
	return params
}
