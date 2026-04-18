//go:build integration

package codexrpc

import (
	"bytes"
	"context"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"feidex/internal/config"
)

func TestLiveCodexWebSocketProcessCWDDoesNotAffectWorkspaceWriteFileApproval(t *testing.T) {
	requireLiveTokenTests(t)

	command := firstNonEmptyEnv("FEIDEX_CODEX_COMMAND", "codex")

	cases := []struct {
		name              string
		processCWDForRepo func(repo string) string
		wantFileApproval  bool
	}{
		{
			name: "process_cwd_matches_workspace",
			processCWDForRepo: func(repo string) string {
				return repo
			},
			wantFileApproval: false,
		},
		{
			name: "process_cwd_differs_from_workspace",
			processCWDForRepo: func(string) string {
				return t.TempDir()
			},
			wantFileApproval: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := initTinyWorkspaceWriteRepo(t)
			processCWD := tc.processCWDForRepo(repo)
			server := startLoopbackWSCodexAppServer(t, command, processCWD)
			defer server.Close(t)

			result := runWorkspaceWriteEditProbe(t, config.CodexConfig{
				Command:         command,
				Transport:       "ws",
				WSURL:           server.URL,
				ExperimentalAPI: true,
				ServiceName:     "feidex-integration",
			}, repo)

			if got := len(result.fileApprovalPayloads) > 0; got != tc.wantFileApproval {
				t.Fatalf(
					"file approval mismatch: got=%v want=%v request_methods=%v payloads=%v notifications=%s",
					got,
					tc.wantFileApproval,
					result.requestMethods,
					result.fileApprovalPayloads,
					summarizeLiveNotifications(result.lifecycle),
				)
			}
			if !strings.Contains(result.calcContents, "return a - b") {
				t.Fatalf("calc.go contents = %q, want subtraction edit", result.calcContents)
			}
			if !threadReadContainsAgentText(result.read, "FILE_OK") {
				t.Fatalf("thread/read missing FILE_OK final agent text: %+v", result.read.Thread.Turns)
			}
			t.Logf(
				"process_cwd=%s request_methods=%v file_approval_count=%d",
				processCWD,
				result.requestMethods,
				len(result.fileApprovalPayloads),
			)
		})
	}
}

func TestLiveCodexProcessCWDTransportComparisonForWorkspaceWriteFileApproval(t *testing.T) {
	requireLiveTokenTests(t)

	command := firstNonEmptyEnv("FEIDEX_CODEX_COMMAND", "codex")
	processCWD := t.TempDir()

	stdioRepo := initTinyWorkspaceWriteRepo(t)
	initAuxLaunchRepo(t, processCWD)
	stdioResult := runWorkspaceWriteEditProbe(t, config.CodexConfig{
		Command:         command,
		Transport:       "stdio",
		AppServerDir:    processCWD,
		ExperimentalAPI: true,
		ServiceName:     "feidex-integration",
	}, stdioRepo)
	t.Logf(
		"stdio mismatch result: request_methods=%v file_approval_count=%d",
		stdioResult.requestMethods,
		len(stdioResult.fileApprovalPayloads),
	)

	wsRepo := initTinyWorkspaceWriteRepo(t)
	server := startLoopbackWSCodexAppServer(t, command, processCWD)
	defer server.Close(t)
	wsResult := runWorkspaceWriteEditProbe(t, config.CodexConfig{
		Command:         command,
		Transport:       "ws",
		WSURL:           server.URL,
		ExperimentalAPI: true,
		ServiceName:     "feidex-integration",
	}, wsRepo)
	t.Logf(
		"websocket mismatch result: request_methods=%v file_approval_count=%d",
		wsResult.requestMethods,
		len(wsResult.fileApprovalPayloads),
	)
	if len(stdioResult.fileApprovalPayloads) == 0 {
		t.Skipf(
			"stdio mismatch did not reproduce file approval, so this controlled environment is not a valid causal comparison; stdio_request_methods=%v ws_request_methods=%v",
			stdioResult.requestMethods,
			wsResult.requestMethods,
		)
	}
	if len(wsResult.fileApprovalPayloads) != 0 {
		t.Fatalf(
			"websocket mismatch also reproduced file approval; request_methods=%v payloads=%v notifications=%s",
			wsResult.requestMethods,
			wsResult.fileApprovalPayloads,
			summarizeLiveNotifications(wsResult.lifecycle),
		)
	}
}

