package conversation

import (
	"feidex/internal/application/backendops"
	modelapp "feidex/internal/application/modelconfig"
	"feidex/internal/domain/conversation"
	"feidex/internal/domain/modelconfig"
	"strings"
)

type Configuration struct {
	Models      modelapp.SnapshotService
	ServiceName func() string
}

func (c Configuration) ThreadStart(r Request) backendops.ThreadStartConfig {
	revision := c.Models.Repository.ModelSourceRevision(r.Session)
	settings := conversation.ResolveSettings(r.Session, revision.Binding, revision.Profile, r.Workspace, "")
	snapshot := c.Models.TurnSnapshot("codex", r.Session)
	model := strings.TrimSpace(r.Model)
	if model == "" {
		model = snapshot.Model
	}
	return backendops.ThreadStartConfig{Cwd: r.Workspace.Cwd, ApprovalPolicy: settings.ApprovalPolicy, SandboxMode: settings.SandboxMode, ServiceName: c.ServiceName(), PersistExtendedHistory: true, ServiceTier: settings.ServiceTier, Model: model, Initialization: snapshot}
}

func (c Configuration) Resume(sess *conversation.Session) modelconfig.Snapshot {
	return c.Models.TurnSnapshot("codex", sess)
}

func (c Configuration) ThreadFork(r Request) backendops.ThreadForkRequest {
	revision := c.Models.Repository.ModelSourceRevision(r.Session)
	settings := conversation.ResolveSettings(r.Session, revision.Binding, revision.Profile, r.Workspace, "")
	return backendops.ThreadForkRequest{ThreadID: strings.TrimSpace(r.Session.ActiveThreadID), Cwd: r.Workspace.Cwd, ApprovalPolicy: settings.ApprovalPolicy, SandboxMode: settings.SandboxMode, ServiceTier: settings.ServiceTier, Model: r.Model, MultiAgentMode: settings.MultiAgentMode}
}
