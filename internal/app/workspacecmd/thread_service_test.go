package workspacecmd

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	appcore "feidex/internal/app/appcore"
	appruntime "feidex/internal/app/runtime"
	"feidex/internal/config"
	"feidex/internal/state"
)

func TestCreateWorkspaceAndSwitchUsesClaudeRuntimeWhenBackendIsClaude(t *testing.T) {
	app, session, _ := newTestWorkspaceApp(t, appruntime.BackendClaude)
	claude := &testClaudeCore{ensureSessionID: "claude-thread-new"}
	liveThreadID := ""

	threadSvc := newTestThreadService(app, session, threadServiceOptions{
		claude: claude,
		markLive: func(_ string, threadID string) {
			liveThreadID = threadID
		},
	})
	mgmt := NewManagementService(ManagementDeps{
		App:            app,
		State:          newTestStateDeps(&session),
		SessionContext: testSessionContextDeps(),
		Threads: ThreadDeps{
			EnsureWorkspaceThreadBinding: threadSvc.EnsureWorkspaceThreadBinding,
		},
		Async: AsyncDeps{
			RunAsync: func(fn func()) { fn() },
		},
	})

	targetDir := filepath.Join(t.TempDir(), "claude-created")
	if err := mgmt.CreateWorkspaceAndSwitch(session.Key, session.OwnerUserID, session.ChatID, session.ChatType, "claude-created", "Claude Created", targetDir); err != nil {
		t.Fatalf("CreateWorkspaceAndSwitch() error = %v", err)
	}

	ws := config.FindWorkspace(app.cfg, "claude-created")
	if ws == nil {
		t.Fatal("created workspace missing from config")
	}
	if len(claude.ensureCalls) != 1 || claude.ensureCalls[0].workspaceID != "claude-created" {
		t.Fatalf("Claude EnsureSession calls = %#v", claude.ensureCalls)
	}
	if session.WorkspaceID != "claude-created" || session.ActiveThreadID != "claude-thread-new" || session.ActiveThreadWorkspaceID != "claude-created" {
		t.Fatalf("session after workspace creation = %+v", session)
	}
	if liveThreadID != "claude-thread-new" {
		t.Fatalf("live thread id = %q, want claude-thread-new", liveThreadID)
	}
}

func TestStartWorkspaceThreadReturnsErrorWhenCodexClientMissing(t *testing.T) {
	app, session, ws := newTestWorkspaceApp(t, appruntime.BackendCodex)
	threadSvc := newTestThreadService(app, session, threadServiceOptions{
		codexErr: errors.New("codex client not initialized"),
	})

	_, err := threadSvc.StartWorkspaceThread(session.Key, session, ws)
	if err == nil || !strings.Contains(err.Error(), "codex client not initialized") {
		t.Fatalf("StartWorkspaceThread() error = %v, want codex client not initialized", err)
	}
}

type testWorkspaceApp struct {
	cfg     *config.Config
	cfgPath string
	backend string
	mu      sync.RWMutex
}

func (a *testWorkspaceApp) Config() *config.Config       { return a.cfg }
func (a *testWorkspaceApp) ConfigMu() *sync.RWMutex      { return &a.mu }
func (a *testWorkspaceApp) Backend() string              { return a.backend }
func (a *testWorkspaceApp) FrontendID() string           { return "" }
func (a *testWorkspaceApp) FrontendConfigIndex() int     { return -1 }
func (a *testWorkspaceApp) Store() *state.Store          { return nil }
func (a *testWorkspaceApp) SetBackend(backend string)    { a.backend = backend }
func (a *testWorkspaceApp) ConfigPath() string           { return a.cfgPath }
func (a *testWorkspaceApp) Feishu() appcore.FeishuClient { return nil }

type threadServiceOptions struct {
	claude   appcore.ClaudeCore
	codexErr error
	markLive func(sessionKey, threadID string)
}

func newTestWorkspaceApp(t *testing.T, backend string) (*testWorkspaceApp, *state.Session, *config.Workspace) {
	t.Helper()
	baseDir := t.TempDir()
	cwd := filepath.Join(baseDir, "default")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatalf("MkdirAll(default cwd) error = %v", err)
	}
	cfg := &config.Config{
		Claude: config.ClaudeConfig{Model: "claude-3-7-sonnet"},
		Workspaces: []config.Workspace{{
			ID:   "default",
			Name: "Default",
			Cwd:  cwd,
		}},
	}
	app := &testWorkspaceApp{
		cfg:     cfg,
		cfgPath: filepath.Join(baseDir, "feidex.json"),
		backend: backend,
	}
	if err := config.Save(app.cfgPath, cfg); err != nil {
		t.Fatalf("config.Save() error = %v", err)
	}
	session := &state.Session{
		Key:         "sess-1",
		WorkspaceID: "default",
		OwnerUserID: "user-1",
		ChatID:      "chat-1",
		ChatType:    "p2p",
		Status:      state.SessionStatusIdle.String(),
	}
	return app, session, &cfg.Workspaces[0]
}

