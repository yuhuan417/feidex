package codexrpc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"feidex/internal/config"
)

type recordingWriteCloser struct {
	mu     sync.Mutex
	writes [][]byte
	closed bool
}

func (w *recordingWriteCloser) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.writes = append(w.writes, append([]byte(nil), p...))
	return len(p), nil
}

func (w *recordingWriteCloser) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.closed = true
	return nil
}

func (w *recordingWriteCloser) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()

	var b strings.Builder
	for _, chunk := range w.writes {
		b.Write(chunk)
	}
	return b.String()
}

func TestNewDefaults(t *testing.T) {
	client := New(config.CodexConfig{})
	if client.cfg.Command != "codex" {
		t.Fatalf("default command = %q, want codex", client.cfg.Command)
	}
	if client.pending == nil {
		t.Fatal("pending map was not initialized")
	}
}

func TestNotifyReplyAndReplyErrorWriteJSON(t *testing.T) {
	writer := &recordingWriteCloser{}
	client := New(config.CodexConfig{})
	client.stdin = writer

	if err := client.Notify("initialized", map[string]string{"ok": "yes"}); err != nil {
		t.Fatalf("Notify() error = %v", err)
	}
	if err := client.Reply(json.RawMessage(`"id-1"`), map[string]string{"status": "done"}); err != nil {
		t.Fatalf("Reply() error = %v", err)
	}
	if err := client.ReplyError(json.RawMessage(`1`), 400, "bad request"); err != nil {
		t.Fatalf("ReplyError() error = %v", err)
	}

	output := writer.String()
	for _, want := range []string{`"method":"initialized"`, `"id":"id-1"`, `"message":"bad request"`} {
		if !strings.Contains(output, want) {
			t.Fatalf("writer output %q does not contain %q", output, want)
		}
	}
	if !strings.HasSuffix(output, "\n") {
		t.Fatalf("writer output %q does not end with newline", output)
	}
}

func TestNotifyWithoutStartFails(t *testing.T) {
	client := New(config.CodexConfig{})
	err := client.Notify("initialized", map[string]any{})
	if err == nil || !strings.Contains(err.Error(), "client not started") {
		t.Fatalf("Notify() error = %v, want client not started", err)
	}
}

