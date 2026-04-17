package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"feidex/internal/codexrpc"
	"feidex/internal/config"
	"feidex/internal/daemon"
	"feidex/internal/feishu"
	"feidex/internal/logcontrol"
	"feidex/internal/release"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

type fakeCodexClient struct {
	startErr error
	closeErr error
	callErr  error
	replyErr error
	started  bool
	closed   bool
	replies  []struct {
		id     json.RawMessage
		result any
	}
	replyErrors []struct {
		id   json.RawMessage
		code int
		msg  string
	}
	callHook       func(context.Context, string, any, any) error
	onNotification func(string, json.RawMessage)
	onRequest      func(codexrpc.RequestEnvelope)
}

type fakeReleaseClient struct {
	info         *release.ReleaseInfo
	devInfo      *release.ReleaseInfo
	versionInfo  map[string]*release.ReleaseInfo
	err          error
	devErr       error
	latestErr    error
	versionErr   error
	latestCalls  int
	devCalls     int
	versionCalls []string
}

type blockingReleaseClient struct {
	started chan struct{}
	release chan struct{}
	info    *release.ReleaseInfo
}

func (f *fakeReleaseClient) LatestLinuxBinary(context.Context, string) (*release.ReleaseInfo, error) {
	f.latestCalls++
	if f.latestErr != nil {
		return nil, f.latestErr
	}
	if f.err != nil {
		return nil, f.err
	}
	if f.info == nil {
		return nil, errors.New("missing release info")
	}
	cp := *f.info
	return &cp, nil
}

func (f *blockingReleaseClient) LatestLinuxBinary(context.Context, string) (*release.ReleaseInfo, error) {
	close(f.started)
	<-f.release
	cp := *f.info
	return &cp, nil
}

func (f *blockingReleaseClient) LatestDevLinuxBinary(context.Context, string) (*release.ReleaseInfo, error) {
	close(f.started)
	<-f.release
	cp := *f.info
	return &cp, nil
}

func (f *blockingReleaseClient) LinuxBinaryByVersion(context.Context, string, string) (*release.ReleaseInfo, error) {
	close(f.started)
	<-f.release
	cp := *f.info
	return &cp, nil
}

func (f *fakeReleaseClient) LatestDevLinuxBinary(context.Context, string) (*release.ReleaseInfo, error) {
	f.devCalls++
	if f.devErr != nil {
		return nil, f.devErr
	}
	if f.err != nil {
		return nil, f.err
	}
	if f.devInfo != nil {
		cp := *f.devInfo
		return &cp, nil
	}
	if info := f.versionInfo[release.DevReleaseTag]; info != nil {
		cp := *info
		return &cp, nil
	}
	if f.info == nil {
		return nil, errors.New("missing release info")
	}
	cp := *f.info
	return &cp, nil
}

func (f *fakeReleaseClient) LinuxBinaryByVersion(_ context.Context, version string, _ string) (*release.ReleaseInfo, error) {
	f.versionCalls = append(f.versionCalls, version)
	if f.versionErr != nil {
		return nil, f.versionErr
	}
	if f.err != nil {
		return nil, f.err
	}
	if f.versionInfo != nil {
		if info := f.versionInfo[version]; info != nil {
			cp := *info
			return &cp, nil
		}
	}
	if f.info == nil {
		return nil, errors.New("missing release info")
	}
	cp := *f.info
	return &cp, nil
}

type fakeDaemonManagerForApp struct {
	status *daemon.Status
	err    error
}

func (f *fakeDaemonManagerForApp) Install(daemon.Config) error { return nil }
func (f *fakeDaemonManagerForApp) Uninstall() error            { return nil }
func (f *fakeDaemonManagerForApp) Start() error                { return nil }
func (f *fakeDaemonManagerForApp) Stop() error                 { return nil }
func (f *fakeDaemonManagerForApp) Restart() error              { return nil }
func (f *fakeDaemonManagerForApp) Status() (*daemon.Status, error) {
	return f.status, f.err
}
func (f *fakeDaemonManagerForApp) Platform() string { return "test" }

func (f *fakeCodexClient) SetHandlers(onNotification func(string, json.RawMessage), onRequest func(codexrpc.RequestEnvelope)) {
	f.onNotification = onNotification
	f.onRequest = onRequest
}

func (f *fakeCodexClient) Start(context.Context, bool) error {
	f.started = true
	return f.startErr
}

func (f *fakeCodexClient) Close() error {
	f.closed = true
	return f.closeErr
}

func (f *fakeCodexClient) Call(ctx context.Context, method string, params any, out any) error {
	if f.callHook != nil {
		return f.callHook(ctx, method, params, out)
	}
	return f.callErr
}

func (f *fakeCodexClient) Reply(id json.RawMessage, result any) error {
	if f.replyErr != nil {
		return f.replyErr
	}
	f.replies = append(f.replies, struct {
		id     json.RawMessage
		result any
	}{append(json.RawMessage(nil), id...), result})
	return nil
}

func (f *fakeCodexClient) ReplyError(id json.RawMessage, code int, msg string) error {
	f.replyErrors = append(f.replyErrors, struct {
		id   json.RawMessage
		code int
		msg  string
	}{append(json.RawMessage(nil), id...), code, msg})
	return nil
}

type fakeFeishuClient struct {
	startErr                error
	replyTextErr            error
	sendTextErr             error
	replyCardErr            error
	sendCardErr             error
	patchCardErr            error
	rewritePreviewErr       error
	addReactionErr          error
	removeReactionErr       error
	downloadErr             error
	shareFileErr            error
	cleanupResult           feishu.PreviewDriveCleanupResult
	cleanupErr              error
	started                 bool
	stopped                 bool
	replyTexts              []string
	sentTexts               []string
	replyCards              []map[string]any
	sendCards               []map[string]any
	patchedCards            []map[string]any
	replyCardInThread       []bool
	replyTextWithIDs        []string
	replyCardID             string
	replyCardIDs            []string
	replyTextIDs            []string
	sendCardID              string
	previewStatePath        string
	previewProcessCWD       string
	rewritePreviewOut       string
	downloadPath            string
	downloadName            string
	mergeForwardText        string
	mergeForwardAttachments []feishu.Attachment
	mergeForwardErr         error
	resolveMergeForwardHook func(context.Context, string, []string) (string, []feishu.Attachment, error)
	mergeForwardCalls       []struct {
		messageID string
		ids       []string
	}
	sharedFileResult   feishu.SharedFileResult
	sharedFileRequests []feishu.SharedFileRequest
	onMessage          func(*feishu.InboundMessage)
}

func (f *fakeFeishuClient) SetHandlers(onMessage func(*feishu.InboundMessage), _ func(*feishu.CardAction) (*callback.CardActionTriggerResponse, error), _ func(*feishu.BotMenuClick), _ func(*feishu.MessageRecall), _ func(*feishu.MessageReaction)) {
	f.onMessage = onMessage
}

func (f *fakeFeishuClient) Start(context.Context) error {
	f.started = true
	return f.startErr
}

func (f *fakeFeishuClient) Stop() {
	f.stopped = true
}

func (f *fakeFeishuClient) ConfigureMarkdownPreview(statePath, processCWD string) {
	f.previewStatePath = statePath
	f.previewProcessCWD = processCWD
}

func (f *fakeFeishuClient) RewriteMarkdownPreview(context.Context, feishu.MarkdownPreviewRequest) (string, error) {
	return f.rewritePreviewOut, f.rewritePreviewErr
}

func (f *fakeFeishuClient) CleanupArtifactsBefore(context.Context, time.Time) (feishu.PreviewDriveCleanupResult, error) {
	return f.cleanupResult, f.cleanupErr
}

func (f *fakeFeishuClient) AddReaction(context.Context, string, string) error {
	return f.addReactionErr
}
func (f *fakeFeishuClient) RemoveReaction(context.Context, string, string) error {
	return f.removeReactionErr
}

func (f *fakeFeishuClient) ReplyText(_ context.Context, _ string, text string, _ bool) error {
	f.replyTexts = append(f.replyTexts, text)
	return f.replyTextErr
}

func (f *fakeFeishuClient) ReplyTextWithID(_ context.Context, _ string, text string, _ bool) (string, error) {
	f.replyTextWithIDs = append(f.replyTextWithIDs, text)
	if len(f.replyTextIDs) > 0 {
		id := f.replyTextIDs[0]
		f.replyTextIDs = f.replyTextIDs[1:]
		return id, nil
	}
	return "reply-text-id", nil
}

func (f *fakeFeishuClient) SendText(_ context.Context, _ string, text string) error {
	f.sentTexts = append(f.sentTexts, text)
	return f.sendTextErr
}

func (f *fakeFeishuClient) ReplyCard(_ context.Context, _ string, card map[string]any, inThread bool) (string, error) {
	f.replyCards = append(f.replyCards, card)
	f.replyCardInThread = append(f.replyCardInThread, inThread)
	if len(f.replyCardIDs) > 0 {
		id := f.replyCardIDs[0]
		f.replyCardIDs = f.replyCardIDs[1:]
		return id, f.replyCardErr
	}
	if f.replyCardID == "" {
		f.replyCardID = "reply-card-id"
	}
	return f.replyCardID, f.replyCardErr
}

func (f *fakeFeishuClient) SendCard(_ context.Context, _ string, card map[string]any) (string, error) {
	f.sendCards = append(f.sendCards, card)
	if f.sendCardID == "" {
		f.sendCardID = "send-card-id"
	}
	return f.sendCardID, f.sendCardErr
}

func (f *fakeFeishuClient) PatchCard(_ context.Context, _ string, card map[string]any) error {
	f.patchedCards = append(f.patchedCards, card)
	return f.patchCardErr
}

func (f *fakeFeishuClient) DownloadMessageResource(context.Context, string, feishu.Attachment, string) (string, string, error) {
	return f.downloadPath, f.downloadName, f.downloadErr
}

func (f *fakeFeishuClient) ResolveMergeForward(ctx context.Context, messageID string, messageIDs []string) (string, []feishu.Attachment, error) {
	ids := append([]string(nil), messageIDs...)
	f.mergeForwardCalls = append(f.mergeForwardCalls, struct {
		messageID string
		ids       []string
	}{messageID: messageID, ids: ids})
	if f.resolveMergeForwardHook != nil {
		return f.resolveMergeForwardHook(ctx, messageID, ids)
	}
	attachments := append([]feishu.Attachment(nil), f.mergeForwardAttachments...)
	return f.mergeForwardText, attachments, f.mergeForwardErr
}

func (f *fakeFeishuClient) ShareLocalFile(_ context.Context, req feishu.SharedFileRequest) (feishu.SharedFileResult, error) {
	f.sharedFileRequests = append(f.sharedFileRequests, req)
	return f.sharedFileResult, f.shareFileErr
}

func (f *fakeFeishuClient) SimpleStatusCard(title, color, body string, buttons []feishu.Button) map[string]any {
	return (&feishu.Adapter{}).SimpleStatusCard(title, color, body, buttons)
}

func cardMarkdownContent(t *testing.T, card map[string]any) string {
	t.Helper()
	elements := cardElementsForTest(card)
	if len(elements) == 0 {
		t.Fatalf("unexpected card elements: %#v", card)
	}
	var parts []string
	for _, elem := range elements {
		if content, ok := elem["content"].(string); ok {
			parts = append(parts, content)
			continue
		}
		if text, ok := elem["text"].(map[string]any); ok {
			if content, ok := text["content"].(string); ok {
				parts = append(parts, content)
			}
		}
	}
	return strings.Join(parts, "\n")
}

func cardElementsForTest(card map[string]any) []map[string]any {
	if elements, ok := card["elements"].([]map[string]any); ok {
		return elements
	}
	body, _ := card["body"].(map[string]any)
	elements, _ := body["elements"].([]map[string]any)
	return elements
}

func cardButtonsForTest(card map[string]any) []map[string]any {
	var buttons []map[string]any
	for _, elem := range cardElementsForTest(card) {
		if actions, ok := elem["actions"].([]map[string]any); ok {
			buttons = append(buttons, actions...)
		}
		if columns, ok := elem["columns"].([]map[string]any); ok {
			for _, column := range columns {
				columnElems, _ := column["elements"].([]map[string]any)
				for _, child := range columnElems {
					if tag, _ := child["tag"].(string); tag == "button" {
						buttons = append(buttons, child)
					}
				}
			}
		}
	}
	return buttons
}

func cardSelectStaticForTest(card map[string]any) []map[string]any {
	var selects []map[string]any
	for _, elem := range cardElementsForTest(card) {
		if tag, _ := elem["tag"].(string); tag == "select_static" {
			selects = append(selects, elem)
		}
	}
	return selects
}

func newTestApp(t *testing.T) (*App, *fakeFeishuClient, *fakeCodexClient) {
	t.Helper()

	cfg := config.Default()
	cfg.Workspaces[0].Cwd = t.TempDir()
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("Save(config) error = %v", err)
	}
	loadedCfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("Load(config) error = %v", err)
	}
	store, err := state.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("Open(store) error = %v", err)
	}
	ff := &fakeFeishuClient{}
	fc := &fakeCodexClient{}
	a := &App{
		cfg:          loadedCfg,
		cfgPath:      cfgPath,
		store:        store,
		codex:        fc,
		feishu:       ff,
		started:      time.Now(),
		turnStreams:  map[string]*turnStream{},
		liveThreads:  map[string]string{},
		turnBindings: map[string]turnBinding{},
		pendingTurns: map[string]turnBinding{},
	}
	return a, ff, fc
}

func seedActiveSubmission(t *testing.T, a *App, sessionKey, threadID, turnID string) *state.Submission {
	t.Helper()

	if err := a.store.UpsertSession(&state.Session{
		Key:                sessionKey,
		WorkspaceID:        a.cfg.Workspaces[0].ID,
		ActiveThreadID:     threadID,
		ActiveTurnID:       turnID,
		ActiveSubmissionID: "sub-1",
		OwnerUserID:        "user-1",
		ChatID:             "chat-1",
		ChatType:           "group",
		Status:             "running",
	}); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}
	subID, err := a.store.CreateSubmission(&state.Submission{
		ID:               "sub-1",
		SessionKey:       sessionKey,
		WorkspaceID:      a.cfg.Workspaces[0].ID,
		ThreadID:         threadID,
		TurnID:           turnID,
		UserID:           "user-1",
		ChatID:           "chat-1",
		TriggerMessageID: "trigger-1",
		Status:           "running",
	})
	if err != nil {
		t.Fatalf("CreateSubmission() error = %v", err)
	}
	return a.store.GetSubmission(subID)
}

func TestNewUsesInjectedClientsAndConfiguresHandlers(t *testing.T) {
	origCodex := newAppCodexClient
	origFeishu := newFeishuClient
	defer func() {
		newAppCodexClient = origCodex
		newFeishuClient = origFeishu
	}()

	fc := &fakeCodexClient{}
	ff := &fakeFeishuClient{}
	newAppCodexClient = func(*config.Config) codexClient { return fc }
	newFeishuClient = func(config.FeishuConfig) feishuClient { return ff }

	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	app, err := New(cfg, filepath.Join(t.TempDir(), "config.toml"))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	notifier, ok := app.feishu.(*notifyingFeishuClient)
	if app.codex != fc || app.codexPool != nil || !ok || notifier.base != ff {
		t.Fatalf("New() did not use injected clients: %+v", app)
	}
	if fc.onNotification == nil || fc.onRequest == nil {
		t.Fatal("expected codex handlers to be configured")
	}
	if ff.onMessage == nil {
		t.Fatal("expected feishu handlers to be configured")
	}
	if ff.previewStatePath != "" {
		t.Fatalf("preview state path = %q, want empty for stateless preview management", ff.previewStatePath)
	}
}

func TestAppStartStopAndRecoverRuntimeState(t *testing.T) {
	a, ff, fc := newTestApp(t)
	fc.startErr = errors.New("codex start failed")
	if err := a.Start(context.Background()); err == nil {
		t.Fatal("expected Start() to fail on codex start error")
	}

	a, ff, fc = newTestApp(t)
	ff.startErr = errors.New("feishu start failed")
	if err := a.Start(context.Background()); err == nil {
		t.Fatal("expected Start() to fail on feishu start error")
	}

	a, ff, fc = newTestApp(t)
	if err := a.Start(context.Background()); err != nil {
		t.Fatalf("Start(success) error = %v", err)
	}
	if !fc.started || !ff.started {
		t.Fatalf("expected both clients to start, codex=%v feishu=%v", fc.started, ff.started)
	}
	if err := a.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if !fc.closed || !ff.stopped {
		t.Fatalf("expected both clients to stop, codex=%v feishu=%v", fc.closed, ff.stopped)
	}

	a, _, _ = newTestApp(t)
	oldAttachmentDir := filepath.Join(a.cfg.Workspaces[0].Cwd, attachmentsDirName, "sess", "old")
	if err := os.MkdirAll(oldAttachmentDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(oldAttachmentDir) error = %v", err)
	}
	oldTime := time.Now().Add(-attachmentRetention - time.Hour)
	if err := os.Chtimes(oldAttachmentDir, oldTime, oldTime); err != nil {
		t.Fatalf("Chtimes(oldAttachmentDir) error = %v", err)
	}
	if err := a.store.UpsertPending(&state.PendingRequest{ID: "req-1", Status: "pending", ExpiresAt: time.Now().Add(time.Hour).Unix()}); err != nil {
		t.Fatalf("UpsertPending() error = %v", err)
	}
	if err := a.store.UpsertSession(&state.Session{
		Key:            "sess-1",
		Status:         "idle",
		ActiveThreadID: "thread-1",
	}); err != nil {
		t.Fatalf("UpsertSession(sess-1) error = %v", err)
	}
	if err := a.store.UpsertSession(&state.Session{
		Key:                     "sess-2",
		Status:                  "running",
		ActiveThreadID:          "thread-2",
		ActiveSubmissionID:      "sub-1",
		ActiveTurnID:            "turn-1",
		Queue:                   []string{"sub-2"},
		StagedImages:            []state.SessionStagedImage{{Name: "img"}},
		ActiveThreadWorkspaceID: "",
	}); err != nil {
		t.Fatalf("UpsertSession(sess-2) error = %v", err)
	}

	a.recoverRuntimeState()

	sess1 := a.store.GetSession("sess-1")
	if sess1.WorkspaceID != a.defaultWorkspaceID() || sess1.ActiveThreadID != "" {
		t.Fatalf("recoverRuntimeState(sess-1) = %+v, want workspace repair and cleared thread context", sess1)
	}
	sess2 := a.store.GetSession("sess-2")
	if sess2.Status != "idle" || sess2.ActiveTurnID != "" || len(sess2.Queue) != 0 || len(sess2.StagedImages) != 0 {
		t.Fatalf("recoverRuntimeState(sess-2) = %+v, want cleared runtime state", sess2)
	}
	if pending := a.store.PendingByID("req-1"); pending == nil || pending.Status != "expired" {
		t.Fatalf("pending request after recover = %+v, want expired", pending)
	}
	if _, err := os.Stat(oldAttachmentDir); !os.IsNotExist(err) {
		t.Fatalf("expected old attachment dir to be removed, stat err = %v", err)
	}
}