func newTestStateDeps(session **state.Session) StateDeps {
	return StateDeps{
		GetSession: func(key string) *state.Session {
			if *session == nil || (*session).Key != key {
				return nil
			}
			return *session
		},
		SaveSession: func(sess *state.Session) error {
			*session = sess
			return nil
		},
	}
}

func testSessionContextDeps() SessionContextDeps {
	return SessionContextDeps{
		SessionHasInFlight: func(*state.Session) bool { return false },
		SwitchSessionWorkspace: func(sess *state.Session, workspaceID string) {
			sess.WorkspaceID = workspaceID
		},
		ClearSessionThreadCtx: func(sess *state.Session) {
			sess.ActiveThreadWorkspaceID = ""
			sess.ActiveThreadID = ""
			sess.ActiveThreadName = ""
			sess.ActiveThreadPreview = ""
		},
		SetSessionThreadCtx: func(sess *state.Session, workspaceID, threadID, name, preview string) {
			sess.ActiveThreadWorkspaceID = workspaceID
			sess.ActiveThreadID = threadID
			sess.ActiveThreadName = name
			sess.ActiveThreadPreview = preview
		},
		SessionResetActiveOps: func(*state.Session) {},
	}
}

func newTestThreadService(app *testWorkspaceApp, session *state.Session, opts threadServiceOptions) *ThreadService {
	return NewThreadService(ThreadServiceDeps{
		App:   app,
		State: newTestStateDeps(&session),
		Threads: ThreadDeps{
			MarkSessionThreadLive: opts.markLive,
		},
		SessionContext: testSessionContextDeps(),
		Codex: CodexDeps{
			RequireCodexClient: func() (CodexClient, error) { return nil, opts.codexErr },
		},
		Claude: ClaudeDeps{
			RequireClaudeCore: func() (appcore.ClaudeCore, error) { return opts.claude, nil },
		},
	})
}

type testClaudeCore struct {
	ensureSessionID string
	ensureCalls     []testEnsureCall
}

type testEnsureCall struct {
	sessionKey     string
	workspaceID    string
	resumeThreadID string
	model          string
}

func (c *testClaudeCore) EnsureSession(_ context.Context, sessionKey string, ws *config.Workspace, resumeThreadID, model string) (string, error) {
	c.ensureCalls = append(c.ensureCalls, testEnsureCall{
		sessionKey:     sessionKey,
		workspaceID:    ws.ID,
		resumeThreadID: resumeThreadID,
		model:          model,
	})
	return c.ensureSessionID, nil
}

func (c *testClaudeCore) ForkSession(context.Context, string, *config.Workspace, string, string) (string, error) {
	return "", nil
}

func (c *testClaudeCore) UpdateConfig(config.ClaudeConfig) {}

func (c *testClaudeCore) ResetSession(string) error { return nil }

func (c *testClaudeCore) StartTurn(context.Context, string, string, string, string) error { return nil }

func (c *testClaudeCore) StartSteerTurn(context.Context, string, string, string, string, string) error {
	return nil
}

func (c *testClaudeCore) Interrupt(context.Context, string) error { return nil }

func (c *testClaudeCore) SetModel(context.Context, string, string) (bool, error) { return false, nil }

func (c *testClaudeCore) SetEffort(context.Context, string, string) (bool, error) { return false, nil }

func (c *testClaudeCore) SetPermissionMode(context.Context, string, string) error { return nil }

func (c *testClaudeCore) ResolveApproval(string, appruntime.ClaudeApprovalResolution) error {
	return nil
}

func (c *testClaudeCore) ResolveUserInput(string, map[string]string) error { return nil }

func (c *testClaudeCore) ResolvePlanFeedback(string, string) error { return nil }

func (c *testClaudeCore) CancelPending(string, string) error { return nil }

func (c *testClaudeCore) SessionStopped(string) bool { return false }

func (c *testClaudeCore) Close() error { return nil }