func TestLiveCodexStdioLaunchDirMismatchDoesNotTriggerWorkspaceWriteFileApproval(t *testing.T) {
	requireLiveTokenTests(t)

	command := firstNonEmptyEnv("FEIDEX_CODEX_COMMAND", "codex")
	launchRoot := t.TempDir()
	launchDir := filepath.Join(launchRoot, "launch-a")
	if err := os.MkdirAll(launchDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(launchDir) error = %v", err)
	}
	repoRoot := t.TempDir()
	repo := filepath.Join(repoRoot, "workspace-b")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatalf("MkdirAll(repo) error = %v", err)
	}
	initTinyWorkspaceWriteRepoInto(t, repo)
	initAuxLaunchRepo(t, launchDir)
	if strings.HasPrefix(filepath.Clean(repo)+string(filepath.Separator), filepath.Clean(launchDir)+string(filepath.Separator)) ||
		strings.HasPrefix(filepath.Clean(launchDir)+string(filepath.Separator), filepath.Clean(repo)+string(filepath.Separator)) {
		t.Fatalf("launchDir=%q and repo=%q should not contain each other", launchDir, repo)
	}
	wrapper := writeCodexLaunchWrapper(t, command, launchDir)

	result := runWorkspaceWriteEditProbe(t, config.CodexConfig{
		Command:         wrapper,
		Transport:       "stdio",
		ExperimentalAPI: true,
		ServiceName:     "feidex-integration",
	}, repo)
	t.Logf(
		"launch_dir=%s workspace=%s request_methods=%v file_approval_count=%d",
		launchDir,
		repo,
		result.requestMethods,
		len(result.fileApprovalPayloads),
	)
	if len(result.fileApprovalPayloads) != 0 {
		t.Fatalf(
			"stdio launch-dir mismatch unexpectedly triggered file approval; launch_dir=%s workspace=%s request_methods=%v payloads=%v notifications=%s",
			launchDir,
			repo,
			result.requestMethods,
			result.fileApprovalPayloads,
			summarizeLiveNotifications(result.lifecycle),
		)
	}
	if !strings.Contains(result.calcContents, "return a - b") {
		t.Fatalf("workspace calc.go contents = %q, want subtraction edit", result.calcContents)
	}
	if !threadReadContainsAgentText(result.read, "FILE_OK") {
		t.Fatalf("thread/read missing FILE_OK final agent text: %+v", result.read.Thread.Turns)
	}
	if readTinyRepoFile(t, launchDir, "README.md") != "# launch repo\n" {
		t.Fatalf("launch repo README.md changed unexpectedly")
	}
	if _, err := os.Stat(filepath.Join(launchDir, "calc.go")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("launch repo unexpectedly gained calc.go: err=%v", err)
	}
}