func TestRecoverRuntimeStateResumesActiveThreadOnStartup(t *testing.T) {
	a, _, fc := newTestApp(t)
	sessionKey := "sess-startup-resume"
	if err := a.store.UpsertSession(&state.Session{
		Key:                     sessionKey,
		WorkspaceID:             a.cfg.Workspaces[0].ID,
		ActiveThreadID:          "thread-1",
		ActiveThreadWorkspaceID: a.cfg.Workspaces[0].ID,
		Status:                  "idle",
	}); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}

	var calls []string
	fc.callHook = func(_ context.Context, method string, params any, out any) error {
		calls = append(calls, method)
		if method != "thread/resume" {
			t.Fatalf("unexpected startup method: %s", method)
		}
		got, _ := params.(map[string]any)
		if got["threadId"] != "thread-1" {
			t.Fatalf("thread/resume params = %+v, want thread-1", got)
		}
		result := out.(*codexrpc.ThreadStartResult)
		result.Thread.ID = "thread-1"
		result.Thread.Name = "Recovered"
		result.Thread.Preview = "preview"
		return nil
	}

	a.recoverRuntimeState()

	if len(calls) != 1 || calls[0] != "thread/resume" {
		t.Fatalf("startup recovery calls = %+v, want thread/resume", calls)
	}
	sess := a.store.GetSession(sessionKey)
	if sess == nil || sess.ActiveThreadID != "thread-1" || sess.ActiveThreadName != "Recovered" || sess.ActiveThreadPreview != "preview" {
		t.Fatalf("session after startup resume = %+v", sess)
	}
	if !a.sessionHasLiveThread(sessionKey, "thread-1") {
		t.Fatal("expected resumed thread to be marked live")
	}
}

func TestRecoverRuntimeStateStartsFreshThreadWhenResumeFails(t *testing.T) {
	a, _, fc := newTestApp(t)
	sessionKey := "sess-startup-fresh"
	if err := a.store.UpsertSession(&state.Session{
		Key:                        sessionKey,
		WorkspaceID:                a.cfg.Workspaces[0].ID,
		ActiveThreadID:             "thread-old",
		ActiveThreadWorkspaceID:    a.cfg.Workspaces[0].ID,
		ActiveThreadApprovalPolicy: "never",
		ActiveThreadSandboxMode:    "read-only",
		ActiveThreadServiceTier:    "fast",
		Status:                     "idle",
	}); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}

	var calls []string
	var startParams map[string]any
	fc.callHook = func(_ context.Context, method string, params any, out any) error {
		calls = append(calls, method)
		switch method {
		case "thread/resume":
			return errors.New("thread not found")
		case "thread/start":
			startParams, _ = params.(map[string]any)
			result := out.(*codexrpc.ThreadStartResult)
			result.Thread.ID = "thread-new"
			result.Thread.Name = "Fresh"
			result.Thread.Preview = "preview"
			return nil
		default:
			t.Fatalf("unexpected startup method: %s", method)
			return nil
		}
	}

	a.recoverRuntimeState()

	if len(calls) != 2 || calls[0] != "thread/resume" || calls[1] != "thread/start" {
		t.Fatalf("startup recovery calls = %+v, want resume then start", calls)
	}
	if startParams["approvalPolicy"] != "never" || startParams["sandbox"] != "read-only" || startParams["serviceTier"] != "fast" || startParams["cwd"] != a.cfg.Workspaces[0].Cwd {
		t.Fatalf("thread/start params = %+v, want session overrides and workspace cwd", startParams)
	}
	sess := a.store.GetSession(sessionKey)
	if sess == nil || sess.ActiveThreadID != "thread-new" || sess.ActiveThreadName != "Fresh" || sess.ActiveThreadPreview != "preview" {
		t.Fatalf("session after startup fresh thread = %+v", sess)
	}
	if !a.sessionHasLiveThread(sessionKey, "thread-new") {
		t.Fatal("expected fresh thread to be marked live")
	}
}

func TestAppMiscMessageHelpers(t *testing.T) {
	a, ff, _ := newTestApp(t)

	if err := a.replyError(nil, nil); err != nil {
		t.Fatalf("replyError(nil, nil) error = %v", err)
	}
	msg := &feishu.InboundMessage{MessageID: "m-1", ChatID: "chat-1", ChatType: "group"}
	if err := a.replyError(msg, errors.New("boom")); err != nil {
		t.Fatalf("replyError(reply) error = %v", err)
	}
	if len(ff.replyTexts) == 0 || !strings.Contains(ff.replyTexts[0], "执行失败: boom") {
		t.Fatalf("replyError() did not send reply text: %+v", ff.replyTexts)
	}

	ff.replyTexts = nil
	ff.sentTexts = nil
	if err := a.replyError(&feishu.InboundMessage{ChatID: "chat-1"}, errors.New("boom2")); err != nil {
		t.Fatalf("replyError(send) error = %v", err)
	}
	if len(ff.sentTexts) == 0 || !strings.Contains(ff.sentTexts[0], "执行失败: boom2") {
		t.Fatalf("replyError() did not send fallback text: %+v", ff.sentTexts)
	}

	a.handleBotMenu(&feishu.BotMenuClick{UserID: "u-1", Command: "/unknown"})
	if len(ff.sentTexts) == 0 || !strings.Contains(ff.sentTexts[len(ff.sentTexts)-1], "命令执行失败") {
		t.Fatalf("handleBotMenu() did not send command failure: %+v", ff.sentTexts)
	}

	if !a.isStaleInboundMessage(&feishu.InboundMessage{CreatedAt: a.started.Add(-31 * time.Second).Unix()}) {
		t.Fatal("expected stale inbound message")
	}
	if a.isStaleInboundMessage(&feishu.InboundMessage{CreatedAt: a.started.Unix()}) {
		t.Fatal("expected fresh inbound message")
	}
	if got := nonZero(0, 0, 3, 4); got != 3 {
		t.Fatalf("nonZero() = %d, want 3", got)
	}

	sessionKey := a.makeSessionKey(&feishu.InboundMessage{ChatType: "group", ChatID: "chat", RootMessageID: "root", MessageID: "msg"})
	if sessionKey != "feishu:group:chat:root:root" {
		t.Fatalf("makeSessionKey(group) = %q", sessionKey)
	}
	sessionKey = a.makeSessionKey(&feishu.InboundMessage{ChatType: "p2p", ChatID: "chat", UserID: "user"})
	if sessionKey != "feishu:p2p:chat:user" {
		t.Fatalf("makeSessionKey(p2p) = %q", sessionKey)
	}
}

func TestSendCommandMenuAndStartupReadyNotifications(t *testing.T) {
	a, ff, _ := newTestApp(t)
	if err := a.sendCommandMenu(&feishu.InboundMessage{MessageID: "m-1", ChatType: "group"}); err != nil {
		t.Fatalf("sendCommandMenu() error = %v", err)
	}
	if len(ff.replyCards) != 1 {
		t.Fatalf("expected one reply card, got %d", len(ff.replyCards))
	}

	if err := a.store.UpsertSession(&state.Session{Key: "s1", ChatID: "chat-2"}); err != nil {
		t.Fatalf("UpsertSession(s1) error = %v", err)
	}
	if err := a.store.UpsertSession(&state.Session{Key: "s2", ChatID: "chat-1"}); err != nil {
		t.Fatalf("UpsertSession(s2) error = %v", err)
	}
	if err := a.store.UpsertSession(&state.Session{Key: "s3", ChatID: "chat-1"}); err != nil {
		t.Fatalf("UpsertSession(s3) error = %v", err)
	}
	a.sendStartupReadyNotifications()
	if len(ff.sentTexts) < 2 {
		t.Fatalf("expected startup notifications to known chats, got %+v", ff.sentTexts)
	}
}

func TestCommandWorkspaceAndCommandThreads(t *testing.T) {
	a, ff, fc := newTestApp(t)
	a.cfg.Workspaces = append(a.cfg.Workspaces, config.Workspace{ID: "alt", Name: "Alt", Cwd: t.TempDir(), ApprovalPolicy: "never", SandboxMode: "read-only"})
	msg := &feishu.InboundMessage{MessageID: "m-1", ChatID: "chat-1", ChatType: "group", UserID: "user-1"}

	if err := a.commandWorkspace(msg, []string{"list"}); err != nil {
		t.Fatalf("commandWorkspace(list) error = %v", err)
	}
	if len(ff.replyCards) == 0 {
		t.Fatalf("commandWorkspace(list) cards = %+v, want workspace menu card", ff.replyCards)
	}

	fc.callHook = func(_ context.Context, method string, _ any, out any) error {
		switch method {
		case "thread/list":
			*out.(*codexrpc.ThreadListResult) = codexrpc.ThreadListResult{}
			return nil
		case "thread/start":
			result := out.(*codexrpc.ThreadStartResult)
			result.Thread.ID = "thread-alt-new"
			result.Thread.Name = "Alt Thread"
			result.Thread.Preview = "Alt Preview"
			return nil
		default:
			t.Fatalf("unexpected codex method: %s", method)
			return nil
		}
	}
	if err := a.commandWorkspace(msg, []string{"use", "alt"}); err != nil {
		t.Fatalf("commandWorkspace(use) error = %v", err)
	}
	sess := a.store.GetSession(a.makeSessionKey(msg))
	if sess == nil || sess.WorkspaceID != "alt" {
		t.Fatalf("workspace switch did not persist session: %+v", sess)
	}
	if sess.ActiveThreadID != "thread-alt-new" || sess.ActiveThreadWorkspaceID != "alt" {
		t.Fatalf("workspace switch should auto-bind new thread: %+v", sess)
	}

	ff.replyCards = nil
	if err := a.commandWorkspace(msg, nil); err != nil {
		t.Fatalf("commandWorkspace(menu) error = %v", err)
	}
	if len(ff.replyCards) != 1 {
		t.Fatalf("expected workspace menu card, got %d", len(ff.replyCards))
	}
	if got := cardSelectStaticForTest(ff.replyCards[0]); len(got) != 1 {
		t.Fatalf("workspace menu selects = %+v, want 1 select", got)
	}

	ff.replyCards = nil
	if err := a.commandWorkspace(msg, []string{"sandbox"}); err != nil {
		t.Fatalf("commandWorkspace(sandbox) error = %v", err)
	}
	if len(ff.replyCards) != 1 {
		t.Fatalf("expected sandbox menu card, got %d", len(ff.replyCards))
	}

	ff.replyCards = nil
	if err := a.commandWorkspace(msg, []string{"policy"}); err != nil {
		t.Fatalf("commandWorkspace(policy) error = %v", err)
	}
	if len(ff.replyCards) != 1 {
		t.Fatalf("expected policy menu card, got %d", len(ff.replyCards))
	}

	ff.replyCards = nil
	if err := a.commandWorkspace(msg, []string{"new"}); err != nil {
		t.Fatalf("commandWorkspace(new) error = %v", err)
	}
	if len(ff.replyCards) != 1 {
		t.Fatalf("expected workspace new card, got %d", len(ff.replyCards))
	}

	fc.callHook = func(_ context.Context, method string, params any, out any) error {
		if method != "thread/list" {
			t.Fatalf("unexpected codex method: %s", method)
		}
		result := out.(*codexrpc.ThreadListResult)
		result.Data = nil
		return nil
	}
	ff.replyTexts = nil
	ff.replyCards = nil
	if err := a.commandThreads(msg, false); err != nil {
		t.Fatalf("commandThreads(empty) error = %v", err)
	}
	if len(ff.replyCards) == 0 {
		t.Fatalf("commandThreads(empty) card = %+v", ff.replyCards)
	}
	if body := cardMarkdownContent(t, ff.replyCards[len(ff.replyCards)-1]); !strings.Contains(body, "当前没有可切换的线程。") {
		t.Fatalf("commandThreads(empty) body = %q", body)
	}
}

func TestCompleteWorkspaceNewTextAndCommandNotifications(t *testing.T) {
	a, ff, fc := newTestApp(t)
	msg := &feishu.InboundMessage{MessageID: "m-1", ChatID: "chat-1", ChatType: "group", UserID: "user-1", Text: "repo /tmp/test-workspace Repo Name"}
	pending := &state.PendingRequest{ID: "req-1", FeishuMsgID: "card-1", SessionKey: a.makeSessionKey(msg)}
	if err := a.store.UpsertPending(pending); err != nil {
		t.Fatalf("UpsertPending() error = %v", err)
	}
	fc.callHook = func(_ context.Context, method string, _ any, out any) error {
		switch method {
		case "thread/list":
			*out.(*codexrpc.ThreadListResult) = codexrpc.ThreadListResult{}
			return nil
		case "thread/start":
			result := out.(*codexrpc.ThreadStartResult)
			result.Thread.ID = "thread-repo"
			result.Thread.Name = "Repo Thread"
			result.Thread.Preview = "Repo Preview"
			return nil
		default:
			return nil
		}
	}

	if err := a.completeWorkspaceNewText(msg, pending); err != nil {
		t.Fatalf("completeWorkspaceNewText() error = %v", err)
	}
	if config.FindWorkspace(a.cfg, "repo") == nil {
		t.Fatal("expected workspace to be appended to config")
	}
	if got := a.store.GetSession(a.makeSessionKey(msg)); got == nil || got.WorkspaceID != "repo" {
		t.Fatalf("workspace session after creation = %+v, want switched workspace", got)
	}
	if got := a.store.GetSession(a.makeSessionKey(msg)); got == nil || got.ActiveThreadID != "thread-repo" || got.ActiveThreadWorkspaceID != "repo" {
		t.Fatalf("workspace session should auto-bind thread after creation = %+v", got)
	}
	if len(ff.patchedCards) == 0 || len(ff.replyTexts) == 0 {
		t.Fatalf("expected workspace creation to patch card and reply, patches=%d replies=%d", len(ff.patchedCards), len(ff.replyTexts))
	}

	sessionKey := a.makeSessionKey(msg)
	sub := seedActiveSubmission(t, a, sessionKey, "thread-1", "turn-1")
	ff.sendCards = nil
	ff.replyTexts = nil
	fc.replyErrors = nil
	a.onCommandApproval(codexrpc.RequestEnvelope{ID: json.RawMessage(`"cmd-1"`), Params: json.RawMessage(`{"threadId":"thread-1","turnId":"turn-1","itemId":"item-1","command":"ls -la","cwd":"/repo","reason":"need approval"}`)})
	if len(ff.sendCards) == 0 {
		t.Fatal("expected command approval to send a card")
	}
	if got, _ := ff.sendCards[0]["schema"].(string); got != "2.0" {
		t.Fatalf("approval card schema = %#v, want 2.0", ff.sendCards[0]["schema"])
	}
	if got := cardMarkdownContent(t, ff.sendCards[0]); strings.Contains(got, `<at `) || !strings.Contains(got, "命令审批") || !strings.Contains(got, "ls -la") || !strings.Contains(got, "/repo") {
		t.Fatalf("command approval card body = %q", got)
	}
	if len(ff.replyTexts) != 0 {
		t.Fatalf("command approval should not send extra text, got replies=%+v", ff.replyTexts)
	}
	if pending := a.store.PendingByID("cmd-1"); pending == nil || pending.Kind != "command" {
		t.Fatalf("command approval pending = %+v, want command request", pending)
	}
	if refreshed := a.store.GetSubmission(sub.ID); refreshed.Status != "waiting_approval" {
		t.Fatalf("submission status = %q, want waiting_approval", refreshed.Status)
	}

	ff.sendCards = nil
	a.onPermissionsApproval(codexrpc.RequestEnvelope{ID: json.RawMessage(`"perm-1"`), Params: json.RawMessage(`{"threadId":"thread-1","turnId":"turn-1","itemId":"item-2","reason":"sandbox","permissions":{"mode":"write","network":true,"sandbox":{"type":"workspace-write"},"writable_roots":["/repo","/tmp/work"]}}`)})
	if len(ff.sendCards) == 0 {
		t.Fatal("expected permissions approval to send a card")
	}
	if got, _ := ff.sendCards[0]["schema"].(string); got != "2.0" {
		t.Fatalf("permissions card schema = %#v, want 2.0", ff.sendCards[0]["schema"])
	}
	if got := cardMarkdownContent(t, ff.sendCards[0]); strings.Contains(got, `<at `) || !strings.Contains(got, "权限审批") || !strings.Contains(got, "mode") || !strings.Contains(got, "network") || !strings.Contains(got, "/repo") {
		t.Fatalf("permissions approval card body = %q", got)
	}
	if len(ff.replyTexts) != 0 {
		t.Fatalf("permissions approval should not send extra text, got replies=%+v", ff.replyTexts)
	}

	ff.sendCards = nil
	a.onToolUserInput(codexrpc.RequestEnvelope{ID: json.RawMessage(`"input-1"`), Params: json.RawMessage(`{"threadId":"thread-1","turnId":"turn-1","itemId":"item-3","questions":[{"id":"q1","question":"Choose","options":[{"label":"A"},{"label":"B"}]}]}`)})
	if pending := a.store.PendingByID("input-1"); pending == nil || pending.Kind != "tool_request_user_input" {
		t.Fatalf("tool user input pending = %+v, want quick-pick request", pending)
	}

	ff.sendCards = nil
	a.onToolUserInput(codexrpc.RequestEnvelope{ID: json.RawMessage(`"input-2"`), Params: json.RawMessage(`{"threadId":"thread-1","turnId":"turn-1","itemId":"item-4","questions":[{"id":"q1","question":"A"},{"id":"q2","question":"B"}]}`)})
	if pending := a.store.PendingByID("input-2"); pending == nil || pending.Kind != "tool_request_user_input_form" {
		t.Fatalf("tool user input form pending = %+v, want form request", pending)
	}

	ff.sendCards = nil
	a.onMcpElicitationRequest(codexrpc.RequestEnvelope{ID: json.RawMessage(`"elicit-1"`), Params: json.RawMessage(`{"mode":"url","threadId":"thread-1","turnId":"turn-1","serverName":"srv","message":"visit","url":"https://example.test"}`)})
	if pending := a.store.PendingByID("elicit-1"); pending == nil || pending.Kind != "mcp_elicitation_url" {
		t.Fatalf("elicitation url pending = %+v, want url request", pending)
	}

	a.onMcpElicitationRequest(codexrpc.RequestEnvelope{ID: json.RawMessage(`"elicit-2"`), Params: json.RawMessage(`{"mode":"form","threadId":"thread-1","turnId":"turn-1","serverName":"srv","message":"fill","requestedSchema":{"properties":{"name":{"type":"string"}}}}`)})
	if pending := a.store.PendingByID("elicit-2"); pending == nil || pending.Kind != "mcp_elicitation_form" {
		t.Fatalf("elicitation form pending = %+v, want form request", pending)
	}
}

