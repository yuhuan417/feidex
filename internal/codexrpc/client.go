package codexrpc

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"feidex/internal/codexcli"
	"feidex/internal/config"
)

const (
	codexClientInfoName    = "codex_cli_rs"
	codexClientInfoTitle   = "Codex CLI"
	versionProbeMaxWait    = 2 * time.Second
	versionProbeMaxOutSize = 64 * 1024
)

type Client struct {
	id     uint64
	cfg    config.CodexConfig
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	mcp    mcpServerPublication

	nextID    atomic.Int64
	writeMu   sync.Mutex
	pendingMu sync.Mutex
	pending   map[int64]chan responseEnvelope
	stateMu   sync.Mutex
	closing   bool
	exitErr   error
	waitDone  chan struct{}
	userAgent string
	errorOnce sync.Once

	onNotification func(string, json.RawMessage)
	onRequest      func(RequestEnvelope)
	onError        func(error)
}

var nextClientID atomic.Uint64

var codexCommandVersionLookup = lookupCodexCommandVersion

type responseEnvelope struct {
	ID     int64           `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *rpcError       `json:"error"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type RequestEnvelope struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

type notificationEnvelope struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

type mcpServerPublication struct {
	Name             string
	URL              string
	BearerTokenEnv   string
	BearerTokenValue string
}

func New(cfg config.CodexConfig) *Client {
	if cfg.Command == "" {
		cfg.Command = "codex"
	}
	return &Client{
		id:       nextClientID.Add(1),
		cfg:      cfg,
		pending:  map[int64]chan responseEnvelope{},
		waitDone: make(chan struct{}),
	}
}

func (c *Client) SetHandlers(onNotification func(string, json.RawMessage), onRequest func(RequestEnvelope)) {
	c.onNotification = onNotification
	c.onRequest = onRequest
}

func (c *Client) SetMCPServerPublication(name, url, bearerTokenEnvVar, bearerToken string) {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	c.mcp = mcpServerPublication{
		Name:             strings.TrimSpace(name),
		URL:              strings.TrimSpace(url),
		BearerTokenEnv:   strings.TrimSpace(bearerTokenEnvVar),
		BearerTokenValue: strings.TrimSpace(bearerToken),
	}
}

func (c *Client) SetErrorHandler(onError func(error)) {
	c.onError = onError
}

func (c *Client) Start(ctx context.Context, experimentalAPI bool) error {
	if err := validateTransportConfig(c.cfg); err != nil {
		return err
	}
	if err := c.startStdio(); err != nil {
		return err
	}
	started := false
	defer func() {
		if started {
			return
		}
		if err := c.Close(); err != nil && !errors.Is(err, os.ErrProcessDone) {
			slog.Warn("codex app-server startup cleanup failed",
				"client_id", c.id,
				"pid", c.pid(),
				"error", err,
			)
		}
	}()

	var initResp struct {
		UserAgent      string `json:"userAgent"`
		CodexHome      string `json:"codexHome"`
		PlatformFamily string `json:"platformFamily"`
		PlatformOS     string `json:"platformOs"`
	}
	if err := c.Call(ctx, "initialize", map[string]any{
		"clientInfo": c.clientInfo(ctx),
		"capabilities": map[string]any{
			"experimentalApi": experimentalAPI,
			"optOutNotificationMethods": []string{
				"item/agentMessage/delta",
				"item/plan/delta",
				"item/commandExecution/outputDelta",
				"item/fileChange/outputDelta",
				"item/reasoning/summaryTextDelta",
				"item/reasoning/summaryPartAdded",
				"item/reasoning/textDelta",
			},
		},
	}, &initResp); err != nil {
		return err
	}
	c.stateMu.Lock()
	c.userAgent = strings.TrimSpace(initResp.UserAgent)
	c.stateMu.Unlock()
	if err := c.Notify("initialized", map[string]any{}); err != nil {
		return err
	}
	started = true
	slog.Info("codex app-server ready",
		"client_id", c.id,
		"pid", c.pid(),
	)
	return nil
}

func (c *Client) clientInfo(ctx context.Context) map[string]any {
	info := map[string]any{
		"name":  codexClientInfoName,
		"title": codexClientInfoTitle,
	}
	if version := c.commandVersion(ctx); version != "" {
		info["version"] = version
	}
	return info
}

func (c *Client) commandVersion(ctx context.Context) string {
	if c == nil {
		return ""
	}
	command := strings.TrimSpace(c.cfg.Command)
	if command == "" {
		command = "codex"
	}
	return codexCommandVersionLookup(ctx, command)
}

func lookupCodexCommandVersion(ctx context.Context, command string) string {
	command = strings.TrimSpace(command)
	if command == "" {
		command = "codex"
	}
	if ctx == nil {
		ctx = context.Background()
	}
	versionCtx, cancel := context.WithTimeout(ctx, versionProbeMaxWait)
	defer cancel()
	cmd := exec.CommandContext(versionCtx, command, "--version")
	output, err := cmd.CombinedOutput()
	if err != nil || len(output) > versionProbeMaxOutSize {
		return ""
	}
	return codexcli.ParseVersion(string(output))
}

func (c *Client) UserAgent() string {
	if c == nil {
		return ""
	}
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	return c.userAgent
}

func (c *Client) Close() error {
	c.stateMu.Lock()
	c.closing = true
	c.stateMu.Unlock()
	slog.Info("codex app-server closing",
		"client_id", c.id,
		"pid", c.pid(),
	)
	if c.stdin != nil {
		_ = c.stdin.Close()
	}
	if c.cmd != nil && c.cmd.Process != nil {
		return c.cmd.Process.Kill()
	}
	return nil
}

func (c *Client) Call(ctx context.Context, method string, params any, out any) error {
	id := c.nextID.Add(1)
	ch := make(chan responseEnvelope, 1)
	c.pendingMu.Lock()
	c.pending[id] = ch
	c.pendingMu.Unlock()

	if err := c.send(map[string]any{"method": method, "id": id, "params": params}); err != nil {
		c.pendingMu.Lock()
		delete(c.pending, id)
		c.pendingMu.Unlock()
		return err
	}

	select {
	case <-ctx.Done():
		c.pendingMu.Lock()
		delete(c.pending, id)
		c.pendingMu.Unlock()
		return ctx.Err()
	case resp := <-ch:
		if resp.Error != nil {
			return fmt.Errorf("%s", resp.Error.Message)
		}
		if out == nil {
			return nil
		}
		return json.Unmarshal(resp.Result, out)
	}
}

func (c *Client) Notify(method string, params any) error {
	return c.send(map[string]any{"method": method, "params": params})
}

func (c *Client) Reply(id json.RawMessage, result any) error {
	return c.send(map[string]any{"id": json.RawMessage(id), "result": result})
}

func (c *Client) ReplyError(id json.RawMessage, code int, message string) error {
	return c.send(map[string]any{
		"id": id,
		"error": map[string]any{
			"code":    code,
			"message": message,
		},
	})
}

func (c *Client) send(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if c.stdin == nil {
		return errors.New("client not started")
	}
	_, err = c.stdin.Write(append(b, '\n'))
	if err != nil {
		c.emitTransportError(fmt.Errorf("codex app-server stdin write failed: %w", err))
	}
	return err
}

func (c *Client) readLoop() {
	scanner := bufio.NewScanner(c.stdout)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 8*1024*1024)
	for scanner.Scan() {
		c.handleIncoming(scanner.Bytes())
	}
	if c.isClosing() {
		slog.Info("codex app-server read loop stopped",
			"client_id", c.id,
			"pid", c.pid(),
		)
		return
	}
	err := c.transportReadError(scanner.Err())
	slog.Warn("codex app-server read loop exited",
		"client_id", c.id,
		"pid", c.pid(),
		"error", err,
	)
	c.emitTransportError(err)
}