func TestLiveCodexInheritedProcessCWDDoesNotTriggerWorkspaceWriteFileApproval(t *testing.T) {
	requireLiveTokenTests(t)

	command := firstNonEmptyEnv("FEIDEX_CODEX_COMMAND", "codex")
	launchDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	repo := strings.TrimSpace(os.Getenv("FEIDEX_CODEX_PROBE_WORKSPACE_DIR"))
	if repo == "" {
		repoRoot := t.TempDir()
		repo = filepath.Join(repoRoot, "workspace-b")
	}
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatalf("MkdirAll(repo) error = %v", err)
	}
	initTinyWorkspaceWriteRepoInto(t, repo)
	initAuxLaunchRepo(t, launchDir)
	if strings.HasPrefix(filepath.Clean(repo)+string(filepath.Separator), filepath.Clean(launchDir)+string(filepath.Separator)) ||
		strings.HasPrefix(filepath.Clean(launchDir)+string(filepath.Separator), filepath.Clean(repo)+string(filepath.Separator)) {
		t.Fatalf("launchDir=%q and repo=%q should not contain each other", launchDir, repo)
	}

	result := runWorkspaceWriteEditProbe(t, config.CodexConfig{
		Command:         command,
		Transport:       "stdio",
		ExperimentalAPI: true,
		ServiceName:     "feidex-integration",
	}, repo)
	t.Logf(
		"process_cwd=%s workspace=%s request_methods=%v file_approval_count=%d",
		launchDir,
		repo,
		result.requestMethods,
		len(result.fileApprovalPayloads),
	)
	if len(result.fileApprovalPayloads) != 0 {
		t.Fatalf(
			"inherited process cwd unexpectedly triggered file approval; process_cwd=%s workspace=%s request_methods=%v payloads=%v notifications=%s",
			launchDir,
			repo,
			result.requestMethods,
			result.fileApprovalPayloads,
			summarizeLiveNotifications(result.lifecycle),
		)
	}
	if !strings.Contains(result.calcContents, "return a - b") {
		t.Fatalf("workspace calc.go contents = %q, want subtraction edit", result.calcContents)
	}
	if !threadReadContainsAgentText(result.read, "FILE_OK") {
		t.Fatalf("thread/read missing FILE_OK final agent text: %+v", result.read.Thread.Turns)
	}
	if readTinyRepoFile(t, launchDir, "README.md") != "# launch repo\n" {
		t.Fatalf("launch repo README.md changed unexpectedly")
	}
	if _, err := os.Stat(filepath.Join(launchDir, "calc.go")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("launch repo unexpectedly gained calc.go: err=%v", err)
	}
}

func TestLiveCodexWebSocketExplicitProcessCWDDoesNotTriggerWorkspaceWriteFileApproval(t *testing.T) {
	requireLiveTokenTests(t)

	command := firstNonEmptyEnv("FEIDEX_CODEX_COMMAND", "codex")
	processCWD := strings.TrimSpace(os.Getenv("FEIDEX_CODEX_PROBE_PROCESS_CWD_DIR"))
	if processCWD == "" {
		processCWD = t.TempDir()
	}
	repo := strings.TrimSpace(os.Getenv("FEIDEX_CODEX_PROBE_WORKSPACE_DIR"))
	if repo == "" {
		repoRoot := t.TempDir()
		repo = filepath.Join(repoRoot, "workspace-b")
	}
	if err := os.MkdirAll(processCWD, 0o755); err != nil {
		t.Fatalf("MkdirAll(processCWD) error = %v", err)
	}
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatalf("MkdirAll(repo) error = %v", err)
	}
	initTinyWorkspaceWriteRepoInto(t, repo)
	initAuxLaunchRepo(t, processCWD)
	if strings.HasPrefix(filepath.Clean(repo)+string(filepath.Separator), filepath.Clean(processCWD)+string(filepath.Separator)) ||
		strings.HasPrefix(filepath.Clean(processCWD)+string(filepath.Separator), filepath.Clean(repo)+string(filepath.Separator)) {
		t.Fatalf("processCWD=%q and repo=%q should not contain each other", processCWD, repo)
	}

	server := startLoopbackWSCodexAppServer(t, command, processCWD)
	defer server.Close(t)

	result := runWorkspaceWriteEditProbe(t, config.CodexConfig{
		Command:         command,
		Transport:       "ws",
		WSURL:           server.URL,
		ExperimentalAPI: true,
		ServiceName:     "feidex-integration",
	}, repo)
	t.Logf(
		"ws_process_cwd=%s workspace=%s request_methods=%v file_approval_count=%d",
		processCWD,
		repo,
		result.requestMethods,
		len(result.fileApprovalPayloads),
	)
	if len(result.fileApprovalPayloads) != 0 {
		t.Fatalf(
			"websocket process cwd unexpectedly triggered file approval; process_cwd=%s workspace=%s request_methods=%v payloads=%v notifications=%s",
			processCWD,
			repo,
			result.requestMethods,
			result.fileApprovalPayloads,
			summarizeLiveNotifications(result.lifecycle),
		)
	}
	if !strings.Contains(result.calcContents, "return a - b") {
		t.Fatalf("workspace calc.go contents = %q, want subtraction edit", result.calcContents)
	}
	if !threadReadContainsAgentText(result.read, "FILE_OK") {
		t.Fatalf("thread/read missing FILE_OK final agent text: %+v", result.read.Thread.Turns)
	}
	if readTinyRepoFile(t, processCWD, "README.md") != "# launch repo\n" {
		t.Fatalf("launch repo README.md changed unexpectedly")
	}
	if _, err := os.Stat(filepath.Join(processCWD, "calc.go")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("launch repo unexpectedly gained calc.go: err=%v", err)
	}
}