func TestCommandWorkspaceCloneCreatesAndSwitchesWorkspace(t *testing.T) {
	a, ff, fc := newTestApp(t)
	baseDir := t.TempDir()
	currentDir := filepath.Join(baseDir, "current")
	if err := os.MkdirAll(currentDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(currentDir) error = %v", err)
	}
	a.cfg.Workspaces[0].Cwd = currentDir

	origClone := workspaceGitClone
	defer func() { workspaceGitClone = origClone }()

	var gotRepoURL string
	var gotTargetDir string
	workspaceGitClone = func(_ context.Context, repoURL, targetDir string) error {
		gotRepoURL = repoURL
		gotTargetDir = targetDir
		return os.MkdirAll(filepath.Join(targetDir, ".git"), 0o755)
	}

	fc.callHook = func(_ context.Context, method string, _ any, out any) error {
		switch method {
		case "thread/list":
			*out.(*codexrpc.ThreadListResult) = codexrpc.ThreadListResult{}
			return nil
		case "thread/start":
			result := out.(*codexrpc.ThreadStartResult)
			result.Thread.ID = "thread-clone"
			result.Thread.Name = "Clone Thread"
			result.Thread.Preview = "Clone Preview"
			return nil
		default:
			return nil
		}
	}

	msg := &feishu.InboundMessage{MessageID: "m-1", ChatID: "chat-1", ChatType: "group", UserID: "user-1"}
	repoURL := "git@github.com:example/repo.git"
	if err := a.commandWorkspace(msg, []string{"clone", repoURL}); err != nil {
		t.Fatalf("commandWorkspace(clone) error = %v", err)
	}

	wantTargetDir := filepath.Join(baseDir, "repo")
	if gotRepoURL != repoURL {
		t.Fatalf("workspaceGitClone repoURL = %q, want %q", gotRepoURL, repoURL)
	}
	if gotTargetDir != wantTargetDir {
		t.Fatalf("workspaceGitClone targetDir = %q, want %q", gotTargetDir, wantTargetDir)
	}
	if ws := config.FindWorkspace(a.cfg, "repo"); ws == nil || ws.Cwd != wantTargetDir {
		t.Fatalf("cloned workspace = %+v, want cwd %q", ws, wantTargetDir)
	}
	if sess := a.store.GetSession(a.makeSessionKey(msg)); sess == nil || sess.WorkspaceID != "repo" || sess.ActiveThreadID != "thread-clone" || sess.ActiveThreadWorkspaceID != "repo" {
		t.Fatalf("session after clone = %+v", sess)
	}
	if len(ff.replyTexts) == 0 || !strings.Contains(ff.replyTexts[0], "已 clone 仓库并切换到工作区 repo") || !strings.Contains(ff.replyTexts[0], wantTargetDir) {
		t.Fatalf("workspace clone replyTexts = %+v", ff.replyTexts)
	}
}

func TestHandleServerRequestAndAppNotificationsErrorPaths(t *testing.T) {
	a, _, fc := newTestApp(t)

	a.handleServerRequest(codexrpc.RequestEnvelope{ID: json.RawMessage(`"req-1"`), Method: "unknown"})
	if len(fc.replyErrors) == 0 || fc.replyErrors[0].code != -32601 {
		t.Fatalf("handleServerRequest(unknown) replyErrors = %+v", fc.replyErrors)
	}

	fc.replyErrors = nil
	a.onCommandApproval(codexrpc.RequestEnvelope{ID: json.RawMessage(`"bad-cmd"`), Params: json.RawMessage(`{`)})
	if len(fc.replyErrors) == 0 || fc.replyErrors[0].code != -32602 {
		t.Fatalf("onCommandApproval(invalid params) = %+v", fc.replyErrors)
	}

	fc.replyErrors = nil
	a.onMcpElicitationRequest(codexrpc.RequestEnvelope{ID: json.RawMessage(`"bad-elicit"`), Params: json.RawMessage(`{"mode":"other"}`)})
	if len(fc.replyErrors) == 0 || fc.replyErrors[0].code != -32601 {
		t.Fatalf("onMcpElicitationRequest(unsupported mode) = %+v", fc.replyErrors)
	}

	a.handleFeishuRecall(nil)
	a.handleFeishuReaction(&feishu.MessageReaction{EmojiType: "smile"})
}

func TestApprovalMentionIncludedOutsideGroupChats(t *testing.T) {
	a, ff, _ := newTestApp(t)
	sessionKey := "sess-p2p"
	if err := a.store.UpsertSession(&state.Session{
		Key:                sessionKey,
		WorkspaceID:        a.cfg.Workspaces[0].ID,
		ActiveThreadID:     "thread-p2p",
		ActiveTurnID:       "turn-p2p",
		ActiveSubmissionID: "sub-p2p",
		OwnerUserID:        "user-1",
		ChatID:             "chat-p2p",
		ChatType:           "p2p",
		Status:             "running",
	}); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}
	subID, err := a.store.CreateSubmission(&state.Submission{
		ID:               "sub-p2p",
		SessionKey:       sessionKey,
		WorkspaceID:      a.cfg.Workspaces[0].ID,
		ThreadID:         "thread-p2p",
		TurnID:           "turn-p2p",
		UserID:           "user-1",
		ChatID:           "chat-p2p",
		TriggerMessageID: "trigger-p2p",
		Status:           "running",
	})
	if err != nil {
		t.Fatalf("CreateSubmission() error = %v", err)
	}

	a.sendApprovalCard("command", json.RawMessage(`"req-p2p"`), "thread-p2p", "turn-p2p", "item-1", "命令审批\n`pwd`")

	if len(ff.sendCards) != 1 {
		t.Fatalf("approval card count = %d, want 1", len(ff.sendCards))
	}
	if got := cardMarkdownContent(t, ff.sendCards[0]); !strings.Contains(got, "命令审批") || strings.Contains(got, `<at `) {
		t.Fatalf("approval card in p2p body = %q", got)
	}
	if pending := a.store.PendingByID("req-p2p"); pending == nil || pending.FeishuMsgID == "" {
		t.Fatalf("pending approval = %+v", pending)
	}
	if got := a.store.GetSubmission(subID); got == nil || got.Status != "waiting_approval" {
		t.Fatalf("submission status = %+v, want waiting_approval", got)
	}
}

func TestActionWrappersAndDispatchFallbacks(t *testing.T) {
	a, _, fc := newTestApp(t)
	a.cfg.Feishu.DebugAllowFrom = []string{"user-1"}
	prevLevel := runtimeLogLevelText()
	t.Cleanup(func() {
		_ = logcontrol.SetName(prevLevel)
		if a.cfg != nil {
			a.cfg.Log.Level = runtimeLogLevelText()
		}
	})
	fc.callHook = func(_ context.Context, method string, _ any, out any) error {
		if method == "thread/fork" {
			result := out.(*codexrpc.ThreadStartResult)
			result.Thread.ID = "thread-fork"
			result.Thread.Name = "Forked"
			result.Thread.Preview = "fork preview"
		}
		return nil
	}
	action := &feishu.CardAction{
		UserID:    "user-1",
		ChatID:    "chat-1",
		MessageID: "msg-1",
		ActionValue: map[string]any{
			"session_key": "feishu:group:chat-1:root:root-1",
		},
	}

	if resp, err := a.dispatchCardAction(nil); err != nil || resp == nil {
		t.Fatalf("dispatchCardAction(nil) = %#v, %v", resp, err)
	}
	if resp, err := a.dispatchCardAction(&feishu.CardAction{Name: "unknown"}); err != nil || resp.Toast == nil || resp.Toast.Type != "warning" {
		t.Fatalf("dispatchCardAction(unknown) = %#v, %v", resp, err)
	}

	for name, fn := range map[string]func() (*callback.CardActionTriggerResponse, error){
		"menu.root": func() (*callback.CardActionTriggerResponse, error) {
			return a.completeMenuRoot(action, action.ActionValue["session_key"].(string))
		},
		"menu.tools": func() (*callback.CardActionTriggerResponse, error) {
			return a.completeMenuTools(action, action.ActionValue["session_key"].(string))
		},
		"menu.thread": func() (*callback.CardActionTriggerResponse, error) {
			return a.completeMenuThread(action, action.ActionValue["session_key"].(string))
		},
		"menu.download": func() (*callback.CardActionTriggerResponse, error) {
			const downloadSessionKey = "feishu:group:chat-1:root:download-root"
			if err := a.store.UpsertSession(&state.Session{
				Key:         downloadSessionKey,
				WorkspaceID: a.cfg.Workspaces[0].ID,
			}); err != nil {
				t.Fatalf("UpsertSession(download) error = %v", err)
			}
			return a.completeMenuDownload(&feishu.CardAction{
				UserID:      "user-1",
				ChatID:      "chat-1",
				MessageID:   "msg-download",
				ActionValue: map[string]any{"session_key": downloadSessionKey, "parent_action": "menu.tools"},
			}, downloadSessionKey)
		},
		"menu.fork": func() (*callback.CardActionTriggerResponse, error) {
			const forkSessionKey = "feishu:group:chat-1:root:fork-root"
			if err := a.store.UpsertSession(&state.Session{
				Key:                     forkSessionKey,
				WorkspaceID:             a.cfg.Workspaces[0].ID,
				ActiveThreadID:          "thread-1",
				ActiveThreadWorkspaceID: a.cfg.Workspaces[0].ID,
			}); err != nil {
				t.Fatalf("UpsertSession(fork) error = %v", err)
			}
			return a.completeMenuFork(&feishu.CardAction{ActionValue: map[string]any{
				"session_key":   forkSessionKey,
				"parent_action": "menu.thread",
			}}, forkSessionKey)
		},
		"menu.compact": func() (*callback.CardActionTriggerResponse, error) {
			const compactSessionKey = "feishu:group:chat-1:root:compact-root"
			if err := a.store.UpsertSession(&state.Session{
				Key:                     compactSessionKey,
				WorkspaceID:             a.cfg.Workspaces[0].ID,
				ActiveThreadID:          "thread-1",
				ActiveThreadWorkspaceID: a.cfg.Workspaces[0].ID,
			}); err != nil {
				t.Fatalf("UpsertSession(compact) error = %v", err)
			}
			return a.completeMenuCompact(&feishu.CardAction{ActionValue: map[string]any{
				"session_key":   compactSessionKey,
				"parent_action": "menu.tools",
			}}, compactSessionKey)
		},
		"menu.group.model": func() (*callback.CardActionTriggerResponse, error) {
			return a.completeMenuGroupModel(action, action.ActionValue["session_key"].(string))
		},
		"menu.group.system": func() (*callback.CardActionTriggerResponse, error) {
			return a.completeMenuGroupSystem(action, action.ActionValue["session_key"].(string))
		},
		"menu.quiet": func() (*callback.CardActionTriggerResponse, error) {
			return a.completeMenuQuiet(action, action.ActionValue["session_key"].(string))
		},
		"menu.fast": func() (*callback.CardActionTriggerResponse, error) {
			return a.completeMenuFast(action, action.ActionValue["session_key"].(string))
		},
		"menu.model": func() (*callback.CardActionTriggerResponse, error) {
			return a.completeMenuModel(action, action.ActionValue["session_key"].(string))
		},
		"menu.status": func() (*callback.CardActionTriggerResponse, error) {
			return a.completeMenuStatus(action, action.ActionValue["session_key"].(string))
		},
		"menu.debug": func() (*callback.CardActionTriggerResponse, error) {
			return a.completeMenuDebug(action, action.ActionValue["session_key"].(string))
		},
		"menu.debug.logs": func() (*callback.CardActionTriggerResponse, error) {
			return a.completeMenuDebugLogs(action, action.ActionValue["session_key"].(string))
		},
		"menu.help": func() (*callback.CardActionTriggerResponse, error) {
			return a.completeMenuHelp(action, action.ActionValue["session_key"].(string))
		},
		"menu.history": func() (*callback.CardActionTriggerResponse, error) {
			return a.completeMenuHistory(action, action.ActionValue["session_key"].(string))
		},
		"menu.skills": func() (*callback.CardActionTriggerResponse, error) {
			return a.completeMenuSkills(action, action.ActionValue["session_key"].(string))
		},
		"menu.workspace": func() (*callback.CardActionTriggerResponse, error) {
			return a.completeMenuWorkspace(action, action.ActionValue["session_key"].(string))
		},
		"workspace.new": func() (*callback.CardActionTriggerResponse, error) {
			return a.completeWorkspaceNew(action, action.ActionValue["session_key"].(string))
		},
		"workspace.sandbox.menu": func() (*callback.CardActionTriggerResponse, error) {
			return a.completeWorkspaceSandboxMenu(action, action.ActionValue["session_key"].(string))
		},
		"workspace.policy.menu": func() (*callback.CardActionTriggerResponse, error) {
			return a.completeWorkspacePolicyMenu(action, action.ActionValue["session_key"].(string))
		},
		"thread.sandbox.menu": func() (*callback.CardActionTriggerResponse, error) {
			return a.completeThreadSandboxMenu(action, action.ActionValue["session_key"].(string))
		},
		"thread.policy.menu": func() (*callback.CardActionTriggerResponse, error) {
			return a.completeThreadPolicyMenu(action, action.ActionValue["session_key"].(string))
		},
	} {
		resp, err := fn()
		if err != nil || resp == nil || resp.Toast == nil {
			t.Fatalf("%s = %#v, %v", name, resp, err)
		}
		wantToastType := "info"
		if name == "thread.sandbox.menu" || name == "thread.policy.menu" || name == "menu.history" {
			wantToastType = "warning"
		}
		if name == "menu.compact" || name == "menu.fork" || name == "menu.debug" {
			wantToastType = "success"
		}
		if name == "menu.debug.logs" {
			wantToastType = "info"
		}
		if resp.Toast.Type != wantToastType {
			t.Fatalf("%s toast type = %q, want %s", name, resp.Toast.Type, wantToastType)
		}
		switch name {
		case "menu.root", "menu.tools", "menu.thread", "menu.download", "menu.fork", "menu.compact", "menu.group.model", "menu.group.system", "menu.quiet", "menu.fast", "menu.model", "menu.status", "menu.debug", "menu.debug.logs", "menu.help", "menu.skills", "menu.workspace", "workspace.new", "workspace.sandbox.menu", "workspace.policy.menu":
			if resp.Card == nil {
				t.Fatalf("%s should update current card", name)
			}
		}
	}
}

func TestWorkspaceMenuCardsIncludeBackNavigation(t *testing.T) {
	a, _, _ := newTestApp(t)
	a.cfg.Workspaces = append(a.cfg.Workspaces, config.Workspace{ID: "alt", Name: "Alt", Cwd: t.TempDir(), ApprovalPolicy: "never", SandboxMode: "read-only"})
	sessionKey := "feishu:p2p:chat:user"
	if err := a.store.UpsertSession(&state.Session{Key: sessionKey, WorkspaceID: "alt"}); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}

	workspaceCard := a.renderWorkspaceMenuCard(sessionKey)
	workspaceActions := cardButtonsForTest(workspaceCard)
	foundBackToMenu := false
	for _, action := range workspaceActions {
		value, _ := action["value"].(map[string]any)
		if len(value) == 0 {
			behaviors, _ := action["behaviors"].([]map[string]any)
			if len(behaviors) > 0 {
				value, _ = behaviors[0]["value"].(map[string]any)
			}
		}
		if value["action"] == "menu.root" {
			foundBackToMenu = true
		}
	}
	if !foundBackToMenu {
		t.Fatalf("workspace menu missing back button: %+v", workspaceActions)
	}

	sandboxCard, err := a.renderWorkspaceSandboxMenuCard(sessionKey)
	if err != nil {
		t.Fatalf("renderWorkspaceSandboxMenuCard() error = %v", err)
	}
	sandboxActions := cardButtonsForTest(sandboxCard)
	foundBackToWorkspace := false
	for _, action := range sandboxActions {
		value, _ := action["value"].(map[string]any)
		if len(value) == 0 {
			behaviors, _ := action["behaviors"].([]map[string]any)
			if len(behaviors) > 0 {
				value, _ = behaviors[0]["value"].(map[string]any)
			}
		}
		if value["action"] == "menu.workspace" {
			foundBackToWorkspace = true
		}
	}
	if !foundBackToWorkspace {
		t.Fatalf("workspace sandbox menu missing back button: %+v", sandboxActions)
	}
}

