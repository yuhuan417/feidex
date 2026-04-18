//go:build integration

package codexrpc

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"feidex/internal/config"
)

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
	t.Fatalf("timed out waiting for live app-server readiness: %v", lastErr)
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