func (c *Client) startStdio() error {
	args := []string{"app-server"}
	if mcpArgs, mcpEnv := c.mcpLaunchArgs(); len(mcpArgs) > 0 || len(mcpEnv) > 0 {
		args = append(args, mcpArgs...)
		c.cmd = exec.Command(c.cfg.Command, args...)
		c.cmd.Env = append(os.Environ(), mcpEnv...)
	} else {
		c.cmd = exec.Command(c.cfg.Command, args...)
	}
	if dir := strings.TrimSpace(c.cfg.AppServerDir); dir != "" {
		c.cmd.Dir = dir
	}
	stdin, err := c.cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := c.cmd.StdoutPipe()
	if err != nil {
		return err
	}
	c.stdin = stdin
	c.stdout = stdout
	c.cmd.Stderr = os.Stderr
	if err := c.cmd.Start(); err != nil {
		return err
	}
	slog.Info("codex app-server started",
		"client_id", c.id,
		"pid", c.pid(),
		"command", c.cfg.Command,
		"cwd", strings.TrimSpace(c.cmd.Dir),
	)
	go c.waitLoop()
	go c.readLoop()
	return nil
}

func (c *Client) mcpLaunchArgs() ([]string, []string) {
	c.stateMu.Lock()
	pub := c.mcp
	c.stateMu.Unlock()
	if strings.TrimSpace(pub.Name) == "" || strings.TrimSpace(pub.URL) == "" || strings.TrimSpace(pub.BearerTokenEnv) == "" || strings.TrimSpace(pub.BearerTokenValue) == "" {
		return nil, nil
	}
	args := []string{
		"-c", fmt.Sprintf("mcp_servers.%s.url=%s", pub.Name, strconv.Quote(pub.URL)),
		"-c", fmt.Sprintf("mcp_servers.%s.bearer_token_env_var=%s", pub.Name, strconv.Quote(pub.BearerTokenEnv)),
	}
	env := []string{pub.BearerTokenEnv + "=" + pub.BearerTokenValue}
	return args, env
}