func TestMenuCardsShowBreadcrumbsAndSubmenuIndicators(t *testing.T) {
	a, _, _ := newTestApp(t)
	sessionKey := "feishu:p2p:chat:user"

	rootCard := a.renderCommandMenuCard(sessionKey)
	if body := cardMarkdownContent(t, rootCard); !strings.Contains(body, "当前位置：主菜单") {
		t.Fatalf("root menu missing breadcrumb: %q", body)
	}
	rootActions := cardButtonsForTest(rootCard)
	for _, action := range rootActions {
		text, _ := action["text"].(map[string]any)
		label, _ := text["content"].(string)
		if !strings.HasSuffix(label, "›") {
			t.Fatalf("root submenu label missing indicator: %q", label)
		}
	}

	toolsCard := a.renderToolsMenuCard(sessionKey)
	if body := cardMarkdownContent(t, toolsCard); !strings.Contains(body, "当前位置：主菜单 / 常用工具") {
		t.Fatalf("tools menu missing breadcrumb: %q", body)
	}
	contextActions := cardButtonsForTest(toolsCard)
	indicatorByAction := map[string]bool{}
	labelByAction := map[string]string{}
	for _, action := range contextActions {
		text, _ := action["text"].(map[string]any)
		label, _ := text["content"].(string)
		value, _ := action["value"].(map[string]any)
		if len(value) == 0 {
			behaviors, _ := action["behaviors"].([]map[string]any)
			if len(behaviors) > 0 {
				value, _ = behaviors[0]["value"].(map[string]any)
			}
		}
		actionName, _ := value["action"].(string)
		indicatorByAction[actionName] = strings.HasSuffix(label, "›")
		labelByAction[actionName] = label
	}
	if indicatorByAction["menu.quiet"] != true || indicatorByAction["menu.history"] != true || indicatorByAction["menu.usage"] != true {
		t.Fatalf("expected tools submenu indicators, got %#v", indicatorByAction)
	}
	if indicatorByAction["menu.interrupt"] || indicatorByAction["menu.download"] || indicatorByAction["menu.compact"] {
		t.Fatalf("direct tools commands should not show submenu indicator, got %#v", indicatorByAction)
	}
	if !strings.Contains(labelByAction["menu.quiet"], "/quiet") || !strings.Contains(labelByAction["menu.history"], "/history") || !strings.Contains(labelByAction["menu.usage"], "/usage") || !strings.Contains(labelByAction["menu.interrupt"], "/stop") || !strings.Contains(labelByAction["menu.download"], "/download") || !strings.Contains(labelByAction["menu.compact"], "/compact") {
		t.Fatalf("expected real command labels in tools menu, got %#v", labelByAction)
	}

	modelCard := a.renderModelMenuCard(sessionKey)
	modelActions := cardButtonsForTest(modelCard)
	modelLabelByAction := map[string]string{}
	for _, action := range modelActions {
		text, _ := action["text"].(map[string]any)
		label, _ := text["content"].(string)
		value, _ := action["value"].(map[string]any)
		if len(value) == 0 {
			behaviors, _ := action["behaviors"].([]map[string]any)
			if len(behaviors) > 0 {
				value, _ = behaviors[0]["value"].(map[string]any)
			}
		}
		actionName, _ := value["action"].(string)
		modelLabelByAction[actionName] = label
	}
	if !strings.Contains(modelLabelByAction["menu.model"], "/model") || !strings.Contains(modelLabelByAction["menu.fast"], "/fast config") {
		t.Fatalf("expected real command labels in model menu, got %#v", modelLabelByAction)
	}

	helpCard := a.renderHelpCard(sessionKey)
	if body := cardMarkdownContent(t, helpCard); !strings.Contains(body, "当前位置：主菜单 / 系统运维 / 命令帮助") {
		t.Fatalf("help card missing breadcrumb: %q", body)
	}
}

func TestApprovalAndUserInputActions(t *testing.T) {
	a, _, fc := newTestApp(t)
	sessionKey := "sess-1"
	sub := seedActiveSubmission(t, a, sessionKey, "thread-1", "turn-1")

	for _, req := range []*state.PendingRequest{
		{ID: "command-1", Kind: "command", SessionKey: sessionKey, ThreadID: "thread-1", TurnID: "turn-1", OwnerUserID: "user-1", Status: "pending", PayloadJSON: mustJSON(map[string]any{"body": "命令审批\n`ls`\nneed approval"})},
		{ID: "file-1", Kind: "file", SessionKey: sessionKey, ThreadID: "thread-1", TurnID: "turn-1", OwnerUserID: "user-1", Status: "pending", PayloadJSON: mustJSON(map[string]any{"body": "文件变更审批\nneed review"})},
		{ID: "perm-1", Kind: "permissions", SessionKey: sessionKey, ThreadID: "thread-1", TurnID: "turn-1", OwnerUserID: "user-1", Status: "pending", PayloadJSON: mustJSON(map[string]any{"body": "权限审批\n需要写权限", "permissions": map[string]any{"mode": "write"}})},
		{ID: "input-1", Kind: "tool_request_user_input", SessionKey: sessionKey, ThreadID: "thread-1", TurnID: "turn-1", OwnerUserID: "user-1", Status: "pending"},
	} {
		if err := a.store.UpsertPending(req); err != nil {
			t.Fatalf("UpsertPending(%s) error = %v", req.ID, err)
		}
	}

	action := &feishu.CardAction{UserID: "user-1", ActionValue: map[string]any{"request_id": "command-1"}}
	resp, err := a.completeApprovalAction(action, "approval.command.accept_session")
	if err != nil || resp.Toast == nil || resp.Toast.Type != "success" {
		t.Fatalf("completeApprovalAction(command) = %#v, %v", resp, err)
	}
	if len(fc.replies) == 0 {
		t.Fatal("expected command approval to reply to codex")
	}
	if pending := a.store.PendingByID("command-1"); pending == nil || pending.Status != "replied" {
		t.Fatalf("command pending = %+v, want replied", pending)
	}
	if got := string(fc.replies[0].id); got != `"command-1"` {
		t.Fatalf("codex reply id = %s, want %q", got, `"command-1"`)
	}
	if resp.Card == nil {
		t.Fatal("expected command approval response card")
	}
	cardData, _ := resp.Card.Data.(map[string]any)
	if got := cardMarkdownContent(t, cardData); !strings.Contains(got, "已允许本会话执行") || !strings.Contains(got, "命令审批") || !strings.Contains(got, "`ls`") {
		t.Fatalf("command approval resolved card = %q", got)
	}

	if err := a.store.UpsertPending(&state.PendingRequest{
		ID:          "command-2",
		Kind:        "command",
		SessionKey:  sessionKey,
		ThreadID:    "thread-1",
		TurnID:      "turn-1",
		OwnerUserID: "user-1",
		Status:      "pending",
		PayloadJSON: mustJSON(map[string]any{"request": map[string]any{
			"command": "git status --short",
			"cwd":     "/workspace/feidex",
			"reason":  "inspect working tree",
		}}),
	}); err != nil {
		t.Fatalf("UpsertPending(command-2) error = %v", err)
	}
	resp, err = a.completeApprovalAction(&feishu.CardAction{UserID: "user-1", ActionValue: map[string]any{"request_id": "command-2"}}, "approval.command.accept")
	if err != nil || resp.Toast == nil || resp.Toast.Type != "success" {
		t.Fatalf("completeApprovalAction(command-2) = %#v, %v", resp, err)
	}
	if resp.Card == nil {
		t.Fatal("expected command approval response card from raw request payload")
	}
	cardData, _ = resp.Card.Data.(map[string]any)
	if got := cardMarkdownContent(t, cardData); !strings.Contains(got, "git status --short") || !strings.Contains(got, "/workspace/feidex") {
		t.Fatalf("command approval resolved-from-request card = %q", got)
	}

	resp, err = a.completeApprovalAction(&feishu.CardAction{UserID: "user-1", ActionValue: map[string]any{"request_id": "file-1"}}, "approval.file.decline")
	if err != nil || resp.Toast == nil || resp.Toast.Type != "success" {
		t.Fatalf("completeApprovalAction(file) = %#v, %v", resp, err)
	}
	if resp.Card == nil {
		t.Fatal("expected file approval response card")
	}
	cardData, _ = resp.Card.Data.(map[string]any)
	if got := cardMarkdownContent(t, cardData); !strings.Contains(got, "已拒绝") || !strings.Contains(got, "文件变更审批") {
		t.Fatalf("file approval resolved card = %q", got)
	}

	if err := a.store.UpsertPending(&state.PendingRequest{
		ID:          "file-2",
		Kind:        "file",
		SessionKey:  sessionKey,
		ThreadID:    "thread-1",
		TurnID:      "turn-1",
		OwnerUserID: "user-1",
		Status:      "pending",
		PayloadJSON: mustJSON(map[string]any{"request": map[string]any{
			"reason": "need review",
			"changes": []map[string]any{
				{"path": "internal/app/actions.go", "kind": "modified"},
				{"path": "README.md", "kind": "added"},
			},
		}}),
	}); err != nil {
		t.Fatalf("UpsertPending(file-2) error = %v", err)
	}
	resp, err = a.completeApprovalAction(&feishu.CardAction{UserID: "user-1", ActionValue: map[string]any{"request_id": "file-2"}}, "approval.file.accept")
	if err != nil || resp.Toast == nil || resp.Toast.Type != "success" {
		t.Fatalf("completeApprovalAction(file-2) = %#v, %v", resp, err)
	}
	if resp.Card == nil {
		t.Fatal("expected file approval response card from raw request payload")
	}
	cardData, _ = resp.Card.Data.(map[string]any)
	if got := cardMarkdownContent(t, cardData); !strings.Contains(got, "internal/app/actions.go") || !strings.Contains(got, "README.md") {
		t.Fatalf("file approval resolved-from-request card = %q", got)
	}

	resp, err = a.completeApprovalAction(&feishu.CardAction{UserID: "user-1", ActionValue: map[string]any{"request_id": "perm-1"}}, "approval.permissions.accept_session")
	if err != nil || resp.Toast == nil || resp.Toast.Type != "success" {
		t.Fatalf("completeApprovalAction(permissions) = %#v, %v", resp, err)
	}
	if refreshed := a.store.GetSubmission(sub.ID); refreshed.Status != "running" {
		t.Fatalf("submission status after permissions reply = %q, want running", refreshed.Status)
	}

	if err := a.store.UpsertPending(&state.PendingRequest{
		ID:          "perm-2",
		Kind:        "permissions",
		SessionKey:  sessionKey,
		ThreadID:    "thread-1",
		TurnID:      "turn-1",
		OwnerUserID: "user-1",
		Status:      "pending",
		PayloadJSON: mustJSON(map[string]any{
			"request": map[string]any{
				"reason": "need write access",
				"permissions": map[string]any{
					"mode":           "write",
					"networkAccess":  false,
					"writable_roots": []string{"/workspace"},
				},
			},
		}),
	}); err != nil {
		t.Fatalf("UpsertPending(perm-2) error = %v", err)
	}
	resp, err = a.completeApprovalAction(&feishu.CardAction{UserID: "user-1", ActionValue: map[string]any{"request_id": "perm-2"}}, "approval.permissions.accept_turn")
	if err != nil || resp.Toast == nil || resp.Toast.Type != "success" {
		t.Fatalf("completeApprovalAction(perm-2) = %#v, %v", resp, err)
	}
	if resp.Card == nil {
		t.Fatal("expected permissions response card from raw request payload")
	}
	cardData, _ = resp.Card.Data.(map[string]any)
	if got := cardMarkdownContent(t, cardData); !strings.Contains(got, "mode") || !strings.Contains(got, "/workspace") || !strings.Contains(got, "network") {
		t.Fatalf("permissions resolved-from-request card = %q", got)
	}

	resp, err = a.completeUserInputAnswer(&feishu.CardAction{
		UserID:      "user-1",
		ActionValue: map[string]any{"request_id": "input-1", "question_id": "q-1", "answer": "A"},
	})
	if err != nil || resp.Toast == nil || resp.Toast.Type != "success" {
		t.Fatalf("completeUserInputAnswer() = %#v, %v", resp, err)
	}
	if pending := a.store.PendingByID("input-1"); pending == nil || pending.Status != "replied" {
		t.Fatalf("user input pending = %+v, want replied", pending)
	}

	if got := a.approvalDecisionText("approval.command.accept"); got != "已允许本次执行" {
		t.Fatalf("approvalDecisionText(command.accept) = %q", got)
	}
	if got := a.approvalDecisionText("approval.command.cancel"); got != "已拒绝并中断任务" {
		t.Fatalf("approvalDecisionText(command.cancel) = %q", got)
	}
	if got := a.approvalDecisionText("approval.permissions.accept_session"); got != "已授权本会话权限请求" {
		t.Fatalf("approvalDecisionText(permissions.accept_session) = %q", got)
	}
	if got := a.approvalDecisionText("other"); got != "已拒绝" {
		t.Fatalf("approvalDecisionText(default) = %q", got)
	}
}

func TestCompleteApprovalActionSupportsExtendedCommandDecisions(t *testing.T) {
	a, _, fc := newTestApp(t)
	fc.replies = nil
	resp, err := a.completeApprovalAction(&feishu.CardAction{
		UserID:      "user-1",
		ActionValue: map[string]any{"request_id": "missing"},
	}, "approval.command.decline")
	if err != nil || resp == nil || resp.Toast == nil || resp.Toast.Type != "warning" {
		t.Fatalf("completeApprovalAction(expired) = %#v, %v", resp, err)
	}
	if len(fc.replies) != 0 {
		t.Fatalf("expired command approval should not reply, got %d replies", len(fc.replies))
	}
}

func TestCompleteApprovalActionSupportsFileCancelDecision(t *testing.T) {
	a, _, fc := newTestApp(t)
	if err := a.store.UpsertPending(&state.PendingRequest{
		ID:          "file-cancel",
		Kind:        "file",
		SessionKey:  "sess-1",
		ThreadID:    "thread-1",
		TurnID:      "turn-1",
		OwnerUserID: "user-1",
		Status:      "pending",
		PayloadJSON: mustJSON(map[string]any{"body": "文件变更审批\nneed review"}),
	}); err != nil {
		t.Fatalf("UpsertPending(file-cancel) error = %v", err)
	}

	resp, err := a.completeApprovalAction(&feishu.CardAction{
		UserID:      "user-1",
		ActionValue: map[string]any{"request_id": "file-cancel"},
	}, "approval.file.cancel")
	if err != nil || resp == nil || resp.Toast == nil || resp.Toast.Type != "success" {
		t.Fatalf("completeApprovalAction(file.cancel) = %#v, %v", resp, err)
	}
	if len(fc.replies) != 1 {
		t.Fatalf("file cancel reply count = %d, want 1", len(fc.replies))
	}
	reply, _ := fc.replies[0].result.(map[string]any)
	if got := strings.TrimSpace(stringValue(reply["decision"])); got != "cancel" {
		t.Fatalf("file cancel decision = %q, want cancel", got)
	}
	cardData, _ := resp.Card.Data.(map[string]any)
	if got := cardMarkdownContent(t, cardData); !strings.Contains(got, "已拒绝并中断任务") || !strings.Contains(got, "该 turn 会立即中断") {
		t.Fatalf("file cancel resolved card = %q", got)
	}
}

func TestCompleteApprovalActionPreservesNumericRequestID(t *testing.T) {
	a, _, fc := newTestApp(t)
	sessionKey := "sess-1"
	if err := a.store.UpsertSession(&state.Session{
		Key:                sessionKey,
		WorkspaceID:        a.cfg.Workspaces[0].ID,
		ActiveThreadID:     "thread-1",
		ActiveTurnID:       "turn-1",
		ActiveSubmissionID: "sub-1",
		Status:             "waiting_approval",
	}); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}
	if _, err := a.store.CreateSubmission(&state.Submission{
		ID:          "sub-1",
		SessionKey:  sessionKey,
		WorkspaceID: a.cfg.Workspaces[0].ID,
		ThreadID:    "thread-1",
		TurnID:      "turn-1",
		Status:      "waiting_approval",
	}); err != nil {
		t.Fatalf("CreateSubmission() error = %v", err)
	}
	if err := a.store.UpsertPending(&state.PendingRequest{
		ID:           "0",
		RequestIDRaw: "0",
		Kind:         "command",
		SessionKey:   sessionKey,
		ThreadID:     "thread-1",
		TurnID:       "turn-1",
		OwnerUserID:  "user-1",
		Status:       "pending",
	}); err != nil {
		t.Fatalf("UpsertPending() error = %v", err)
	}

	resp, err := a.completeApprovalAction(&feishu.CardAction{
		UserID:      "user-1",
		ActionValue: map[string]any{"request_id": "0"},
	}, "approval.command.accept")
	if err != nil || resp == nil || resp.Toast == nil || resp.Toast.Type != "success" {
		t.Fatalf("completeApprovalAction() = %#v, %v", resp, err)
	}
	if len(fc.replies) != 1 {
		t.Fatalf("reply count = %d, want 1", len(fc.replies))
	}
	if got := string(fc.replies[0].id); got != "0" {
		t.Fatalf("codex reply id = %s, want numeric 0", got)
	}
}

