package codexrpc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"feidex/internal/config"
)

type recordingWriteCloser struct {
	mu     sync.Mutex
	writes [][]byte
	closed bool
}

type fakeWebsocketConn struct {
	mu      sync.Mutex
	readCh  chan []byte
	writeCh chan []byte
	closed  bool
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

func (f *fakeWebsocketConn) ReadMessage() (int, []byte, error) {
	msg, ok := <-f.readCh
	if !ok {
		return 0, nil, io.EOF
	}
	return 1, msg, nil
}

func (f *fakeWebsocketConn) WriteMessage(_ int, data []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return io.EOF
	}
	select {
	case f.writeCh <- append([]byte(nil), data...):
	default:
	}
	return nil
}

func (f *fakeWebsocketConn) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return nil
	}
	f.closed = true
	close(f.readCh)
	return nil
}

func TestNewDefaultsAndTransportMode(t *testing.T) {
	client := New(config.CodexConfig{})
	if client.cfg.Command != "codex" {
		t.Fatalf("default command = %q, want codex", client.cfg.Command)
	}
	if client.pending == nil {
		t.Fatal("pending map was not initialized")
	}

	if got := codexTransportMode(config.CodexConfig{}); got != "stdio" {
		t.Fatalf("codexTransportMode(stdio) = %q, want stdio", got)
	}
	if got := codexTransportMode(config.CodexConfig{Transport: " WS "}); got != "ws" {
		t.Fatalf("codexTransportMode(explicit ws) = %q, want ws", got)
	}
	if got := codexTransportMode(config.CodexConfig{WSURL: "wss://example.test/ws"}); got != "ws" {
		t.Fatalf("codexTransportMode(url) = %q, want ws", got)
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

func TestStartWebSocketInitializesClientAndUsesBearerToken(t *testing.T) {
	origDial := websocketDial
	defer func() { websocketDial = origDial }()

	conn := &fakeWebsocketConn{readCh: make(chan []byte, 4), writeCh: make(chan []byte, 4)}
	authCh := make(chan string, 1)
	initCh := make(chan map[string]any, 1)
	initializedCh := make(chan map[string]any, 1)

	websocketDial = func(ctx context.Context, url string, headers http.Header) (websocketConn, error) {
		authCh <- headers.Get("Authorization")
		go func() {
			var initReq map[string]any
			first, ok := <-conn.writeCh
			if ok {
				_ = json.Unmarshal(first, &initReq)
				initCh <- initReq
			}
			conn.readCh <- []byte(`{"id":1,"result":{"userAgent":"ua","codexHome":"/tmp/codex","platformFamily":"unix","platformOs":"linux"}}`)
			var initialized map[string]any
			second, ok := <-conn.writeCh
			if ok {
				_ = json.Unmarshal(second, &initialized)
				initializedCh <- initialized
			}
			_ = conn.Close()
		}()
		return conn, nil
	}

	client := New(config.CodexConfig{
		Transport:     "ws",
		WSURL:         "ws://example.test/ws",
		WSBearerToken: "secret-token",
	})
	if err := client.Start(context.Background(), false); err != nil {
		t.Fatalf("Start(websocket) error = %v", err)
	}
	defer func() {
		if err := client.Close(); err != nil {
			t.Fatalf("Close(websocket) error = %v", err)
		}
	}()

	if auth := <-authCh; auth != "Bearer secret-token" {
		t.Fatalf("Authorization header = %q, want Bearer secret-token", auth)
	}
	initReq := <-initCh
	if initReq["method"] != "initialize" {
		t.Fatalf("first ws message = %+v, want initialize", initReq)
	}
	params, _ := initReq["params"].(map[string]any)
	capabilities, _ := params["capabilities"].(map[string]any)
	if capabilities["experimentalApi"] != false {
		t.Fatalf("initialize capabilities = %+v, want experimentalApi false", capabilities)
	}
	optOutRaw, _ := capabilities["optOutNotificationMethods"].([]any)
	gotOptOut := make([]string, 0, len(optOutRaw))
	for _, item := range optOutRaw {
		gotOptOut = append(gotOptOut, fmt.Sprint(item))
	}
	wantOptOut := []string{
		"item/agentMessage/delta",
		"item/plan/delta",
		"item/commandExecution/outputDelta",
		"item/fileChange/outputDelta",
		"item/reasoning/summaryTextDelta",
		"item/reasoning/summaryPartAdded",
		"item/reasoning/textDelta",
	}
	if !reflect.DeepEqual(gotOptOut, wantOptOut) {
		t.Fatalf("initialize optOutNotificationMethods = %+v, want %+v", gotOptOut, wantOptOut)
	}

	second := <-initializedCh
	if second["method"] != "initialized" {
		t.Fatalf("second ws message = %+v, want initialized", second)
	}
}

func TestStartWebSocketRequiresURL(t *testing.T) {
	client := New(config.CodexConfig{Transport: "ws"})
	err := client.Start(context.Background(), true)
	if err == nil || !strings.Contains(err.Error(), "ws_url is required") {
		t.Fatalf("Start(websocket without url) error = %v, want missing ws_url", err)
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