func TestCallHandlesSuccessErrorAndCancellation(t *testing.T) {
	writer := &recordingWriteCloser{}
	client := New(config.CodexConfig{})
	client.stdin = writer

	go func() {
		for {
			if strings.Contains(writer.String(), `"id":1`) {
				client.handleIncoming([]byte(`{"id":1,"result":{"value":"ok"}}`))
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
	}()

	var out struct {
		Value string `json:"value"`
	}
	if err := client.Call(context.Background(), "test/method", map[string]string{"name": "value"}, &out); err != nil {
		t.Fatalf("Call(success) error = %v", err)
	}
	if out.Value != "ok" {
		t.Fatalf("decoded result = %+v, want value ok", out)
	}

	go func() {
		for {
			if strings.Contains(writer.String(), `"id":2`) {
				client.handleIncoming([]byte(`{"id":2,"error":{"code":500,"message":"boom"}}`))
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
	}()
	err := client.Call(context.Background(), "test/error", nil, nil)
	if err == nil || err.Error() != "boom" {
		t.Fatalf("Call(error) = %v, want boom", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = client.Call(ctx, "test/cancel", nil, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Call(canceled) error = %v, want %v", err, context.Canceled)
	}
}

func TestHandleIncomingDispatchesRequestNotificationAndResponse(t *testing.T) {
	client := New(config.CodexConfig{})

	requestCh := make(chan RequestEnvelope, 1)
	notifCh := make(chan notificationEnvelope, 1)
	client.SetHandlers(func(method string, params json.RawMessage) {
		notifCh <- notificationEnvelope{Method: method, Params: params}
	}, func(req RequestEnvelope) {
		requestCh <- req
	})

	respCh := make(chan responseEnvelope, 1)
	client.pending[9] = respCh

	client.handleIncoming([]byte(`{"id":"req-1","method":"approval/request","params":{"kind":"file"}}`))
	req := <-requestCh
	if req.Method != "approval/request" || string(req.ID) != `"req-1"` {
		t.Fatalf("request = %+v, want approval/request with id req-1", req)
	}

	client.handleIncoming([]byte(`{"method":"item/completed","params":{"status":"done"}}`))
	notif := <-notifCh
	if notif.Method != "item/completed" || !strings.Contains(string(notif.Params), `"done"`) {
		t.Fatalf("notification = %+v, want item/completed", notif)
	}

	client.handleIncoming([]byte(`{"id":9,"result":{"ok":true}}`))
	resp := <-respCh
	if !strings.Contains(string(resp.Result), `"ok":true`) {
		t.Fatalf("response = %+v, want ok result", resp)
	}
	if _, ok := client.pending[9]; ok {
		t.Fatal("pending response channel was not removed")
	}

	client.handleIncoming([]byte(`not json`))
}

func TestReadLoopProcessesScannerInput(t *testing.T) {
	client := New(config.CodexConfig{})
	notifCh := make(chan string, 1)
	client.onNotification = func(method string, _ json.RawMessage) {
		notifCh <- method
	}
	client.stdout = io.NopCloser(strings.NewReader("{\"method\":\"first\",\"params\":{}}\n"))

	client.readLoop()

	if method := <-notifCh; method != "first" {
		t.Fatalf("readLoop() delivered %q, want first", method)
	}
}

func TestReadLoopTransportFailureNotifiesAndFailsPendingCalls(t *testing.T) {
	client := New(config.CodexConfig{})
	client.waitDone = nil
	writer := &recordingWriteCloser{}
	client.stdin = writer
	reader, pipeWriter := io.Pipe()
	client.stdout = reader

	errCh := make(chan error, 1)
	client.SetErrorHandler(func(err error) {
		errCh <- err
	})

	callDone := make(chan error, 1)
	go func() {
		callDone <- client.Call(context.Background(), "test/method", nil, nil)
	}()

	go client.readLoop()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(writer.String(), `"id":1`) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	_ = pipeWriter.Close()

	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("expected transport error callback")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for transport error callback")
	}

	select {
	case err := <-callDone:
		if err == nil || !strings.Contains(err.Error(), "EOF") {
			t.Fatalf("pending Call() error = %v, want EOF-derived failure", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for pending call failure")
	}
}

func TestStartStdioInitializesClient(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "rpc.log")
	scriptPath := filepath.Join(dir, "codex-rpc.sh")
	script := fmt.Sprintf(`#!/bin/sh
logfile=%q
while IFS= read -r line; do
  printf '%%s\n' "$line" >> "$logfile"
  case "$line" in
    *'"method":"initialize"'*)
      printf '%%s\n' '{"id":1,"result":{"userAgent":"ua","codexHome":"/tmp/codex","platformFamily":"unix","platformOs":"linux"}}'
      ;;
    *'"method":"initialized"'*)
      exit 0
      ;;
  esac
done
`, logPath)
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile(script) error = %v", err)
	}

	client := New(config.CodexConfig{Command: scriptPath})
	if err := client.Start(context.Background(), true); err != nil {
		t.Fatalf("Start(stdio) error = %v", err)
	}
	defer func() { _ = client.Close() }()

	var logText string
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(logPath)
		if err == nil {
			logText = string(data)
			if strings.Contains(logText, `"method":"initialized"`) {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !strings.Contains(logText, `"method":"initialize"`) || !strings.Contains(logText, `"method":"initialized"`) {
		t.Fatalf("stdio log = %q, want initialize and initialized messages", logText)
	}
}

func TestStartStdioUsesConfiguredAppServerDir(t *testing.T) {
	dir := t.TempDir()
	workDir := filepath.Join(dir, "workspace")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(workDir) error = %v", err)
	}
	cwdPath := filepath.Join(dir, "cwd.log")
	scriptPath := filepath.Join(dir, "codex-rpc.sh")
	script := fmt.Sprintf(`#!/bin/sh
pwd > %q
while IFS= read -r line; do
  case "$line" in
    *'"method":"initialize"'*)
      printf '%%s\n' '{"id":1,"result":{"userAgent":"ua","codexHome":"/tmp/codex","platformFamily":"unix","platformOs":"linux"}}'
      ;;
    *'"method":"initialized"'*)
      exit 0
      ;;
  esac
done
`, cwdPath)
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile(script) error = %v", err)
	}

	client := New(config.CodexConfig{
		Command:      scriptPath,
		AppServerDir: workDir,
	})
	if err := client.Start(context.Background(), true); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	cwdBytes, err := os.ReadFile(cwdPath)
	if err != nil {
		t.Fatalf("ReadFile(cwdPath) error = %v", err)
	}
	if got := strings.TrimSpace(string(cwdBytes)); got != workDir {
		t.Fatalf("app-server cwd = %q, want %q", got, workDir)
	}
}

func TestStartStdioIncludesMCPPublicationArgsAndEnv(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.log")
	envPath := filepath.Join(dir, "env.log")
	scriptPath := filepath.Join(dir, "codex-rpc.sh")
	script := fmt.Sprintf(`#!/bin/sh
printf '%%s\n' "$@" > %q
printf '%%s' "$FEIDEX_MCP_TOKEN_TEST" > %q
while IFS= read -r line; do
  case "$line" in
    *'"method":"initialize"'*)
      printf '%%s\n' '{"id":1,"result":{"userAgent":"ua","codexHome":"/tmp/codex","platformFamily":"unix","platformOs":"linux"}}'
      ;;
    *'"method":"initialized"'*)
      exit 0
      ;;
  esac
done
`, argsPath, envPath)
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile(script) error = %v", err)
	}

	client := New(config.CodexConfig{Command: scriptPath})
	client.SetMCPServerPublication("feidex-send", "http://127.0.0.1:12345/mcp", "FEIDEX_MCP_TOKEN_TEST", "secret-token")
	if err := client.Start(context.Background(), true); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer func() { _ = client.Close() }()

	argsBytes, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("ReadFile(argsPath) error = %v", err)
	}
	argsText := string(argsBytes)
	for _, want := range []string{
		"app-server",
		"-c",
		`mcp_servers.feidex-send.url="http://127.0.0.1:12345/mcp"`,
		`mcp_servers.feidex-send.bearer_token_env_var="FEIDEX_MCP_TOKEN_TEST"`,
	} {
		if !strings.Contains(argsText, want) {
			t.Fatalf("args log = %q, want %q", argsText, want)
		}
	}

	envBytes, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatalf("ReadFile(envPath) error = %v", err)
	}
	if got := strings.TrimSpace(string(envBytes)); got != "secret-token" {
		t.Fatalf("env token = %q, want secret-token", got)
	}
}

func TestStartKeepsProcessAliveAfterStartupContextCancel(t *testing.T) {
	dir := t.TempDir()
	pidPath := filepath.Join(dir, "app-server.pid")
	scriptPath := filepath.Join(dir, "codex-rpc.sh")
	script := fmt.Sprintf(`#!/bin/sh
echo $$ > %q
trap 'exit 0' TERM INT
while IFS= read -r line; do
  case "$line" in
    *'"method":"initialize"'*)
      printf '%%s\n' '{"id":1,"result":{"userAgent":"ua","codexHome":"/tmp/codex","platformFamily":"unix","platformOs":"linux"}}'
      ;;
    *'"method":"initialized"'*)
      while :; do
        sleep 1
      done
      ;;
  esac
done
`, pidPath)
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile(script) error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	client := New(config.CodexConfig{Command: scriptPath})
	if err := client.Start(ctx, true); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer func() { _ = client.Close() }()

	pid := waitForPIDFile(t, pidPath)
	cancel()
	time.Sleep(200 * time.Millisecond)

	if !processAlive(pid) {
		t.Fatalf("app-server pid %d exited after startup context cancellation", pid)
	}
}

func TestStartCleansUpProcessWhenInitializationContextTimesOut(t *testing.T) {
	dir := t.TempDir()
	pidPath := filepath.Join(dir, "app-server.pid")
	scriptPath := filepath.Join(dir, "codex-rpc.sh")
	script := fmt.Sprintf(`#!/bin/sh
echo $$ > %q
trap 'exit 0' TERM INT
while IFS= read -r line; do
  case "$line" in
    *'"method":"initialize"'*)
      sleep 1
      printf '%%s\n' '{"id":1,"result":{"userAgent":"ua","codexHome":"/tmp/codex","platformFamily":"unix","platformOs":"linux"}}'
      ;;
  esac
done
`, pidPath)
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile(script) error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	client := New(config.CodexConfig{Command: scriptPath})
	err := client.Start(ctx, true)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Start() error = %v, want %v", err, context.DeadlineExceeded)
	}

	pid := waitForPIDFile(t, pidPath)
	waitForProcessExit(t, pid)
}

func TestStartRejectsRemovedWebSocketTransport(t *testing.T) {
	client := New(config.CodexConfig{Transport: "ws"})
	err := client.Start(context.Background(), true)
	if err == nil || !strings.Contains(err.Error(), "stdio only") {
		t.Fatalf("Start(removed websocket transport) error = %v, want stdio-only failure", err)
	}

	client = New(config.CodexConfig{WSURL: "ws://example.test/ws"})
	err = client.Start(context.Background(), true)
	if err == nil || !strings.Contains(err.Error(), "stdio only") {
		t.Fatalf("Start(removed websocket url) error = %v, want stdio-only failure", err)
	}
}

func TestCloseClosesStdinWhenNoProcess(t *testing.T) {
	writer := &recordingWriteCloser{}
	client := New(config.CodexConfig{})
	client.stdin = writer

	if err := client.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if !writer.closed {
		t.Fatal("Close() did not close stdin writer")
	}
}

func waitForPIDFile(t *testing.T, path string) int {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil {
			pid, convErr := strconv.Atoi(strings.TrimSpace(string(data)))
			if convErr == nil && pid > 0 {
				return pid
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for pid file %s", path)
	return 0
}

func waitForProcessExit(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if !processAlive(pid) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("process %d still alive", pid)
}

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
}