func TestCompleteApprovalActionKeepsPendingWhenCodexReplyFails(t *testing.T) {
	a, _, fc := newTestApp(t)
	sessionKey := "sess-1"
	if err := a.store.UpsertSession(&state.Session{
		Key:                sessionKey,
		WorkspaceID:        a.cfg.Workspaces[0].ID,
		ActiveThreadID:     "thread-1",
		ActiveTurnID:       "turn-1",
		ActiveSubmissionID: "sub-1",
		Status:             "waiting_approval",
	}); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}
	if _, err := a.store.CreateSubmission(&state.Submission{
		ID:          "sub-1",
		SessionKey:  sessionKey,
		WorkspaceID: a.cfg.Workspaces[0].ID,
		ThreadID:    "thread-1",
		TurnID:      "turn-1",
		Status:      "waiting_approval",
	}); err != nil {
		t.Fatalf("CreateSubmission() error = %v", err)
	}
	if err := a.store.UpsertPending(&state.PendingRequest{
		ID:          "command-1",
		Kind:        "command",
		SessionKey:  sessionKey,
		ThreadID:    "thread-1",
		TurnID:      "turn-1",
		OwnerUserID: "user-1",
		Status:      "pending",
	}); err != nil {
		t.Fatalf("UpsertPending() error = %v", err)
	}
	fc.replyErr = errors.New("write failed")

	resp, err := a.completeApprovalAction(&feishu.CardAction{
		UserID:      "user-1",
		ActionValue: map[string]any{"request_id": "command-1"},
	}, "approval.command.accept")
	if err != nil {
		t.Fatalf("completeApprovalAction() error = %v", err)
	}
	if resp == nil || resp.Toast == nil || resp.Toast.Type != "warning" {
		t.Fatalf("completeApprovalAction() = %#v, want warning toast", resp)
	}
	if pending := a.store.PendingByID("command-1"); pending == nil || pending.Status != "pending" {
		t.Fatalf("pending after failed reply = %+v, want pending", pending)
	}
	if sub := a.store.GetSubmission("sub-1"); sub == nil || sub.Status != "waiting_approval" {
		t.Fatalf("submission after failed reply = %+v, want waiting_approval", sub)
	}
}

func TestCommandUpgradeShowsConfirmationForNewVersion(t *testing.T) {
	origRelease := newReleaseClient
	origManager := newDaemonManager
	origVersion := currentVersion
	origGOARCH := currentGOARCH
	defer func() {
		newReleaseClient = origRelease
		newDaemonManager = origManager
		currentVersion = origVersion
		currentGOARCH = origGOARCH
	}()

	a, ff, _ := newTestApp(t)
	newReleaseClient = func() releaseClient {
		return &fakeReleaseClient{info: &release.ReleaseInfo{
			Version:        "v0.2.0",
			HTMLURL:        "https://example.test/releases/v0.2.0",
			BinaryName:     "feidex-linux-aarch64",
			BinaryURL:      "https://github.com/example/feidex-linux-aarch64",
			ExpectedSHA256: "abc123",
		}}
	}
	newDaemonManager = func(string) (daemon.Manager, error) {
		return &fakeDaemonManagerForApp{status: &daemon.Status{Installed: true, Running: true, PID: os.Getpid()}}, nil
	}
	currentVersion = func() string { return "0.1.0" }
	currentGOARCH = func() string { return "arm64" }

	msg := &feishu.InboundMessage{MessageID: "m-1", ChatID: "chat-1", ChatType: "p2p", UserID: "user-1"}
	if err := a.commandUpgrade(msg, nil); err != nil {
		t.Fatalf("commandUpgrade() error = %v", err)
	}
	if len(ff.replyCards) != 1 {
		t.Fatalf("reply card count = %d, want 1", len(ff.replyCards))
	}
	body := cardMarkdownContent(t, ff.replyCards[0])
	if !strings.Contains(body, "当前版本: `0.1.0`") || !strings.Contains(body, "最新版本: `v0.2.0`") || !strings.Contains(body, "目标架构: `arm64`") || !strings.Contains(body, "目标包: `feidex-linux-aarch64`") {
		t.Fatalf("upgrade card body = %q", body)
	}
	var pending *state.PendingRequest
	for _, req := range a.store.AllPendingRequests() {
		if req.Kind == "upgrade_release" {
			pending = req
			break
		}
	}
	if pending == nil {
		t.Fatal("expected upgrade pending request to be created")
	}
}

func TestCommandUpgradeSupportsSpecifiedVersion(t *testing.T) {
	origRelease := newReleaseClient
	origManager := newDaemonManager
	origVersion := currentVersion
	origGOARCH := currentGOARCH
	defer func() {
		newReleaseClient = origRelease
		newDaemonManager = origManager
		currentVersion = origVersion
		currentGOARCH = origGOARCH
	}()

	a, ff, _ := newTestApp(t)
	releaseStub := &fakeReleaseClient{
		latestErr: errors.New("latest query should not be called"),
		versionInfo: map[string]*release.ReleaseInfo{
			"v0.3.0": {
				Version:        "v0.3.0",
				HTMLURL:        "https://example.test/releases/v0.3.0",
				BinaryName:     "feidex-linux-amd64",
				BinaryURL:      "https://github.com/example/feidex-linux-amd64",
				ExpectedSHA256: "def456",
			},
		},
	}
	newReleaseClient = func() releaseClient { return releaseStub }
	newDaemonManager = func(string) (daemon.Manager, error) {
		return &fakeDaemonManagerForApp{status: &daemon.Status{Installed: true, Running: true, PID: os.Getpid()}}, nil
	}
	currentVersion = func() string { return "v9.9.9" }
	currentGOARCH = func() string { return "amd64" }

	msg := &feishu.InboundMessage{MessageID: "m-2", ChatID: "chat-1", ChatType: "p2p", UserID: "user-1"}
	if err := a.commandUpgrade(msg, []string{"v0.3.0"}); err != nil {
		t.Fatalf("commandUpgrade(specified version) error = %v", err)
	}
	if releaseStub.latestCalls != 0 {
		t.Fatalf("latest release call count = %d, want 0", releaseStub.latestCalls)
	}
	if len(releaseStub.versionCalls) != 1 || releaseStub.versionCalls[0] != "v0.3.0" {
		t.Fatalf("version calls = %#v, want v0.3.0", releaseStub.versionCalls)
	}
	if len(ff.replyCards) != 1 {
		t.Fatalf("reply card count = %d, want 1", len(ff.replyCards))
	}
	body := cardMarkdownContent(t, ff.replyCards[0])
	if !strings.Contains(body, "当前版本: `v9.9.9`") || !strings.Contains(body, "指定版本: `v0.3.0`") || !strings.Contains(body, "已跳过最新版本检查") {
		t.Fatalf("upgrade card body = %q", body)
	}
	var pending *state.PendingRequest
	for _, req := range a.store.AllPendingRequests() {
		if req.Kind == "upgrade_release" {
			pending = req
			break
		}
	}
	if pending == nil || !strings.Contains(pending.PayloadJSON, "\"target_version\":\"v0.3.0\"") {
		t.Fatalf("pending = %+v, want target v0.3.0", pending)
	}
}

func TestCommandUpgradeSupportsDevRelease(t *testing.T) {
	origRelease := newReleaseClient
	origManager := newDaemonManager
	origVersion := currentVersion
	origGOARCH := currentGOARCH
	origUpgradeDisplayLocation := upgradeDisplayLocation
	defer func() {
		newReleaseClient = origRelease
		newDaemonManager = origManager
		currentVersion = origVersion
		currentGOARCH = origGOARCH
		upgradeDisplayLocation = origUpgradeDisplayLocation
	}()

	a, ff, _ := newTestApp(t)
	releaseStub := &fakeReleaseClient{
		latestErr: errors.New("latest query should not be called"),
		devInfo: &release.ReleaseInfo{
			Version:        "dev-20260415T080000-a1b2c3d4e5f6",
			ReleaseTag:     release.DevReleaseTag,
			PublishedAt:    time.Date(2026, time.April, 15, 0, 0, 0, 0, time.UTC),
			SourceCommit:   "a1b2c3d4e5f67890",
			HTMLURL:        "https://example.test/releases/dev-latest",
			BinaryName:     "feidex-linux-amd64",
			BinaryURL:      "https://github.com/example/feidex-linux-amd64",
			ExpectedSHA256: "dev123",
			Prerelease:     true,
		},
	}
	newReleaseClient = func() releaseClient { return releaseStub }
	newDaemonManager = func(string) (daemon.Manager, error) {
		return &fakeDaemonManagerForApp{status: &daemon.Status{Installed: true, Running: true, PID: os.Getpid()}}, nil
	}
	currentVersion = func() string { return "v0.3.0" }
	currentGOARCH = func() string { return "amd64" }
	upgradeDisplayLocation = time.FixedZone("Asia/Shanghai", 8*60*60)

	msg := &feishu.InboundMessage{MessageID: "m-dev", ChatID: "chat-1", ChatType: "p2p", UserID: "user-1"}
	if err := a.commandUpgrade(msg, []string{"dev"}); err != nil {
		t.Fatalf("commandUpgrade(dev) error = %v", err)
	}
	if releaseStub.latestCalls != 0 {
		t.Fatalf("latest release call count = %d, want 0", releaseStub.latestCalls)
	}
	if releaseStub.devCalls != 1 {
		t.Fatalf("dev release call count = %d, want 1", releaseStub.devCalls)
	}
	if len(ff.replyCards) != 1 {
		t.Fatalf("reply card count = %d, want 1", len(ff.replyCards))
	}
	body := cardMarkdownContent(t, ff.replyCards[0])
	for _, want := range []string{"开发版本: `dev-20260415T080000-a1b2c3d4e5f6`", "Release Tag: `dev-latest`", "发布时间(本机时区): `2026-04-15 08:00:00`", "提交: `a1b2c3d4e5f6`", "当前指向的开发版构建"} {
		if !strings.Contains(body, want) {
			t.Fatalf("upgrade dev card body = %q, want %q", body, want)
		}
	}
	var pending *state.PendingRequest
	for _, req := range a.store.AllPendingRequests() {
		if req.Kind == "upgrade_release" {
			pending = req
			break
		}
	}
	if pending == nil || !strings.Contains(pending.PayloadJSON, "\"target_version\":\"dev-20260415T080000-a1b2c3d4e5f6\"") || !strings.Contains(pending.PayloadJSON, "\"release_tag\":\"dev-latest\"") {
		t.Fatalf("pending = %+v, want dev release payload", pending)
	}
}

func TestCommandUpgradeSupportsLocalPicker(t *testing.T) {
	origManager := newDaemonManager
	origGOARCH := currentGOARCH
	defer func() {
		newDaemonManager = origManager
		currentGOARCH = origGOARCH
	}()

	a, ff, _ := newTestApp(t)
	ff.replyCardIDs = []string{"upgrade-local-picker-card"}
	newDaemonManager = func(string) (daemon.Manager, error) {
		return &fakeDaemonManagerForApp{status: &daemon.Status{Installed: true, Running: true, PID: os.Getpid()}}, nil
	}
	currentGOARCH = func() string { return "amd64" }

	msg := &feishu.InboundMessage{MessageID: "m-upgrade-local", ChatID: "chat-1", ChatType: "p2p", UserID: "user-1"}
	if err := a.commandUpgrade(msg, []string{"local"}); err != nil {
		t.Fatalf("commandUpgrade(local) error = %v", err)
	}
	if len(ff.replyCards) != 1 {
		t.Fatalf("reply card count = %d, want 1", len(ff.replyCards))
	}
	if got := cardSelectStaticForTest(ff.replyCards[0]); len(got) != 1 {
		t.Fatalf("expected path picker select element, got %#v", got)
	}
	var pending *state.PendingRequest
	for _, req := range a.store.AllPendingRequests() {
		if req.Kind == upgradeLocalBinaryPendingKind {
			pending = req
			break
		}
	}
	if pending == nil {
		t.Fatal("expected local picker pending request")
	}
	if pending.FeishuMsgID != "upgrade-local-picker-card" {
		t.Fatalf("pending FeishuMsgID = %q, want upgrade-local-picker-card", pending.FeishuMsgID)
	}
	var payload pathPickerPayload
	if err := json.Unmarshal([]byte(pending.PayloadJSON), &payload); err != nil {
		t.Fatalf("Unmarshal(local picker payload) error = %v", err)
	}
	if payload.RootPath != a.cfg.Workspaces[0].Cwd {
		t.Fatalf("picker root = %q, want %q", payload.RootPath, a.cfg.Workspaces[0].Cwd)
	}
}

func TestCommandUpgradeSupportsLocalPath(t *testing.T) {
	origManager := newDaemonManager
	origVersion := currentVersion
	origGOARCH := currentGOARCH
	defer func() {
		newDaemonManager = origManager
		currentVersion = origVersion
		currentGOARCH = origGOARCH
	}()

	a, ff, _ := newTestApp(t)
	ff.replyCardIDs = []string{"upgrade-local-path-card"}
	newDaemonManager = func(string) (daemon.Manager, error) {
		return &fakeDaemonManagerForApp{status: &daemon.Status{Installed: true, Running: true, PID: os.Getpid()}}, nil
	}
	currentVersion = func() string { return "v0.3.0" }
	currentGOARCH = func() string { return "amd64" }

	localArtifact := filepath.Join(a.cfg.Workspaces[0].Cwd, "dist", "feidex linux amd64")
	if err := os.MkdirAll(filepath.Dir(localArtifact), 0o755); err != nil {
		t.Fatalf("MkdirAll(localArtifact) error = %v", err)
	}
	if err := os.WriteFile(localArtifact, []byte("local-binary"), 0o755); err != nil {
		t.Fatalf("WriteFile(localArtifact) error = %v", err)
	}

	msg := &feishu.InboundMessage{MessageID: "m-upgrade-path", ChatID: "chat-1", ChatType: "p2p", UserID: "user-1"}
	if err := a.handleCommand(msg, "/upgrade path dist/feidex linux amd64"); err != nil {
		t.Fatalf("handleCommand(/upgrade path ...) error = %v", err)
	}
	if len(ff.replyCards) != 1 {
		t.Fatalf("reply card count = %d, want 1", len(ff.replyCards))
	}
	body := cardMarkdownContent(t, ff.replyCards[0])
	if !strings.Contains(body, "来源: 本地文件") || !strings.Contains(body, "文件: `feidex linux amd64`") {
		t.Fatalf("upgrade local path body = %q", body)
	}

	var pending *state.PendingRequest
	for _, req := range a.store.AllPendingRequests() {
		if req.Kind != "upgrade_release" {
			continue
		}
		var payload upgradePendingPayload
		if err := json.Unmarshal([]byte(req.PayloadJSON), &payload); err != nil {
			t.Fatalf("Unmarshal(upgrade payload) error = %v", err)
		}
		if payload.SourcePath == "" {
			continue
		}
		pending = req
		if payload.DownloadURL != "" {
			t.Fatalf("payload.DownloadURL = %q, want empty", payload.DownloadURL)
		}
		if payload.SourceName != "feidex linux amd64" {
			t.Fatalf("payload.SourceName = %q, want feidex linux amd64", payload.SourceName)
		}
		if _, err := os.Stat(payload.SourcePath); err != nil {
			t.Fatalf("staged local artifact stat error = %v", err)
		}
		break
	}
	if pending == nil {
		t.Fatal("expected local upgrade pending request")
	}
	if pending.FeishuMsgID != "upgrade-local-path-card" {
		t.Fatalf("pending FeishuMsgID = %q, want upgrade-local-path-card", pending.FeishuMsgID)
	}
}

func TestCompleteUpgradeActionStartsBackgroundUpgrade(t *testing.T) {
	origUpgrade := startDaemonUpgrade
	defer func() { startDaemonUpgrade = origUpgrade }()

	a, _, _ := newTestApp(t)
	if err := a.store.UpsertPending(&state.PendingRequest{
		ID:          "upgrade-1",
		Kind:        "upgrade_release",
		OwnerUserID: "user-1",
		Status:      "pending",
		PayloadJSON: mustJSON(upgradePendingPayload{
			TargetVersion:  "v0.2.0",
			BinaryPath:     "/tmp/feidex",
			DownloadURL:    "https://github.com/example/feidex-linux-amd64",
			ExpectedSHA256: "abc123",
		}),
	}); err != nil {
		t.Fatalf("UpsertPending() error = %v", err)
	}
	started := false
	startDaemonUpgrade = func(spec daemon.UpgradeSpec) (string, error) {
		started = true
		if spec.Version != "v0.2.0" || spec.BinaryPath != "/tmp/feidex" {
			t.Fatalf("unexpected upgrade spec: %+v", spec)
		}
		return "feidex-upgrade-1", nil
	}

	resp, err := a.completeUpgradeAction(&feishu.CardAction{
		UserID:      "user-1",
		ActionValue: map[string]any{"request_id": "upgrade-1"},
	}, "upgrade.confirm")
	if err != nil {
		t.Fatalf("completeUpgradeAction() error = %v", err)
	}
	if !started {
		t.Fatal("expected background upgrade to start")
	}
	if resp == nil || resp.Toast == nil || resp.Toast.Type != "success" {
		t.Fatalf("completeUpgradeAction() = %#v, want success", resp)
	}
	if pending := a.store.PendingByID("upgrade-1"); pending == nil || pending.Status != "resolved" {
		t.Fatalf("upgrade pending = %+v, want resolved", pending)
	}
}