func (c *Client) waitLoop() {
	err := c.cmd.Wait()
	c.stateMu.Lock()
	c.exitErr = err
	waitDone := c.waitDone
	c.stateMu.Unlock()
	if waitDone != nil {
		close(waitDone)
	}
	level := slog.LevelInfo
	if err != nil && !c.isClosing() {
		level = slog.LevelWarn
	}
	slog.Log(context.Background(), level, "codex app-server exited",
		"client_id", c.id,
		"pid", c.pid(),
		"error", err,
	)
}

func (c *Client) handleIncoming(line []byte) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(line, &obj); err != nil {
		return
	}
	if rawID, ok := obj["id"]; ok {
		if _, hasMethod := obj["method"]; hasMethod {
			var req RequestEnvelope
			if err := json.Unmarshal(line, &req); err == nil && c.onRequest != nil {
				c.onRequest(req)
			}
			return
		}
		var id int64
		if err := json.Unmarshal(rawID, &id); err == nil {
			var resp responseEnvelope
			if err := json.Unmarshal(line, &resp); err == nil {
				c.pendingMu.Lock()
				ch := c.pending[id]
				delete(c.pending, id)
				c.pendingMu.Unlock()
				if ch != nil {
					ch <- resp
				}
			}
		}
		return
	}
	var notif notificationEnvelope
	if err := json.Unmarshal(line, &notif); err == nil && notif.Method != "" && c.onNotification != nil {
		c.onNotification(notif.Method, notif.Params)
	}
}

func validateTransportConfig(cfg config.CodexConfig) error {
	transport := strings.TrimSpace(cfg.Transport)
	switch {
	case strings.TrimSpace(cfg.WSURL) != "" || strings.TrimSpace(cfg.WSBearerToken) != "":
		return errors.New("codex websocket transport has been removed; use stdio only")
	case transport == "", strings.EqualFold(transport, "stdio"):
		return nil
	case strings.EqualFold(transport, "ws"):
		return errors.New("codex websocket transport has been removed; use stdio only")
	default:
		return fmt.Errorf("unsupported codex transport %q; only stdio is supported", transport)
	}
}

func (c *Client) isClosing() bool {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	return c.closing
}

func (c *Client) transportReadError(readErr error) error {
	if readErr != nil {
		return fmt.Errorf("codex app-server read failed: %w", readErr)
	}
	waitDone := c.waitDone
	if waitDone != nil {
		timer := time.NewTimer(250 * time.Millisecond)
		defer timer.Stop()
		select {
		case <-waitDone:
		case <-timer.C:
		}
	}
	c.stateMu.Lock()
	exitErr := c.exitErr
	c.stateMu.Unlock()
	if exitErr != nil {
		return fmt.Errorf("codex app-server process exited: %w", exitErr)
	}
	if readErr == nil {
		return io.EOF
	}
	return readErr
}

func (c *Client) emitTransportError(err error) {
	if err == nil {
		err = io.EOF
	}
	c.errorOnce.Do(func() {
		c.failPending(err)
		if c.onError != nil {
			c.onError(err)
		}
	})
}

func (c *Client) failPending(err error) {
	if err == nil {
		err = io.EOF
	}
	resp := responseEnvelope{
		Error: &rpcError{Message: err.Error()},
	}
	c.pendingMu.Lock()
	pending := c.pending
	c.pending = map[int64]chan responseEnvelope{}
	c.pendingMu.Unlock()
	for _, ch := range pending {
		if ch == nil {
			continue
		}
		select {
		case ch <- resp:
		default:
		}
	}
}

func (c *Client) pid() int {
	if c == nil || c.cmd == nil || c.cmd.Process == nil {
		return 0
	}
	return c.cmd.Process.Pid
}