type workspaceWriteProbeResult struct {
	requestMethods       []string
	fileApprovalPayloads []string
	lifecycle            []liveNotification
	calcContents         string
	read                 ThreadReadResult
}

func runWorkspaceWriteEditProbe(t *testing.T, cfg config.CodexConfig, repo string) workspaceWriteProbeResult {
	t.Helper()

	client, notifications, requests := startLiveClientWithNotificationsAndRequests(t, cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	startLiveClientEventually(t, ctx, client)
	defer func() { _ = client.Close() }()

	threadID := startLiveThreadWithOptions(t, ctx, client, repo, "on-request", "workspace-write")
	start := notifications.len()
	turnID := startLiveTurn(
		t,
		ctx,
		client,
		threadID,
		repo,
		"on-request",
		map[string]any{"type": "workspaceWrite"},
		"You must edit calc.go so Add returns subtraction (`a - b`) instead of addition. Change only that file. If approval is required, use the built-in approval request mechanism and wait. After the edit finishes, reply with exactly FILE_OK.",
	)

	var (
		mu                   sync.Mutex
		requestMethods       []string
		fileApprovalPayloads []string
	)
	requestCtx, requestCancel := context.WithCancel(ctx)
	defer requestCancel()
	var requestWG sync.WaitGroup
	requestWG.Add(1)
	go func() {
		defer requestWG.Done()
		for {
			select {
			case req := <-requests.ch:
				mu.Lock()
				requestMethods = append(requestMethods, req.Method)
				if req.Method == "item/fileChange/requestApproval" {
					fileApprovalPayloads = append(fileApprovalPayloads, string(req.Params))
				}
				mu.Unlock()
				if strings.HasSuffix(req.Method, "/requestApproval") {
					_ = client.Reply(req.ID, map[string]any{"decision": "accept"})
				}
			case <-requestCtx.Done():
				return
			}
		}
	}()

	completedTurnID, status, lifecycle := waitForTurnCompletionOnThread(t, notifications, start, threadID, 120*time.Second)
	requestCancel()
	requestWG.Wait()
	if status != "completed" {
		t.Fatalf("workspace-write edit turn completed with status %q, want completed; notifications: %s", status, summarizeLiveNotifications(lifecycle))
	}
	if completedTurnID != turnID {
		t.Fatalf("workspace-write edit completed turn id = %s, want %s", completedTurnID, turnID)
	}

	calcContents := readTinyRepoFile(t, repo, "calc.go")
	read := waitForThreadReadItemPredicate(t, ctx, client, threadID, 15*time.Second, func(item ThreadReadItem) bool {
		return strings.TrimSpace(item.Type) == "agentMessage" && threadReadItemContains(item, "FILE_OK")
	})

	mu.Lock()
	defer mu.Unlock()
	return workspaceWriteProbeResult{
		requestMethods:       append([]string(nil), requestMethods...),
		fileApprovalPayloads: append([]string(nil), fileApprovalPayloads...),
		lifecycle:            append([]liveNotification(nil), lifecycle...),
		calcContents:         calcContents,
		read:                 read,
	}
}

func startLiveClientEventually(t *testing.T, parent context.Context, client *Client) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(parent, 10*time.Second)
		err := client.Start(ctx, true)
		cancel()
		if err == nil {
			return
		}
		lastErr = err
		time.Sleep(300 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for websocket app-server readiness: %v", lastErr)
}

type loopbackWSCodexAppServer struct {
	URL    string
	cmd    *exec.Cmd
	cancel context.CancelFunc
	logs   *bytes.Buffer
	waitCh chan error
}

func startLoopbackWSCodexAppServer(t *testing.T, command, processCWD string) *loopbackWSCodexAppServer {
	t.Helper()

	addr := "127.0.0.1:" + chooseFreeTCPPort(t)
	listenURL := "ws://" + addr
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, command, "app-server", "--listen", listenURL)
	cmd.Dir = processCWD
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var logs bytes.Buffer
	cmd.Stdout = &logs
	cmd.Stderr = &logs
	if err := cmd.Start(); err != nil {
		cancel()
		t.Fatalf("start websocket app-server error = %v", err)
	}
	waitCh := make(chan error, 1)
	go func() {
		waitCh <- cmd.Wait()
	}()
	return &loopbackWSCodexAppServer{
		URL:    listenURL,
		cmd:    cmd,
		cancel: cancel,
		logs:   &logs,
		waitCh: waitCh,
	}
}