func TestCompleteMenuUpgradeReturnsPreparingCardAndPatchesAsync(t *testing.T) {
	origRelease := newReleaseClient
	origManager := newDaemonManager
	origVersion := currentVersion
	origGOARCH := currentGOARCH
	defer func() {
		newReleaseClient = origRelease
		newDaemonManager = origManager
		currentVersion = origVersion
		currentGOARCH = origGOARCH
	}()

	a, ff, _ := newTestApp(t)
	blocking := &blockingReleaseClient{
		started: make(chan struct{}),
		release: make(chan struct{}),
		info: &release.ReleaseInfo{
			Version:        "v0.2.0",
			HTMLURL:        "https://example.test/releases/v0.2.0",
			BinaryName:     "feidex-linux-amd64",
			BinaryURL:      "https://github.com/example/feidex-linux-amd64",
			ExpectedSHA256: "abc123",
		},
	}
	newReleaseClient = func() releaseClient { return blocking }
	newDaemonManager = func(string) (daemon.Manager, error) {
		return &fakeDaemonManagerForApp{status: &daemon.Status{Installed: true, Running: true, PID: os.Getpid()}}, nil
	}
	currentVersion = func() string { return "0.1.0" }
	currentGOARCH = func() string { return "amd64" }

	resp, err := a.completeMenuUpgrade(&feishu.CardAction{
		UserID:      "user-1",
		MessageID:   "msg-upgrade",
		ActionValue: map[string]any{"session_key": "sess-1"},
	})
	if err != nil || resp == nil || resp.Toast == nil || resp.Toast.Type != "info" || resp.Card == nil {
		t.Fatalf("completeMenuUpgrade(async) = %#v, %v", resp, err)
	}
	card, _ := resp.Card.Data.(map[string]any)
	if body := cardMarkdownContent(t, card); !strings.Contains(body, "正在检查可升级版本") {
		t.Fatalf("upgrade preparing body = %q", body)
	}
	if len(ff.patchedCards) != 0 {
		t.Fatalf("patched cards before release completes = %+v, want none", ff.patchedCards)
	}

	<-blocking.started
	close(blocking.release)

	deadline := time.Now().Add(1 * time.Second)
	for len(ff.patchedCards) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if len(ff.patchedCards) == 0 {
		t.Fatal("expected upgrade card to be patched asynchronously")
	}
	if body := cardMarkdownContent(t, ff.patchedCards[len(ff.patchedCards)-1]); !strings.Contains(body, "最新版本: `v0.2.0`") {
		t.Fatalf("patched upgrade body = %q", body)
	}
}

func TestPendingFormCompletionHelpers(t *testing.T) {
	a, ff, fc := newTestApp(t)
	sessionKey := "sess-1"
	sub := seedActiveSubmission(t, a, sessionKey, "thread-1", "turn-1")

	if err := a.store.UpsertPending(&state.PendingRequest{
		ID:          "tool-form-1",
		Kind:        "tool_request_user_input_form",
		SessionKey:  sessionKey,
		ThreadID:    "thread-1",
		TurnID:      "turn-1",
		OwnerUserID: "user-1",
		Status:      "pending",
		PayloadJSON: mustJSON(toolUserInputPayload{Questions: []toolUserInputQuestion{{ID: "choice"}}}),
	}); err != nil {
		t.Fatalf("UpsertPending(tool form) error = %v", err)
	}
	if err := a.completeToolUserInputText(&feishu.InboundMessage{Text: "option-a"}, a.store.PendingByID("tool-form-1")); err != nil {
		t.Fatalf("completeToolUserInputText() error = %v", err)
	}
	if len(fc.replies) == 0 {
		t.Fatal("expected completeToolUserInputText to reply to codex")
	}

	if err := a.store.UpsertPending(&state.PendingRequest{
		ID:          "elicitation-form-1",
		Kind:        "mcp_elicitation_form",
		SessionKey:  sessionKey,
		ThreadID:    "thread-1",
		TurnID:      "turn-1",
		OwnerUserID: "user-1",
		FeishuMsgID: "card-elicitation",
		Status:      "pending",
		PayloadJSON: mustJSON(elicitationFormPayload{
			Schema: map[string]any{"properties": map[string]any{"name": map[string]any{"type": "string"}}},
		}),
	}); err != nil {
		t.Fatalf("UpsertPending(elicitation form) error = %v", err)
	}
	if err := a.completeElicitationFormText(&feishu.InboundMessage{Text: "Feidex"}, a.store.PendingByID("elicitation-form-1")); err != nil {
		t.Fatalf("completeElicitationFormText() error = %v", err)
	}

	if err := a.store.UpsertPending(&state.PendingRequest{
		ID:          "url-1",
		Kind:        "mcp_elicitation_url",
		SessionKey:  sessionKey,
		ThreadID:    "thread-1",
		TurnID:      "turn-1",
		OwnerUserID: "user-1",
		Status:      "pending",
	}); err != nil {
		t.Fatalf("UpsertPending(url) error = %v", err)
	}
	resp, err := a.completeElicitationURLAction(&feishu.CardAction{UserID: "user-1", ActionValue: map[string]any{"request_id": "url-1"}}, "elicitation_url.accept")
	if err != nil || resp.Toast == nil || resp.Toast.Type != "success" {
		t.Fatalf("completeElicitationURLAction() = %#v, %v", resp, err)
	}

	if refreshed := a.store.GetSubmission(sub.ID); refreshed.Status != "running" {
		t.Fatalf("submission after pending form completion = %q, want running", refreshed.Status)
	}
	if len(ff.patchedCards) == 0 {
		t.Fatal("expected pending form completion to patch cards")
	}
}

func TestTurnStartAndFinishFlowHelpers(t *testing.T) {
	a, ff, fc := newTestApp(t)
	sessionKey := "sess-1"
	subID, err := a.store.CreateSubmission(&state.Submission{
		ID:               "sub-queued",
		SessionKey:       sessionKey,
		WorkspaceID:      a.cfg.Workspaces[0].ID,
		UserID:           "user-1",
		ChatID:           "chat-1",
		TriggerMessageID: "trigger-1",
		InputText:        "hello",
		Status:           "queued",
	})
	if err != nil {
		t.Fatalf("CreateSubmission() error = %v", err)
	}
	if err := a.store.UpsertSession(&state.Session{
		Key:         sessionKey,
		WorkspaceID: a.cfg.Workspaces[0].ID,
		ChatID:      "chat-1",
		ChatType:    "group",
		Queue:       []string{subID},
		Status:      "idle",
	}); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}

	var calls []string
	paramsSeen := map[string]any{}
	fc.callHook = func(_ context.Context, method string, params any, out any) error {
		calls = append(calls, method)
		paramsSeen[method] = params
		switch method {
		case "thread/start":
			result := out.(*codexrpc.ThreadStartResult)
			result.Thread.ID = "thread-1"
			result.Thread.Name = "Thread Name"
			result.Thread.Preview = "Preview"
			return nil
		case "turn/start":
			result := out.(*codexrpc.TurnStartResult)
			result.Turn.ID = "turn-1"
			return nil
		default:
			return nil
		}
	}

	if got := buildTurnSandboxPolicy("read-only"); got["type"] != "readOnly" {
		t.Fatalf("buildTurnSandboxPolicy(read-only) = %+v", got)
	}
	if got := buildTurnSandboxPolicy("workspace-write"); got["type"] != "workspaceWrite" {
		t.Fatalf("buildTurnSandboxPolicy(workspace-write) = %+v", got)
	}
	if got := buildTurnSandboxPolicy("danger-full-access"); got["type"] != "dangerFullAccess" {
		t.Fatalf("buildTurnSandboxPolicy(danger-full-access) = %+v", got)
	}
	if got := buildTurnSandboxPolicy("bad"); got != nil {
		t.Fatalf("buildTurnSandboxPolicy(bad) = %+v, want nil", got)
	}

	if _, err := a.startSubmissionTurn(context.Background(), sessionKey, "thread-1", nil, a.cfg.Workspaces[0].Cwd, "on-request", "workspace-write", "", "", ""); err == nil {
		t.Fatal("expected startSubmissionTurn(nil submission) to fail")
	}
	if _, err := a.startSubmissionTurn(context.Background(), sessionKey, "thread-1", &state.Submission{ID: "empty"}, a.cfg.Workspaces[0].Cwd, "on-request", "workspace-write", "", "", ""); err == nil {
		t.Fatal("expected startSubmissionTurn(empty input) to fail")
	}

	if err := a.startNextSubmission(sessionKey); err != nil {
		t.Fatalf("startNextSubmission() error = %v", err)
	}
	if len(calls) != 2 || calls[0] != "thread/start" || calls[1] != "turn/start" {
		t.Fatalf("codex calls = %+v, want thread/start then turn/start", calls)
	}
	if _, ok := paramsSeen["thread/start"].(map[string]any)["serviceTier"]; ok {
		t.Fatalf("thread/start serviceTier should be omitted when unset: %+v", paramsSeen["thread/start"])
	}
	if _, ok := paramsSeen["turn/start"].(map[string]any)["serviceTier"]; ok {
		t.Fatalf("turn/start serviceTier should be omitted when unset: %+v", paramsSeen["turn/start"])
	}
	sess := a.store.GetSession(sessionKey)
	if sess == nil || sess.ActiveThreadID != "thread-1" || sess.ActiveTurnID != "turn-1" || sess.Status != "turn_in_progress" {
		t.Fatalf("session after startNextSubmission = %+v", sess)
	}
	sub := a.store.GetSubmission(subID)
	if sub == nil || sub.ThreadID != "thread-1" || sub.TurnID != "turn-1" || sub.Status != "running" {
		t.Fatalf("submission after startNextSubmission = %+v", sub)
	}

	a.finishTurn("thread-1", "turn-1", "completed")
	time.Sleep(20 * time.Millisecond)
	sub = a.store.GetSubmission(subID)
	if sub != nil {
		t.Fatalf("submission after finishTurn should be released from runtime store, got %+v", sub)
	}
	sess = a.store.GetSession(sessionKey)
	if sess == nil || sess.Status != "idle" || sess.ActiveTurnID != "" {
		t.Fatalf("session after finishTurn = %+v", sess)
	}
	if len(ff.replyCards) == 0 && len(ff.replyTextWithIDs) == 0 {
		t.Fatal("expected finishTurn to send final output")
	}
}

func TestStartSubmissionTurnIncludesFastServiceTier(t *testing.T) {
	a, _, fc := newTestApp(t)
	var gotParams map[string]any
	fc.callHook = func(_ context.Context, method string, params any, out any) error {
		if method != "turn/start" {
			return nil
		}
		gotParams, _ = params.(map[string]any)
		if result, ok := out.(*codexrpc.TurnStartResult); ok {
			result.Turn.ID = "turn-fast"
		}
		return nil
	}
	sub := &state.Submission{ID: "sub-1", InputText: "hello"}
	if _, err := a.startSubmissionTurn(context.Background(), "sess-1", "thread-1", sub, a.cfg.Workspaces[0].Cwd, "on-request", "workspace-write", "fast", "", ""); err != nil {
		t.Fatalf("startSubmissionTurn() error = %v", err)
	}
	if gotParams == nil {
		t.Fatal("expected turn/start params to be captured")
	}
	if got, _ := gotParams["serviceTier"].(string); got != "fast" {
		t.Fatalf("serviceTier = %q, want fast", got)
	}
}

func TestNotificationHelpers(t *testing.T) {
	a, _, fc := newTestApp(t)
	sessionKey := "sess-1"
	sub := seedActiveSubmission(t, a, sessionKey, "thread-1", "turn-1")
	if err := a.store.UpsertPending(&state.PendingRequest{ID: "req-1", Kind: "command", SessionKey: sessionKey, ThreadID: "thread-1", TurnID: "turn-1", Status: "pending"}); err != nil {
		t.Fatalf("UpsertPending(notification) error = %v", err)
	}

	a.handleNotification("turn/plan/updated", json.RawMessage(`{"turnId":"turn-1","plan":[{"step":"a","status":"completed"}]}`))
	a.handleNotification("error", json.RawMessage(`{"threadId":"thread-1","turnId":"turn-1","error":{"message":"boom"}}`))
	a.handleNotification("serverRequest/resolved", json.RawMessage(`{"threadId":"thread-1","requestId":"req-1"}`))

	stream := a.turnStreams["turn-1"]
	if stream == nil || !strings.Contains(stream.PendingPlan, "a") {
		t.Fatalf("turn stream after notifications = %+v", stream)
	}
	if pending := a.store.PendingByID("req-1"); pending == nil || pending.Status != "resolved" {
		t.Fatalf("resolved pending = %+v, want resolved", pending)
	}
	updated := a.store.GetSubmission(sub.ID)
	if updated == nil || updated.Status != "running" && updated.Status != "failed" {
		t.Fatalf("submission after error notification = %+v", updated)
	}

	a.updateSubmissionByTurn("thread-1", "turn-1", func(s *state.Submission) { s.Status = "custom" })
	if got := a.store.GetSubmission(sub.ID); got == nil || got.Status != "custom" {
		t.Fatalf("updateSubmissionByTurn() = %+v, want updated status", got)
	}

	a.startNextSubmissionAsync("", "test")
	if got := truncate("  abcdef  ", 3); got != "abc..." {
		t.Fatalf("truncate() = %q, want abc...", got)
	}
	if _, err := a.handleCardAction(&feishu.CardAction{Name: "unknown"}); err != nil {
		t.Fatalf("handleCardAction() error = %v", err)
	}
	if len(fc.replyErrors) != 0 {
		t.Fatalf("unexpected codex reply errors: %+v", fc.replyErrors)
	}
}

func TestHandleFeishuMessageReplySteersToLinkedTurn(t *testing.T) {
	a, _, fc := newTestApp(t)
	targetSessionKey := "feishu:group:chat-1:root:root-msg"
	if err := a.store.UpsertSession(&state.Session{
		Key:            targetSessionKey,
		WorkspaceID:    a.cfg.Workspaces[0].ID,
		ChatID:         "chat-1",
		ChatType:       "group",
		OwnerUserID:    "user-1",
		ActiveThreadID: "thread-1",
		ActiveTurnID:   "turn-1",
		Status:         "turn_in_progress",
	}); err != nil {
		t.Fatalf("UpsertSession(target) error = %v", err)
	}
	if err := a.store.UpsertMessageLink(&state.MessageLink{
		MessageID:  "root-msg",
		SessionKey: targetSessionKey,
		ThreadID:   "thread-1",
		TurnID:     "turn-1",
	}); err != nil {
		t.Fatalf("UpsertMessageLink(target) error = %v", err)
	}

	steered := false
	fc.callHook = func(_ context.Context, method string, params any, out any) error {
		if method != "turn/steer" {
			return nil
		}
		steered = true
		got, _ := params.(map[string]any)
		if got["threadId"] != "thread-1" || got["expectedTurnId"] != "turn-1" {
			t.Fatalf("turn/steer params = %+v", got)
		}
		return nil
	}

	a.handleFeishuMessage(&feishu.InboundMessage{
		MessageID:       "reply-1",
		ChatID:          "chat-1",
		ChatType:        "group",
		UserID:          "user-1",
		Text:            "follow up",
		RootMessageID:   "root-msg",
		ParentMessageID: "target-msg",
	})

	if !steered {
		t.Fatal("expected reply message to steer")
	}
	if sess := a.store.GetSession(targetSessionKey); sess == nil || len(sess.Queue) != 0 {
		t.Fatalf("target session queue = %+v, want no queued submissions", sess)
	}
	if link := a.store.GetMessageLink("root-msg"); link == nil || link.ThreadID != "thread-1" || link.TurnID != "turn-1" {
		t.Fatalf("root message link = %+v, want root turn binding", link)
	}
}

func TestHandleFeishuMessageReplySteersWithStagedImages(t *testing.T) {
	a, _, fc := newTestApp(t)
	targetSessionKey := "feishu:group:chat-1:root:root-msg"
	bucketSessionKey := a.pendingInputSessionKey(&feishu.InboundMessage{ChatID: "chat-1", ChatType: "group", UserID: "user-1"})
	if err := a.store.UpsertSession(&state.Session{
		Key:            targetSessionKey,
		WorkspaceID:    a.cfg.Workspaces[0].ID,
		ChatID:         "chat-1",
		ChatType:       "group",
		OwnerUserID:    "user-1",
		ActiveThreadID: "thread-1",
		ActiveTurnID:   "turn-1",
		Status:         "turn_in_progress",
	}); err != nil {
		t.Fatalf("UpsertSession(target) error = %v", err)
	}
	if err := a.store.UpsertSession(&state.Session{
		Key:         bucketSessionKey,
		WorkspaceID: a.cfg.Workspaces[0].ID,
		ChatID:      "chat-1",
		ChatType:    "group",
		OwnerUserID: "user-1",
		Status:      "queued",
		StagedImages: []state.SessionStagedImage{
			{SourceMessageID: "img-1", RootMessageID: "img-1", Name: "a.png", LocalPath: "/tmp/a.png", CreatedAt: 1},
			{SourceMessageID: "img-2", RootMessageID: "img-2", Name: "b.png", LocalPath: "/tmp/b.png", CreatedAt: 2},
		},
	}); err != nil {
		t.Fatalf("UpsertSession(staged bucket) error = %v", err)
	}
	if err := a.store.UpsertMessageLink(&state.MessageLink{
		MessageID:  "root-msg",
		SessionKey: targetSessionKey,
		ThreadID:   "thread-1",
		TurnID:     "turn-1",
	}); err != nil {
		t.Fatalf("UpsertMessageLink(target) error = %v", err)
	}

	var seenInputs []map[string]any
	fc.callHook = func(_ context.Context, method string, params any, out any) error {
		if method != "turn/steer" {
			return nil
		}
		got, _ := params.(map[string]any)
		if got["threadId"] != "thread-1" || got["expectedTurnId"] != "turn-1" {
			t.Fatalf("turn/steer params = %+v", got)
		}
		if items, ok := got["input"].([]map[string]any); ok {
			seenInputs = items
		}
		return nil
	}

	a.handleFeishuMessage(&feishu.InboundMessage{
		MessageID:       "reply-1",
		ChatID:          "chat-1",
		ChatType:        "group",
		UserID:          "user-1",
		Text:            "follow up",
		RootMessageID:   "root-msg",
		ParentMessageID: "target-msg",
	})

	if len(seenInputs) != 3 {
		t.Fatalf("turn/steer inputs = %+v, want text + 2 images", seenInputs)
	}
	if seenInputs[0]["type"] != "text" || seenInputs[1]["type"] != "localImage" || seenInputs[2]["type"] != "localImage" {
		t.Fatalf("turn/steer input types = %+v, want text + 2 localImage", seenInputs)
	}
	if bucket := a.store.GetSession(bucketSessionKey); bucket == nil || len(bucket.StagedImages) != 0 {
		t.Fatalf("staged bucket after steer = %+v, want empty", bucket)
	}
}

func TestHandleFeishuMessageReplySteerFallsBackToQueue(t *testing.T) {
	a, _, fc := newTestApp(t)
	targetSessionKey := "feishu:group:chat-1:root:root-msg"
	if err := a.store.UpsertSession(&state.Session{
		Key:            targetSessionKey,
		WorkspaceID:    a.cfg.Workspaces[0].ID,
		ChatID:         "chat-1",
		ChatType:       "group",
		OwnerUserID:    "user-1",
		ActiveThreadID: "thread-current",
		ActiveTurnID:   "turn-current",
		Status:         "turn_in_progress",
	}); err != nil {
		t.Fatalf("UpsertSession(target) error = %v", err)
	}
	if err := a.store.UpsertMessageLink(&state.MessageLink{
		MessageID:  "root-msg",
		SessionKey: targetSessionKey,
		ThreadID:   "thread-old",
		TurnID:     "turn-old",
	}); err != nil {
		t.Fatalf("UpsertMessageLink(target) error = %v", err)
	}

	steerAttempts := 0
	fc.callHook = func(_ context.Context, method string, params any, out any) error {
		if method == "turn/steer" {
			steerAttempts++
			return errors.New("no active turn to steer")
		}
		return nil
	}

	msg := &feishu.InboundMessage{
		MessageID:       "reply-2",
		ChatID:          "chat-1",
		ChatType:        "group",
		UserID:          "user-1",
		Text:            "fallback to queue",
		RootMessageID:   "root-msg",
		ParentMessageID: "target-msg",
	}
	a.handleFeishuMessage(msg)

	if steerAttempts != 1 {
		t.Fatalf("steer attempts = %d, want 1", steerAttempts)
	}
	targetSess := a.store.GetSession(targetSessionKey)
	if targetSess == nil || len(targetSess.Queue) != 1 {
		t.Fatalf("target session after fallback = %+v, want one queued submission", targetSess)
	}
}

func TestStartNextSubmissionRefreshesRootTurnBinding(t *testing.T) {
	a, _, fc := newTestApp(t)
	msg := &feishu.InboundMessage{
		MessageID:     "root-msg",
		ChatID:        "chat-1",
		ChatType:      "group",
		UserID:        "user-1",
		Text:          "hello",
		RootMessageID: "root-msg",
	}
	fc.callHook = func(_ context.Context, method string, params any, out any) error {
		switch method {
		case "thread/start":
			result := out.(*codexrpc.ThreadStartResult)
			result.Thread.ID = "thread-1"
			return nil
		case "turn/start":
			result := out.(*codexrpc.TurnStartResult)
			result.Turn.ID = "turn-1"
			return nil
		default:
			return nil
		}
	}
	if err := a.enqueueSubmission(msg); err != nil {
		t.Fatalf("enqueueSubmission() error = %v", err)
	}
	if link := a.store.GetMessageLink("root-msg"); link == nil || link.ThreadID != "thread-1" || link.TurnID != "turn-1" {
		t.Fatalf("root turn binding = %+v, want current turn", link)
	}
}

func TestTopLevelStagedImagesBindRootsToNextTurn(t *testing.T) {
	a, _, fc := newTestApp(t)
	sessionKey := a.pendingInputSessionKey(&feishu.InboundMessage{ChatID: "chat-1", ChatType: "group", UserID: "user-1"})
	if err := a.store.UpsertSession(&state.Session{
		Key:         sessionKey,
		WorkspaceID: a.cfg.Workspaces[0].ID,
		ChatID:      "chat-1",
		ChatType:    "group",
		OwnerUserID: "user-1",
		Status:      "queued",
		StagedImages: []state.SessionStagedImage{
			{SourceMessageID: "a", RootMessageID: "a", Name: "a.png", LocalPath: "/tmp/a.png", CreatedAt: 1},
			{SourceMessageID: "b", RootMessageID: "b", Name: "b.png", LocalPath: "/tmp/b.png", CreatedAt: 2},
		},
	}); err != nil {
		t.Fatalf("UpsertSession(staged bucket) error = %v", err)
	}

	var seenInputs []map[string]any
	fc.callHook = func(_ context.Context, method string, params any, out any) error {
		switch method {
		case "thread/start":
			result := out.(*codexrpc.ThreadStartResult)
			result.Thread.ID = "thread-1"
			return nil
		case "turn/start":
			got, _ := params.(map[string]any)
			if items, ok := got["input"].([]map[string]any); ok {
				seenInputs = items
			}
			result := out.(*codexrpc.TurnStartResult)
			result.Turn.ID = "turn-1"
			return nil
		default:
			return nil
		}
	}

	msg := &feishu.InboundMessage{
		MessageID:     "c",
		ChatID:        "chat-1",
		ChatType:      "group",
		UserID:        "user-1",
		Text:          "describe images",
		RootMessageID: "c",
	}
	if err := a.enqueueSubmission(msg); err != nil {
		t.Fatalf("enqueueSubmission() error = %v", err)
	}
	if len(seenInputs) != 3 || seenInputs[0]["type"] != "text" || seenInputs[1]["type"] != "localImage" || seenInputs[2]["type"] != "localImage" {
		t.Fatalf("thread/start inputs = %+v, want text + 2 images", seenInputs)
	}
	for _, rootID := range []string{"a", "b", "c"} {
		link := a.store.GetMessageLink(rootID)
		if link == nil || link.ThreadID != "thread-1" || link.TurnID != "turn-1" {
			t.Fatalf("root %s binding = %+v, want thread-1/turn-1", rootID, link)
		}
	}
	if bucket := a.store.GetSession(sessionKey); bucket == nil || len(bucket.StagedImages) != 0 {
		t.Fatalf("staged bucket after consume = %+v, want empty", bucket)
	}
}

func TestReplyFallbackTurnBindsOnlyReplyRoot(t *testing.T) {
	a, _, fc := newTestApp(t)
	replySessionKey := "feishu:group:chat-1:root:reply-root"
	bucketSessionKey := a.pendingInputSessionKey(&feishu.InboundMessage{ChatID: "chat-1", ChatType: "group", UserID: "user-1"})
	if err := a.store.UpsertSession(&state.Session{
		Key:         replySessionKey,
		WorkspaceID: a.cfg.Workspaces[0].ID,
		ChatID:      "chat-1",
		ChatType:    "group",
		OwnerUserID: "user-1",
		Status:      "idle",
	}); err != nil {
		t.Fatalf("UpsertSession(reply session) error = %v", err)
	}
	if err := a.store.UpsertSession(&state.Session{
		Key:         bucketSessionKey,
		WorkspaceID: a.cfg.Workspaces[0].ID,
		ChatID:      "chat-1",
		ChatType:    "group",
		OwnerUserID: "user-1",
		Status:      "queued",
		StagedImages: []state.SessionStagedImage{
			{SourceMessageID: "a", RootMessageID: "a", Name: "a.png", LocalPath: "/tmp/a.png", CreatedAt: 1},
			{SourceMessageID: "b", RootMessageID: "b", Name: "b.png", LocalPath: "/tmp/b.png", CreatedAt: 2},
		},
	}); err != nil {
		t.Fatalf("UpsertSession(staged bucket) error = %v", err)
	}
	if err := a.store.UpsertMessageLink(&state.MessageLink{
		MessageID:  "reply-root",
		SessionKey: replySessionKey,
		ThreadID:   "thread-old",
		TurnID:     "turn-old",
	}); err != nil {
		t.Fatalf("UpsertMessageLink(root binding) error = %v", err)
	}

	steerAttempts := 0
	fc.callHook = func(_ context.Context, method string, params any, out any) error {
		switch method {
		case "turn/steer":
			steerAttempts++
			return errors.New("no active turn to steer")
		case "thread/start":
			result := out.(*codexrpc.ThreadStartResult)
			result.Thread.ID = "thread-new"
			return nil
		case "turn/start":
			got, _ := params.(map[string]any)
			items, _ := got["input"].([]map[string]any)
			if len(items) != 3 {
				t.Fatalf("fallback turn/start inputs = %+v, want text + 2 images", items)
			}
			result := out.(*codexrpc.TurnStartResult)
			result.Turn.ID = "turn-new"
			return nil
		default:
			return nil
		}
	}

	msg := &feishu.InboundMessage{
		MessageID:       "reply-msg",
		ChatID:          "chat-1",
		ChatType:        "group",
		UserID:          "user-1",
		Text:            "follow up after closed turn",
		RootMessageID:   "reply-root",
		ParentMessageID: "some-parent",
	}
	a.handleFeishuMessage(msg)

	if steerAttempts != 1 {
		t.Fatalf("steer attempts = %d, want 1", steerAttempts)
	}
	if link := a.store.GetMessageLink("reply-root"); link == nil || link.ThreadID != "thread-new" || link.TurnID != "turn-new" {
		t.Fatalf("reply root binding = %+v, want new turn binding", link)
	}
	for _, stagedRoot := range []string{"a", "b"} {
		if link := a.store.GetMessageLink(stagedRoot); link != nil {
			t.Fatalf("staged root %s should not be rebound on fallback, got %+v", stagedRoot, link)
		}
	}
}

func TestAdditionalCommandHelpers(t *testing.T) {
	a, ff, fc := newTestApp(t)
	sessionKey := a.makeSessionKey(&feishu.InboundMessage{ChatType: "group", ChatID: "chat-1", RootMessageID: "root-1"})
	if err := a.store.UpsertSession(&state.Session{
		Key:                        sessionKey,
		WorkspaceID:                a.cfg.Workspaces[0].ID,
		ActiveThreadID:             "thread-1",
		ActiveThreadWorkspaceID:    a.cfg.Workspaces[0].ID,
		ActiveThreadSandboxMode:    "read-only",
		ActiveThreadApprovalPolicy: "never",
		ActiveTurnID:               "turn-1",
		ChatID:                     "chat-1",
		ChatType:                   "group",
	}); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}

	msg := &feishu.InboundMessage{MessageID: "m-1", ChatID: "chat-1", ChatType: "group", RootMessageID: "root-1", UserID: "user-1"}
	if err := a.showThreadSandboxMenu(msg); err != nil {
		t.Fatalf("showThreadSandboxMenu() error = %v", err)
	}
	if err := a.showThreadPolicyMenu(msg); err != nil {
		t.Fatalf("showThreadPolicyMenu() error = %v", err)
	}
	if len(ff.replyCards) < 2 {
		t.Fatalf("expected thread menu cards, got %d", len(ff.replyCards))
	}

	if got := renderThreadButtonLabel("Very Long Thread Name", "", "id"); got == "" {
		t.Fatal("renderThreadButtonLabel() should produce a label")
	}
	if got := renderThreadListEntry("", "preview text", "id"); !strings.Contains(got, "preview") {
		t.Fatalf("renderThreadListEntry() = %q, want preview text", got)
	}

	emptyMsg := &feishu.InboundMessage{MessageID: "m-2", ChatID: "chat-2", ChatType: "group", RootMessageID: "root-2", UserID: "user-2"}
	if err := a.commandAppend(emptyMsg, "  more text  "); err == nil {
		t.Fatal("expected commandAppend without active session to fail")
	}
	fc.callHook = func(_ context.Context, method string, params any, out any) error {
		switch method {
		case "turn/steer", "turn/interrupt":
			return nil
		case "thread/start":
			result := out.(*codexrpc.ThreadStartResult)
			result.Thread.ID = "thread-new"
			result.Thread.Name = "New Thread"
			result.Thread.Preview = "new preview"
			return nil
		default:
			t.Fatalf("unexpected codex method in command helper test: %s", method)
			return nil
		}
	}
	if err := a.commandAppend(msg, "  more text  "); err != nil {
		t.Fatalf("commandAppend() error = %v", err)
	}
	if err := a.commandInterrupt(msg); err != nil {
		t.Fatalf("commandInterrupt() error = %v", err)
	}

	sess := a.store.GetSession(sessionKey)
	sess.ActiveTurnID = ""
	sess.ActiveSubmissionID = ""
	sess.Status = "idle"
	if err := a.store.UpsertSession(sess); err != nil {
		t.Fatalf("UpsertSession(reset) error = %v", err)
	}
	if err := a.commandThreadsNew(msg); err != nil {
		t.Fatalf("commandThreadsNew() error = %v", err)
	}
}

func TestMoreActionAndModelHandlers(t *testing.T) {
	a, ff, fc := newTestApp(t)
	models := codexrpc.ModelListResult{
		Data: []codexrpc.ModelListEntry{
			{
				ID:                     "gpt-5",
				DisplayName:            "GPT-5",
				DefaultReasoningEffort: "medium",
				SupportedReasoningEfforts: []codexrpc.ModelReasoningEffortEntry{
					{ReasoningEffort: "low"},
					{ReasoningEffort: "medium"},
					{ReasoningEffort: "high"},
				},
				IsDefault: true,
			},
		},
	}
	fc.callHook = func(_ context.Context, method string, params any, out any) error {
		switch method {
		case "model/list":
			*out.(*codexrpc.ModelListResult) = models
			return nil
		case "thread/start":
			result := out.(*codexrpc.ThreadStartResult)
			result.Thread.ID = "thread-new"
			result.Thread.Name = "New Thread"
			result.Thread.Preview = "new preview"
			return nil
		case "thread/resume":
			result := out.(*codexrpc.ThreadStartResult)
			result.Thread.ID = "thread-9"
			result.Thread.Name = "Resumed"
			result.Thread.Preview = "preview"
			return nil
		case "thread/fork":
			result := out.(*codexrpc.ThreadStartResult)
			result.Thread.ID = "thread-fork"
			result.Thread.Name = "Forked"
			result.Thread.Preview = "fork preview"
			return nil
		default:
			return nil
		}
	}

	resp, err := a.completeGlobalModelSet(&feishu.CardAction{}, "gpt-5")
	if err != nil || resp.Toast == nil || resp.Toast.Type != "success" {
		t.Fatalf("completeGlobalModelSet() = %#v, %v", resp, err)
	}
	resp, err = a.completeGlobalReasoningEffortSet(&feishu.CardAction{}, "high")
	if err != nil || resp.Toast == nil || resp.Toast.Type != "success" {
		t.Fatalf("completeGlobalReasoningEffortSet() = %#v, %v", resp, err)
	}
	resp, err = a.completeQuietSet(&feishu.CardAction{}, config.QuietModeProgress)
	if err != nil || resp.Toast == nil || resp.Toast.Type != "success" {
		t.Fatalf("completeQuietSet() = %#v, %v", resp, err)
	}
	if a.cfg.Feishu.Quiet != config.QuietModeProgress {
		t.Fatalf("expected quiet mode to be progress, got %q", a.cfg.Feishu.Quiet)
	}

	sessionKey := "sess-1"
	if err := a.store.UpsertSession(&state.Session{
		Key:                 sessionKey,
		WorkspaceID:         a.cfg.Workspaces[0].ID,
		ActiveThreadID:      "thread-9",
		OwnerUserID:         "user-1",
		ChatID:              "chat-1",
		ChatType:            "group",
		Status:              "idle",
		ActiveThreadPreview: "",
	}); err != nil {
		t.Fatalf("UpsertSession(thread resume) error = %v", err)
	}
	resp, err = a.completeThreadResume(&feishu.CardAction{
		UserID:      "user-1",
		ChatID:      "chat-1",
		ActionValue: map[string]any{"thread_name": "Selected", "thread_preview": "chosen"},
	}, sessionKey, "thread-9")
	if err != nil || resp.Toast == nil || resp.Toast.Type != "success" {
		t.Fatalf("completeThreadResume() = %#v, %v", resp, err)
	}
	if got := a.store.GetSession(sessionKey); got == nil || got.ActiveThreadID != "thread-9" || got.Status != "idle" {
		t.Fatalf("session after completeThreadResume = %+v", got)
	}
	resp, err = a.completeMenuFork(&feishu.CardAction{
		UserID:      "user-1",
		ChatID:      "chat-1",
		ActionValue: map[string]any{"parent_action": "menu.thread"},
	}, sessionKey)
	if err != nil || resp.Toast == nil || resp.Toast.Type != "success" {
		t.Fatalf("completeMenuFork() = %#v, %v", resp, err)
	}
	if got := a.store.GetSession(sessionKey); got == nil || got.ActiveThreadID != "thread-fork" || got.Status != "idle" {
		t.Fatalf("session after completeMenuFork = %+v", got)
	}

	seedActiveSubmission(t, a, sessionKey, "thread-9", "turn-1")
	ff.sendCards = nil
	a.onFileApproval(codexrpc.RequestEnvelope{ID: json.RawMessage(`"file-approval"`), Params: json.RawMessage(`{"threadId":"thread-9","turnId":"turn-1","itemId":"item-2","reason":"need review","changes":[{"path":"internal/app/notifications.go","kind":"modified"},{"path":"README.md","kind":"added"}]}`)})
	if pending := a.store.PendingByID("file-approval"); pending == nil || pending.Kind != "file" {
		t.Fatalf("file approval pending = %+v, want file request", pending)
	}
	if len(ff.sendCards) == 0 {
		t.Fatal("expected file approval to send a card")
	}
	if got := cardMarkdownContent(t, ff.sendCards[0]); !strings.Contains(got, "2 个文件") || !strings.Contains(got, "internal/app/notifications.go") || !strings.Contains(got, "README.md") {
		t.Fatalf("file approval card body = %q", got)
	}

	sess := a.store.GetSession(sessionKey)
	sess.ActiveTurnID = ""
	sess.ActiveSubmissionID = ""
	sess.Status = "idle"
	if err := a.store.UpsertSession(sess); err != nil {
		t.Fatalf("UpsertSession(reset for menu new) error = %v", err)
	}
	resp, err = a.completeMenuNew(&feishu.CardAction{UserID: "user-1", ChatID: "chat-1"}, sessionKey)
	if err != nil || resp.Toast == nil || resp.Toast.Type != "success" {
		t.Fatalf("completeMenuNew() = %#v, %v", resp, err)
	}
}