func (s *loopbackWSCodexAppServer) Close(t *testing.T) {
	t.Helper()
	if s == nil {
		return
	}
	if s.cancel != nil {
		s.cancel()
	}
	if s.cmd != nil && s.cmd.Process != nil {
		_ = syscall.Kill(-s.cmd.Process.Pid, syscall.SIGKILL)
	}
	select {
	case err := <-s.waitCh:
		if err != nil && !isExpectedLoopbackWSClose(err) {
			t.Fatalf("websocket app-server exit error = %v\nlogs:\n%s", err, s.logs.String())
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("timed out waiting for websocket app-server to exit\nlogs:\n%s", s.logs.String())
	}
}

func isExpectedLoopbackWSClose(err error) bool {
	if err == nil {
		return true
	}
	text := strings.ToLower(strings.TrimSpace(err.Error()))
	return strings.Contains(text, "signal: killed") || strings.Contains(text, "context canceled")
}

func chooseFreeTCPPort(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen(tcp) error = %v", err)
	}
	defer ln.Close()
	_, port, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatalf("SplitHostPort(%q) error = %v", ln.Addr().String(), err)
	}
	return port
}

func writeCodexLaunchWrapper(t *testing.T, command, launchDir string) string {
	t.Helper()
	scriptPath := filepath.Join(t.TempDir(), "codex-launch-wrapper.sh")
	script := "#!/bin/sh\nset -eu\ncd " + shellQuote(launchDir) + "\nexec " + shellQuote(command) + " app-server\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile(wrapper) error = %v", err)
	}
	return scriptPath
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

func initTinyWorkspaceWriteRepo(t *testing.T) string {
	t.Helper()

	repo := t.TempDir()
	initTinyWorkspaceWriteRepoInto(t, repo)
	return repo
}

func initTinyWorkspaceWriteRepoInto(t *testing.T, repo string) {
	t.Helper()

	runGitIntegration(t, repo, "init")
	runGitIntegration(t, repo, "config", "user.email", "feidex-test@example.com")
	runGitIntegration(t, repo, "config", "user.name", "Feidex Test")

	writeIntegrationFile(t, filepath.Join(repo, "calc.go"), strings.TrimSpace(`
package calc

// Add returns the sum of a and b.
func Add(a, b int) int {
	return a + b
}
`)+"\n")
	runGitIntegration(t, repo, "add", "calc.go")
	runGitIntegration(t, repo, "commit", "-m", "initial workspace-write fixture")
}

func initAuxLaunchRepo(t *testing.T, dir string) {
	t.Helper()

	runGitIntegration(t, dir, "init")
	runGitIntegration(t, dir, "config", "user.email", "feidex-test@example.com")
	runGitIntegration(t, dir, "config", "user.name", "Feidex Test")
	writeIntegrationFile(t, filepath.Join(dir, "README.md"), "# launch repo\n")
	runGitIntegration(t, dir, "add", "README.md")
	runGitIntegration(t, dir, "commit", "-m", "initial launch fixture")
}