func TestHandleCommandAndInboundDiscardHelpers(t *testing.T) {
	a, ff, fc := newTestApp(t)
	msg := &feishu.InboundMessage{MessageID: "m-1", ChatID: "chat-1", ChatType: "group", RootMessageID: "root-1", UserID: "user-1"}
	sessionKey := a.makeSessionKey(msg)
	if err := a.store.UpsertSession(&state.Session{
		Key:                        sessionKey,
		WorkspaceID:                a.cfg.Workspaces[0].ID,
		ActiveThreadID:             "thread-1",
		ActiveThreadWorkspaceID:    a.cfg.Workspaces[0].ID,
		ActiveThreadApprovalPolicy: "on-request",
		ActiveThreadSandboxMode:    "workspace-write",
		ChatID:                     "chat-1",
		ChatType:                   "group",
		Status:                     "idle",
	}); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}

	fc.callHook = func(_ context.Context, method string, params any, out any) error {
		switch method {
		case "model/list":
			*out.(*codexrpc.ModelListResult) = codexrpc.ModelListResult{
				Data: []codexrpc.ModelListEntry{{
					ID:                     "gpt-5",
					DisplayName:            "GPT-5",
					DefaultReasoningEffort: "medium",
					SupportedReasoningEfforts: []codexrpc.ModelReasoningEffortEntry{
						{ReasoningEffort: "medium"},
					},
					IsDefault: true,
				}},
			}
			return nil
		case "thread/list":
			*out.(*codexrpc.ThreadListResult) = codexrpc.ThreadListResult{}
			return nil
		case "thread/fork":
			result := out.(*codexrpc.ThreadStartResult)
			result.Thread.ID = "thread-fork"
			result.Thread.Name = "Forked"
			result.Thread.Preview = "fork preview"
			return nil
		default:
			return nil
		}
	}

	for _, raw := range []string{
		"",
		"/menu",
		"/help",
		"/status",
		"/model",
		"/quiet",
		"/debug",
		"/download",
		"/fork",
		"/threads",
		"/thread fork",
		"/thread sandbox",
		"/thread policy",
		"/workspace",
		"/workspace list",
		"/workspace sandbox",
		"/workspace policy",
		"/workspace new",
	} {
		if err := a.handleCommand(msg, raw); err != nil && raw != "/threads" {
			t.Fatalf("handleCommand(%q) error = %v", raw, err)
		}
	}
	if err := a.handleCommand(msg, "/unknown"); err == nil {
		t.Fatal("expected unknown command to fail")
	}
	if len(ff.replyCards) == 0 {
		t.Fatal("expected handleCommand to send at least one reply card")
	}

	// Real discard paths for recall/reaction.
	subID, err := a.store.CreateSubmission(&state.Submission{
		ID:               "queued-sub",
		SessionKey:       sessionKey,
		WorkspaceID:      a.cfg.Workspaces[0].ID,
		SourceMessageIDs: []string{"queued-msg"},
		Status:           "queued",
	})
	if err != nil {
		t.Fatalf("CreateSubmission(queued) error = %v", err)
	}
	sess := a.store.GetSession(sessionKey)
	sess.StagedImages = []state.SessionStagedImage{{SourceMessageID: "staged-msg", Name: "image.png"}}
	sess.Queue = []string{subID}
	sess.Status = "queued"
	if err := a.store.UpsertSession(sess); err != nil {
		t.Fatalf("UpsertSession(with pending) error = %v", err)
	}

	a.handleFeishuRecall(&feishu.MessageRecall{MessageID: "staged-msg", ChatID: "chat-1"})
	updated := a.store.GetSession(sessionKey)
	if len(updated.StagedImages) != 0 {
		t.Fatalf("handleFeishuRecall() did not discard staged image: %+v", updated.StagedImages)
	}

	a.handleFeishuReaction(&feishu.MessageReaction{MessageID: "queued-msg", EmojiType: discardReactionEmoji, ChatID: "chat-1"})
	updated = a.store.GetSession(sessionKey)
	if len(updated.Queue) != 0 {
		t.Fatalf("handleFeishuReaction() did not discard queued submission: %+v", updated.Queue)
	}
}

func TestCommandHelpRendersHelpCard(t *testing.T) {
	a, ff, _ := newTestApp(t)
	msg := &feishu.InboundMessage{MessageID: "m-help", ChatID: "chat-1", ChatType: "p2p", UserID: "user-1"}

	if err := a.commandHelp(msg, nil); err != nil {
		t.Fatalf("commandHelp() error = %v", err)
	}
	if len(ff.replyCards) == 0 {
		t.Fatal("expected help card to be sent")
	}
	body := cardMarkdownContent(t, ff.replyCards[len(ff.replyCards)-1])
	for _, want := range []string{"/help", "/history", "/skills", "/debug", "/debug logs", "/download", "/fork", "/compact", "/workspace use ID", "/thread policy", "/upgrade", "/upgrade dev", "/upgrade local", "/upgrade path ./dist/feidex-linux-amd64", "$skill-name <内容>"} {
		if !strings.Contains(body, want) {
			t.Fatalf("help body missing %q: %q", want, body)
		}
	}
	for _, unwanted := range []string{"/threads all", "/threads new", "/threads fork", "/threads sandbox", "/threads policy"} {
		if strings.Contains(body, unwanted) {
			t.Fatalf("help body should not expose legacy thread subcommand %q: %q", unwanted, body)
		}
	}
}

func TestCommandHistoryRendersCurrentThreadTurns(t *testing.T) {
	a, ff, fc := newTestApp(t)
	msg := &feishu.InboundMessage{MessageID: "m-history", ChatID: "chat-1", ChatType: "p2p", UserID: "user-1"}
	sessionKey := a.makeSessionKey(msg)
	if err := a.store.UpsertSession(&state.Session{
		Key:            sessionKey,
		WorkspaceID:    a.cfg.Workspaces[0].ID,
		ActiveThreadID: "thread-1",
		ActiveTurnID:   "turn-2",
	}); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}

	fc.callHook = func(_ context.Context, method string, params any, out any) error {
		if method != "thread/read" {
			return nil
		}
		result := out.(*codexrpc.ThreadReadResult)
		name := "Demo Thread"
		result.Thread.ID = "thread-1"
		result.Thread.Name = &name
		result.Thread.Preview = "preview"
		result.Thread.Cwd = a.cfg.Workspaces[0].Cwd
		result.Thread.Turns = []codexrpc.ThreadReadTurn{
			{
				ID:     "turn-1",
				Status: "completed",
				Items: []codexrpc.ThreadReadItem{
					{Type: "userMessage", ID: "item-u1", Content: json.RawMessage(`[{"type":"text","text":"first input","text_elements":[]}]`)},
					{Type: "agentMessage", ID: "item-a1", Text: "first answer"},
				},
			},
			{
				ID:     "turn-2",
				Status: "running",
				Items: []codexrpc.ThreadReadItem{
					{Type: "userMessage", ID: "item-u2", Content: json.RawMessage(`[{"type":"text","text":"second input","text_elements":[]}]`)},
				},
			},
		}
		return nil
	}

	if err := a.commandHistory(msg, nil); err != nil {
		t.Fatalf("commandHistory() error = %v", err)
	}
	if len(ff.replyCards) == 0 {
		t.Fatal("expected history card to be sent")
	}
	body := cardMarkdownContent(t, ff.replyCards[len(ff.replyCards)-1])
	for _, want := range []string{"当前位置：主菜单 / 常用工具 / 历史记录", "当前页: `1-2 / 2`", "当前 turn: `Turn #2`", "在线下拉菜单中选择要查看的 turn。"} {
		if !strings.Contains(body, want) {
			t.Fatalf("history body missing %q: %q", want, body)
		}
	}
	selects := cardSelectStaticForTest(ff.replyCards[len(ff.replyCards)-1])
	if len(selects) != 1 {
		t.Fatalf("history card selects = %+v, want 1 select", selects)
	}
	options, _ := selects[0]["options"].([]map[string]any)
	if len(options) != 2 {
		t.Fatalf("history options = %+v, want 2 options", options)
	}
	wantLabels := []string{"当前 · Turn #2 | running | second input", "Turn #1 | completed | first input"}
	for i, want := range wantLabels {
		text, _ := options[i]["text"].(map[string]any)
		label, _ := text["content"].(string)
		if !strings.Contains(label, want) {
			t.Fatalf("history option %d = %q, want %q", i, label, want)
		}
	}
}

func TestCompleteHistoryDetailShowsInputs(t *testing.T) {
	a, _, fc := newTestApp(t)
	sessionKey := "sess-1"
	if err := a.store.UpsertSession(&state.Session{
		Key:            sessionKey,
		WorkspaceID:    a.cfg.Workspaces[0].ID,
		ActiveThreadID: "thread-1",
	}); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}

	fc.callHook = func(_ context.Context, method string, params any, out any) error {
		if method != "thread/read" {
			return nil
		}
		result := out.(*codexrpc.ThreadReadResult)
		result.Thread.ID = "thread-1"
		result.Thread.Turns = []codexrpc.ThreadReadTurn{
			{
				ID:     "turn-1",
				Status: "completed",
				Items: []codexrpc.ThreadReadItem{
					{Type: "userMessage", ID: "u1", Content: json.RawMessage(`[{"type":"text","text":"hello","text_elements":[]},{"type":"image","url":"https://example.test/a.png"}]`)},
					{Type: "agentMessage", ID: "a1", Text: "world"},
				},
			},
		}
		return nil
	}

	resp, err := a.completeHistoryDetail(&feishu.CardAction{}, sessionKey, 0)
	if err != nil || resp == nil || resp.Card == nil {
		t.Fatalf("completeHistoryDetail() = %#v, %v", resp, err)
	}
	card, _ := resp.Card.Data.(map[string]any)
	body := cardMarkdownContent(t, card)
	for _, want := range []string{"输入：", "hello", "[image] https://example.test/a.png", "回复：", "world"} {
		if !strings.Contains(body, want) {
			t.Fatalf("history detail missing %q: %q", want, body)
		}
	}
}

func TestSmallHelperBranches(t *testing.T) {
	if got := renderThreadButtonLabel("", "", "thread-id-1234567890"); got == "" {
		t.Fatal("renderThreadButtonLabel(id fallback) should not be empty")
	}
	if got := renderThreadListEntry("name", "preview", "id"); !strings.Contains(got, "name") {
		t.Fatalf("renderThreadListEntry(name+preview) = %q", got)
	}
	if got := renderThreadListEntry("", "", "thread-id"); !strings.Contains(got, "thread-id") {
		t.Fatalf("renderThreadListEntry(id fallback) = %q", got)
	}
}

func TestCommandThreadsDisplaysThreadList(t *testing.T) {
	a, ff, fc := newTestApp(t)
	msg := &feishu.InboundMessage{MessageID: "m-1", ChatID: "chat-1", ChatType: "group", RootMessageID: "root-1", UserID: "user-1"}
	sessionKey := a.makeSessionKey(msg)
	if err := a.store.UpsertSession(&state.Session{
		Key:                        sessionKey,
		WorkspaceID:                a.cfg.Workspaces[0].ID,
		ActiveThreadID:             "thread-current",
		ActiveThreadWorkspaceID:    a.cfg.Workspaces[0].ID,
		ActiveThreadName:           "Current Thread",
		ActiveThreadPreview:        "Current Preview",
		ActiveThreadSandboxMode:    "workspace-write",
		ActiveThreadApprovalPolicy: "on-request",
	}); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}

	fc.callHook = func(_ context.Context, method string, params any, out any) error {
		if method != "thread/list" {
			t.Fatalf("unexpected method: %s", method)
		}
		result := out.(*codexrpc.ThreadListResult)
		result.Data = []codexrpc.ThreadListEntry{
			{ID: "thread-current", Name: "Current Thread", Preview: "Current Preview", UpdatedAt: 20, Cwd: a.cfg.Workspaces[0].Cwd},
			{ID: "thread-older", Name: "Older", Preview: "Older Preview", UpdatedAt: 10, Cwd: a.cfg.Workspaces[0].Cwd},
		}
		return nil
	}

	if err := a.commandThreads(msg, false); err != nil {
		t.Fatalf("commandThreads(display) error = %v", err)
	}
	if len(ff.replyCards) == 0 {
		t.Fatal("expected thread list card to be sent")
	}
	if body := cardMarkdownContent(t, ff.replyCards[len(ff.replyCards)-1]); !strings.Contains(body, "通过下拉 list 选择要切换的线程。") || strings.Contains(body, "Older Preview") {
		t.Fatalf("thread list body = %q, want summary without duplicated list", body)
	}
	if got := cardSelectStaticForTest(ff.replyCards[len(ff.replyCards)-1]); len(got) != 1 {
		t.Fatalf("thread list selects = %+v, want 1 select", got)
	}
}

func TestCommandThreadsFiltersByWorkspaceCWD(t *testing.T) {
	a, ff, fc := newTestApp(t)
	a.cfg.Workspaces = append(a.cfg.Workspaces, config.Workspace{ID: "alt", Name: "Alt", Cwd: t.TempDir(), ApprovalPolicy: "never", SandboxMode: "read-only"})
	msg := &feishu.InboundMessage{MessageID: "m-1", ChatID: "chat-1", ChatType: "group", RootMessageID: "root-1", UserID: "user-1"}
	sessionKey := a.makeSessionKey(msg)
	if err := a.store.UpsertSession(&state.Session{
		Key:         sessionKey,
		WorkspaceID: a.cfg.Workspaces[0].ID,
	}); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}

	attempts := 0
	fc.callHook = func(_ context.Context, method string, params any, out any) error {
		if method != "thread/list" {
			t.Fatalf("unexpected method: %s", method)
		}
		attempts++
		result := out.(*codexrpc.ThreadListResult)
		if attempts < 3 {
			result.Data = nil
			return nil
		}
		result.Data = []codexrpc.ThreadListEntry{
			{ID: "thread-default", Name: "Default Thread", Preview: "Default Preview", UpdatedAt: 20, Cwd: a.cfg.Workspaces[0].Cwd},
			{ID: "thread-alt", Name: "Alt Thread", Preview: "Alt Preview", UpdatedAt: 10, Cwd: a.cfg.Workspaces[1].Cwd},
		}
		return nil
	}

	if err := a.commandThreads(msg, false); err != nil {
		t.Fatalf("commandThreads(filter) error = %v", err)
	}
	if attempts != 3 {
		t.Fatalf("thread/list attempts = %d, want 3", attempts)
	}
	if len(ff.replyCards) == 0 {
		t.Fatal("expected thread list card to be sent")
	}
	elements := cardElementsForTest(ff.replyCards[0])
	body := elements[0]["content"].(string)
	if !strings.Contains(body, "list 数量: `1`") || !strings.Contains(body, "通过下拉 list 选择要切换的线程。") {
		t.Fatalf("thread list body = %q, want summary only", body)
	}
	selects := cardSelectStaticForTest(ff.replyCards[0])
	if len(selects) != 1 {
		t.Fatalf("thread list selects = %+v, want 1", selects)
	}
	options, _ := selects[0]["options"].([]map[string]any)
	if len(options) != 1 {
		t.Fatalf("thread list options = %+v, want 1 filtered option", options)
	}
	if got, _ := options[0]["value"].(string); got != "thread-default" {
		t.Fatalf("thread list option value = %q, want thread-default", got)
	}
}

func TestCompleteThreadResumeRejectsThreadFromDifferentWorkspace(t *testing.T) {
	a, _, fc := newTestApp(t)
	a.cfg.Workspaces = append(a.cfg.Workspaces, config.Workspace{ID: "alt", Name: "Alt", Cwd: t.TempDir(), ApprovalPolicy: "never", SandboxMode: "read-only"})
	sessionKey := "sess-1"
	if err := a.store.UpsertSession(&state.Session{
		Key:         sessionKey,
		WorkspaceID: a.cfg.Workspaces[0].ID,
		OwnerUserID: "user-1",
		ChatID:      "chat-1",
		ChatType:    "group",
		Status:      "idle",
	}); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}

	fc.callHook = func(_ context.Context, method string, params any, out any) error {
		t.Fatalf("unexpected method: %s", method)
		return nil
	}

	resp, err := a.completeThreadResume(&feishu.CardAction{
		UserID: "user-1",
		ChatID: "chat-1",
		ActionValue: map[string]any{
			"thread_name":    "Alt Thread",
			"thread_preview": "Alt Preview",
			"thread_cwd":     a.cfg.Workspaces[1].Cwd,
		},
	}, sessionKey, "thread-alt")
	if err != nil {
		t.Fatalf("completeThreadResume() error = %v", err)
	}
	if resp == nil || resp.Toast == nil || resp.Toast.Type != "warning" {
		t.Fatalf("expected warning toast, got %#v", resp)
	}
	if got := a.store.GetSession(sessionKey); got == nil || got.ActiveThreadID != "" {
		t.Fatalf("session after rejected resume = %+v, want no active thread", got)
	}
}
